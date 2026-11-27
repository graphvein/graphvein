package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/storeloc"
	"github.com/graphvein/graphvein/internal/workspace"
)

const storeUsage = "usage: graphvein store where [path] | store move <path> [<folder>] [--yes] | store list"

// storeCommand is graphvein store where|move|list (ADR 0093): where a
// folder's index is kept, moving it, and every index Graphvein knows.
func storeCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, storeUsage)
		return 2
	}
	switch args[0] {
	case "where":
		return storeWhere(args[1:], stdout, stderr)
	case "move":
		return storeMove(args[1:], stdin, stdout, stderr)
	case "list":
		return storeList(args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, storeUsage)
	return 2
}

// indexSubject is what a folder's index holds, for messages: a project
// (the folder holds graphvein-project.yaml) or a repository, with the
// repository names whose runs the store keeps.
func indexSubject(folder string) (kind, name string, repositories []string) {
	if project, err := config.LoadProject(folder); err == nil {
		for _, m := range project.Members {
			repositories = append(repositories, m.Repository)
		}
		return "project", project.Name, repositories
	}
	repository, err := config.ResolveRepository(folder, nil)
	if err != nil {
		repository = filepath.Base(folder)
	}
	return "repository", repository, []string{repository}
}

func storeWhere(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "usage: graphvein store where [path]")
		return 2
	}
	folder := "."
	if len(args) == 1 {
		folder = args[0]
	}
	loc, err := locate(folder)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	kind, name, repositories := indexSubject(loc.Folder)
	if loc.Origin == storeloc.OriginPostgres {
		fmt.Fprintf(stdout, "Postgres database from %s (%s %s)\n", storeloc.EnvPostgres, kind, name)
		fmt.Fprintln(stdout, "  kept:     team mode; no local file, nothing to move")
		return 0
	}
	fmt.Fprintln(stdout, loc.Path)
	fmt.Fprintf(stdout, "  kept:     %s\n", placeOf(loc))
	if !loc.Exists {
		fmt.Fprintln(stdout, "  size:     not created yet; graphvein index creates it")
		return 0
	}
	size := fileSizeOf(loc.Path) + fileSizeOf(loc.Path+"-wal")
	fmt.Fprintf(stdout, "  size:     %s; %s\n", formatSize(size), lastRunText(loc.Folder, repositories))
	return 0
}

// placeOf says where a store is kept and why, as store where prints it.
func placeOf(loc storeloc.Location) string {
	switch loc.Origin {
	case storeloc.OriginEnvFile:
		return "from " + storeloc.EnvSQLitePath + ", for this folder only"
	case storeloc.OriginInFolder:
		return "inside the repository (.graphvein/); to move it out: graphvein store move " + quoteArg(loc.Folder)
	}
	where := "outside the repository"
	if loc.Inside() {
		where = "inside the repository"
	}
	if loc.Origin == storeloc.OriginMapped {
		return where + " (recorded in " + loc.StoresFile + ")"
	}
	switch loc.DirSource {
	case storeloc.DirEnv:
		return where + " (folder from " + storeloc.EnvStoreDir + ")"
	case storeloc.DirStoresFile:
		return where + " (default folder from " + loc.StoresFile + ")"
	}
	return where + " (default folder)"
}

// lastRunText is the last run line of store where: the latest finished
// run of the folder's repositories and how many runs the store keeps, or
// why they cannot be read.
func lastRunText(folder string, repositories []string) string {
	ctx := context.Background()
	s, err := openReadStore(ctx, folder)
	if err != nil {
		return fmt.Sprintf("runs not read: %v", err)
	}
	defer s.Close()
	var all []store.Run
	for _, r := range repositories {
		runs, err := s.Runs(ctx, r)
		if err != nil {
			return fmt.Sprintf("runs not read: %v", err)
		}
		all = append(all, runs...)
	}
	last := store.LatestRun(all)
	if last == nil {
		return "no finished run"
	}
	at := last.FinishedAt
	if t, err := time.Parse(time.RFC3339Nano, last.FinishedAt); err == nil {
		at = t.Local().Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("last run %s (%d runs stored)", at, len(all))
}

func fileSizeOf(p string) int64 {
	info, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return info.Size()
}

// registeredFolders lists the workspace registry's folders; a registry
// that cannot be read lists none.
func registeredFolders() []string {
	path, err := workspace.DefaultPath()
	if err != nil {
		return nil
	}
	entries, err := workspace.Open(path).Entries()
	if err != nil {
		return nil
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
}

func storeMove(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("store move", flag.ContinueOnError)
	flags.SetOutput(stderr)
	yes := flags.Bool("yes", false, "move without asking")
	var positional []string
	for len(args) > 0 {
		if err := flags.Parse(args); err != nil {
			return 2
		}
		args = flags.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	if len(positional) < 1 || len(positional) > 2 {
		fmt.Fprintln(stderr, "usage: graphvein store move <path> [<folder>] [--yes]")
		return 2
	}
	if os.Getenv(storeloc.EnvSQLitePath) != "" && !postgresSet() {
		fmt.Fprintf(stderr, "%s is set: it chooses the index of the folder on every command, so its own store is not the one in use; unset it to move the folder's store\n", storeloc.EnvSQLitePath)
		return 1
	}
	r, err := storeResolver()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	dest := ""
	if len(positional) == 2 {
		dest = positional[1]
	}
	plan, err := r.PlanMove(positional[0], dest, registeredFolders())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_, name, _ := indexSubject(plan.From.Folder)
	fmt.Fprintf(stdout, "Move the index of %s\n  from %s\n  to   %s\n", name, plan.From.Path, plan.To)
	if !*yes {
		fmt.Fprint(stdout, "Move it? [y/N] ")
		answer, _ := bufio.NewReader(stdin).ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(stdout, "Nothing was moved.")
			return 1
		}
	}
	res, err := r.Move(context.Background(), plan, storeloc.MoveOptions{Wait: indexrun.LockWait})
	var moveErr *storeloc.MoveError
	if err != nil && !(errors.As(err, &moveErr) && moveErr.Moved) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "Copied %s and checked it (%d runs, same row counts).\n", formatSize(res.Bytes), res.Runs)
	if len(res.Deleted) > 0 {
		fmt.Fprintf(stdout, "Deleted the old %s.\n", joinAnd(res.Deleted))
	}
	if res.Recorded {
		fmt.Fprintf(stdout, "Recorded in %s.\n", r.StoresFile)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "A graphvein serve that has this index open shows the moved index after it is restarted.")
	return 0
}

// joinAnd joins names as "a, b and c".
func joinAnd(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func storeList(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: graphvein store list")
		return 2
	}
	r, err := storeResolver()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if postgresSet() {
		fmt.Fprintf(stdout, "Team mode: %s is set, so every index is in that Postgres database; the local indexes below are not used while it is set.\n", storeloc.EnvPostgres)
	}
	stores, err := r.List(registeredFolders()...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(stores) == 0 {
		dir, _, err := r.StoresDir()
		if err != nil {
			fmt.Fprintln(stdout, "No local indexes found.")
		} else {
			fmt.Fprintf(stdout, "No local indexes found (new indexes go to %s).\n", dir)
		}
		return 0
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FOLDER\tINDEX\tSIZE")
	for _, s := range stores {
		folder := s.Folder
		switch {
		case folder == "":
			folder = "(unknown: no " + storeloc.FolderFile + ")"
		case !s.FolderFound:
			folder += " (not found)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", folder, s.Path, formatSize(s.Size))
	}
	w.Flush()
	return 0
}
