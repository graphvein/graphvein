package gorm

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
	"github.com/graphvein/graphvein/internal/parse"
)

// input builds the link-time input from in-memory sources as the indexer
// does: every source retained, Go files parsed and extracted.
func input(t *testing.T, sources map[string]string) Input {
	t.Helper()
	raw := map[string][]byte{}
	for f, s := range sources {
		raw[f] = []byte(s)
	}
	return build(t, raw)
}

// build parses every Go file of sources and fills the input.
func build(t *testing.T, sources map[string][]byte) Input {
	t.Helper()
	in := Input{Files: map[string]facts.Facts{}, Sources: sources}
	var files []string
	var jobs []parse.Job
	for f, s := range sources {
		if strings.HasSuffix(f, ".go") {
			files = append(files, f)
			jobs = append(jobs, parse.Job{Language: "go", Source: s})
		}
	}
	options := parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}
	err := parse.Consume(context.Background(), jobs, options, func(i int, r parse.Result) {
		if r.Err != nil {
			t.Errorf("%s: %v", files[i], r.Err)
			return
		}
		in.Files[files[i]] = golang.Extract(files[i], sources[files[i]], r.Root)
	})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// discover runs Discover with a tag recorder.
func discover(t *testing.T, sources map[string]string) ([]*Module, []contract.Diagnostic, *modeltest.Tags) {
	t.Helper()
	in := input(t, sources)
	tags := modeltest.NewTags()
	in.Tag = tags.Tag
	mods, diags := Discover(in)
	return mods, diags, tags
}

// one returns the only module.
func one(t *testing.T, mods []*Module) *Module {
	t.Helper()
	if len(mods) != 1 {
		t.Fatalf("%d modules, want 1", len(mods))
	}
	return mods[0]
}

// messages lists the diagnostics of a code as "<file>|<message>", sorted.
func messages(diags []contract.Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d.File+"|"+d.Message)
		}
	}
	sort.Strings(out)
	return out
}

// identities lists the module's model identities in order.
func identities(m *Module) []string {
	var out []string
	for _, mo := range m.Models {
		out = append(out, mo.Identity())
	}
	return out
}

// sequence lists one sequence as "<file>:<line>".
func sequence(seq []*MigrateSite) []string {
	var out []string
	for _, s := range seq {
		out = append(out, s.File+":"+itoa(s.Line))
	}
	return out
}

func itoa(n int) string {
	const digits = "0123456789"
	if n < 10 {
		return digits[n : n+1]
	}
	return itoa(n/10) + digits[n%10:n%10+1]
}

// goMod is a go.mod requiring gorm at version v plus extra lines.
func goMod(v string, extra ...string) string {
	return "module example.com/app\n\ngo 1.22\n\nrequire (\n\tgorm.io/gorm " + v + "\n\tgorm.io/driver/sqlite v1.6.0\n\tgithub.com/jinzhu/inflection v1.0.0 // indirect\n" + strings.Join(extra, "") + ")\n"
}
