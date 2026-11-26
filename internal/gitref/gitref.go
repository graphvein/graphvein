// Package gitref resolves git refs and materialises a commit's tree into a
// temporary directory for a ref run (ADR 0056, branch versus base).
//
// Only local git subprocesses run here, through scan.GitCommand: every call
// disables transport protocols, lazy fetching of a partial clone's missing
// objects, credential prompts, optional index locks and automatic
// maintenance, so a missing object is an error instead of a network
// fetch (AGENTS.md: only internal/egress opens network connections). The
// tree is read with git ls-tree and git cat-file --batch, so nothing is
// written into the repository's .git and no checkout filter or attribute
// (export-ignore, eol, filters) changes the bytes: a ref snapshot is the
// commit's tree exactly.
package gitref

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/scan"
)

// Errors callers map to the API's error codes.
var (
	// ErrGitUnavailable: git is not installed or dir is not a repository.
	ErrGitUnavailable = errors.New("git is unavailable for this repository")
	// ErrRefUnknown: the name resolves to no commit.
	ErrRefUnknown = errors.New("ref does not name a commit")
	// ErrObjectsMissing: the commit's objects are not all present locally
	// (a partial or shallow clone); nothing is fetched.
	ErrObjectsMissing = errors.New("objects of the ref are missing locally")
)

// command runs git in the repository at dir with Graphvein's read-only
// git options and environment (scan.GitCommand).
func command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	return scan.GitCommand(ctx, dir, args...)
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := command(ctx, dir, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("running git: %v: %w", err, ErrGitUnavailable)
	}
	return strings.TrimSpace(string(out)), nil
}

// Resolve returns the full commit id ref names in the repository at dir
// (`git rev-parse --verify <ref>^{commit}`). Names starting with "-" are
// refused so a ref cannot become an option.
func Resolve(ctx context.Context, dir, ref string) (string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\n") {
		return "", fmt.Errorf("resolving %q: %w", ref, ErrRefUnknown)
	}
	if _, err := run(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return "", fmt.Errorf("resolving %q: %v: %w", ref, err, ErrGitUnavailable)
	}
	commit, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil || commit == "" {
		return "", fmt.Errorf("resolving %q: %w", ref, ErrRefUnknown)
	}
	return commit, nil
}

// MergeBase returns the best common ancestor of two commits, the default
// base side of a branch comparison (ADR 0056, open point O2).
func MergeBase(ctx context.Context, dir, a, b string) (string, error) {
	for _, c := range []string{a, b} {
		if c == "" || strings.HasPrefix(c, "-") {
			return "", fmt.Errorf("merge base of %q and %q: %w", a, b, ErrRefUnknown)
		}
	}
	base, err := run(ctx, dir, "merge-base", a, b)
	if err != nil || base == "" {
		return "", fmt.Errorf("merge base of %q and %q: %w", a, b, ErrRefUnknown)
	}
	return base, nil
}

// Tree is a commit materialised into a temporary directory.
type Tree struct {
	// Dir holds the commit's regular files and a fresh, empty git
	// repository of the same object format (so the scanner computes the same
	// blob object ids as for the working tree).
	Dir    string
	Commit string
	// Diagnostics describe entries that were not written: symlinks (the
	// scanner's own "symlink" diagnostic, as for a working tree),
	// submodules, and paths that could not be written on this platform.
	Diagnostics []contract.Diagnostic
	// Files counts the files written.
	Files int
}

// Remove deletes the temporary directory.
func (t *Tree) Remove() error {
	if t == nil || t.Dir == "" {
		return nil
	}
	return os.RemoveAll(t.Dir)
}

// Paths lists the paths of commit's tree (blobs, links and submodules), so
// a comparison can tell a deleted file from one that is only excluded from
// the index (G8, Revision 3 R3-4).
func Paths(ctx context.Context, dir, commit string) (map[string]bool, error) {
	if commit == "" || strings.HasPrefix(commit, "-") {
		return nil, fmt.Errorf("listing %q: %w", commit, ErrRefUnknown)
	}
	listing, err := command(ctx, dir, "ls-tree", "-r", "-z", "--full-tree", commit).Output()
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", commit, missingOr(err, ErrRefUnknown))
	}
	entries, err := scan.ParseTree(listing)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", commit, err)
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Path] = true
	}
	return out, nil
}

// Materialise writes commit's tree into a new temporary directory. On any
// error the directory is removed before Materialise returns; on success the
// caller removes it with Tree.Remove.
func Materialise(ctx context.Context, dir, commit string) (tree *Tree, err error) {
	if commit == "" || strings.HasPrefix(commit, "-") {
		return nil, fmt.Errorf("materialising %q: %w", commit, ErrRefUnknown)
	}
	format, err := run(ctx, dir, "rev-parse", "--show-object-format")
	if err != nil {
		return nil, fmt.Errorf("materialising %s: %v: %w", commit, err, ErrGitUnavailable)
	}
	if format != "sha1" && format != "sha256" {
		return nil, fmt.Errorf("materialising %s: unsupported object format %q", commit, format)
	}
	listing, err := command(ctx, dir, "ls-tree", "-r", "-z", "--full-tree", commit).Output()
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", commit, missingOr(err, ErrRefUnknown))
	}
	entries, err := scan.ParseTree(listing)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", commit, err)
	}
	tmp, err := os.MkdirTemp("", "graphvein-ref-*")
	if err != nil {
		return nil, fmt.Errorf("materialising %s: %w", commit, err)
	}
	tree = &Tree{Dir: tmp, Commit: commit}
	defer func() {
		if err != nil {
			tree.Remove()
			tree = nil
		}
	}()
	initArgs := []string{"-c", "init.defaultObjectFormat=" + format, "init", "-q", "--template="}
	if format == "sha256" {
		initArgs = append(initArgs, "--object-format=sha256")
	}
	init := scan.GitCommand(ctx, "", append(initArgs, tmp)...)
	if out, e := init.CombinedOutput(); e != nil {
		return tree, fmt.Errorf("preparing %s: %s: %w", tmp, strings.TrimSpace(string(out)), ErrGitUnavailable)
	}
	var blobs []scan.TreeEntry
	for _, e := range entries {
		switch {
		case e.Mode == "120000":
			tree.Diagnostics = append(tree.Diagnostics, contract.Diagnostic{Type: "diagnostic", File: e.Path, Severity: "warning", Code: "symlink", Message: "symlink is not followed"})
		case e.Type == "commit":
			tree.Diagnostics = append(tree.Diagnostics, contract.Diagnostic{Type: "diagnostic", File: e.Path, Severity: "warning", Code: "ref_submodule", Message: "submodule is not part of the ref snapshot"})
		case e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755"):
			if !safePath(e.Path) {
				tree.Diagnostics = append(tree.Diagnostics, contract.Diagnostic{Type: "diagnostic", Severity: "error", Code: "ref_unwritable", Message: fmt.Sprintf("unsupported path %q in the ref", e.Path)})
				continue
			}
			blobs = append(blobs, e)
		}
	}
	if err := writeBlobs(ctx, dir, tree, blobs); err != nil {
		return tree, err
	}
	return tree, nil
}

// missingOr maps a git failure that reports missing objects to
// ErrObjectsMissing and any other failure to fallback.
func missingOr(err error, fallback error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return missingText(string(exit.Stderr), fallback)
	}
	return fmt.Errorf("%v: %w", err, ErrGitUnavailable)
}

// missingText maps git's stderr text to ErrObjectsMissing when it reports
// missing objects (a partial clone with lazy fetching disabled) and to
// fallback otherwise.
func missingText(text string, fallback error) error {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "missing") || strings.Contains(text, "promisor") || strings.Contains(text, "lazy fetch") || strings.Contains(text, "unable to read") {
		return fmt.Errorf("%s: %w", text, ErrObjectsMissing)
	}
	return fmt.Errorf("%s: %w", text, fallback)
}

// safePath accepts the repository paths the scanner accepts and refuses any
// component that would leave the directory or reach a .git directory.
func safePath(p string) bool {
	if !contract.ValidPath(p, false) || path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

// writeBlobs streams the blobs through one git cat-file --batch process.
func writeBlobs(ctx context.Context, dir string, tree *Tree, blobs []scan.TreeEntry) error {
	if len(blobs) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := command(ctx, dir, "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("reading blobs: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("reading blobs: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("reading blobs: %v: %w", err, ErrGitUnavailable)
	}
	written := make(chan error, 1)
	go func() {
		w := bufio.NewWriter(stdin)
		var err error
		for _, b := range blobs {
			if _, err = w.WriteString(b.OID + "\n"); err != nil {
				break
			}
		}
		if err == nil {
			err = w.Flush()
		}
		stdin.Close()
		written <- err
	}()
	readErr := readBlobs(bufio.NewReaderSize(stdout, 1<<16), tree, blobs)
	if readErr != nil {
		cancel()
	}
	// Drain so git can exit, then wait for both sides.
	io.Copy(io.Discard, stdout)
	writeErr := <-written
	waitErr := cmd.Wait()
	// git's stderr and exit status win over what the read saw: a partial
	// clone's cat-file dies on a blob it may not fetch, and the reader
	// only sees EOF (Revision 3, R3-9).
	var exit *exec.ExitError
	switch {
	case readErr != nil && errors.Is(readErr, ErrObjectsMissing):
		return readErr
	case waitErr != nil && errors.As(waitErr, &exit) && strings.TrimSpace(stderr.String()) != "":
		return fmt.Errorf("reading blobs: %w", missingText(stderr.String(), ErrGitUnavailable))
	case readErr != nil:
		return readErr
	case writeErr != nil:
		return fmt.Errorf("reading blobs: %w", writeErr)
	case waitErr != nil:
		return fmt.Errorf("reading blobs: %v: %w", waitErr, ErrGitUnavailable)
	}
	return nil
}

func readBlobs(r *bufio.Reader, tree *Tree, blobs []scan.TreeEntry) error {
	for _, b := range blobs {
		line, err := r.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading blob %s of %s: %w", b.OID, b.Path, err)
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "missing" {
			return fmt.Errorf("blob %s of %s: %w", b.OID, b.Path, ErrObjectsMissing)
		}
		if len(fields) != 3 || fields[0] != b.OID || fields[1] != "blob" {
			return fmt.Errorf("unexpected cat-file reply %q for %s", strings.TrimSpace(line), b.Path)
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return fmt.Errorf("unexpected cat-file size %q for %s", fields[2], b.Path)
		}
		content := io.LimitReader(r, size)
		if err := writeFile(tree, b, content); err != nil {
			tree.Diagnostics = append(tree.Diagnostics, contract.Diagnostic{Type: "diagnostic", File: b.Path, Severity: "error", Code: "ref_unwritable", Message: fmt.Sprintf("writing %s from the ref: %v", b.Path, err)})
		} else {
			tree.Files++
		}
		// Skip what writeFile did not read, then the newline after the blob.
		if _, err := io.Copy(io.Discard, content); err != nil {
			return fmt.Errorf("reading blob %s of %s: %w", b.OID, b.Path, err)
		}
		if c, err := r.ReadByte(); err != nil || c != '\n' {
			return fmt.Errorf("reading blob %s of %s: malformed cat-file record", b.OID, b.Path)
		}
	}
	return nil
}

func writeFile(tree *Tree, b scan.TreeEntry, content io.Reader) error {
	target := filepath.Join(tree.Dir, filepath.FromSlash(b.Path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if b.Mode == "100755" {
		mode = 0o755
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, content)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// Remote is a checkout's remote URL as git reports it: metadata only,
// never a repository ID (ADR 0075 identity rule 4).
type Remote struct {
	// URL is remote.origin.url with any user name and password removed
	// from a URL form (https://user:token@host/... becomes
	// https://host/...); an scp-like form (git@host:owner/repo) is kept.
	// Empty when the checkout has no origin remote.
	URL string
	// TopLevel is the absolute top-level directory of the git repository
	// holding dir (git rev-parse --show-toplevel), so a caller can tell a
	// repository root from a folder inside one.
	TopLevel string
}

// RemoteOf reads the origin remote of the git repository holding dir. It
// runs only local git commands (config and rev-parse); nothing is fetched.
// A directory outside any git repository fails with ErrGitUnavailable.
func RemoteOf(ctx context.Context, dir string) (Remote, error) {
	top, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Remote{}, fmt.Errorf("reading the remote of %s: %v: %w", dir, err, ErrGitUnavailable)
	}
	out := Remote{TopLevel: filepath.Clean(filepath.FromSlash(top))}
	// git config exits 1 when the key is unset; that is no remote, not
	// an error.
	raw, err := run(ctx, dir, "config", "--get", "remote.origin.url")
	if err == nil {
		out.URL = StripCredentials(raw)
	}
	return out, nil
}

// StripCredentials removes the user information of a URL-form remote
// (scheme://user:password@host/path), which may hold a token. Other forms
// (scp-like user@host:path, local paths) are returned unchanged: their
// user part is a login name, never a secret git stores there.
func StripCredentials(remote string) string {
	scheme, rest, ok := strings.Cut(remote, "://")
	if !ok {
		return remote
	}
	authority, path, _ := strings.Cut(rest, "/")
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = authority[at+1:]
	}
	if path == "" && !strings.Contains(rest, "/") {
		return scheme + "://" + authority
	}
	return scheme + "://" + authority + "/" + path
}
