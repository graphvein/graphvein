package server

import (
	"bytes"
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
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/openapi"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds the generated OpenAPI document (T029, ADR 0081,
// docs/contract/claims/openapi-generation.md) and the guarded edit of the
// explorer's user-supplied path values in graphvein.yaml. The document is
// the same bytes graphvein openapi writes; the edit follows the project
// manifest edit (ADR 0075): a typed operation, a diff preview, a hash of
// the file the edit was based on, a session token, a loopback peer and an
// atomic write. The values change only the explorer, try-it and the
// generated document, never the index.

// Paths of the OpenAPI endpoints.
const (
	openapiPath         = "/api/v0/openapi"
	explorerValuesPath  = "/api/v0/explorer/path-values"
	maxExplorerEditBody = 64 << 10
	maxExplorerEditOps  = 64
)

// openapiHandler answers GET /api/v0/openapi?format=json|yaml.
type openapiHandler struct{ opt Options }

func (h openapiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	format, _, err := param(r.URL.Query(), "format")
	if err == nil && format != "" && format != openapi.FormatJSON && format != openapi.FormatYAML {
		err = badRequest("format must be json or yaml")
	}
	var data []byte
	if err == nil {
		var doc *openapi.Document
		if doc, err = generateDocument(ctx, h.opt); err == nil {
			data, err = openapi.Encode(doc, format)
		}
	}
	if ctx.Err() != nil {
		cancelled(w, ctx)
		return
	}
	var ae *apiError
	switch {
	case err == nil:
		contentType := "application/json; charset=utf-8"
		if format == openapi.FormatYAML {
			contentType = "application/yaml; charset=utf-8"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	case errors.As(err, &ae):
		writeError(w, ae.status, ae.code, ae.message)
	case errors.Is(err, errIndexChanged):
		writeError(w, http.StatusServiceUnavailable, "index_changed", err.Error())
	default:
		logFailure(h.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
	}
}

// generateDocument reads the repository's routes and graphvein.yaml and
// builds the document. The read is repeated once when an index run
// commits in between, as every API read is (apiHandler.consistent).
func generateDocument(ctx context.Context, opt Options) (*openapi.Document, error) {
	cfg, err := opt.loadConfig()
	if err != nil {
		return nil, &apiError{http.StatusConflict, "invalid_config", "graphvein.yaml does not load, so the explorer's path values are unknown: " + err.Error()}
	}
	for attempt := 0; attempt < 2; attempt++ {
		before, err := opt.Store.Runs(ctx, opt.Repository)
		if err != nil {
			return nil, fmt.Errorf("reading runs: %w", err)
		}
		in, err := openapi.Read(ctx, opt.Store, opt.Repository, cfg.Explorer.PathValues)
		if err != nil {
			return nil, err
		}
		var projectEnvs []config.ProjectEnvironment
		if opt.project != nil {
			projectEnvs = opt.project.project.TryIt
		}
		in.Servers = openapi.Servers(projectEnvs, cfg.TryIt.Environments, opt.Repository)
		after, err := opt.Store.Runs(ctx, opt.Repository)
		if err != nil {
			return nil, fmt.Errorf("reading runs: %w", err)
		}
		if sameRuns(before, after) {
			return openapi.Generate(in), nil
		}
	}
	return nil, errIndexChanged
}

// openapiMembersHandler answers GET /api/v0/openapi on a project server:
// with a repository parameter (or one member) the member's document, else
// the list of members, each with its document's URL (one document per
// member, row OG-61).
type openapiMembersHandler struct {
	ps       *projectState
	dispatch http.Handler
}

type openapiMembersResponse struct {
	APIVersion int                 `json:"api_version"`
	Project    string              `json:"project"`
	Members    []openapiMemberLink `json:"members"`
}

type openapiMemberLink struct {
	Repository string `json:"repository"`
	Document   string `json:"document"`
	Checkout   bool   `json:"checkout"`
}

func (h openapiMembersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, present := r.URL.Query()["repository"]; present || len(h.ps.project.Members) == 1 {
		h.dispatch.ServeHTTP(w, r)
		return
	}
	out := openapiMembersResponse{Project: h.ps.project.Name, Members: []openapiMemberLink{}}
	for _, m := range h.ps.project.Members {
		out.Members = append(out.Members, openapiMemberLink{Repository: m.Repository, Document: openapiPath + "?repository=" + url.QueryEscape(m.Repository), Checkout: m.Checkout})
	}
	writeJSON(w, http.StatusOK, out)
}

// explorerEditMu serialises path value edits (read, check the base,
// write) across every handler of this process: a project server builds
// one handler per member and per manifest reload. Every edit that writes
// a graphvein.yaml holds it: the Settings edit and the try-it environment
// edit of a repository file too (ADR 0088), so no two of them interleave.
var explorerEditMu sync.Mutex

// explorerValuesHandler answers GET and POST /api/v0/explorer/path-values.
type explorerValuesHandler struct {
	opt   Options
	token string
}

func newExplorerValuesHandler(opt Options) explorerValuesHandler {
	return explorerValuesHandler{opt: opt, token: rand.Text()}
}

type explorerValuesResponse struct {
	APIVersion int `json:"api_version"`
	// File is the configuration file's name; never a path to choose.
	File string `json:"file"`
	// Exists is false when the repository has no graphvein.yaml: an edit
	// then creates it (version 1, nothing else but the values).
	Exists bool   `json:"exists"`
	Hash   string `json:"hash"`
	// Editable is false for a project member without a checkout here;
	// Reason says why.
	Editable bool    `json:"editable"`
	Reason   *string `json:"reason"`
	// Token goes in the X-Graphvein-Token header of every edit.
	Token string `json:"token"`
	// Values are the effective explorer.path_values with their use in
	// the generated document (row OG-93).
	Values []openapi.UserValue `json:"values"`
	// Unresolved are the routes a value could place (row OG-45), with the
	// keys that would.
	Unresolved []openapi.Unresolved `json:"unresolved"`
	// Mounts are the unresolved mount hops a mount key can supply a
	// prefix for, with the number of routes below each (rows OG-94,
	// OG-95).
	Mounts []openapi.Mount `json:"mounts"`
}

func (h explorerValuesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	case errors.Is(err, errIndexChanged):
		writeError(w, http.StatusServiceUnavailable, "index_changed", err.Error())
	case err != nil:
		logFailure(h.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "explorer_error", err.Error())
	}
}

// configFile is the one file an edit may write.
func (h explorerValuesHandler) configFile() string {
	return filepath.Join(h.opt.Root, config.File)
}

// readConfig reads graphvein.yaml as it is on disk now; a missing file is
// empty text.
func (h explorerValuesHandler) readConfig() ([]byte, bool, error) {
	data, err := readBounded(h.configFile())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (h explorerValuesHandler) get(w http.ResponseWriter, r *http.Request) error {
	out := explorerValuesResponse{File: config.File, Token: h.token, Values: []openapi.UserValue{}, Unresolved: []openapi.Unresolved{}, Mounts: []openapi.Mount{}}
	if h.opt.noCheckout {
		e := h.opt.noCheckoutError("editing path values in graphvein.yaml")
		out.Reason = &e.message
	} else {
		data, exists, err := h.readConfig()
		if err != nil {
			return err
		}
		out.Exists, out.Hash, out.Editable = exists, manifestHash(data), true
	}
	doc, err := generateDocument(r.Context(), h.opt)
	if err != nil {
		return err
	}
	out.Values, out.Unresolved = doc.X.UserValues, doc.X.Unresolved
	if doc.Mounts != nil {
		out.Mounts = doc.Mounts
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// explorerEditRequest is one edit: typed operations on the path values,
// previewed unless apply is set with the preview's result hash.
type explorerEditRequest struct {
	BaseHash   string              `json:"base_hash"`
	Operations []explorerOperation `json:"operations"`
	Apply      bool                `json:"apply"`
	ResultHash string              `json:"result_hash"`
}

// explorerOperation sets (op "set", key and value) or removes (op
// "remove", key) one path value.
type explorerOperation struct {
	Op    string  `json:"op"`
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

type explorerEditResponse struct {
	APIVersion int      `json:"api_version"`
	Applied    bool     `json:"applied"`
	BaseHash   string   `json:"base_hash"`
	ResultHash string   `json:"result_hash"`
	Diff       string   `json:"diff"`
	Valid      bool     `json:"valid"`
	Problems   []string `json:"problems"`
	Changes    []string `json:"changes"`
}

func (h explorerValuesHandler) post(w http.ResponseWriter, r *http.Request) error {
	if !loopbackPeer(r) {
		return &apiError{http.StatusForbidden, "not_loopback", "path value edits are accepted only from this machine"}
	}
	got := r.Header.Values(tokenHeader)
	if len(got) != 1 || subtle.ConstantTimeCompare([]byte(got[0]), []byte(h.token)) != 1 {
		return &apiError{http.StatusForbidden, "token_required", "a path value edit needs this server's session token in " + tokenHeader + " (from GET " + explorerValuesPath + ")"}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &apiError{http.StatusUnsupportedMediaType, "unsupported_media_type", "POST " + r.URL.Path + " needs Content-Type: application/json"}
	}
	if h.opt.noCheckout {
		return h.opt.noCheckoutError("editing path values in graphvein.yaml")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxExplorerEditBody))
	if err != nil {
		return &apiError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("a path value edit is at most %d KiB", maxExplorerEditBody>>10)}
	}
	if err := duplicateKeys(body); err != nil {
		return badRequest("invalid body: %v", err)
	}
	var req explorerEditRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		return badRequest("invalid body: one JSON object with base_hash, operations, apply and result_hash expected")
	}
	if len(req.Operations) == 0 || len(req.Operations) > maxExplorerEditOps {
		return badRequest("an edit has 1 to %d operations", maxExplorerEditOps)
	}
	if req.Apply != (req.ResultHash != "") {
		return badRequest("apply needs result_hash (the result_hash of the preview being confirmed), and only apply takes it")
	}
	for i, op := range req.Operations {
		switch {
		case op.Op == "set" && op.Value != nil:
			if err := config.CheckPathValue(op.Key, *op.Value); err != nil {
				return &apiError{http.StatusBadRequest, "invalid_value", fmt.Sprintf("operation %d: %v", i+1, err)}
			}
		case op.Op == "remove" && op.Value == nil && op.Key != "":
		default:
			return badRequest("operation %d: an operation is {op: set, key, value} or {op: remove, key}", i+1)
		}
	}

	explorerEditMu.Lock()
	defer explorerEditMu.Unlock()
	data, _, err := h.readConfig()
	if err != nil {
		return err
	}
	base := manifestHash(data)
	if req.BaseHash != base {
		return &apiError{http.StatusConflict, "config_changed", "graphvein.yaml changed on disk since this edit was prepared; reload the values and make the edit again"}
	}
	out := explorerEditResponse{BaseHash: base, Problems: []string{}, Changes: []string{}}
	next := data
	for i, op := range req.Operations {
		var change string
		if op.Op == "set" {
			registration, rerr := h.registration(r.Context(), op.Key, *op.Value)
			if rerr != nil {
				return rerr
			}
			next, err = config.SetExplorerPathValue(next, op.Key, *op.Value, registration)
			change = fmt.Sprintf("sets the path value of %s to %q (shown as set by you)", op.Key, *op.Value)
			switch {
			case registration != "" && strings.HasPrefix(op.Key, config.PathValueMountPrefix):
				change += "; records the hop's registration " + registration + " so the value is not used if the hop changes"
			case registration != "":
				change += "; records the registration " + registration + " because the key ends with a declaration-order ordinal"
			}
		} else {
			next, err = config.RemoveExplorerPathValue(next, op.Key)
			change = "removes the path value of " + op.Key
		}
		if err != nil {
			return &apiError{http.StatusBadRequest, "edit_refused", fmt.Sprintf("operation %d (%s): %v", i+1, op.Op, err)}
		}
		out.Changes = append(out.Changes, change)
	}
	out.ResultHash, out.Diff = manifestHash(next), unifiedDiff(config.File, string(data), string(next))
	_, verr := config.Parse(config.File, next)
	out.Valid = verr == nil
	if verr != nil {
		out.Problems = append(out.Problems, verr.Error())
	}
	if !req.Apply {
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	switch {
	case req.ResultHash != out.ResultHash:
		return &apiError{http.StatusConflict, "preview_changed", "the edit no longer gives the previewed file; preview it again"}
	case !out.Valid:
		return &apiError{http.StatusUnprocessableEntity, "invalid_config", "the edited graphvein.yaml does not validate, so nothing was written: " + out.Problems[0]}
	case out.Diff == "":
		return &apiError{http.StatusBadRequest, "no_change", "the edit changes nothing"}
	}
	if err := config.WriteFileAtomic(h.configFile(), next); err != nil {
		return err
	}
	out.Applied = true
	h.opt.logger().Info("explorer path values edited from the UI", "repository", h.opt.Repository, "operations", len(req.Operations))
	writeJSON(w, http.StatusOK, out)
	return nil
}

// registration is the stored content hash of the route a route key names
// (row OG-90), recorded with its value so the value is not used for
// another registration when identities move (a registration with the same
// method and pattern added or removed above it in the file). Other keys
// record none. A route key no stored route has, or whose registration has
// a byte-identical twin in the same file (one hash for both), is refused:
// there is nothing to tie the value to.
func (h explorerValuesHandler) registration(ctx context.Context, key, value string) (string, error) {
	if strings.HasPrefix(key, config.PathValueMountPrefix) {
		return h.mountRegistration(ctx, key, value)
	}
	if !strings.HasPrefix(key, config.PathValueRoutePrefix) {
		return "", nil
	}
	nodes, err := h.opt.Store.NodesByIdentity(ctx, h.opt.Repository, []string{key})
	if err != nil {
		return "", fmt.Errorf("reading route %s: %w", key, err)
	}
	if len(nodes) != 1 || nodes[0].Kind != "route" {
		return "", &apiError{http.StatusBadRequest, "invalid_value", "no stored route has the identity " + key + ", so its registration cannot be recorded"}
	}
	same, err := h.opt.Store.Nodes(ctx, h.opt.Repository, store.Query{Kind: "route", File: nodes[0].File})
	if err != nil {
		return "", fmt.Errorf("reading routes of %s: %w", nodes[0].File, err)
	}
	for _, n := range same {
		if n.Identity != key && n.ContentHash == nodes[0].ContentHash {
			return "", &apiError{http.StatusBadRequest, "invalid_value", "route " + key + " has a registration with the same text in " + nodes[0].File + " (" + n.Identity + "), so a value cannot be tied to it"}
		}
	}
	return nodes[0].ContentHash, nil
}

// mountRegistration is the stored hash of the mount hop a mount key names
// (row OG-94), read from the hops of the routes without a path. A key no
// stored route has, a hop with a byte-identical twin in its function, or
// a value other than "/" for a hop that adds no unread part (OG-95), is
// refused.
func (h explorerValuesHandler) mountRegistration(ctx context.Context, key, value string) (string, error) {
	doc, err := generateDocument(ctx, h.opt)
	if err != nil {
		return "", err
	}
	for _, m := range doc.Mounts {
		if m.Key != key {
			continue
		}
		if m.Ambiguous {
			return "", &apiError{http.StatusBadRequest, "invalid_value", "the hop " + key + " has a twin with the same text in the same function, so a value cannot be tied to it"}
		}
		if m.Covers == openapi.MountCoversNothing && value != "/" {
			return "", &apiError{http.StatusBadRequest, "invalid_value", "the hop " + key + " adds no prefix Graphvein could not read: the paths below it are the served paths, so the only value is / (it passes them on)"}
		}
		return m.Registration, nil
	}
	return "", &apiError{http.StatusBadRequest, "invalid_value", "no stored route without a path is below the hop " + key + ", so its registration cannot be recorded"}
}
