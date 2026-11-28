package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/workspace"
)

// This file holds the typed-path flow of the workspace (ADR 0041
// revision, ADR 0087, D5): POST /api/v0/workspace/check validates a
// folder the user typed and returns a one-time confirm value; POST
// /api/v0/workspace/add needs that value, registers the folder and starts
// its index run; POST /api/v0/workspace/remove drops an entry from the
// registry. Each passes the guard of every state-changing POST plus a
// loopback peer, the session token, Content-Type: application/json and a
// strict JSON body. check reads nothing but file metadata, graphvein.yaml
// and graphvein-project.yaml, and the folder's store (opened read-only).

// maxWorkspaceBody bounds a workspace POST body.
const maxWorkspaceBody = 4 << 10

// Confirm values (WS-35).
const (
	confirmLifetime = 10 * time.Minute
	maxConfirms     = 32
)

// Check outcomes.
const (
	outcomeReady      = "ready"
	outcomeRegistered = "registered"
	outcomeRefused    = "refused"
)

// Index presence states of a checked folder (WS-34).
const (
	indexPresent = "present"
	indexAbsent  = "absent"
	indexUnknown = "unknown"
)

// Confirm value operations: a value issued by one check is accepted only
// by its own POST.
const (
	opAdd     = "add"
	opMove    = "move"
	opDefault = "default_dir"
)

// confirmRecord is an issued confirm value: what the user was shown.
type confirmRecord struct {
	op   string
	path string
	kind workspace.Kind
	name string
	// choice is the store place of an add (WS-76) and storePath the store
	// file the check showed: the add refuses when the folder would get
	// another one.
	choice    storeChoice
	storePath string
	// entry, from and to are a move's (WS-78) or a default folder
	// change's (WS-80, from and to the folders) checked values.
	entry, from, to string
	expires         time.Time
}

// refusal is a typed path the server will not index, with its claim row.
type refusal struct {
	Row     string  `json:"row"`
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Entry   *string `json:"entry"`
}

type checkRequest struct {
	Path *string `json:"path"`
	// StorePlace (default, inside or folder) and StoreDir (the typed
	// folder of folder) choose where the new index is kept (WS-74);
	// absent, the store resolver's place.
	StorePlace *string `json:"store_place"`
	StoreDir   *string `json:"store_dir"`
}

type checkResponse struct {
	APIVersion int    `json:"api_version"`
	Path       string `json:"path"`
	// Resolved is the absolute folder with symbolic links resolved, the
	// one the confirmation names; null when the path was refused before.
	Resolved *string         `json:"resolved"`
	Outcome  string          `json:"outcome"`
	Refusal  *refusal        `json:"refusal"`
	Kind     *workspace.Kind `json:"kind"`
	Name     *string         `json:"name"`
	Members  []checkMember   `json:"members"`
	Index    *indexPresence  `json:"index"`
	Store    *storeDoc       `json:"store"`
	// Places are the places a ready folder's new index can be kept
	// (WS-73), for the confirmation's "Change…"; null unless ready.
	Places  *storePlaces `json:"places"`
	Entry   *string      `json:"entry"`
	Confirm *string      `json:"confirm"`
	Expires *string      `json:"confirm_expires_at"`
	Command *string      `json:"command"`
	// Contains lists the registered entries inside a ready folder (WS-54):
	// indexing it reads their files again into a store of its own, which
	// the confirmation says. Empty when none; null unless ready.
	Contains []containedEntry `json:"contains"`
}

// containedEntry is a registered entry inside a checked folder.
type containedEntry struct {
	ID   string         `json:"id"`
	Kind workspace.Kind `json:"kind"`
	Name string         `json:"name"`
	Path string         `json:"path"`
}

type checkMember struct {
	Repository string  `json:"repository"`
	Path       *string `json:"path"`
	Checkout   bool    `json:"checkout"`
}

type indexPresence struct {
	State   string  `json:"state"`
	LastRun *string `json:"last_run"`
	Error   *string `json:"error"`
}

// post answers the workspace and store POST paths.
func (s *workspaceServer) post(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", methodText(r, "POST"))
		return
	}
	body, err := s.postBody(w, r)
	if err == nil {
		switch r.URL.Path {
		case workspaceCheckPath:
			err = s.check(w, r, body)
		case workspaceAddPath:
			err = s.add(w, r, body)
		case storeCheckPath:
			err = s.storeCheck(w, r, body)
		case storeMovePath:
			err = s.storeMove(w, r, body)
		case storeDefaultPath:
			err = s.storeDefault(w, r, body)
		default:
			err = s.remove(w, r, body)
		}
	}
	if err == nil {
		return
	}
	var ae *apiError
	if errors.As(err, &ae) {
		writeError(w, ae.status, ae.code, ae.message)
		return
	}
	logFailure(s.opt.logger(), r, err)
	writeError(w, http.StatusInternalServerError, "store_error", err.Error())
}

// postBody applies the checks every workspace POST adds to the guard
// (WS-21) and returns the body.
func (s *workspaceServer) postBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if !loopbackPeer(r) {
		return nil, &apiError{http.StatusForbidden, "not_loopback", "workspace changes are accepted only from this machine"}
	}
	got := r.Header.Values(tokenHeader)
	if len(got) != 1 || subtle.ConstantTimeCompare([]byte(got[0]), []byte(s.token)) != 1 {
		return nil, &apiError{http.StatusForbidden, "token_required", "a workspace change needs this server's session token in " + tokenHeader + " (from GET " + workspacePath + ")"}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, &apiError{http.StatusUnsupportedMediaType, "unsupported_media_type", "POST " + r.URL.Path + " needs Content-Type: application/json"}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWorkspaceBody))
	if err != nil {
		return nil, &apiError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("a workspace request is at most %d KiB", maxWorkspaceBody>>10)}
	}
	if err := duplicateKeys(body); err != nil {
		return nil, badRequest("invalid body: %v", err)
	}
	return body, nil
}

// decodeWorkspaceBody decodes one JSON object with only v's fields.
func decodeWorkspaceBody(body []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return badRequest("invalid body: %v", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return badRequest("invalid body: one JSON object expected")
	}
	return nil
}

// POST /api/v0/workspace/check

func (s *workspaceServer) check(w http.ResponseWriter, r *http.Request, body []byte) error {
	var req checkRequest
	if err := decodeWorkspaceBody(body, &req); err != nil {
		return err
	}
	if req.Path == nil || *req.Path == "" {
		return badRequest("path is required")
	}
	choice, err := choiceOf(req.StorePlace, req.StoreDir)
	if err != nil {
		return err
	}
	out := s.examine(r.Context(), *req.Path, choice)
	if out.Outcome == outcomeReady {
		rec := confirmRecord{op: opAdd, path: *out.Resolved, kind: *out.Kind, name: *out.Name, choice: choice}
		if out.Store != nil && out.Store.Path != nil {
			rec.storePath = *out.Store.Path
		}
		value, expires := s.issueConfirm(rec)
		out.Confirm = &value
		out.Expires = newPtr(expires.UTC().Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// examine runs every rule on a typed path, in table order, with the store
// place the check chose. It never issues a confirm value.
func (s *workspaceServer) examine(ctx context.Context, typed string, choice storeChoice) checkResponse {
	out := checkResponse{APIVersion: 0, Path: typed, Outcome: outcomeRefused}
	refuse := func(row, code, message string) checkResponse {
		out.Refusal = &refusal{Row: row, Code: code, Message: message}
		return out
	}
	// WS-22: the server's working directory means nothing to the user,
	// and ~ or variables are shell features: only absolute paths.
	if !filepath.IsAbs(typed) {
		return refuse("WS-22", "path_not_absolute", "type the folder's absolute path (for example /home/you/src/app or C:\\src\\app); ~ and variables are not expanded")
	}
	// WS-23: symbolic links are resolved; the confirmation names the
	// resolved folder.
	resolved, err := workspace.Resolve(typed)
	if errors.Is(err, fs.ErrNotExist) {
		return refuse("WS-24", "path_not_found", "no folder exists at "+filepath.Clean(typed))
	}
	if err != nil {
		return refuse("WS-D", "path_unreadable", "the path cannot be read: "+err.Error())
	}
	out.Resolved = &resolved
	info, err := os.Stat(resolved)
	if err != nil {
		return refuse("WS-D", "path_unreadable", "the path cannot be read: "+err.Error())
	}
	if !info.IsDir() {
		return refuse("WS-25", "not_a_directory", resolved+" is a file, not a folder")
	}
	if filepath.Dir(resolved) == resolved {
		return refuse("WS-26", "filesystem_root", "the filesystem root cannot be indexed; type a project or repository folder")
	}
	if dir, ok := systemFolder(filepath.Clean(typed), resolved, runtime.GOOS, os.Getenv, sameFolderOnDisk); ok {
		return refuse("WS-53", "system_directory", "the operating system's folder "+dir+" cannot be indexed; type a project or repository folder")
	}
	if home := s.home(); home != "" && workspace.SamePath(resolved, home) {
		return refuse("WS-27", "home_directory", "the home folder itself cannot be indexed; type a project or repository folder inside it")
	}
	if dir, ok := s.configDir(resolved); ok {
		return refuse("WS-28", "config_directory", "the configuration folder "+dir+" cannot be indexed")
	}
	if part, ok := storeOrGitPart(resolved); ok {
		return refuse("WS-29", "inside_index_store", "the path is inside a "+part+" folder, which Graphvein never indexes")
	}
	entries, regErr := s.entries()
	for _, e := range entries {
		if workspace.SamePath(e.Path, resolved) {
			out.Outcome = outcomeRegistered
			out.Entry = newPtr(e.ID)
			out.Kind, out.Name = newPtr(e.Kind), newPtr(e.Name)
			out.Command = newPtr(indexCommandFor(e.Kind, resolved))
			out.Store = s.entryStoreDoc(e)
			return out
		}
	}
	for _, e := range entries {
		if workspace.Inside(resolved, e.Path) {
			r := refuse("WS-30", "inside_registered", fmt.Sprintf("the folder is inside %s (%s), which is already in the workspace", e.Name, e.Path))
			r.Refusal.Entry = newPtr(e.ID)
			return r
		}
	}
	if regErr != nil {
		return refuse("WS-D", "registry_error", "the workspace registry cannot be read: "+regErr.Error())
	}
	// WS-54: a folder that holds registered entries (a monorepo above its
	// parts) may be indexed; the confirmation names what it contains.
	contains := []containedEntry{}
	for _, e := range entries {
		// Only entries whose folder is there: a moved one is no part of
		// what the index would read.
		if _, err := os.Stat(e.Path); err == nil && workspace.Inside(e.Path, resolved) {
			contains = append(contains, containedEntry{ID: e.ID, Kind: e.Kind, Name: e.Name, Path: e.Path})
		}
	}
	kind, name := workspace.KindRepository, ""
	var project config.Project
	if _, err := os.Stat(filepath.Join(resolved, config.ProjectFile)); err == nil {
		p, err := config.LoadProject(resolved)
		if err != nil {
			return refuse("WS-32", "invalid_manifest", config.ProjectFile+" does not load: "+err.Error())
		}
		kind, name, project = workspace.KindProject, p.Name, p
		out.Members = []checkMember{}
		for _, m := range p.Members {
			cm := checkMember{Repository: m.Repository, Checkout: m.Checkout}
			if m.Path != "" {
				cm.Path = newPtr(m.Path)
			}
			out.Members = append(out.Members, cm)
		}
	} else {
		name, err = config.ResolveRepository(resolved, nil)
		if err != nil {
			return refuse("WS-33", "invalid_config", config.File+" does not load: "+err.Error())
		}
	}
	// WS-57: in a store every entry shares, a name another folder uses
	// would answer one folder with the other's facts. Checked before the
	// store is read, whose runs under that name may be the other's.
	if s.ws.SharedStore {
		claims := RepositoryClaims(name, resolved)
		if kind == workspace.KindProject {
			claims = ProjectClaims(project)
		}
		if c := FindStoreConflict(resolved, claims, entries, s.claimsOf); c != nil {
			r := refuse("WS-57", codeSharedStore, c.Message(name))
			r.Refusal.Entry = newPtr(c.Other.ID)
			return r
		}
	}
	// WS-71, WS-73..WS-75: where the index is or would be kept, and the
	// place the check chose.
	if s.ws.Stores != nil {
		store, places, rf := s.placeOf(resolved, choice, entries)
		if rf != nil {
			out.Refusal = rf
			return out
		}
		out.Store, out.Places = store, places
	}
	out.Outcome = outcomeReady
	out.Contains = contains
	out.Kind, out.Name = &kind, &name
	out.Index = s.presence(ctx, workspace.Entry{Kind: kind, Name: name, Path: resolved, ID: workspace.EntryID(kind, resolved, name)})
	out.Command = newPtr(indexCommandFor(kind, resolved))
	return out
}

// systemFolder reports a typed path that is one of the operating system's
// own folders (WS-53): on Windows the system drive's Windows, Program
// Files, Program Files (x86) and ProgramData folders (also as the
// environment names them); elsewhere /tmp, /etc, /dev, /proc, /sys, /run,
// /var, /var/lib, /usr, /bin, /sbin, /lib, /lib64 and /boot themselves (on
// macOS also their /private targets), and any folder inside the virtual
// /dev, /proc and /sys, whose files are no source and can block a read.
// Folders inside the others (/tmp/app, /usr/src/app, /var/www/app, and
// /run/media/<user>/<drive>, where removable drives are mounted) are
// ordinary folders. Both the typed path and the resolved one are
// compared, so /etc on macOS (a link to /private/etc) is refused as
// typed. same, when not nil, also compares each with each system folder
// on disk (os.SameFile), which catches spellings no string rule knows,
// such as a Windows 8.3 short name (C:\PROGRA~1).
func systemFolder(typed, resolved, goos string, getenv func(string) string, same func(a, b string) bool) (string, bool) {
	var exact, inside []string
	if goos == "windows" {
		drive := getenv("SystemDrive")
		if drive == "" {
			drive = "C:"
		}
		for _, name := range []string{"Windows", "Program Files", "Program Files (x86)", "ProgramData"} {
			exact = append(exact, drive+`\`+name)
		}
		for _, v := range []string{"WINDIR", "SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramData"} {
			if dir := getenv(v); dir != "" {
				exact = append(exact, dir)
			}
		}
	} else {
		exact = []string{"/tmp", "/etc", "/dev", "/proc", "/sys", "/run", "/var", "/var/lib", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/boot"}
		inside = []string{"/dev", "/proc", "/sys"}
		if goos == "darwin" {
			exact = append(exact, "/private/tmp", "/private/etc", "/private/var")
		}
	}
	for _, p := range []string{typed, resolved} {
		for _, dir := range exact {
			if samePathOn(goos, p, dir) || (same != nil && same(p, dir)) {
				return dir, true
			}
		}
		for _, dir := range inside {
			if strings.HasPrefix(p, dir+"/") {
				return dir, true
			}
		}
	}
	return "", false
}

// sameFolderOnDisk reports two paths that name one folder on this
// machine (os.SameFile); a path that cannot be read is no match.
func sameFolderOnDisk(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	return err == nil && os.SameFile(ia, ib)
}

// samePathOn compares a path with a system folder: on Windows after
// windowsFolderKey (letter case, separators, device prefixes and the
// trailing dots and spaces Windows ignores do not matter), elsewhere the
// strings must be equal or name one folder.
func samePathOn(goos, a, b string) bool {
	if goos == "windows" {
		return windowsFolderKey(a) == windowsFolderKey(b)
	}
	return a == b || workspace.SamePath(a, b)
}

// windowsFolderKey spells a Windows path the way the file system resolves
// it, for comparing with a system folder: forward slashes as
// backslashes; the device prefixes \\?\ and \\.\ (also \\?\UNC\) and
// the local administrative share \\localhost\C$\ reduced to the drive;
// "." and ".." components applied; the trailing dots and spaces Windows
// drops from each component removed (C:\Windows. is C:\Windows); and
// ASCII letter case folded. It is a comparison key only, never a path to
// open.
func windowsFolderKey(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	lower := asciiLower(p)
	for _, prefix := range []string{`\\?\unc\`, `\\.\unc\`} {
		if strings.HasPrefix(lower, prefix) {
			p, lower = `\\`+p[len(prefix):], `\\`+lower[len(prefix):]
		}
	}
	for _, prefix := range []string{`\\?\`, `\\.\`} {
		if strings.HasPrefix(lower, prefix) {
			p, lower = p[len(prefix):], lower[len(prefix):]
		}
	}
	// \\localhost\C$\Windows is C:\Windows.
	const localShare = `\\localhost\`
	if rest, ok := strings.CutPrefix(lower, localShare); ok && len(rest) >= 2 && rest[1] == '$' && (len(rest) == 2 || rest[2] == '\\') {
		rest = p[len(localShare):]
		p = rest[:1] + ":" + rest[2:]
	}
	var parts []string
	for i, part := range strings.Split(p, `\`) {
		if i == 0 {
			parts = append(parts, part)
			continue
		}
		switch part {
		case "", ".":
			continue
		case "..":
			if len(parts) > 1 {
				parts = parts[:len(parts)-1]
			}
			continue
		}
		if part = strings.TrimRight(part, ". "); part != "" {
			parts = append(parts, part)
		}
	}
	return asciiLower(strings.Join(parts, `\`))
}

func (s *workspaceServer) home() string {
	home := s.ws.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return ""
	}
	if r, err := workspace.Resolve(home); err == nil {
		return r
	}
	return filepath.Clean(home)
}

// configDir reports a path that is the user configuration folder itself,
// or a configuration folder of Graphvein's (the registry's) or inside one.
func (s *workspaceServer) configDir(path string) (string, bool) {
	if dir, err := os.UserConfigDir(); err == nil {
		if r, err := workspace.Resolve(dir); err == nil && workspace.SamePath(r, path) {
			return r, true
		}
	}
	dirs := append([]string{}, s.ws.ConfigDirs...)
	if p := s.ws.Registry.Path(); p != "" {
		dirs = append(dirs, filepath.Dir(p))
	}
	for _, d := range dirs {
		r, err := workspace.Resolve(d)
		if err != nil {
			r = filepath.Clean(d)
		}
		if workspace.SamePath(path, r) || workspace.Inside(path, r) {
			return r, true
		}
	}
	return "", false
}

// storeOrGitPart finds a .graphvein or .git component (ASCII case folded,
// for case-insensitive file systems).
func storeOrGitPart(path string) (string, bool) {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		switch asciiLower(part) {
		case ".graphvein", ".git":
			return part, true
		}
	}
	return "", false
}

// presence opens the folder's store read-only (WS-34): present only when
// runs are stored, absent only when the store does not exist or holds no
// run, unknown otherwise.
func (s *workspaceServer) presence(ctx context.Context, e workspace.Entry) *indexPresence {
	wi, err := s.ws.Open(ctx, e, false)
	if errors.Is(err, ErrNoIndex) {
		return &indexPresence{State: indexAbsent}
	}
	if err != nil {
		return &indexPresence{State: indexUnknown, Error: newPtr(err.Error())}
	}
	if wi.Close != nil {
		defer wi.Close()
	}
	repos := []string{wi.Options.Repository}
	if wi.Options.Project != nil {
		repos = repos[:0]
		for _, m := range wi.Options.Project.current().Members {
			repos = append(repos, m.Repository)
		}
	}
	var latest *lastRun
	for _, repo := range repos {
		runs, err := wi.Options.Store.Runs(ctx, repo)
		if err != nil {
			return &indexPresence{State: indexUnknown, Error: newPtr(err.Error())}
		}
		if l := latestRun(runs); l != nil && (latest == nil || l.FinishedAt > latest.FinishedAt) {
			latest = l
		}
	}
	if latest == nil {
		return &indexPresence{State: indexAbsent}
	}
	return &indexPresence{State: indexPresent, LastRun: newPtr(latest.FinishedAt)}
}

// indexCommandFor is the terminal command that indexes the folder, for
// the "copy the command" helper, quoted for POSIX shells and Git Bash.
func indexCommandFor(kind workspace.Kind, path string) string {
	quoted := "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	if kind == workspace.KindProject {
		return "graphvein index --project " + quoted
	}
	return "graphvein index " + quoted
}

// issueConfirm records a confirm value for what a check showed (WS-35,
// WS-76, WS-78, WS-80).
func (s *workspaceServer) issueConfirm(rec confirmRecord) (string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opt.now()
	for v, c := range s.confirms {
		if !now.Before(c.expires) {
			delete(s.confirms, v)
		}
	}
	for len(s.confirms) >= maxConfirms {
		var oldest string
		for v, c := range s.confirms {
			if oldest == "" || c.expires.Before(s.confirms[oldest].expires) {
				oldest = v
			}
		}
		delete(s.confirms, oldest)
	}
	value := rand.Text()
	rec.expires = now.Add(confirmLifetime)
	s.confirms[value] = rec
	return value, rec.expires
}

// takeConfirm uses up a confirm value: it is gone after this call,
// whatever the outcome. A value issued for another operation is not
// accepted (and is used up too).
func (s *workspaceServer) takeConfirm(value, op string) (confirmRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.confirms[value]
	delete(s.confirms, value)
	if !ok || c.op != op || !s.opt.now().Before(c.expires) {
		return confirmRecord{}, false
	}
	return c, true
}

// POST /api/v0/workspace/add

type addRequest struct {
	Path    *string `json:"path"`
	Confirm *string `json:"confirm"`
	Index   *bool   `json:"index"`
}

type addResponse struct {
	APIVersion int           `json:"api_version"`
	Entry      workspaceItem `json:"entry"`
	Added      bool          `json:"added"`
	Job        *jobDoc       `json:"job"`
	// RunError says why the run did not start although the entry was
	// registered (WS-36; T038 review): the index could not be opened,
	// or the run was refused. Null when the run started or none was asked.
	RunError *runError `json:"run_error"`
}

// runError is a run that did not start after an add registered the entry.
type runError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *workspaceServer) add(w http.ResponseWriter, r *http.Request, body []byte) error {
	var req addRequest
	if err := decodeWorkspaceBody(body, &req); err != nil {
		return err
	}
	if req.Path == nil || req.Confirm == nil {
		return badRequest("path and confirm are required")
	}
	ctx := r.Context()
	c, ok := s.takeConfirm(*req.Confirm, opAdd)
	if !ok {
		return &apiError{http.StatusConflict, "confirm_required", "adding a folder needs the confirm value of a check of it from the last 10 minutes, used once; check the folder again"}
	}
	if c.path != *req.Path {
		return &apiError{http.StatusConflict, "confirm_required", "the confirm value was issued for another folder; check this folder first"}
	}
	again := s.examine(ctx, c.path, c.choice)
	storePath := ""
	if again.Store != nil && again.Store.Path != nil {
		storePath = *again.Store.Path
	}
	if again.Outcome != outcomeReady || *again.Kind != c.kind || *again.Name != c.name || storePath != c.storePath {
		msg := "the folder changed since it was checked; check it again"
		if again.Refusal != nil {
			msg = again.Refusal.Message
		} else if storePath != c.storePath {
			msg = "the folder's index would now be kept at " + storePath + ", not where the check said; check it again"
		}
		return &apiError{http.StatusConflict, "path_changed", msg}
	}
	// WS-76: the chosen place is recorded (and an in-folder store
	// created) before the entry is registered, so the index is written
	// only where the confirmation said.
	if err := s.recordPlace(ctx, c.path, c.choice); err != nil {
		return err
	}
	e, added, err := s.ws.Registry.Add(workspace.Entry{Kind: c.kind, Name: c.name, Path: c.path})
	if err != nil {
		return &apiError{http.StatusInternalServerError, "registry_error", "writing the workspace registry: " + err.Error()}
	}
	out := addResponse{APIVersion: 0, Added: added}
	status := http.StatusOK
	if req.Index == nil || *req.Index {
		// The entry is registered whatever happens next: a failure to open
		// the index or to start the run is reported with it (R9), never as
		// the add's own error.
		oi, err := s.acquire(ctx, e.ID, true, true)
		if err == nil {
			err = oi.handler.runs.begin(ctx, false)
			if err == nil {
				out.Job = oi.handler.runs.latestJob()
				status = http.StatusAccepted
			}
			s.release(oi)
		}
		if err != nil {
			out.RunError = runErrorOf(err)
			if out.RunError.Code == "store_error" {
				logFailure(s.opt.logger(), r, err)
			}
		}
	}
	// A pass checks the new entry now, so the list can say "not indexed"
	// of an entry added without a run instead of "not checked yet".
	s.poke()
	out.Entry = s.item(ctx, e)
	writeJSON(w, status, out)
	return nil
}

// runErrorOf words a run that did not start: an API refusal keeps its code
// and message, anything else is a store error.
func runErrorOf(err error) *runError {
	var ae *apiError
	if errors.As(err, &ae) {
		return &runError{Code: ae.code, Message: ae.message}
	}
	return &runError{Code: "store_error", Message: err.Error()}
}

// item is one entry as GET /api/v0/workspace lists it.
func (s *workspaceServer) item(ctx context.Context, e workspace.Entry) workspaceItem {
	rec := &captureWriter{header: http.Header{}, status: http.StatusOK}
	s.list(rec, (&http.Request{}).WithContext(ctx))
	var doc workspaceResponse
	if err := json.Unmarshal(rec.body.Bytes(), &doc); err == nil {
		for _, it := range doc.Entries {
			if it.ID == e.ID {
				return it
			}
		}
	}
	return workspaceItem{ID: e.ID, Kind: e.Kind, Name: e.Name, Path: e.Path, Registered: true, State: entryNotOpen}
}

// POST /api/v0/workspace/remove

type removeRequest struct {
	ID *string `json:"id"`
}

func (s *workspaceServer) remove(w http.ResponseWriter, r *http.Request, body []byte) error {
	var req removeRequest
	if err := decodeWorkspaceBody(body, &req); err != nil {
		return err
	}
	if req.ID == nil || *req.ID == "" {
		return badRequest("id is required")
	}
	id := *req.ID
	if c := s.ws.Current; c != nil && c.Entry.ID == id {
		return &apiError{http.StatusConflict, "entry_in_use", "this server was started on " + c.Entry.Name + "; stop it, or start graphvein serve without a path, to remove it"}
	}
	s.mu.Lock()
	oi := s.open[id]
	if oi != nil && oi.handler.busy() {
		s.mu.Unlock()
		return &apiError{http.StatusConflict, "index_running", "an index run of this entry is running; remove it when it has finished"}
	}
	s.mu.Unlock()
	removed, err := s.ws.Registry.Remove(id)
	if err != nil {
		return &apiError{http.StatusInternalServerError, "registry_error", "writing the workspace registry: " + err.Error()}
	}
	if !removed {
		return &apiError{http.StatusNotFound, "unknown_entry", fmt.Sprintf("the workspace registry has no entry %q", id)}
	}
	s.mu.Lock()
	delete(s.checks, id)
	if s.currentID == id {
		s.currentID = ""
	}
	var closeNow *openIndex
	if oi := s.open[id]; oi != nil {
		delete(s.open, id)
		if oi.refs == 0 {
			closeNow = oi
		} else {
			oi.detached = true
		}
	}
	s.mu.Unlock()
	if closeNow != nil {
		closeNow.shutdown()
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_version": 0, "removed": id})
	return nil
}
