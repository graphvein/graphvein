package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/storeloc"
	"github.com/graphvein/graphvein/internal/workspace"
)

// This file holds where the workspace's indexes are stored (ADR 0093, ADR
// 0087 revision 5, ADR 0041 revision 2): the store of each entry and of a
// typed folder as the one store resolver finds it, the place a folder
// being added gets, and the machine-level Storage endpoints of Settings
// (ADR 0088 revision): GET /api/v0/store, which needs no index, and the
// guarded POSTs /api/v0/store/check, /move and /default. The POSTs pass
// the same checks as the workspace POSTs (WS-21), and a move or a default
// folder change needs the one-time confirm value of its check, bound to
// what the check showed.

// Paths of the store endpoints.
const (
	storePath        = "/api/v0/store"
	storeCheckPath   = "/api/v0/store/check"
	storeMovePath    = "/api/v0/store/move"
	storeDefaultPath = "/api/v0/store/default"
)

// moveIdleWait bounds how long a move waits for the requests and
// background checks using the entry's index to finish before it closes the
// index.
const moveIdleWait = 30 * time.Second

// Places of a store, as the UI words them: inside the indexed folder (Git
// lists it), outside it, set by GRAPHVEIN_SQLITE_PATH, or Postgres.
const (
	placeInside      = "inside"
	placeOutside     = "outside"
	placeEnvironment = "environment"
	placePostgres    = "postgres"
)

// storeDoc says where one folder's index is stored (WS-73, WS-77): the
// SQLite file, or Postgres named by its variable, never its URL (which may
// hold a password).
type storeDoc struct {
	Kind string `json:"kind"`
	// Path is the SQLite file; null for Postgres or when it is not known.
	Path *string `json:"path"`
	// Env is the variable that chose the store (GRAPHVEIN_POSTGRES_URL or
	// GRAPHVEIN_SQLITE_PATH); null otherwise.
	Env *string `json:"env"`
	// Origin is the lookup step (postgres, sqlite_path_env, in_folder,
	// stores_file, default); null when the store could not be resolved.
	Origin *string `json:"origin"`
	// Place is inside, outside, environment or postgres.
	Place *string `json:"place"`
	// Exists is true when Path holds a file now (false for Postgres).
	Exists bool `json:"exists"`
	// Size is the bytes of index.db and its -wal file when it exists.
	Size *int64 `json:"size"`
	// StoresDir is the folder new indexes go to and DirSource where it
	// comes from (platform, env, stores_file); null for Postgres.
	StoresDir *string `json:"stores_dir"`
	DirSource *string `json:"dir_source"`
	// ErrorCode and Error say why the store could not be resolved:
	// two_indexes, foreign_store or store_error.
	ErrorCode *string `json:"error_code"`
	Error     *string `json:"error"`
}

// storePlaces are the places a ready folder's new index can be kept
// (WS-73): Choosable is false when an index exists already (it stays where
// it is) or in team mode, with the reason.
type storePlaces struct {
	Choosable bool    `json:"choosable"`
	Reason    *string `json:"reason"`
	// Chosen is the place the check was asked for (default, inside or
	// folder); null when none was (the resolver's place).
	Chosen *string `json:"chosen"`
	// Default and Inside are the index files of those places; Folder is
	// the typed folder's index file when that place was chosen.
	Default *string `json:"default"`
	Inside  *string `json:"inside"`
	Folder  *string `json:"folder"`
}

// storeChoice is a check's store place; the zero value is none.
type storeChoice struct {
	Place storeloc.Place
	Dir   string
}

// choiceOf reads store_place and store_dir (WS-74).
func choiceOf(place, dir *string) (storeChoice, error) {
	if place == nil {
		if dir != nil {
			return storeChoice{}, badRequest("store_dir needs store_place \"folder\"")
		}
		return storeChoice{}, nil
	}
	switch p := storeloc.Place(*place); p {
	case storeloc.PlaceDefault, storeloc.PlaceInside:
		if dir != nil {
			return storeChoice{}, badRequest("store_dir is only for store_place \"folder\"")
		}
		return storeChoice{Place: p}, nil
	case storeloc.PlaceFolder:
		if dir == nil || *dir == "" {
			return storeChoice{}, badRequest("store_place \"folder\" needs store_dir, the absolute folder to keep the index in")
		}
		return storeChoice{Place: p, Dir: *dir}, nil
	}
	return storeChoice{}, badRequest("store_place must be \"default\", \"inside\" or \"folder\"")
}

// docOf words a resolved location.
func docOf(loc storeloc.Location) *storeDoc {
	origin := string(loc.Origin)
	d := &storeDoc{Kind: "sqlite", Origin: &origin, Exists: loc.Exists}
	switch {
	case loc.Origin == storeloc.OriginPostgres:
		d.Kind = "postgres"
		d.Env, d.Place = newPtr(storeloc.EnvPostgres), newPtr(placePostgres)
		return d
	case loc.Origin == storeloc.OriginEnvFile:
		d.Env, d.Place = newPtr(storeloc.EnvSQLitePath), newPtr(placeEnvironment)
	case loc.Inside():
		d.Place = newPtr(placeInside)
	default:
		d.Place = newPtr(placeOutside)
	}
	d.Path = newPtr(loc.Path)
	if loc.Exists {
		d.Size = newPtr(fileSize(loc.Path) + fileSize(loc.Path+"-wal"))
	}
	if loc.StoresDir != "" {
		d.StoresDir, d.DirSource = newPtr(loc.StoresDir), newPtr(string(loc.DirSource))
	}
	return d
}

// errorDoc is the store of a folder whose store could not be resolved.
func errorDoc(err error) *storeDoc {
	code := "store_error"
	var two *storeloc.TwoIndexesError
	var foreign *storeloc.ForeignStoreError
	switch {
	case errors.As(err, &two):
		code = "two_indexes"
	case errors.As(err, &foreign):
		code = "foreign_store"
	}
	// Postgres resolves without error (only a folder that cannot be
	// resolved fails), so a failed store is a local one.
	return &storeDoc{Kind: "sqlite", ErrorCode: &code, Error: newPtr(err.Error())}
}

func fileSize(p string) int64 {
	info, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return info.Size()
}

// commandLineEntry reports the entry named on serve's command line, the
// only one GRAPHVEIN_SQLITE_PATH applies to (WS-70).
func (s *workspaceServer) commandLineEntry(id string) bool {
	return s.ws.Current != nil && s.ws.Current.Entry.ID == id
}

// entryLocation resolves an entry's store as its index opens it.
func (s *workspaceServer) entryLocation(e workspace.Entry) (storeloc.Location, error) {
	return s.ws.Stores.Resolve(e.Path, storeloc.Scope{CommandLine: s.commandLineEntry(e.ID)})
}

// entryStoreDoc is an entry's store, or nil without a resolver.
func (s *workspaceServer) entryStoreDoc(e workspace.Entry) *storeDoc {
	if s.ws.Stores == nil {
		return nil
	}
	loc, err := s.entryLocation(e)
	if err != nil {
		return errorDoc(err)
	}
	return docOf(loc)
}

// storeOpenError words an entry's store that cannot be opened for a
// reason the user can act on (WS-71, WS-81); nil for any other error.
func storeOpenError(err error) *apiError {
	var two *storeloc.TwoIndexesError
	var foreign *storeloc.ForeignStoreError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &two):
		return &apiError{http.StatusConflict, "two_indexes", err.Error()}
	case errors.As(err, &foreign):
		return &apiError{http.StatusConflict, "foreign_store", err.Error()}
	case errors.Is(err, storeloc.ErrMoved):
		return &apiError{http.StatusConflict, "store_moved", err.Error()}
	}
	return nil
}

// registeredFolders lists the registered folders (a store must not be put
// inside one: its scan and Git would see it).
func registeredFolders(entries []workspace.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

// placeOf resolves the store of a folder being added (WS-71, WS-73) and
// applies the check's place (WS-74, WS-75): the store the folder gets, the
// places it could get, or a refusal.
func (s *workspaceServer) placeOf(folder string, choice storeChoice, entries []workspace.Entry) (*storeDoc, *storePlaces, *refusal) {
	cur, err := s.ws.Stores.Resolve(folder, storeloc.Scope{})
	if err != nil {
		d := errorDoc(err)
		if *d.ErrorCode == "store_error" {
			return nil, nil, &refusal{Row: "WS-D", Code: "store_error", Message: "where the index would be kept cannot be found: " + err.Error()}
		}
		return nil, nil, &refusal{Row: "WS-71", Code: *d.ErrorCode, Message: err.Error()}
	}
	places := &storePlaces{Choosable: true}
	key := storeloc.Key(cur.Folder)
	if cur.StoresDir != "" {
		places.Default = newPtr(filepath.Join(cur.StoresDir, key, storeloc.StoreFile))
	}
	places.Inside = newPtr(filepath.Join(cur.Folder, storeloc.InFolderDir, storeloc.StoreFile))
	switch {
	case cur.Origin == storeloc.OriginPostgres:
		places = &storePlaces{Reason: newPtr("every index is in the Postgres database from " + storeloc.EnvPostgres + " (team mode)")}
	case cur.Exists:
		places.Choosable = false
		places.Reason = newPtr("this folder already has an index at " + cur.Path + "; it stays there (Settings can move it once the folder is added)")
	}
	if choice.Place == "" {
		return docOf(cur), places, nil
	}
	places.Chosen = newPtr(string(choice.Place))
	if choice.Place == storeloc.PlaceInside && s.ws.CreateStore == nil {
		return nil, nil, &refusal{Row: "WS-74", Code: "place_unavailable", Message: "this server cannot create an index inside the folder"}
	}
	if choice.Place == storeloc.PlaceFolder && !filepath.IsAbs(choice.Dir) {
		return nil, nil, &refusal{Row: "WS-75", Code: "not_absolute", Message: "type the absolute path of the folder to keep the index in; ~ and variables are not expanded"}
	}
	loc, err := s.ws.Stores.PlanPlace(folder, choice.Place, choice.Dir, registeredFolders(entries))
	if err != nil {
		return nil, nil, placeRefusal(err)
	}
	if choice.Place == storeloc.PlaceFolder {
		places.Folder = newPtr(loc.Path)
	}
	return docOf(loc), places, nil
}

// destinationCodes are the refusals of a typed store folder (WS-75); the
// others are a check's own (WS-74 for a place, WS-78 for a move).
var destinationCodes = map[string]bool{
	"not_absolute": true, "network_path": true, "filesystem_root": true, "home_directory": true,
	"inside_git": true, "inside_folder": true, "inside_registered": true, "not_a_directory": true,
	"not_writable": true, "unreadable": true, "index_exists": true, "foreign_store": true,
}

// placeRefusal words a refusal of a store place.
func placeRefusal(err error) *refusal {
	var re *storeloc.RefusalError
	if errors.As(err, &re) {
		if destinationCodes[re.Code] {
			return &refusal{Row: "WS-75", Code: re.Code, Message: re.Message}
		}
		return &refusal{Row: "WS-74", Code: re.Code, Message: re.Message}
	}
	if d := errorDoc(err); *d.ErrorCode != "store_error" {
		return &refusal{Row: "WS-71", Code: *d.ErrorCode, Message: err.Error()}
	}
	return &refusal{Row: "WS-D", Code: "store_error", Message: err.Error()}
}

// recordPlace makes an add's chosen place the folder's (WS-76): a
// stores.yaml entry for a typed folder or the removal of one for the
// default place, and for the folder's own .graphvein an empty store there
// (an in-folder store is found by its file).
func (s *workspaceServer) recordPlace(ctx context.Context, folder string, choice storeChoice) error {
	if choice.Place == "" || s.ws.Stores == nil {
		return nil
	}
	entries, _ := s.entries()
	loc, err := s.ws.Stores.PlanPlace(folder, choice.Place, choice.Dir, registeredFolders(entries))
	if err != nil {
		return &apiError{http.StatusConflict, "path_changed", err.Error()}
	}
	if loc.Exists {
		return nil
	}
	if err := s.ws.Stores.RecordPlace(loc); err != nil {
		return fmt.Errorf("recording where the index of %s is kept: %w", folder, err)
	}
	if loc.Origin == storeloc.OriginInFolder {
		if err := s.ws.CreateStore(ctx, loc); err != nil {
			return fmt.Errorf("creating the index of %s at %s: %w", folder, loc.Path, err)
		}
	}
	return nil
}

// notMoving refuses a request for an entry whose store is being moved.
func (s *workspaceServer) notMoving(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.moving[id] {
		return &apiError{http.StatusConflict, "store_moving", "the index of this entry is being moved; try again when the move has finished"}
	}
	return nil
}

// beginDirect counts an open of the entry's store outside the open
// indexes; false while the store is being moved.
func (s *workspaceServer) beginDirect(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.moving[id] {
		return false
	}
	s.direct[id]++
	return true
}

func (s *workspaceServer) endDirect(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.direct[id]--; s.direct[id] <= 0 {
		delete(s.direct, id)
	}
}

// GET /api/v0/store (WS-77)

type storeSettingsResponse struct {
	APIVersion int `json:"api_version"`
	// TeamMode is set when GRAPHVEIN_POSTGRES_URL is: every index is in
	// that database (the URL is never shown).
	TeamMode envDoc `json:"team_mode"`
	// SQLitePath is GRAPHVEIN_SQLITE_PATH: set, and the entry named on
	// serve's command line that uses it (null when none does).
	SQLitePath envEntryDoc `json:"sqlite_path"`
	// DefaultDir is where new indexes are kept ("New indexes are kept
	// in").
	DefaultDir defaultDirDoc `json:"default_dir"`
	// StoresFile and Registry are the two machine files, read-only here.
	StoresFile fileDoc     `json:"stores_file"`
	Registry   fileDoc     `json:"registry"`
	Entries    []entryDocs `json:"entries"`
}

type envDoc struct {
	Env string `json:"env"`
	Set bool   `json:"set"`
}

type envEntryDoc struct {
	Env   string  `json:"env"`
	Set   bool    `json:"set"`
	Entry *string `json:"entry"`
}

type defaultDirDoc struct {
	Path *string `json:"path"`
	// Source is platform (the per-user data folder), env
	// (GRAPHVEIN_STORE_DIR) or stores_file (default_dir).
	Source *string `json:"source"`
	Env    string  `json:"env"`
	// Editable is false when GRAPHVEIN_STORE_DIR or team mode decides,
	// with the reason.
	Editable bool    `json:"editable"`
	Reason   *string `json:"reason"`
	Error    *string `json:"error"`
}

type fileDoc struct {
	Path  *string `json:"path"`
	Error *string `json:"error"`
}

// entryDocs is one entry's store in GET /api/v0/store.
type entryDocs struct {
	ID          string         `json:"id"`
	Kind        workspace.Kind `json:"kind"`
	Name        string         `json:"name"`
	Folder      string         `json:"folder"`
	FolderFound bool           `json:"folder_found"`
	Current     bool           `json:"current"`
	// Store is the entry's store; null when its folder is gone.
	Store *storeDoc `json:"store"`
	// Movable says whether POST /api/v0/store/check can move it, and
	// Reason why not.
	Movable bool    `json:"movable"`
	Reason  *string `json:"reason"`
}

func (s *workspaceServer) storeSettings(w http.ResponseWriter, r *http.Request) {
	if s.ws.Stores == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "this server has no store resolver")
		return
	}
	out := storeSettingsResponse{APIVersion: 0,
		TeamMode:   envDoc{Env: storeloc.EnvPostgres, Set: s.ws.Stores.Env(storeloc.EnvPostgres) != ""},
		SQLitePath: envEntryDoc{Env: storeloc.EnvSQLitePath, Set: s.ws.Stores.Env(storeloc.EnvSQLitePath) != ""},
		StoresFile: fileDoc{Path: pathOrNull(s.ws.Stores.StoresFile)},
		Registry:   fileDoc{Path: pathOrNull(s.ws.Registry.Path())},
		Entries:    []entryDocs{},
	}
	out.DefaultDir = s.defaultDirDoc(out.TeamMode.Set)
	if out.DefaultDir.Error != nil {
		out.StoresFile.Error = out.DefaultDir.Error
	}
	entries, regErr := s.entries()
	if regErr != nil {
		out.Registry.Error = newPtr(regErr.Error())
	}
	current := s.current()
	for _, e := range entries {
		d := entryDocs{ID: e.ID, Kind: e.Kind, Name: e.Name, Folder: e.Path, FolderFound: !folderGone(e.Path), Current: e.ID == current}
		// A folder that is gone has no key to resolve: its store is not
		// named (graphvein store list finds it).
		var loc storeloc.Location
		var err error
		if d.FolderFound {
			loc, err = s.entryLocation(e)
		}
		switch {
		case !d.FolderFound:
		case err != nil:
			d.Store = errorDoc(err)
		default:
			d.Store = docOf(loc)
			if loc.Origin == storeloc.OriginEnvFile {
				out.SQLitePath.Entry = newPtr(e.ID)
			}
		}
		if why := s.notMovable(e, loc, err); why != "" {
			d.Reason = &why
		} else {
			d.Movable = true
		}
		out.Entries = append(out.Entries, d)
	}
	writeJSON(w, http.StatusOK, out)
}

func pathOrNull(p string) *string {
	if p == "" {
		return nil
	}
	return &p
}

// defaultDirDoc is "New indexes are kept in" (WS-77, WS-80).
func (s *workspaceServer) defaultDirDoc(teamMode bool) defaultDirDoc {
	d := defaultDirDoc{Env: storeloc.EnvStoreDir}
	dir, source, err := s.ws.Stores.StoresDir()
	if err != nil {
		d.Error = newPtr(err.Error())
	} else {
		d.Path, d.Source = &dir, newPtr(string(source))
	}
	switch {
	case teamMode:
		d.Reason = newPtr("every index is in the Postgres database from " + storeloc.EnvPostgres + " (team mode)")
	case s.ws.Stores.Env(storeloc.EnvStoreDir) != "":
		d.Reason = newPtr(storeloc.EnvStoreDir + " is set in the environment of graphvein serve; it decides the folder until it is unset")
	case err != nil && s.ws.Stores.DataDir != "":
		// stores.yaml does not load: it is never rewritten.
		d.Reason = newPtr(err.Error())
	default:
		// Without a per-user data folder a typed folder is the way out.
		d.Editable = true
	}
	return d
}

// notMovable says why an entry's store cannot be moved from the UI, ""
// when it can (WS-77, WS-78).
func (s *workspaceServer) notMovable(e workspace.Entry, loc storeloc.Location, resolveErr error) string {
	switch {
	case folderGone(e.Path):
		return "the folder is not at " + e.Path + " any more"
	case resolveErr != nil:
		return resolveErr.Error()
	case loc.Origin == storeloc.OriginPostgres:
		return "every index is in the Postgres database from " + storeloc.EnvPostgres + " (team mode); there is no local file to move"
	case loc.Origin == storeloc.OriginEnvFile:
		return storeloc.EnvSQLitePath + " names this index's file for this server run only; unset it to move the folder's own index, or move the file yourself"
	case !loc.Exists:
		return "there is no index yet; it will be created at " + loc.Path
	case s.commandLineEntry(e.ID) && s.ws.Current.Close == nil:
		return "this server was started on this folder; stop it and run graphvein store move"
	}
	return ""
}

// POST /api/v0/store/check (WS-78, WS-80)

type storeCheckRequest struct {
	// ID and DestDir check a move of an entry's store to DestDir ("" for
	// the stores folder).
	ID      *string `json:"id"`
	DestDir *string `json:"dest_dir"`
	// DefaultDir checks a new folder for new indexes ("" for the
	// platform's).
	DefaultDir *string `json:"default_dir"`
}

type storeCheckResponse struct {
	APIVersion int      `json:"api_version"`
	Outcome    string   `json:"outcome"`
	Refusal    *refusal `json:"refusal"`
	// Move is a ready move: the entry, its folder, the index file now and
	// after the move, whether that is the stores folder (no stores.yaml
	// entry) and the size to copy.
	Move *movePlanDoc `json:"move"`
	// DefaultDir is a ready change of the folder for new indexes.
	DefaultDir *defaultChangeDoc `json:"default_dir"`
	Confirm    *string           `json:"confirm"`
	Expires    *string           `json:"confirm_expires_at"`
	// Command is the terminal command that does the same move.
	Command *string `json:"command"`
}

type movePlanDoc struct {
	ID        string `json:"id"`
	Folder    string `json:"folder"`
	From      string `json:"from"`
	FromPlace string `json:"from_place"`
	To        string `json:"to"`
	ToDefault bool   `json:"to_default"`
	Size      int64  `json:"size"`
}

type defaultChangeDoc struct {
	From       *string `json:"from"`
	FromSource *string `json:"from_source"`
	To         string  `json:"to"`
	// ToPlatform is true when To is the platform's folder (the request's
	// ""), which removes default_dir from stores.yaml.
	ToPlatform bool `json:"to_platform"`
}

func (s *workspaceServer) storeCheck(w http.ResponseWriter, r *http.Request, body []byte) error {
	var req storeCheckRequest
	if err := decodeWorkspaceBody(body, &req); err != nil {
		return err
	}
	if s.ws.Stores == nil {
		return &apiError{http.StatusServiceUnavailable, "store_unavailable", "this server has no store resolver"}
	}
	var out storeCheckResponse
	var err error
	switch {
	case req.DefaultDir != nil && req.ID == nil && req.DestDir == nil:
		out, err = s.checkDefault(*req.DefaultDir)
	case req.ID != nil && req.DestDir != nil && req.DefaultDir == nil:
		out, err = s.checkMove(*req.ID, *req.DestDir)
	default:
		return badRequest("give either id and dest_dir (a move) or default_dir")
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// checkMove checks a move of entry id's store to dest ("" for its place in
// the stores folder) and issues a confirm value bound to the entry and
// both index files.
func (s *workspaceServer) checkMove(id, dest string) (storeCheckResponse, error) {
	out := storeCheckResponse{Outcome: outcomeRefused}
	plan, rf, err := s.planMove(id, dest)
	if err != nil {
		return out, err
	}
	if rf != nil {
		out.Refusal = rf
		return out, nil
	}
	out.Outcome = outcomeReady
	from := docOf(plan.From)
	out.Move = &movePlanDoc{ID: id, Folder: plan.From.Folder, From: plan.From.Path, FromPlace: *from.Place, To: plan.To, ToDefault: plan.ToDefault, Size: *from.Size}
	value, expires := s.issueConfirm(confirmRecord{op: opMove, entry: id, path: dest, from: plan.From.Path, to: plan.To})
	out.Confirm, out.Expires = &value, newPtr(expires.UTC().Format(time.RFC3339))
	command := "graphvein store move " + shellQuote(plan.From.Folder)
	if dest != "" {
		command += " " + shellQuote(dest)
	}
	out.Command = &command
	return out, nil
}

// planMove plans a move of entry id's store (WS-78): an unknown entry is
// an error; a store that cannot be moved, or a destination refused, is a
// refusal.
func (s *workspaceServer) planMove(id, dest string) (storeloc.MovePlan, *refusal, error) {
	e, err := s.entry(id)
	if err != nil {
		return storeloc.MovePlan{}, nil, err
	}
	loc, resolveErr := s.entryLocation(e)
	if resolveErr != nil {
		rf := placeRefusal(resolveErr)
		rf.Entry = newPtr(id)
		return storeloc.MovePlan{}, rf, nil
	}
	if why := s.notMovable(e, loc, nil); why != "" {
		code := "not_movable"
		switch {
		case loc.Origin == storeloc.OriginPostgres:
			code = "team_mode"
		case loc.Origin == storeloc.OriginEnvFile:
			code = "sqlite_path_env"
		case folderGone(e.Path):
			code = "folder_gone"
		case !loc.Exists:
			code = "no_index"
		case s.commandLineEntry(e.ID):
			code = "entry_in_use"
		}
		return storeloc.MovePlan{}, &refusal{Row: "WS-78", Code: code, Message: why, Entry: newPtr(id)}, nil
	}
	if dest != "" && !filepath.IsAbs(dest) {
		return storeloc.MovePlan{}, &refusal{Row: "WS-75", Code: "not_absolute", Message: "type the absolute path of the folder to move the index to; ~ and variables are not expanded"}, nil
	}
	entries, _ := s.entries()
	plan, err := s.ws.Stores.PlanMove(e.Path, dest, registeredFolders(entries))
	if err != nil {
		rf := placeRefusal(err)
		if rf.Row == "WS-74" {
			rf.Row = "WS-78"
		}
		return storeloc.MovePlan{}, rf, nil
	}
	return plan, nil, nil
}

// checkDefault checks a new folder for new indexes (WS-80): "" is the
// platform's folder.
func (s *workspaceServer) checkDefault(dir string) (storeCheckResponse, error) {
	out := storeCheckResponse{Outcome: outcomeRefused}
	refuse := func(row, code, message string) (storeCheckResponse, error) {
		out.Refusal = &refusal{Row: row, Code: code, Message: message}
		return out, nil
	}
	cur := s.defaultDirDoc(s.ws.Stores.Env(storeloc.EnvPostgres) != "")
	if !cur.Editable {
		code := "store_error"
		switch {
		case s.ws.Stores.Env(storeloc.EnvPostgres) != "":
			code = "team_mode"
		case s.ws.Stores.Env(storeloc.EnvStoreDir) != "":
			code = "set_by_environment"
		}
		return refuse("WS-80", code, *cur.Reason)
	}
	change := &defaultChangeDoc{From: cur.Path, FromSource: cur.Source}
	if dir == "" {
		platform, err := s.platformStoresDir()
		if err != nil {
			return refuse("WS-80", "no_platform_folder", err.Error())
		}
		change.To, change.ToPlatform = platform, true
	} else {
		if !filepath.IsAbs(dir) {
			return refuse("WS-75", "not_absolute", "type the absolute path of the folder for new indexes; ~ and variables are not expanded")
		}
		dest, err := storeloc.ResolveDest(dir)
		if err != nil {
			return refuse("WS-75", "unreadable", err.Error())
		}
		entries, _ := s.entries()
		if err := storeloc.CheckStoresDir(dest, registeredFolders(entries)); err != nil {
			out.Refusal = placeRefusal(err)
			return out, nil
		}
		change.To = dest
	}
	if cur.Path != nil && storeloc.SamePath(*cur.Path, change.To) {
		return refuse("WS-80", "unchanged", "new indexes are already kept in "+change.To)
	}
	out.Outcome, out.DefaultDir = outcomeReady, change
	value, expires := s.issueConfirm(confirmRecord{op: opDefault, path: dir, to: change.To})
	out.Confirm, out.Expires = &value, newPtr(expires.UTC().Format(time.RFC3339))
	return out, nil
}

// platformStoresDir is the per-user data folder's stores folder.
func (s *workspaceServer) platformStoresDir() (string, error) {
	if s.ws.Stores.DataDir == "" {
		return "", fmt.Errorf("no per-user data folder is known on this system; type a folder")
	}
	return filepath.Join(s.ws.Stores.DataDir, "graphvein", "stores"), nil
}

// shellQuote quotes a path for POSIX shells and Git Bash.
func shellQuote(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

// POST /api/v0/store/move (WS-79)

type storeMoveRequest struct {
	ID      *string `json:"id"`
	DestDir *string `json:"dest_dir"`
	Confirm *string `json:"confirm"`
}

type storeMoveResponse struct {
	APIVersion int    `json:"api_version"`
	ID         string `json:"id"`
	From       string `json:"from"`
	To         string `json:"to"`
	// Bytes and Runs are the copy's size and its index runs (checked
	// equal to the original's).
	Bytes int64 `json:"bytes"`
	Runs  int   `json:"runs"`
	// Deleted lists the original's files that were deleted; Remaining
	// those that could not be (the folder then has two indexes and is
	// refused until they are deleted), with Warning.
	Deleted   []string `json:"deleted"`
	Remaining []string `json:"remaining"`
	Warning   *string  `json:"warning"`
	// Recorded is true when stores.yaml changed.
	Recorded bool `json:"recorded"`
}

func (s *workspaceServer) storeMove(w http.ResponseWriter, r *http.Request, body []byte) error {
	var req storeMoveRequest
	if err := decodeWorkspaceBody(body, &req); err != nil {
		return err
	}
	if req.ID == nil || req.DestDir == nil || req.Confirm == nil {
		return badRequest("id, dest_dir and confirm are required")
	}
	if s.ws.Stores == nil {
		return &apiError{http.StatusServiceUnavailable, "store_unavailable", "this server has no store resolver"}
	}
	c, ok := s.takeConfirm(*req.Confirm, opMove)
	if !ok {
		return &apiError{http.StatusConflict, "confirm_required", "moving an index needs the confirm value of a check of that move from the last 10 minutes, used once; check the move again"}
	}
	if c.entry != *req.ID || c.path != *req.DestDir {
		return &apiError{http.StatusConflict, "confirm_required", "the confirm value was issued for another move; check this move first"}
	}
	id := *req.ID
	plan, rf, err := s.planMove(id, c.path)
	if err != nil {
		return err
	}
	if rf != nil || plan.From.Path != c.from || plan.To != c.to {
		msg := "the index or the destination changed since the move was checked; check it again"
		if rf != nil {
			msg = rf.Message
		}
		return &apiError{http.StatusConflict, "path_changed", msg}
	}
	release, err := s.stopServing(id)
	if err != nil {
		return err
	}
	defer release()
	res, err := s.ws.Stores.Move(r.Context(), plan, storeloc.MoveOptions{Wait: indexrun.LockWait})
	var moveErr *storeloc.MoveError
	switch {
	case errors.Is(err, storeloc.ErrLocked):
		return &apiError{http.StatusConflict, "index_locked", err.Error()}
	case err != nil && !(errors.As(err, &moveErr) && moveErr.Moved):
		logFailure(s.opt.logger(), r, err)
		return &apiError{http.StatusInternalServerError, "move_failed", err.Error() + "; the index was not moved"}
	}
	// The entry's next request opens it at its new place; its freshness
	// is checked again.
	s.mu.Lock()
	delete(s.checks, id)
	s.mu.Unlock()
	s.poke()
	out := storeMoveResponse{APIVersion: 0, ID: id, From: plan.From.Path, To: plan.To, Bytes: res.Bytes, Runs: res.Runs, Deleted: res.Deleted, Remaining: []string{}, Recorded: res.Recorded}
	if out.Deleted == nil {
		out.Deleted = []string{}
	}
	if moveErr != nil {
		out.Remaining = moveErr.Remaining
		out.Warning = newPtr(moveErr.Error())
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// stopServing stops serving entry id for a move (WS-79): new requests and
// background checks for it are refused (store_moving), opens that already
// started finish, requests in flight end (at most moveIdleWait), a
// running re-index or ref job refuses the move (index_running), and the
// index is closed, so its store files are no longer open. release ends
// the refusal; the entry's next request opens the index again.
func (s *workspaceServer) stopServing(id string) (release func(), err error) {
	s.mu.Lock()
	if s.moving[id] {
		s.mu.Unlock()
		return nil, &apiError{http.StatusConflict, "store_moving", "this index is already being moved"}
	}
	if oi := s.open[id]; oi != nil && oi.handler.busy() {
		s.mu.Unlock()
		return nil, indexRunning()
	}
	s.moving[id] = true
	s.mu.Unlock()
	release = func() {
		s.mu.Lock()
		delete(s.moving, id)
		s.mu.Unlock()
	}
	// Opens that passed their check before the mark run under openMu:
	// taking it once waits for them to finish.
	s.openMu.Lock()
	s.openMu.Unlock()
	deadline := time.Now().Add(moveIdleWait)
	for {
		s.mu.Lock()
		oi := s.open[id]
		idle := s.direct[id] == 0 && (oi == nil || oi.refs == 0)
		if idle {
			if oi != nil && oi.handler.busy() {
				s.mu.Unlock()
				release()
				return nil, indexRunning()
			}
			if oi != nil {
				delete(s.open, id)
			}
			s.mu.Unlock()
			if oi != nil {
				oi.shutdown()
				if oi.pinned && s.ws.Current != nil && s.ws.Current.Close != nil {
					s.ws.Current.Close()
				}
			}
			return release, nil
		}
		s.mu.Unlock()
		if time.Now().After(deadline) {
			release()
			return nil, &apiError{http.StatusConflict, "index_busy", "requests are still reading this index; try the move again"}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func indexRunning() error {
	return &apiError{http.StatusConflict, "index_running", "an index run or a ref job of this entry is running; move its index when it has finished"}
}

// POST /api/v0/store/default (WS-80)

type storeDefaultRequest struct {
	DefaultDir *string `json:"default_dir"`
	Confirm    *string `json:"confirm"`
}

func (s *workspaceServer) storeDefault(w http.ResponseWriter, r *http.Request, body []byte) error {
	var req storeDefaultRequest
	if err := decodeWorkspaceBody(body, &req); err != nil {
		return err
	}
	if req.DefaultDir == nil || req.Confirm == nil {
		return badRequest("default_dir and confirm are required")
	}
	if s.ws.Stores == nil {
		return &apiError{http.StatusServiceUnavailable, "store_unavailable", "this server has no store resolver"}
	}
	c, ok := s.takeConfirm(*req.Confirm, opDefault)
	if !ok || c.path != *req.DefaultDir {
		return &apiError{http.StatusConflict, "confirm_required", "changing the folder for new indexes needs the confirm value of a check of that folder from the last 10 minutes, used once; check it again"}
	}
	again, err := s.checkDefault(c.path)
	if err != nil {
		return err
	}
	if again.Outcome != outcomeReady || again.DefaultDir.To != c.to {
		msg := "the folder changed since it was checked; check it again"
		if again.Refusal != nil {
			msg = again.Refusal.Message
		}
		return &apiError{http.StatusConflict, "path_changed", msg}
	}
	dir := c.to
	if again.DefaultDir.ToPlatform {
		dir = ""
	}
	if err := s.ws.Stores.SetDefaultDir(dir); err != nil {
		return fmt.Errorf("changing the folder for new indexes: %w", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_version": 0, "default_dir": s.defaultDirDoc(s.ws.Stores.Env(storeloc.EnvPostgres) != "")})
	return nil
}
