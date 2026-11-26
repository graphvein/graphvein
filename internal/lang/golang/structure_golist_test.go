package golang

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestGoStructureGoList is the differential check of the Go structural
// contract's package rules against the go command itself, on the fixture
// testdata/fixtures/go-structure only (with the cases the repository cannot
// store written into the copy). It is opt-in
// (GRAPHVEIN_GO_STRUCTURE_GOLIST=1): it runs `go list -e -deps -json` of
// the toolchain that runs the test, offline (GOFLAGS=-mod=mod GOPROXY=off
// GOTOOLCHAIN=local GOWORK=off), once per port of `go tool dist list`, with
// CGO_ENABLED 0 and 1 and every subset of the custom tags the fixture's
// packages report (AllTags). It never builds or runs the fixture.
//
// It checks, for every package directory of the fixture's root module:
//   - goPorts equals `go tool dist list`;
//   - a directory go list reports a package error for in some configuration
//     ("found packages", "use of cgo in test", ...) is refused by Package;
//   - every other directory Package does not refuse has go list's package
//     name and, joined over all configurations, its GoFiles+CgoFiles,
//     TestGoFiles and XTestGoFiles;
//   - a directory Package refuses while go list reports no error is
//     refused for a documented Graphvein rule (a lone-CR file, a file
//     go/parser rejects);
//   - no import spec go list reports an error at ("malformed import path",
//     "is a program, not an importable package", a package error of the
//     imported directory) carries an imports edge to a file, and no
//     malformed import path carries any imports edge.
func TestGoStructureGoList(t *testing.T) {
	if os.Getenv("GRAPHVEIN_GO_STRUCTURE_GOLIST") != "1" {
		t.Skip("opt-in: GRAPHVEIN_GO_STRUCTURE_GOLIST=1")
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	root := copyStructureFixture(t)
	run := runStructure(t, root)
	const module = "example.test/structure"
	modules := ModulePaths(run.sources)

	out, err := exec.Command(goTool, "tool", "dist", "list").Output()
	if err != nil {
		t.Fatalf("go tool dist list: %v", err)
	}
	if got, want := strings.Join(strings.Fields(string(out)), " "), strings.Join(goPorts, " "); got != want {
		t.Errorf("goPorts differ from go tool dist list:\n got %s\nwant %s", want, got)
	}

	// The root module's package directories: every directory with a Go
	// file that no nested go.mod governs.
	dirSet := map[string]bool{}
	for _, p := range run.goFiles {
		if base, _ := ModuleAt(p, modules); base == "." {
			dirSet[path.Dir(p)] = true
		}
	}
	var dirs, args []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	importPathOf := map[string]string{}
	for _, d := range dirs {
		ip := module
		if d != "." {
			ip = module + "/" + d
		}
		importPathOf[ip] = d
		args = append(args, ip)
	}

	type listed struct {
		ImportPath, Name                             string
		GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
		AllTags                                      []string
		Error                                        *struct{ Pos, Err string }
		DepsErrors                                   []struct{ Pos, Err string }
	}
	list := func(goos, goarch string, cgo bool, tags []string) []listed {
		t.Helper()
		cgoValue := "0"
		if cgo {
			cgoValue = "1"
		}
		cmd := exec.Command(goTool, append([]string{"list", "-e", "-deps", "-tags=" + strings.Join(tags, ","), "-json=ImportPath,Name,GoFiles,CgoFiles,TestGoFiles,XTestGoFiles,Error,DepsErrors"}, args...)...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED="+cgoValue)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list %s/%s cgo=%v tags=%v: %v\n%s", goos, goarch, cgo, tags, err, stderr.String())
		}
		var pkgs []listed
		dec := json.NewDecoder(bytes.NewReader(data))
		for {
			var p listed
			if err := dec.Decode(&p); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("decoding go list output: %v", err)
			}
			pkgs = append(pkgs, p)
		}
		return pkgs
	}

	// The custom tags: go/build's AllTags of the fixture's package
	// directories over every port (go list does not report them).
	free := map[string]bool{}
	for _, dir := range dirs {
		for _, port := range goPorts {
			goos, goarch, _ := strings.Cut(port, "/")
			ctxt := build.Context{GOOS: goos, GOARCH: goarch, CgoEnabled: true, Compiler: "gc", ReleaseTags: oracleReleaseTags()}
			if p, _ := ctxt.ImportDir(filepath.Join(root, filepath.FromSlash(dir)), 0); p != nil {
				for _, tag := range oracleFreeTags(p.AllTags) {
					free[tag] = true
				}
			}
		}
	}
	var tags []string
	for tag := range free {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	type joined struct {
		names                      map[string]bool
		files, tests, xtests, errs map[string]bool
	}
	got := map[string]*joined{}
	// specErrors are the go list errors at an import spec ("file:line").
	specErrors := map[string]map[string]bool{}
	configs := 0
	for _, port := range goPorts {
		goos, goarch, _ := strings.Cut(port, "/")
		for _, cgo := range []bool{false, true} {
			for mask := 0; mask < 1<<len(tags); mask++ {
				var set []string
				for i, tag := range tags {
					if mask&(1<<i) != 0 {
						set = append(set, tag)
					}
				}
				configs++
				for _, p := range list(goos, goarch, cgo, set) {
					var errs []struct{ Pos, Err string }
					if p.Error != nil {
						errs = append(errs, *p.Error)
					}
					errs = append(errs, p.DepsErrors...)
					for _, e := range errs {
						if file, line, ok := specOf(e.Pos); ok {
							key := file + ":" + strconv.Itoa(line)
							if specErrors[key] == nil {
								specErrors[key] = map[string]bool{}
							}
							specErrors[key][e.Err] = true
						}
					}
					dir, own := importPathOf[p.ImportPath]
					if !own {
						continue
					}
					j := got[dir]
					if j == nil {
						j = &joined{names: map[string]bool{}, files: map[string]bool{}, tests: map[string]bool{}, xtests: map[string]bool{}, errs: map[string]bool{}}
						got[dir] = j
					}
					if p.Error != nil && packageError(p.Error.Err) {
						j.errs[p.Error.Err] = true
					}
					if p.Name != "" {
						j.names[p.Name] = true
					}
					for _, list := range [][]string{p.GoFiles, p.CgoFiles} {
						for _, f := range list {
							j.files[path.Join(dir, f)] = true
						}
					}
					for _, f := range p.TestGoFiles {
						j.tests[path.Join(dir, f)] = true
					}
					for _, f := range p.XTestGoFiles {
						j.xtests[path.Join(dir, f)] = true
					}
				}
			}
		}
	}

	var disagreements []string
	report := func(format string, args ...any) { disagreements = append(disagreements, fmt.Sprintf(format, args...)) }
	refused, onlyGraphvein := 0, 0
	for _, dir := range dirs {
		j := got[dir]
		pkg := Package(dir, run.files)
		if j == nil {
			report("%s: go list did not list it", dir)
			continue
		}
		if len(j.errs) > 0 {
			refused++
			if pkg.Refusal == "" {
				report("%s: go list reports %v, Package does not refuse", dir, sortedSet(j.errs))
			}
			continue
		}
		if pkg.Refusal != "" {
			onlyGraphvein++
			if why := graphveinOnlyRefusal(run, dir); why == "" {
				report("%s: Package refuses, go list reports no error and no documented rule applies", dir)
			} else {
				t.Logf("%s: refused by Graphvein's own rule (%s); go list reports no error", dir, why)
			}
			continue
		}
		name := ""
		for n := range j.names {
			name = n
		}
		if len(j.names) > 1 || name != pkg.Name && len(pkg.Files)+len(pkg.Tests)+len(pkg.XTests) > 0 {
			report("%s: package %v, Package says %q", dir, sortedSet(j.names), pkg.Name)
		}
		for _, c := range []struct {
			what       string
			have, want []string
		}{{"files", pkg.Files, sortedSet(j.files)}, {"tests", pkg.Tests, sortedSet(j.tests)}, {"external tests", pkg.XTests, sortedSet(j.xtests)}} {
			if strings.Join(c.have, " ") != strings.Join(c.want, " ") {
				report("%s: %s %v, go list %v", dir, c.what, c.have, c.want)
			}
		}
	}
	malformedSpecs := 0
	for _, e := range run.edges {
		if e.Kind != "imports" {
			continue
		}
		for _, ev := range e.Evidence {
			key := ev.File + ":" + strconv.Itoa(ev.Span.StartLine)
			errs := specErrors[key]
			if len(errs) == 0 {
				continue
			}
			malformed := false
			for msg := range errs {
				malformed = malformed || strings.HasPrefix(msg, "malformed import path")
			}
			if malformed || strings.HasPrefix(e.Target, "file:") {
				report("%s: imports edge to %s, go list reports %v", key, e.Target, sortedSet(errs))
			}
		}
	}
	for _, errs := range specErrors {
		for msg := range errs {
			if strings.HasPrefix(msg, "malformed import path") {
				malformedSpecs++
				break
			}
		}
	}
	t.Logf("go list: %d configurations, %d tags %v, %d package directories, %d refused by go list, %d refused by Graphvein only, %d import specs with errors (%d malformed paths), %d disagreements", configs, len(tags), tags, len(dirs), refused, onlyGraphvein, len(specErrors), malformedSpecs, len(disagreements))
	for _, d := range disagreements {
		t.Error(d)
	}
}

// specOf splits a go list error position "file:line:column" (relative to
// the module root) into its file and line.
func specOf(pos string) (string, int, bool) {
	parts := strings.Split(pos, ":")
	if len(parts) < 3 {
		return "", 0, false
	}
	line, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil {
		return "", 0, false
	}
	return filepath.ToSlash(strings.Join(parts[:len(parts)-2], ":")), line, true
}

// packageError reports whether a go list error of a listed package is an
// error of the directory itself, which Package must refuse. Not package
// errors: a directory without files in this configuration (row GS-I2); an
// importer's error attached to the imported package ("is a program", which
// the import spec check covers); a program's link mode on some ports
// ("requires external (cgo) linking"); and an import cycle, which is not
// modelled (a follow-up of the S1 review fixes: only a file importing its
// own package is refused, as self_import).
func packageError(msg string) bool {
	for _, not := range []string{"build constraints exclude all Go files", "no Go files", "no non-test Go files", "is a program, not an importable package", "requires external (cgo) linking", "import cycle not allowed"} {
		if strings.Contains(msg, not) {
			return false
		}
	}
	return true
}

// graphveinOnlyRefusal names the documented Graphvein rule that refuses a
// directory go list accepts: a lone-CR file (GS-F3) or a file some
// configuration selects that go/parser rejects (GS-I5); "" when none
// applies.
func graphveinOnlyRefusal(run *structureRun, dir string) string {
	for _, p := range dirGoFiles(dir, run.files) {
		g := run.files[p].Go
		if g == nil {
			continue
		}
		if g.LoneCR {
			return p + ": lone CR"
		}
		if selected, _ := buildSelected(path.Base(p), g.Build, false); selected && g.Rejected {
			return p + ": go/parser rejects it"
		}
	}
	return ""
}
