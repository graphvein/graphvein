package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/openapi"
	"github.com/graphvein/graphvein/internal/store"
)

// openapiCommand writes the OpenAPI document of a repository's stored
// routes (T029, docs/cli/openapi.md). It reads the store and graphvein.yaml
// (explorer.path_values) only: no index run, no source file, no network.
func openapiCommand(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("openapi", flag.ContinueOnError)
	f.SetOutput(stderr)
	repo := f.String("repository", "", "logical repository ID (defaults to graphvein.yaml, then the directory name); with --project, the member")
	format := f.String("format", openapi.FormatJSON, "json or yaml")
	out := f.String("out", "", "write the document to this file instead of standard output")
	projectArg := f.String("project", "", "project manifest (graphvein-project.yaml) or its directory: write the document of one member")
	var positional []string
	rest := args
	for len(rest) > 0 {
		if err := f.Parse(rest); err != nil {
			return 2
		}
		if f.NArg() == 0 {
			break
		}
		positional = append(positional, f.Arg(0))
		rest = f.Args()[1:]
	}
	usage := "usage: graphvein openapi [path] [--format json|yaml] [--out file] [--repository ID] | graphvein openapi --project DIR [--repository MEMBER] [--format json|yaml] [--out file]"
	if len(positional) > 1 || *format != openapi.FormatJSON && *format != openapi.FormatYAML || *projectArg != "" && len(positional) > 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	repoGiven := false
	f.Visit(func(fl *flag.Flag) {
		if fl.Name == "repository" {
			repoGiven = true
		}
	})
	ctx := context.Background()
	var (
		s           store.Store
		repository  string
		projectEnvs []config.ProjectEnvironment
		cfg         config.Config
		err         error
	)
	if *projectArg != "" {
		s, repository, cfg, projectEnvs, err = openProjectMember(ctx, *projectArg, *repo, repoGiven)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	} else {
		root := "."
		if len(positional) == 1 {
			root = positional[0]
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		var override *string
		if repoGiven {
			override = repo
		}
		if repository, err = config.ResolveRepository(abs, override); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if cfg, err = config.Load(abs); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if _, err := os.Stat(sqlitePath(abs)); os.Getenv("GRAPHVEIN_POSTGRES_URL") == "" && err != nil {
			fmt.Fprintf(stderr, "repository %s is not indexed (no store at %s); run graphvein index first\n", repository, sqlitePath(abs))
			return 1
		}
		if s, err = openReadStore(ctx, abs); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	defer s.Close()
	in, err := openapi.Read(ctx, s, repository, cfg.Explorer.PathValues)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	in.Servers = openapi.Servers(projectEnvs, cfg.TryIt.Environments, repository)
	if in.Run == nil {
		fmt.Fprintf(stderr, "repository %s is not indexed; run graphvein index first\n", repository)
		return 1
	}
	data, err := openapi.Encode(openapi.Generate(in), *format)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *out == "" {
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if err := config.WriteFileAtomic(*out, data); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// openProjectMember opens the project store and reads the named member's
// graphvein.yaml (the defaults for a member without a checkout here).
func openProjectMember(ctx context.Context, manifest, repository string, given bool) (store.Store, string, config.Config, []config.ProjectEnvironment, error) {
	dir, err := projectDir(manifest)
	if err != nil {
		return nil, "", config.Config{}, nil, err
	}
	project, err := config.LoadProject(dir)
	if err != nil {
		return nil, "", config.Config{}, nil, err
	}
	var names []string
	var member *config.Member
	for i, m := range project.Members {
		names = append(names, m.Repository)
		if given && m.Repository == repository || !given && len(project.Members) == 1 {
			member = &project.Members[i]
		}
	}
	if member == nil {
		if !given {
			return nil, "", config.Config{}, nil, fmt.Errorf("project %s has members %s; name one with --repository (one document per member)", project.Name, strings.Join(names, ", "))
		}
		return nil, "", config.Config{}, nil, fmt.Errorf("%q is not a member of project %s (members: %s)", repository, project.Name, strings.Join(names, ", "))
	}
	cfg := config.Defaults()
	if member.Checkout {
		if cfg, err = config.Load(member.Path); err != nil {
			return nil, "", config.Config{}, nil, err
		}
	}
	s, err := openReadStore(ctx, project.Dir)
	if err != nil {
		return nil, "", config.Config{}, nil, err
	}
	return s, member.Repository, cfg, project.TryIt, nil
}
