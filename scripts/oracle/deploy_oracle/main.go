//go:build ignore

// Command deploy_oracle is the DC oracle of T026 (ADR 0079).
// Never run by CI, never built in the repository: the build constraint
// keeps it out of the graphvein module. scripts/oracle/deploy_config.sh
// copies it without the constraint into a scratch module at pinned
// versions:
//
//   - github.com/compose-spec/compose-go/v2 v2.16.2 (Apache-2.0): the
//     compose loader, per file, interpolation, extends, include and
//     normalization off, schema validation on, paths resolved.
//     Per directory with a compose file, its default file discovery
//     (cli.WithDefaultConfigPath) and the services of the default file and
//     override merged by its loader (row CD4's default-merge basis).
//   - github.com/moby/buildkit v0.34.2 (Apache-2.0): the Dockerfile parser
//     (frontend/dockerfile/parser), FROM and EXPOSE per instruction.
//
// It reads the repository root as its argument and repository-relative
// paths on standard input, one per line, and writes one JSON object keyed
// by path; the key "#defaults" holds the discovery per directory. It parses only; nothing in the files runs, and no network or
// Docker daemon is used.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

type stage struct {
	Image  string   `json:"image"`
	Alias  string   `json:"alias"`
	Flags  []string `json:"flags"`
	Expose []string `json:"expose"`
}

type result struct {
	Error    string                    `json:"error,omitempty"`
	Defaults map[string]defaults       `json:"defaults,omitempty"`
	Services map[string]map[string]any `json:"services,omitempty"`
	Stages   []stage                   `json:"stages,omitempty"`
}

// defaults is one directory's default discovery: the files Compose loads
// without -f, and their merged services.
type defaults struct {
	Files    []string                  `json:"files"`
	Error    string                    `json:"error,omitempty"`
	Services map[string]map[string]any `json:"services,omitempty"`
}

func discover(root, dir string) defaults {
	abs := filepath.Join(root, filepath.FromSlash(dir))
	o, err := cli.NewProjectOptions(nil, cli.WithWorkingDirectory(abs), cli.WithDefaultConfigPath, cli.WithName("oracle"),
		cli.WithInterpolation(false), cli.WithNormalization(false), cli.WithConsistency(false), cli.WithResolvedPaths(true), cli.WithoutEnvironmentResolution,
		cli.WithLoadOptions(func(lo *loader.Options) {
			lo.SkipExtends, lo.SkipInclude, lo.SkipDefaultValues, lo.SkipValidation = true, true, true, true
		}))
	if err != nil {
		return defaults{Error: err.Error()}
	}
	d := defaults{Files: []string{}}
	for _, p := range o.ConfigPaths {
		if filepath.Dir(p) != abs {
			continue // discovery walked up past this directory
		}
		rel, _ := filepath.Rel(root, p)
		d.Files = append(d.Files, filepath.ToSlash(rel))
	}
	if len(d.Files) != len(o.ConfigPaths) || len(d.Files) < 2 {
		d.Files = d.Files[:0:0]
		return d
	}
	model, err := o.LoadModel(context.Background())
	if err != nil {
		d.Error = err.Error()
		return d
	}
	services, _ := model["services"].(map[string]any)
	d.Services = map[string]map[string]any{}
	for name, v := range services {
		s, _ := v.(map[string]any)
		d.Services[name] = s
	}
	return d
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: deploy_oracle ROOT < paths")
		os.Exit(2)
	}
	root, err := filepath.Abs(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out := map[string]result{}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		rel := strings.TrimSpace(scanner.Text())
		if rel == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			out[rel] = result{Error: err.Error()}
			continue
		}
		base := path.Base(rel)
		if strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml") {
			out[rel] = compose(root, rel, data)
		} else {
			out[rel] = dockerfile(data)
		}
	}
	dirs := map[string]defaults{}
	for rel, r := range out {
		base := path.Base(rel)
		if r.Stages == nil && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")) {
			if _, done := dirs[path.Dir(rel)]; !done {
				dirs[path.Dir(rel)] = discover(root, path.Dir(rel))
			}
		}
	}
	out["#defaults"] = result{Defaults: dirs}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func compose(root, rel string, data []byte) result {
	dir := filepath.Join(root, filepath.FromSlash(path.Dir(rel)))
	details := types.ConfigDetails{WorkingDir: dir, ConfigFiles: []types.ConfigFile{{Filename: filepath.Join(root, filepath.FromSlash(rel)), Content: data}}, Environment: types.Mapping{}}
	model, err := loader.LoadModelWithContext(context.Background(), details, func(o *loader.Options) {
		o.SkipInterpolation = true
		o.SkipNormalization = true
		o.SkipConsistencyCheck = true
		o.SkipExtends = true
		o.SkipInclude = true
		o.SkipResolveEnvironment = true
		o.SkipDefaultValues = true
		o.ResolvePaths = true
		o.SetProjectName("oracle", true)
	})
	if err != nil {
		return result{Error: err.Error()}
	}
	services, _ := model["services"].(map[string]any)
	r := result{Services: map[string]map[string]any{}}
	for name, v := range services {
		s, _ := v.(map[string]any)
		r.Services[name] = s
	}
	return r
}

func dockerfile(data []byte) result {
	parsed, err := parser.Parse(strings.NewReader(string(data)))
	if err != nil {
		return result{Error: err.Error()}
	}
	var r result
	for _, n := range parsed.AST.Children {
		switch strings.ToLower(n.Value) {
		case "from":
			var words []string
			for w := n.Next; w != nil; w = w.Next {
				words = append(words, w.Value)
			}
			s := stage{Flags: n.Flags, Expose: []string{}}
			if len(words) > 0 {
				s.Image = words[0]
			}
			if len(words) == 3 && strings.EqualFold(words[1], "as") {
				s.Alias = words[2]
			}
			r.Stages = append(r.Stages, s)
		case "expose":
			if len(r.Stages) == 0 {
				return result{Error: "EXPOSE before FROM"}
			}
			for w := n.Next; w != nil; w = w.Next {
				r.Stages[len(r.Stages)-1].Expose = append(r.Stages[len(r.Stages)-1].Expose, w.Value)
			}
		}
	}
	if len(r.Stages) == 0 {
		return result{Error: "no FROM"}
	}
	return r
}
