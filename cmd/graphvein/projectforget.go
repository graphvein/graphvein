package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/store"
)

const projectForgetUsage = "usage: graphvein project forget [--manifest DIR] REPOSITORY"

// projectForgetCommand is graphvein project forget (T022 MR-UI, B11
// follow-up): it removes a repository name the manifest no longer has
// from the project store, its project rows and its stored index
// (store.RetireProjectMember). A rename made in the UI does this itself; a
// rename made on disk leaves the old name's rows behind, and this command
// is how they go. A name still in the manifest is refused, and so is a
// name no stored link pass of the project had as a member, so a typo
// never deletes a current member's or an unrelated repository's index. Exit codes: 0 removed (or
// nothing stored); 2 usage error or refused; 1 store or lock error.
func projectForgetCommand(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("project forget", flag.ContinueOnError)
	f.SetOutput(stderr)
	manifestArg := f.String("manifest", ".", "graphvein-project.yaml or the directory holding it")
	var positional, options []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			options = append(options, a)
			name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
			if name != "h" && name != "help" && !strings.Contains(a, "=") && i+1 < len(args) {
				i++
				options = append(options, args[i])
			}
			continue
		}
		positional = append(positional, a)
	}
	if err := f.Parse(options); err != nil {
		return 2
	}
	if len(positional) != 1 || positional[0] == "" {
		fmt.Fprintln(stderr, projectForgetUsage)
		return 2
	}
	repository := positional[0]
	if strings.Contains(repository, store.RefMarker) {
		fmt.Fprintf(stderr, "%s: %v\n", repository, store.ErrReservedRepository)
		return 2
	}
	dir, err := projectDir(*manifestArg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	project, err := config.LoadProject(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if slices.ContainsFunc(project.Members, func(m config.Member) bool { return m.Repository == repository }) {
		fmt.Fprintf(stderr, "%s is a member of project %s in %s; forget removes only a name the manifest no longer has (remove or rename the member first)\n", repository, project.Name, config.ProjectFile)
		return 2
	}
	return forgetMember(context.Background(), project, repository, stdout, stderr)
}

// forgetMember takes the project lock (and, on Postgres, where the
// project lock does not exclude member index runs, the repository's own
// index lock), then retires repository from the project store.
func forgetMember(ctx context.Context, project config.Project, repository string, stdout, stderr io.Writer) int {
	locker, lockName := openProjectLocker(project.Dir, project.Name)
	defer locker.Close()
	lease, err := locker.Acquire(ctx, indexrun.LockWait)
	if errors.Is(err, store.ErrIndexLocked) {
		fmt.Fprintf(stderr, "another run holds the project lock (%s); try again when it has finished\n", lockName)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "taking the project lock (%s): %v\n", lockName, err)
		return 1
	}
	defer lease.Release()
	if os.Getenv("GRAPHVEIN_POSTGRES_URL") != "" {
		memberLocker, memberLockName := openLocker(project.Dir, repository)
		defer memberLocker.Close()
		memberLease, err := memberLocker.Acquire(ctx, indexrun.LockWait)
		if errors.Is(err, store.ErrIndexLocked) {
			fmt.Fprintf(stderr, "an index run of %s holds its lock (%s); try again when it has finished\n", repository, memberLockName)
			return 1
		}
		if err != nil {
			fmt.Fprintf(stderr, "taking the index lock of %s (%s): %v\n", repository, memberLockName, err)
			return 1
		}
		defer memberLease.Release()
	}
	s, err := openStore(ctx, project.Dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer s.Close()
	stored, former, err := storedUnder(ctx, s, project.Name, repository)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !former {
		fmt.Fprintf(stderr, "no stored link pass of project %s names %s as a member; forget removes only an old member name, never another repository's index (nothing was removed)\n", project.Name, repository)
		return 2
	}
	if err := lease.Check(ctx); err != nil {
		fmt.Fprintf(stderr, "project lock lost: %v\n", err)
		return 1
	}
	err = s.RetireProjectMember(ctx, project.Name, repository)
	switch {
	case errors.Is(err, store.ErrRepositoryShared):
		fmt.Fprintf(stderr, "%s is a member of another project in this store; nothing was removed\n", repository)
		return 2
	case errors.Is(err, store.ErrReservedRepository), errors.Is(err, store.ErrInvalid):
		fmt.Fprintln(stderr, err)
		return 2
	case err != nil:
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(stored) == 0 {
		fmt.Fprintf(stdout, "Nothing stored under %s in project %s; nothing to remove.\n", repository, project.Name)
		return 0
	}
	fmt.Fprintf(stdout, "Removed %s from project %s: %s. Links of the other members to it are gone; run graphvein index --project to link the project again.\n", repository, project.Name, strings.Join(stored, " and "))
	return 0
}

// storedUnder says what the store holds under repository for project:
// its stored index and its project rows (a member record or a link of the
// last link pass). former reports whether any stored pass of the project,
// the link journal included, named repository as a member: forget removes
// only such a name, never the index of a repository that was not part of
// the project (a store can hold others).
func storedUnder(ctx context.Context, s store.Store, project, repository string) (stored []string, former bool, err error) {
	isMember := func(m store.ProjectMember) bool { return m.Repository == repository }
	set, err := s.ProjectLinks(ctx, project)
	if err != nil && !errors.Is(err, store.ErrProjectUnknown) {
		return nil, false, fmt.Errorf("reading the links of project %s: %w", project, err)
	}
	runs, err := s.ProjectLinkRuns(ctx, project)
	if err != nil {
		return nil, false, fmt.Errorf("reading the link journal of project %s: %w", project, err)
	}
	former = slices.ContainsFunc(set.Members, isMember)
	for _, r := range runs {
		former = former || slices.ContainsFunc(r.Members, isMember)
	}
	repos, err := s.Repositories(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("listing stored repositories: %w", err)
	}
	if slices.ContainsFunc(repos, func(r store.Repository) bool { return r.ID == repository }) {
		stored = append(stored, "its stored index")
	}
	rows := slices.ContainsFunc(set.Members, isMember)
	for _, l := range set.Links {
		rows = rows || l.Source.Repository == repository || l.Target.Repository == repository
	}
	if rows {
		stored = append(stored, "its project rows")
	}
	return stored, former, nil
}
