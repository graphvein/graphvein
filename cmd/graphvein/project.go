package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/graphvein/graphvein/internal/config"
)

const projectUsage = `usage:
  graphvein project init --name NAME [--dir DIR] [REPOSITORY=]PATH...
  graphvein project add [--manifest DIR] [REPOSITORY=]PATH
  graphvein project suggest [--manifest DIR] [--json] [--accept N]
  graphvein project forget [--manifest DIR] REPOSITORY`

// projectCommand is graphvein project (ADR 0075, T022 B1): it writes the
// project manifest. It never opens a store; graphvein index --project
// indexes and links the project. Usage errors and refused manifests exit 2, file errors exit 1.
func projectCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "suggest" {
		return projectSuggestCommand(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "forget" {
		return projectForgetCommand(args[1:], stdout, stderr)
	}
	if len(args) == 0 || (args[0] != "init" && args[0] != "add") {
		fmt.Fprintln(stderr, projectUsage)
		return 2
	}
	sub := args[0]
	f := flag.NewFlagSet("project "+sub, flag.ContinueOnError)
	f.SetOutput(stderr)
	name := f.String("name", "", "project name (init)")
	dir := f.String("dir", ".", "directory to write graphvein-project.yaml in (init)")
	manifestDir := f.String("manifest", ".", "directory holding graphvein-project.yaml (add)")
	var positional, options []string
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if a == "--" {
			positional = append(positional, rest[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			options = append(options, a)
			flagName := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
			if flagName != "h" && flagName != "help" && !strings.Contains(a, "=") && i+1 < len(rest) {
				i++
				options = append(options, rest[i])
			}
		} else {
			positional = append(positional, a)
		}
	}
	if err := f.Parse(options); err != nil {
		return 2
	}
	set := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	switch {
	case sub == "init" && (set["manifest"] || len(positional) == 0 || *name == ""):
		fmt.Fprintln(stderr, projectUsage)
		return 2
	case sub == "add" && (set["name"] || set["dir"] || len(positional) != 1):
		fmt.Fprintln(stderr, projectUsage)
		return 2
	}
	target := *dir
	if sub == "add" {
		target = *manifestDir
	}
	target, err := filepath.Abs(target)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	manifestPath := filepath.Join(target, config.ProjectFile)
	var members []config.ManifestMember
	for _, arg := range positional {
		m, err := memberFromArg(target, arg)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		members = append(members, m)
	}
	var data []byte
	if sub == "init" {
		if _, err := os.Stat(manifestPath); err == nil {
			fmt.Fprintf(stderr, "%s already exists; use graphvein project add\n", manifestPath)
			return 2
		}
		if data, err = config.NewProjectManifest(*name, members); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		current, err := os.ReadFile(manifestPath)
		if err != nil {
			fmt.Fprintf(stderr, "reading %s: %v (create it with graphvein project init)\n", manifestPath, err)
			return 2
		}
		// The current file must already be valid: add never rewrites a file
		// it could not read back.
		if _, err := config.ParseProject(target, current, readLocal(target)); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if data, err = config.AddProjectMember(current, members[0]); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", manifestPath, err)
			return 2
		}
	}
	project, err := config.ParseProject(target, data, readLocal(target))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if sub == "init" {
		if err := os.MkdirAll(target, 0o755); err != nil {
			fmt.Fprintf(stderr, "creating %s: %v\n", target, err)
			return 1
		}
	}
	if err := writeFileAtomic(manifestPath, data); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "Wrote %s (project %s, %d members)\n", manifestPath, project.Name, len(project.Members))
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, m := range project.Members {
		fmt.Fprintf(w, "  %s\t%s\trepository from %s\n", m.Repository, displayPath(project.Dir, m), m.RepositorySource)
	}
	w.Flush()
	if sub == "init" {
		fmt.Fprintf(stdout, "Commit %s; keep %s (machine-specific member paths) out of version control, for example by adding it to .gitignore.\n", config.ProjectFile, config.ProjectLocalFile)
	}
	return 0
}

// memberFromArg reads one [REPOSITORY=]PATH argument. The first '=' splits
// the name from the path. The checkout must exist; its own graphvein.yaml
// name wins, a different name on the command line is refused, and without
// either the member is refused (the directory name is never used).
func memberFromArg(manifestDir, arg string) (config.ManifestMember, error) {
	given, p := "", arg
	if i := strings.Index(arg, "="); i >= 0 {
		given, p = arg[:i], arg[i+1:]
		if given == "" {
			return config.ManifestMember{}, fmt.Errorf("%q: empty repository name before '='", arg)
		}
	}
	if p == "" {
		return config.ManifestMember{}, fmt.Errorf("%q: empty path", arg)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return config.ManifestMember{}, fmt.Errorf("resolving %s: %w", p, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return config.ManifestMember{}, fmt.Errorf("member %s: %w", p, err)
	}
	if !info.IsDir() {
		return config.ManifestMember{}, fmt.Errorf("member %s is not a directory", p)
	}
	own, err := config.Load(abs)
	if err != nil {
		return config.ManifestMember{}, err
	}
	name := given
	switch {
	case own.Repository != "" && given != "" && given != own.Repository:
		return config.ManifestMember{}, fmt.Errorf("member %s: its graphvein.yaml names repository %q, which wins; drop %q= or use %q=", p, own.Repository, given, own.Repository)
	case own.Repository != "":
		name = own.Repository
	case given == "":
		return config.ManifestMember{}, fmt.Errorf("member %s has no repository name: its graphvein.yaml sets none, so name it as REPOSITORY=%s (the directory name is never used)", p, p)
	}
	rel, err := filepath.Rel(manifestDir, abs)
	if err != nil {
		return config.ManifestMember{}, fmt.Errorf("member %s cannot be written relative to %s (%v); add it with a relative path and put the real path in %s", p, manifestDir, err, config.ProjectLocalFile)
	}
	return config.ManifestMember{Repository: name, Path: filepath.ToSlash(rel)}, nil
}

// readLocal returns the local override file's content, or nil when it is
// absent or unreadable (ParseProject then validates the manifest alone;
// LoadProject reports a read error when the project is used).
func readLocal(dir string) []byte {
	data, err := os.ReadFile(filepath.Join(dir, config.ProjectLocalFile))
	if err != nil {
		return nil
	}
	return data
}

// displayPath shows a member path relative to the manifest when possible.
func displayPath(dir string, m config.Member) string {
	if m.Path == "" {
		return "(no path)"
	}
	if rel, err := filepath.Rel(dir, m.Path); err == nil {
		return filepath.ToSlash(rel)
	}
	return m.Path
}

// writeFileAtomic replaces path with data atomically (config.WriteFileAtomic).
func writeFileAtomic(path string, data []byte) error { return config.WriteFileAtomic(path, data) }
