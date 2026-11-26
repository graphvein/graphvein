package server

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
)

// This file holds the Settings page's endpoint (T039, ADR 0088,
// docs/contract/claims/settings.md): GET /api/v0/config states every key
// the form shows with its effective value, its origin and when a change
// applies; POST /api/v0/config is the guarded edit of graphvein.yaml,
// typed operations on those keys or the whole text, with the explorer
// path value edit's guards (preview, base hash, result hash, session
// token, loopback peer, atomic write). In project mode the member
// dispatch gives each member its own handler (repository=).

const (
	configPath = "/api/v0/config"
	// maxConfigEditBody bounds the POST body: a whole file of at most
	// maxManifestSize bytes, JSON-escaped, fits.
	maxConfigEditBody = 1 << 20
	maxConfigEditOps  = 64
	// ignoreFile is the scanner's root ignore file, shown read-only.
	ignoreFile = ".graphveinignore"
)

// configHandler answers GET and POST /api/v0/config for one repository.
type configHandler struct {
	opt   Options
	token string
}

func newConfigHandler(opt Options) configHandler {
	return configHandler{opt: opt, token: rand.Text()}
}

type configResponse struct {
	APIVersion int    `json:"api_version"`
	Repository string `json:"repository"`
	// File is the configuration file's name; never a path to choose.
	File string `json:"file"`
	// Exists, Hash and Text are null for a member without a checkout here
	// (nothing is known about its file). Hash is the base_hash of an
	// edit: "sha256:..." or "absent".
	Exists *bool   `json:"exists"`
	Hash   *string `json:"hash"`
	Text   *string `json:"text"`
	// Editable is false for a member without a checkout; Reason says why.
	Editable bool    `json:"editable"`
	Reason   *string `json:"reason"`
	Token    string  `json:"token"`
	// ConfigError is the strict decoder's message when the file does not
	// load (row ST-15): no value is reported and typed edits are refused;
	// a whole-text edit can fix it.
	ConfigError *string          `json:"config_error"`
	Keys        []configKeyDoc   `json:"keys"`
	OtherKeys   []configOtherDoc `json:"other_keys"`
	Reindex     configReindexDoc `json:"reindex"`
	// IndexSettings compares the settings the last index run read with
	// the file shown here (rows ST-43..ST-46).
	IndexSettings *settingsStatus `json:"index_settings"`
	Ignore        configIgnoreDoc `json:"ignore"`
	// Project is the manifest's part in project mode; null otherwise.
	Project *configProjectDoc `json:"project"`
}

// configKeyDoc is one form key (rows ST-1..ST-14).
type configKeyDoc struct {
	Key     string   `json:"key"`
	Kind    string   `json:"kind"`
	Choices []string `json:"choices,omitempty"`
	Min     *int     `json:"min,omitempty"`
	Max     *int     `json:"max,omitempty"`
	// Value is the effective value (null when not set or unknown).
	Value any `json:"value"`
	// Default is the built-in value; null when there is none.
	Default any `json:"default"`
	// Origin is file, builtin, not_set, or unknown (the file does not
	// load, or the member has no checkout).
	Origin string `json:"origin"`
	// Line is the key's line in the file (origin file only).
	Line *int `json:"line"`
	// Applies is at_once or next_index_run.
	Applies  string  `json:"applies"`
	Editable bool    `json:"editable"`
	Reason   *string `json:"reason"`
	// LockedBy is the key of the file that decides instead of this one
	// (ide.link_template for ide.editor), with its line; null otherwise.
	// The UI then shows that key as the repository's value in use.
	LockedBy *configLockDoc `json:"locked_by"`
}

// configLockDoc names the key that locks a form key, and its line.
type configLockDoc struct {
	Key  string `json:"key"`
	Line int    `json:"line"`
}

// configOtherDoc is a key the file sets outside the form (row ST-16).
type configOtherDoc struct {
	Key  string `json:"key"`
	Line int    `json:"line"`
	// EditedBy is the guarded edit that edits it; null when only the
	// whole-text edit does.
	EditedBy *string `json:"edited_by"`
}

// configReindexDoc is what applies a next_index_run key (row ST-18).
type configReindexDoc struct {
	Path string `json:"path"`
	// Scope is repository, or project (every member, then the link pass).
	Scope string `json:"scope"`
}

// configIgnoreDoc is the root .graphveinignore, read-only (row ST-19).
type configIgnoreDoc struct {
	File     string  `json:"file"`
	Exists   *bool   `json:"exists"`
	Text     *string `json:"text"`
	Editable bool    `json:"editable"`
}

// configProjectDoc is the manifest's try-it environments (row ST-20).
type configProjectDoc struct {
	File         string                `json:"file"`
	Error        *string               `json:"error"`
	Environments []configProjectEnvDoc `json:"environments"`
	EditedBy     string                `json:"edited_by"`
}

type configProjectEnvDoc struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Line  int    `json:"line"`
	// ForMember is true when the environment has a base URL for this
	// member, so the member's try-it offers it.
	ForMember bool               `json:"for_member"`
	BaseURLs  []configBaseURLDoc `json:"base_urls"`
}

type configBaseURLDoc struct {
	Repository string `json:"repository"`
	BaseURL    string `json:"base_url"`
}

// editedElsewhere names the guarded edits of keys outside the form.
var editedElsewhere = map[string]string{
	"explorer.path_values": explorerValuesPath,
	"tryit.environments":   tryItEnvironmentsPath,
}

func (h configHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error
	if r.Method == http.MethodPost {
		err = h.post(w, r)
	} else {
		err = h.get(w, r)
	}
	if r.Context().Err() != nil {
		cancelled(w, r.Context())
		return
	}
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		writeError(w, ae.status, ae.code, ae.message)
	case err != nil:
		logFailure(h.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
	}
}

func (h configHandler) configFile() string { return filepath.Join(h.opt.Root, config.File) }

func (h configHandler) get(w http.ResponseWriter, r *http.Request) error {
	out := configResponse{Repository: h.opt.Repository, File: config.File, Token: h.token, Keys: []configKeyDoc{}, OtherKeys: []configOtherDoc{},
		Reindex: configReindexDoc{Path: indexRunPath, Scope: "repository"}, Ignore: configIgnoreDoc{File: ignoreFile}}
	if h.opt.project != nil {
		out.Reindex.Scope = "project"
		out.Project = h.projectDoc()
	}
	runs, err := h.opt.Store.Runs(r.Context(), h.opt.Repository)
	if err != nil {
		return fmt.Errorf("reading runs: %w", err)
	}
	if h.opt.noCheckout {
		out.IndexSettings = settingsStatusFor(runs, noCheckoutSettings)
		e := h.opt.noCheckoutError("reading and editing its graphvein.yaml")
		out.Reason = &e.message
		for _, k := range config.SettingKeys() {
			out.Keys = append(out.Keys, keyDoc(k, nil, "unknown", 0, false, e.message))
		}
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	data, err := readEditFile(h.configFile())
	if err != nil {
		return err
	}
	exists, hash, text := data != nil, editHash(data), string(data)
	out.Exists, out.Hash, out.Text, out.Editable = &exists, &hash, &text, true
	ignore, err := readEditFile(filepath.Join(h.opt.Root, ignoreFile))
	if err != nil {
		return err
	}
	ignoreExists := ignore != nil
	ignoreText := string(ignore)
	out.Ignore.Exists, out.Ignore.Text = &ignoreExists, &ignoreText
	if _, perr := config.Parse(config.File, data); data != nil && perr != nil {
		msg := perr.Error()
		out.ConfigError = &msg
		out.IndexSettings = settingsStatusFor(runs, func(record string) config.IndexSettings {
			return config.UnknownIndexSettings(record, "the current "+config.File+" does not load: "+msg)
		})
		reason := config.File + " does not load; fix it in the YAML view"
		for _, k := range config.SettingKeys() {
			out.Keys = append(out.Keys, keyDoc(k, nil, "unknown", 0, false, reason))
		}
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	values, other, err := config.SettingValues(data)
	if err != nil {
		return err
	}
	inputs, err := config.IndexInputsOf(data)
	if err != nil {
		return err
	}
	out.IndexSettings = settingsStatusFor(runs, func(record string) config.IndexSettings {
		return config.CompareIndexInputs(record, inputs)
	})
	for _, v := range values {
		reason := ""
		if v.Locked != "" {
			reason = v.Locked + " is set, so " + v.Key.Key + " does not decide the links; edit the YAML"
		}
		d := keyDoc(v.Key, v.Value, v.Origin, v.Line, v.Locked == "", reason)
		if v.Locked != "" {
			d.LockedBy = &configLockDoc{Key: v.Locked, Line: v.LockedLine}
		}
		out.Keys = append(out.Keys, d)
	}
	for _, o := range other {
		d := configOtherDoc{Key: o.Key, Line: o.Line}
		if p, ok := editedElsewhere[o.Key]; ok {
			d.EditedBy = &p
		}
		out.OtherKeys = append(out.OtherKeys, d)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func keyDoc(k config.SettingKey, value any, origin string, line int, editable bool, reason string) configKeyDoc {
	d := configKeyDoc{Key: k.Key, Kind: k.Kind, Choices: k.Choices, Value: value, Default: k.Default, Origin: origin, Applies: k.Applies, Editable: editable}
	if k.Kind == config.SettingInteger {
		d.Min, d.Max = &k.Min, &k.Max
	}
	if line > 0 {
		d.Line = &line
	}
	if reason != "" {
		d.Reason = &reason
	}
	return d
}

// projectDoc reads the manifest fresh, as the try-it endpoints do.
func (h configHandler) projectDoc() *configProjectDoc {
	doc := &configProjectDoc{File: config.ProjectFile, Environments: []configProjectEnvDoc{}, EditedBy: tryItEnvironmentsPath}
	p, err := config.LoadProject(h.opt.project.opt.Project.current().Dir)
	if err != nil {
		msg := strings.ReplaceAll(err.Error(), h.opt.project.opt.Project.current().Dir+string(filepath.Separator), "")
		doc.Error = &msg
		return doc
	}
	for _, pe := range p.TryIt {
		e := configProjectEnvDoc{Name: pe.Name, Label: pe.Label, Line: pe.Line, BaseURLs: []configBaseURLDoc{}}
		_, e.ForMember = pe.For(h.opt.Repository)
		for _, b := range pe.BaseURLs {
			e.BaseURLs = append(e.BaseURLs, configBaseURLDoc{Repository: b.Repository, BaseURL: b.BaseURL.Text})
		}
		doc.Environments = append(doc.Environments, e)
	}
	return doc
}

// configEditRequest is one edit: typed operations on form keys, or the
// whole text; previewed unless apply is set with the preview's result
// hash.
type configEditRequest struct {
	BaseHash   string            `json:"base_hash"`
	Operations []configOperation `json:"operations"`
	Text       *string           `json:"text"`
	Apply      bool              `json:"apply"`
	ResultHash string            `json:"result_hash"`
}

// configOperation sets (op "set", key, value of the key's type) or
// removes (op "remove", key) one form key.
type configOperation struct {
	Op    string          `json:"op"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type configEditResponse struct {
	APIVersion int      `json:"api_version"`
	Applied    bool     `json:"applied"`
	BaseHash   string   `json:"base_hash"`
	ResultHash string   `json:"result_hash"`
	Diff       string   `json:"diff"`
	Valid      bool     `json:"valid"`
	Problems   []string `json:"problems"`
	Changes    []string `json:"changes"`
	// Reformatted is true when the typed edit could not be limited to the
	// edited lines and the whole file was re-encoded (row ST-27).
	Reformatted bool `json:"reformatted"`
	// LostComments are the comments of the file on disk the result no
	// longer has (with their line on disk), found by comparing the
	// comments of both texts: those of a removed entry, and any a
	// re-encode drops (row ST-27).
	LostComments []config.Comment `json:"lost_comments"`
	// ChangedKeys are the form keys whose effective value the edit
	// changes, with when each applies; OtherKeysChanged is true when it
	// changes anything outside the form (only possible with text).
	ChangedKeys      []configChangedDoc `json:"changed_keys"`
	OtherKeysChanged bool               `json:"other_keys_changed"`
	// ReindexNeeded is true when a changed key applies at the next index
	// run (reindex in GET says which run).
	ReindexNeeded bool `json:"reindex_needed"`
}

type configChangedDoc struct {
	Key     string `json:"key"`
	Applies string `json:"applies"`
}

func (h configHandler) post(w http.ResponseWriter, r *http.Request) error {
	if !loopbackPeer(r) {
		return &apiError{http.StatusForbidden, "not_loopback", "configuration edits are accepted only from this machine"}
	}
	got := r.Header.Values(tokenHeader)
	if len(got) != 1 || subtle.ConstantTimeCompare([]byte(got[0]), []byte(h.token)) != 1 {
		return &apiError{http.StatusForbidden, "token_required", "a configuration edit needs this server's session token in " + tokenHeader + " (from GET " + configPath + ")"}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &apiError{http.StatusUnsupportedMediaType, "unsupported_media_type", "POST " + r.URL.Path + " needs Content-Type: application/json"}
	}
	if h.opt.noCheckout {
		return h.opt.noCheckoutError("editing its graphvein.yaml")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxConfigEditBody))
	if err != nil {
		return &apiError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("a configuration edit is at most %d KiB", maxConfigEditBody>>10)}
	}
	if err := duplicateKeys(body); err != nil {
		return badRequest("invalid body: %v", err)
	}
	var req configEditRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		return badRequest("invalid body: one JSON object with base_hash, operations or text, apply and result_hash expected")
	}
	if (req.Text != nil) == (len(req.Operations) > 0) {
		return badRequest("an edit has either 1 to %d operations or text, not both", maxConfigEditOps)
	}
	if len(req.Operations) > maxConfigEditOps {
		return badRequest("an edit has 1 to %d operations", maxConfigEditOps)
	}
	if req.Text != nil && len(*req.Text) > maxManifestSize {
		return &apiError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("%s is at most %d KiB here; edit it in a text editor", config.File, maxManifestSize>>10)}
	}
	if req.Apply != (req.ResultHash != "") {
		return badRequest("apply needs result_hash (the result_hash of the preview being confirmed), and only apply takes it")
	}
	ops, err := settingOps(req.Operations)
	if err != nil {
		return err
	}

	// The lock is the path value edit's: both write graphvein.yaml (row
	// ST-30).
	explorerEditMu.Lock()
	defer explorerEditMu.Unlock()
	data, err := readEditFile(h.configFile())
	if err != nil {
		return err
	}
	base := editHash(data)
	if req.BaseHash != base {
		return &apiError{http.StatusConflict, "config_changed", config.File + " changed on disk since this edit was prepared; reload the settings and make the edit again"}
	}
	out := configEditResponse{BaseHash: base, Problems: []string{}, Changes: []string{}, ChangedKeys: []configChangedDoc{}}
	var next []byte
	_, baseErr := config.Parse(config.File, data)
	baseValid := data == nil || baseErr == nil
	if req.Text != nil {
		next = []byte(*req.Text)
		out.Changes = append(out.Changes, "replaces "+config.File+" with the text given")
	} else {
		if !baseValid {
			return &apiError{http.StatusConflict, "invalid_config", config.File + " does not load, so its keys cannot be edited one by one; fix it in the YAML view: " + baseErr.Error()}
		}
		next, out.Reformatted, err = config.EditSettings(data, ops)
		if err != nil {
			return &apiError{http.StatusBadRequest, "edit_refused", err.Error()}
		}
		words, err := config.SettingChangeWords(data, ops)
		if err != nil {
			return &apiError{http.StatusBadRequest, "edit_refused", err.Error()}
		}
		out.Changes = append(out.Changes, words...)
		if out.Reformatted {
			out.Changes = append(out.Changes, "re-indents "+config.File+" with two spaces: the edit could not be limited to the edited lines")
			out.Changes = append(out.Changes, config.ReencodeWords(data, next)...)
		}
	}
	out.ResultHash, out.Diff = editHash(next), unifiedDiff(config.File, string(data), string(next))
	out.LostComments = config.LostComments(data, next)
	for _, c := range out.LostComments {
		out.Changes = append(out.Changes, fmt.Sprintf("drops the comment %s (line %d)", c.Text, c.Line))
	}
	_, verr := config.Parse(config.File, next)
	out.Valid = verr == nil
	if verr != nil {
		out.Problems = append(out.Problems, verr.Error())
	}
	if out.Valid && baseValid {
		keys, other, cerr := config.ChangedSettings(data, next)
		if cerr != nil {
			return cerr
		}
		for _, k := range keys {
			out.ChangedKeys = append(out.ChangedKeys, configChangedDoc{Key: k.Key, Applies: k.Applies})
			out.ReindexNeeded = out.ReindexNeeded || k.Applies == config.AppliesNextIndexRun
		}
		out.OtherKeysChanged = other
	} else if out.Valid {
		// The base did not load, so no key had an effective value: every
		// form key is listed as changed (whatever the result sets, a
		// built-in default included) and nothing is known about the rest
		// (row ST-28).
		out.OtherKeysChanged = true
		out.Changes = append(out.Changes, "the file did not load before; every key is treated as changed")
		values, _, _ := config.SettingValues(next)
		for _, v := range values {
			out.ChangedKeys = append(out.ChangedKeys, configChangedDoc{Key: v.Key.Key, Applies: v.Key.Applies})
			out.ReindexNeeded = out.ReindexNeeded || v.Key.Applies == config.AppliesNextIndexRun
		}
	}
	if !req.Apply {
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	switch {
	case req.ResultHash != out.ResultHash:
		return &apiError{http.StatusConflict, "preview_changed", "the edit no longer gives the previewed file; preview it again"}
	case !out.Valid:
		return &apiError{http.StatusUnprocessableEntity, "invalid_config", "the edited " + config.File + " does not validate, so nothing was written: " + out.Problems[0]}
	case out.Diff == "" && data != nil:
		return &apiError{http.StatusBadRequest, "no_change", "the edit changes nothing"}
	}
	if err := config.WriteFileAtomic(h.configFile(), next); err != nil {
		return err
	}
	out.Applied = true
	h.opt.logger().Info("graphvein.yaml edited from Settings", "repository", h.opt.Repository, "operations", len(ops), "text", req.Text != nil)
	writeJSON(w, http.StatusOK, out)
	return nil
}

// settingOps checks each operation against the key table and decodes its
// value strictly to the key's type (row ST-26); anything else is refused.
func settingOps(in []configOperation) ([]config.SettingOp, error) {
	var out []config.SettingOp
	for i, op := range in {
		k, ok := config.SettingKeyByName(op.Key)
		if !ok {
			return nil, &apiError{http.StatusBadRequest, "unknown_key", fmt.Sprintf("operation %d: %q is not a key the Settings form edits; edit it in the YAML view", i+1, op.Key)}
		}
		switch {
		case op.Op == "remove" && op.Value == nil:
			out = append(out, config.SettingOp{Key: k.Key, Remove: true})
		case op.Op == "set" && op.Value != nil:
			v, err := settingValue(k, op.Value)
			if err != nil {
				return nil, &apiError{http.StatusBadRequest, "invalid_value", fmt.Sprintf("operation %d: %s takes %s", i+1, k.Key, err)}
			}
			out = append(out, config.SettingOp{Key: k.Key, Value: v})
		default:
			return nil, badRequest("operation %d: an operation is {op: set, key, value} or {op: remove, key}", i+1)
		}
	}
	return out, nil
}

// settingValue decodes raw as the JSON type of k's kind.
func settingValue(k config.SettingKey, raw json.RawMessage) (any, error) {
	strict := func(v any) error {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil || dec.More() {
			return errors.New("bad")
		}
		return nil
	}
	switch k.Kind {
	case config.SettingEnum:
		var s string
		if strict(&s) != nil {
			return nil, errors.New("a string")
		}
		return s, nil
	case config.SettingInteger:
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) != nil {
			return nil, errors.New("a whole number")
		}
		n, ok := v.(json.Number)
		if !ok {
			return nil, errors.New("a whole number")
		}
		i, err := n.Int64()
		if err != nil || int64(int(i)) != i {
			return nil, errors.New("a whole number")
		}
		return int(i), nil
	case config.SettingBoolean:
		var b bool
		if strict(&b) != nil {
			return nil, errors.New("true or false")
		}
		return b, nil
	case config.SettingList:
		var l []string
		if strict(&l) != nil || l == nil {
			return nil, errors.New("a list of strings")
		}
		return l, nil
	case config.SettingMap:
		var m []config.MapEntry
		if strict(&m) != nil || m == nil {
			return nil, errors.New("a list of {key, value} strings")
		}
		return m, nil
	}
	return nil, fmt.Errorf("an unknown kind %s", k.Kind)
}
