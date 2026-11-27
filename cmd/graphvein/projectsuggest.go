package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/projectsuggest"
)

// projectSuggestCommand is graphvein project suggest (ADR 0075, T022 B6):
// it lists the consumes declarations the project's env, Compose and
// dev-server proxy files support, and writes one only with --accept N.
// Listing never writes; nothing is linked differently until a proposal is
// accepted and the project is indexed again. Exit codes: 0 listed or
// written; 2 usage error, invalid manifest or refused proposal; 1 file
// error.
func projectSuggestCommand(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("project suggest", flag.ContinueOnError)
	f.SetOutput(stderr)
	manifestDir := f.String("manifest", ".", "directory holding graphvein-project.yaml")
	asJSON := f.Bool("json", false, "print the proposals as JSON")
	accept := f.Int("accept", 0, "write proposal N into the manifest")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 0 || *accept < 0 || (*asJSON && *accept != 0) {
		fmt.Fprintln(stderr, projectUsage)
		return 2
	}
	dir, err := filepath.Abs(*manifestDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	project, err := config.LoadProject(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx := context.Background()
	calls, err := readProjectCalls(ctx, project)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	result, err := projectsuggest.Suggest(ctx, project, calls)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *accept != 0 {
		return acceptProposal(project, result, *accept, stdout, stderr)
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	printSuggestions(stdout, result)
	return 0
}

// readProjectCalls reads the members' stored calls from the project's
// store (the one graphvein index --project writes), read-only. Without a
// store nothing is indexed yet: every member then has no stored calls, and
// the listing says to index the project first.
func readProjectCalls(ctx context.Context, project config.Project) (projectsuggest.Calls, error) {
	if os.Getenv("GRAPHVEIN_POSTGRES_URL") == "" {
		if _, err := os.Stat(sqlitePath(project.Dir)); errors.Is(err, fs.ErrNotExist) {
			return projectsuggest.Calls{}, nil
		}
	}
	s, err := openReadStore(ctx, project.Dir)
	if err != nil {
		return projectsuggest.Calls{}, err
	}
	defer s.Close()
	return projectsuggest.ReadCalls(ctx, s, project)
}

func printSuggestions(w io.Writer, r projectsuggest.Result) {
	fmt.Fprintf(w, "Proposals (%d):\n", len(r.Proposals))
	if len(r.Proposals) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for _, p := range r.Proposals {
		state := ""
		if p.Declared {
			state = "  [already declared: edit that consumes entry instead]"
		}
		fmt.Fprintf(w, "  %d. %s consumes %s  select: %s%s\n", p.Number, endpoint(p.Member, p.Scope), endpoint(p.Provider, p.ProviderScope), selectText(p.Select), state)
		for _, e := range p.Evidence {
			fmt.Fprintf(w, "       %s\n", evidenceText(e))
		}
	}
	fmt.Fprintf(w, "Unmatched evidence (%d):\n", len(r.Unmatched))
	for _, u := range r.Unmatched {
		fmt.Fprintf(w, "  %s\n       %s\n", evidenceText(u.Evidence), u.Reason)
	}
	fmt.Fprintf(w, "Not read (%d):\n", len(r.Unread))
	for _, u := range r.Unread {
		fmt.Fprintf(w, "  %s: %s\n", fileText(u.Member, u.File, u.Line), u.Reason)
	}
	fmt.Fprintln(w, "Nothing is applied: graphvein project suggest --accept N writes proposal N into the manifest.")
}

func endpoint(repository, scope string) string {
	if scope == "" {
		return repository
	}
	return repository + "/" + scope
}

func selectText(s projectsuggest.Selector) string {
	var parts []string
	for _, l := range []struct {
		name   string
		values []string
	}{{"bases", s.Bases}, {"origins", s.Origins}, {"path_prefixes", s.PathPrefixes}} {
		if len(l.values) > 0 {
			parts = append(parts, l.name+" ["+strings.Join(l.values, ", ")+"]")
		}
	}
	if len(parts) == 0 {
		return "none (every call without a literal origin)"
	}
	return strings.Join(parts, "; ")
}

func fileText(member, file string, line int) string {
	var where string
	if member == "" {
		where = "(project folder) " + file
	} else {
		where = member + ": " + file
	}
	if line > 0 {
		where = fmt.Sprintf("%s:%d", where, line)
	}
	return where
}

func evidenceText(e projectsuggest.Evidence) string {
	return fmt.Sprintf("%s  %s=%s  (%s)", fileText(e.Member, e.File, e.Line), e.Key, e.Value, e.Row)
}

// acceptProposal writes proposal n through the manifest writer: the new
// manifest is validated in full and replaces the file atomically; a
// refused proposal writes nothing.
func acceptProposal(project config.Project, r projectsuggest.Result, n int, stdout, stderr io.Writer) int {
	if n > len(r.Proposals) {
		fmt.Fprintf(stderr, "no proposal %d (there are %d); run graphvein project suggest to list them\n", n, len(r.Proposals))
		return 2
	}
	p := r.Proposals[n-1]
	if p.Declared {
		fmt.Fprintf(stderr, "proposal %d: %s already has a consumes entry for %s; edit that entry instead (one entry per provider)\n", n, endpoint(p.Member, p.Scope), endpoint(p.Provider, p.ProviderScope))
		return 2
	}
	manifestPath := filepath.Join(project.Dir, config.ProjectFile)
	current, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "reading %s: %v\n", manifestPath, err)
		return 1
	}
	data, err := config.AddProjectConsume(current, p.Member, p.Scope, p.Consume())
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", manifestPath, err)
		return 2
	}
	if _, err := config.ParseProject(project.Dir, data, readLocal(project.Dir)); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	firstEntry := true
	for _, m := range project.Members {
		if m.Repository != p.Member {
			continue
		}
		list := m.Consumes
		for _, sc := range m.Scopes {
			if sc.Name == p.Scope {
				list = sc.Consumes
			}
		}
		firstEntry = len(list) == 0
	}
	if err := writeFileAtomic(manifestPath, data); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "Wrote %s: %s consumes %s (select: %s)\n", manifestPath, endpoint(p.Member, p.Scope), endpoint(p.Provider, p.ProviderScope), selectText(p.Select))
	if firstEntry {
		fmt.Fprintf(stdout, "This is %s's first consumes entry: from now on only calls an entry covers link to another repository.\n", endpoint(p.Member, p.Scope))
	}
	fmt.Fprintln(stdout, "Links change when the project is linked again: graphvein index --project.")
	return 0
}
