package deploy_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/deploy"
)

// oracleResult is one file of scripts/oracle/deploy_config.sh's output.
type oracleResult struct {
	Error    string `json:"error"`
	Defaults map[string]struct {
		Files    []string                  `json:"files"`
		Error    string                    `json:"error"`
		Services map[string]map[string]any `json:"services"`
	} `json:"defaults"`
	Services map[string]map[string]any `json:"services"`
	Stages   []struct {
		Image  string   `json:"image"`
		Alias  string   `json:"alias"`
		Flags  []string `json:"flags"`
		Expose []string `json:"expose"`
	} `json:"stages"`
}

// TestDeployOracle compares the stored deploy facts of a repository with
// compose-go's loader and BuildKit's Dockerfile parser (the DC oracle,
// opt-in, never in CI):
//
//	bash scripts/oracle/deploy_config.sh --scratch DIR --root ROOT --out FILE
//	GRAPHVEIN_DEPLOY_ORACLE=FILE GRAPHVEIN_DEPLOY_ORACLE_ROOT=ROOT \
//	  go test ./internal/extract/deploy -run TestDeployOracle -v
//
// Every disagreement is a precision stop.
func TestDeployOracle(t *testing.T) {
	file, root := os.Getenv("GRAPHVEIN_DEPLOY_ORACLE"), os.Getenv("GRAPHVEIN_DEPLOY_ORACLE_ROOT")
	if file == "" || root == "" {
		t.Skip("set GRAPHVEIN_DEPLOY_ORACLE and GRAPHVEIN_DEPLOY_ORACLE_ROOT")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var oracle map[string]oracleResult
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	discovery := oracle["#defaults"]
	delete(oracle, "#defaults")
	sources := map[string][]byte{}
	for rel := range oracle {
		b, err := os.ReadFile(filepath.Join(abs, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		sources[rel] = b
	}
	nodes, diags := deploy.Link(sources)
	refused := map[string]string{}
	for _, d := range diags {
		refused[d.File] = d.Message
	}
	byFile := map[string][]contract.Node{}
	for _, n := range nodes {
		byFile[n.File] = append(byFile[n.File], n)
	}
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	files := make([]string, 0, len(oracle))
	for f := range oracle {
		files = append(files, f)
	}
	sort.Strings(files)
	checked := 0
	for _, f := range files {
		o := oracle[f]
		_, ours := refused[f]
		switch {
		case o.Error != "" && !ours && len(byFile[f]) > 0:
			report("%s: oracle refuses (%s), stored facts exist", f, firstLine(o.Error))
			continue
		case o.Error == "" && ours:
			report("%s: oracle reads, refused here (%s)", f, refused[f])
			continue
		case o.Error != "":
			continue
		}
		checked++
		if deploy.ComposeFile(f) {
			compareCompose(f, abs, o, byFile[f], report)
		} else {
			compareDockerfile(f, o, byFile[f], report)
		}
	}
	pairs := compareDefaults(abs, discovery, byFile, report)
	t.Logf("%d files, %d compared, %d default pairs, %d disagreements", len(files), checked, pairs, len(problems))
	for _, p := range problems {
		t.Error(p)
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

var routerLabel = regexp.MustCompile(`^traefik\.http\.routers\.([^.]+)\.(rule|entrypoints|tls|tls\.certresolver)$`)

func compareCompose(f, root string, o oracleResult, nodes []contract.Node, report func(string, ...any)) {
	ours := map[string]contract.Node{}
	for _, n := range nodes {
		ours[n.Name] = n
	}
	for name := range o.Services {
		if _, ok := ours[name]; !ok {
			report("%s: service %s only in the oracle", f, name)
		}
	}
	for name, n := range ours {
		s, ok := o.Services[name]
		if !ok {
			report("%s: service %s not in the oracle", f, name)
			continue
		}
		a := n.Attributes
		image, _ := s["image"].(string)
		if got, _ := a["image"].(string); got != image {
			report("%s %s: image %q, oracle %q", f, name, got, image)
		}
		wantContext, wantDockerfile := "", ""
		if b, ok := s["build"].(map[string]any); ok {
			ctx, _ := b["context"].(string)
			if rel, err := filepath.Rel(root, ctx); err == nil && filepath.IsAbs(ctx) && !strings.Contains(ctx, "$") {
				rel = filepath.ToSlash(rel)
				if rel != ".." && !strings.HasPrefix(rel, "../") {
					wantContext = rel
				}
			}
			if df, ok := b["dockerfile"].(string); ok && wantContext != "" && !strings.Contains(df, "$") && !path.IsAbs(df) {
				if j := path.Clean(path.Join(wantContext, df)); j != ".." && !strings.HasPrefix(j, "../") {
					wantDockerfile = j
				}
			}
		}
		if got, _ := a["build_context"].(string); got != wantContext {
			report("%s %s: build context %q, oracle %q", f, name, got, wantContext)
		}
		if got, _ := a["build_dockerfile"].(string); got != wantDockerfile {
			report("%s %s: build dockerfile %q, oracle %q", f, name, got, wantDockerfile)
		}
		var deps []string
		switch d := s["depends_on"].(type) {
		case []any:
			for _, x := range d {
				deps = append(deps, fmt.Sprint(x))
			}
		case map[string]any:
			for k := range d {
				deps = append(deps, k)
			}
		}
		if got := sortedStrings(a["depends_on"]); strings.Join(got, ",") != strings.Join(sorted(deps), ",") {
			report("%s %s: depends_on %v, oracle %v", f, name, got, sorted(deps))
		}
		ports, _ := s["ports"].([]any)
		if got, _ := a["ports"].([]any); len(got) != len(ports) {
			report("%s %s: %d ports, oracle %d", f, name, len(got), len(ports))
		}
		var want []string
		for k, v := range keyValues(s["labels"]) {
			if m := routerLabel.FindStringSubmatch(k); m != nil {
				want = append(want, m[1]+"|"+strings.ReplaceAll(m[2], ".", "_")+"|"+v)
			}
		}
		var got []string
		for _, r := range a["traefik_routers"].([]any) {
			r := r.(map[string]any)
			for _, k := range []string{"rule", "entrypoints", "tls", "tls_certresolver"} {
				if v, ok := r[k].(string); ok {
					got = append(got, r["router"].(string)+"|"+k+"|"+v)
				}
			}
		}
		if strings.Join(sorted(got), "\n") != strings.Join(sorted(want), "\n") {
			report("%s %s: routers %v, oracle %v", f, name, sorted(got), sorted(want))
		}
		stored := map[string]map[string]any{}
		for _, u := range a["environment_urls"].([]any) {
			u := u.(map[string]any)
			stored[u["name"].(string)] = u
		}
		for k, v := range keyValues(s["environment"]) {
			if strings.Contains(v, "$") || !strings.Contains(v, "://") {
				continue
			}
			u, err := url.Parse(v)
			got, ok := stored[k]
			switch {
			case err != nil || u.Scheme == "":
				if ok {
					report("%s %s: environment %s stored, url.Parse refuses it", f, name, k)
				}
			case !ok:
				report("%s %s: environment %s not stored, url.Parse reads %s://%s", f, name, k, u.Scheme, u.Hostname())
			case got["scheme"] != u.Scheme || fmt.Sprint(got["host"]) != hostOrNil(u):
				report("%s %s: environment %s %v://%v, url.Parse %s://%s", f, name, k, got["scheme"], got["host"], u.Scheme, u.Hostname())
			}
		}
	}
}

func hostOrNil(u *url.URL) string {
	if h := u.Hostname(); h != "" {
		if strings.Contains(u.Host, "[") {
			return "[" + h + "]"
		}
		return h
	}
	return "<nil>"
}

// keyValues reads compose-go's list or mapping form of labels and
// environment; mapping values that YAML typed (booleans, numbers) print
// as written.
func keyValues(v any) map[string]string {
	out := map[string]string{}
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			k, val, _ := strings.Cut(fmt.Sprint(item), "=")
			out[k] = val
		}
	case map[string]any:
		for k, val := range x {
			if val != nil {
				out[k] = fmt.Sprint(val)
			}
		}
	}
	return out
}

func compareDockerfile(f string, o oracleResult, nodes []contract.Node, report func(string, ...any)) {
	if len(nodes) != 1 {
		report("%s: %d dockerfile nodes", f, len(nodes))
		return
	}
	stages := nodes[0].Attributes["stages"].([]any)
	if len(stages) != len(o.Stages) {
		report("%s: %d stages, oracle %d", f, len(stages), len(o.Stages))
		return
	}
	for i, s := range stages {
		s := s.(map[string]any)
		want := o.Stages[i]
		alias, _ := s["alias"].(string)
		platform := ""
		for _, fl := range want.Flags {
			if v, ok := strings.CutPrefix(fl, "--platform="); ok {
				platform = v
			}
		}
		gotPlatform, _ := s["platform"].(string)
		if s["image"] != want.Image || alias != want.Alias || gotPlatform != platform || strings.Join(sortedStrings(s["expose"]), " ") != strings.Join(sorted(want.Expose), " ") {
			report("%s stage %d: %v %q %q %v, oracle %q %q %q %v", f, i, s["image"], alias, gotPlatform, s["expose"], want.Image, want.Alias, platform, want.Expose)
		}
	}
}

func sortedStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, fmt.Sprint(x))
	}
	return sorted(out)
}

func sorted(list []string) []string {
	out := append([]string(nil), list...)
	sort.Strings(out)
	return out
}

// compareDefaults checks deploy.DefaultPair against compose-go's default
// file discovery per directory and, for each default pair, that a service
// of one file with neither build nor image gets the build of the
// same-name service of the other file in the merged model (row CD4's
// default-merge basis).
func compareDefaults(root string, discovery oracleResult, byFile map[string][]contract.Node, report func(string, ...any)) int {
	exists := func(p string) bool {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(p)))
		return err == nil
	}
	pairs := 0
	for dir, d := range discovery.Defaults {
		base, override := deploy.DefaultPair(dir, exists)
		var want []string
		if override != "" {
			want = []string{base, override}
		}
		if strings.Join(want, ",") != strings.Join(d.Files, ",") {
			report("%s: default pair %v, compose-go discovery %v", dir, want, d.Files)
			continue
		}
		if override == "" {
			continue
		}
		if d.Error != "" {
			report("%s: compose-go refuses the default merge: %s", dir, firstLine(d.Error))
			continue
		}
		pairs++
		named := func(file string) map[string]contract.Node {
			out := map[string]contract.Node{}
			for _, n := range byFile[file] {
				out[n.Name] = n
			}
			return out
		}
		for _, files := range [][2]string{{base, override}, {override, base}} {
			from, into := named(files[0]), named(files[1])
			for name, n := range into {
				if _, has := n.Attributes["build_context"]; has || n.Attributes["build_context_as_written"] != nil || n.Attributes["image"] != nil {
					continue
				}
				sib, ok := from[name]
				ctx, _ := sib.Attributes["build_context"].(string)
				if !ok || ctx == "" {
					continue
				}
				b, _ := d.Services[name]["build"].(map[string]any)
				got, _ := b["context"].(string)
				if rel, err := filepath.Rel(root, got); err != nil || filepath.ToSlash(rel) != ctx {
					report("%s: merged service %s has build context %q, %s states %q", dir, name, got, sib.File, ctx)
				}
			}
		}
	}
	return pairs
}
