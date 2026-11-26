package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/egress"
)

// This file holds the "try it" proxy (T029, ADR 0081, claim rows TI-* in
// docs/contract/claims/try-it.md). The browser never sends a URL: it names
// an environment, an operation (a stored route and one of its path keys)
// and values; the server builds the request, applies the guardrails and
// sends it through internal/egress with a policy that holds only the
// environment's origin. Session state (selected environment, production
// acknowledgements, "don't ask again" grants, typed auth values and
// pending confirmations) lives only in this process's memory.

// Paths of the try-it endpoints.
const (
	tryItEnvironmentsPath = "/api/v0/tryit/environments"
	tryItSuggestionsPath  = "/api/v0/tryit/suggestions"
	tryItSelectPath       = "/api/v0/tryit/select"
	tryItAuthPath         = "/api/v0/tryit/auth"
	tryItPreparePath      = "/api/v0/tryit/prepare"
	tryItSendPath         = "/api/v0/tryit/send"
)

// Limits of a try-it request (rows TI-3, TI-15, TI-16).
const (
	// maxTryItRequest bounds the JSON a browser posts to a try-it
	// endpoint other than prepare and send.
	maxTryItRequest = 2 << 20
	// maxTryItSendRequest bounds the JSON posted to prepare and send: a
	// body or file parts up to the request_bytes ceiling (64 MiB) as
	// base64, with room for the rest.
	maxTryItSendRequest = 96 << 20
	// maxTryItBody bounds the request body sent upstream unless the
	// environment raises it (limits.request_bytes).
	maxTryItBody = config.DefaultRequestBytes
	// maxTryItResponse bounds the response body read unless the
	// environment raises it; the rest is dropped and the answer marked
	// truncated.
	maxTryItResponse = config.DefaultResponseBytes
	// tryItTimeout bounds one upstream exchange, body included, unless the
	// environment raises it.
	tryItTimeout = config.DefaultTimeoutSeconds * time.Second
	// maxCAFile bounds a tls.ca_file read.
	maxCAFile = 4 << 20
	// maxTryItParts bounds the parts of a multipart body.
	maxTryItParts = 128
	// maxTryItInFlight bounds concurrent upstream sends per process.
	maxTryItInFlight = 4
	// confirmationTTL is how long a confirmation id stays valid.
	confirmationTTL = 2 * time.Minute
	// maxConfirmations bounds the pending confirmations kept.
	maxConfirmations = 64
	// maxTryItHeaders and maxTryItQuery bound the pairs of one request.
	maxTryItHeaders = 64
	maxTryItQuery   = 128
	// maxTryItValue bounds one header, query or path value, and a typed
	// auth value.
	maxTryItValue = 8 << 10
	// hexPreview is how much of a non-text response body is shown.
	hexPreview = 4 << 10
)

// Environment sources and their id prefixes.
const (
	sourceRepository = "repository"
	sourceProject    = "project"
)

// tryItState is shared by every try-it request of one server process.
type tryItState struct {
	opt   Options
	token string
	// editMu serialises environment edits; in project mode it is the
	// manifest edit lock, so a try-it edit and a manifest edit never
	// interleave.
	editMu *sync.Mutex
	// inFlight is a counting semaphore of upstream sends (TI-16).
	inFlight chan struct{}
	// getenv reads the server's environment; tests replace it.
	getenv func(string) (string, bool)
	// timeout and maxResponse are tryItTimeout and maxTryItResponse, the
	// defaults where an environment sets no limit; maxSendPost is
	// maxTryItSendRequest. Tests shorten them.
	timeout     time.Duration
	maxResponse int64
	maxSendPost int64
	// proxyRoots verify an https proxy; nil is the system roots. Tests
	// set it to trust a proxy they start (never an environment's pins).
	proxyRoots *x509.CertPool

	mu sync.Mutex
	// selected is the key of the environment the UI selected last, and
	// selectedBase its base (name, source and member): when the same base
	// later resolves to another key (its origin, base path, label or auth
	// changed in a file), everything held for it is dropped, as on a
	// switch.
	selected     string
	selectedBase string
	// acknowledged holds production environments acknowledged since they
	// were selected; granted holds environments where the user chose
	// "don't ask again until I switch". Both are cleared on a switch.
	acknowledged map[string]bool
	granted      map[string]bool
	// secrets are auth values typed in the UI, by environment base, each
	// bound to the key it was typed for. Never written, logged or
	// returned.
	secrets map[string]sessionSecret
	// secretGen numbers every typed value set, so a confirmation is bound
	// to the exact value in use.
	secretGen int
	confirms  map[string]confirmation
}

type sessionSecret struct {
	key, header, value string
	gen                int
}

type confirmation struct {
	hash    string
	key     string
	expires time.Time
}

// tryItTestHook, when set by a test, adjusts every new try-it state
// (shorter limits, a filled semaphore).
var tryItTestHook func(*tryItState)

func newTryItState(opt Options, editMu *sync.Mutex) *tryItState {
	if editMu == nil {
		editMu = new(sync.Mutex)
	}
	t := &tryItState{
		opt: opt, token: rand.Text(), editMu: editMu,
		inFlight: make(chan struct{}, maxTryItInFlight),
		getenv:   os.LookupEnv, timeout: tryItTimeout, maxResponse: maxTryItResponse, maxSendPost: maxTryItSendRequest,
		acknowledged: map[string]bool{}, granted: map[string]bool{},
		secrets: map[string]sessionSecret{}, confirms: map[string]confirmation{},
	}
	if tryItTestHook != nil {
		tryItTestHook(t)
	}
	return t
}

// handler returns the try-it endpoint at path.
func (t *tryItState) handler(path string) (http.Handler, bool) {
	switch path {
	case tryItEnvironmentsPath:
		return tryItHandler{t, t.environmentsEndpoint}, true
	case tryItSuggestionsPath:
		return tryItHandler{t, t.suggestionsEndpoint}, true
	case tryItSelectPath:
		return tryItHandler{t, t.selectEndpoint}, true
	case tryItAuthPath:
		return tryItHandler{t, t.authEndpoint}, true
	case tryItPreparePath:
		return tryItHandler{t, t.prepareEndpoint}, true
	case tryItSendPath:
		return tryItHandler{t, t.sendEndpoint}, true
	}
	return nil, false
}

type tryItHandler struct {
	t     *tryItState
	serve func(w http.ResponseWriter, r *http.Request) error
}

func (h tryItHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if postPaths[r.URL.Path] && !getAlso[r.URL.Path] && r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", methodText(r, "POST"))
		return
	}
	err := h.serve(w, r)
	var ae *apiError
	switch {
	case err == nil:
	case r.Context().Err() != nil:
		cancelled(w, r.Context())
	case errors.As(err, &ae):
		// Only the code is logged: messages may name values.
		h.t.opt.logger().Info("try-it request refused", "path", r.URL.Path, "reason", ae.code)
		var de *detailedError
		if errors.As(err, &de) {
			writeJSON(w, ae.status, errorEnvelope{Error: errorBody{Code: ae.code, Message: ae.message, Details: de.details}})
			return
		}
		writeError(w, ae.status, ae.code, ae.message)
	default:
		logFailure(h.t.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "tryit_error", err.Error())
	}
}

// detailedError is a refusal whose answer carries structured details
// (errorBody.Details) the UI acts on.
type detailedError struct {
	*apiError
	details any
}

func (e *detailedError) Unwrap() error { return e.apiError }

// checkPost applies the checks of a state-changing try-it request beyond
// the ones every POST gets (Host, Sec-Fetch-Site, Origin,
// CrossOriginProtection): a loopback peer, this process's session token
// and a JSON body (row TI-2), at most maxTryItRequest bytes (TI-3). It
// decodes the body strictly into v.
func (t *tryItState) checkPost(w http.ResponseWriter, r *http.Request, v any) error {
	return t.checkPostLimit(w, r, v, maxTryItRequest)
}

// checkPostLimit is checkPost with another bound on the JSON posted:
// prepare and send carry bodies and file parts up to an environment's
// raised request limit (base64 in JSON).
func (t *tryItState) checkPostLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	if !loopbackPeer(r) {
		return &apiError{http.StatusForbidden, "not_loopback", "try-it requests are accepted only from this machine"}
	}
	got := r.Header.Values(tokenHeader)
	if len(got) != 1 || subtle.ConstantTimeCompare([]byte(got[0]), []byte(t.token)) != 1 {
		return &apiError{http.StatusForbidden, "token_required", "a try-it request needs this server's session token in " + tokenHeader + " (from GET " + tryItEnvironmentsPath + ")"}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &apiError{http.StatusUnsupportedMediaType, "unsupported_media_type", "POST " + r.URL.Path + " needs Content-Type: application/json"}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return &apiError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("a try-it request is at most %d MiB", limit>>20)}
	}
	if err := duplicateKeys(body); err != nil {
		return badRequest("invalid body: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		return badRequest("invalid body: one JSON object of the documented fields expected (%v)", err)
	}
	return nil
}

// tryEnv is one environment as a request resolves it for one repository.
type tryEnv struct {
	// ID is "repository:<name>" or "project:<name>".
	ID         string
	Name       string
	Label      string
	Source     string
	File       string
	Line       int
	Repository string
	Base       config.BaseURL
	Auth       *config.Auth
	// Options are the environment's TLS trust, proxy use and limits; Dir
	// is the directory a relative tls.ca_file is read from (the
	// repository root, or the project directory for the manifest).
	Options config.EnvironmentOptions
	Dir     string
	// caPEM, caHash and caErr are the tls.ca_file as loadCA read it.
	caPEM  []byte
	caHash string
	caErr  error
}

// limits are the environment's effective limits: its own, else the
// state's defaults.
func (t *tryItState) limits(e tryEnv) (requestBytes, responseBytes int64, timeout time.Duration) {
	l := e.Options.Limits
	requestBytes, responseBytes, timeout = maxTryItBody, t.maxResponse, t.timeout
	if l.RequestBytes != 0 {
		requestBytes = l.RequestBytes
	}
	if l.ResponseBytes != 0 {
		responseBytes = l.ResponseBytes
	}
	if l.TimeoutSeconds != 0 {
		timeout = time.Duration(l.TimeoutSeconds) * time.Second
	}
	return requestBytes, responseBytes, timeout
}

// base names the environment for one member: its source, name and the
// member. Session state is per member, a project environment included.
func (e tryEnv) base() string {
	return e.ID + "\x00" + e.Repository
}

// key is the environment's identity for session state: its base plus
// everything a grant, an acknowledgement or a typed value was given for
// (origin, base path, label, auth configuration, and the trust and route
// to the destination: certificate pins, CA file, system proxy). A file
// edit that changes any of them makes another key, so nothing held for
// the old one applies to the new destination. Limits are not
// part of it: they do not change where a request goes.
func (e tryEnv) key() string {
	auth := "-"
	if e.Auth != nil {
		auth = fmt.Sprintf("%q %q %q", e.Auth.Header, e.Auth.Env, e.Auth.Prefix)
	}
	trust := "-"
	if t := e.Options.TLS; t != nil {
		trust = fmt.Sprintf("%q %q %q", strings.Join(t.PinsFor(e.Base.Origin()), ","), t.CAFile, e.caHash)
	}
	return fmt.Sprintf("%s\x00%s\x00%q\x00%q\x00%s\x00%s\x00%t", e.base(), e.Base.Origin(), e.Base.Path, e.Label, auth, trust, e.Options.UseSystemProxy)
}

// reconcile drops session state held for an older form of e; the caller
// holds mu. A selected environment whose key changed is unselected (with
// every acknowledgement, grant and confirmation, as on a switch), and a
// typed value for another key is forgotten.
func (t *tryItState) reconcile(e tryEnv) {
	if t.selectedBase == e.base() && t.selected != e.key() {
		t.switchTo("", "")
		t.opt.logger().Info("try-it environment changed in its file; selection, acknowledgement and grants dropped", "environment", e.ID, "repository", e.Repository)
	}
	if sec, ok := t.secrets[e.base()]; ok && sec.key != e.key() {
		delete(t.secrets, e.base())
		t.opt.logger().Info("try-it environment changed in its file; typed auth value dropped", "environment", e.ID, "repository", e.Repository)
	}
}

func (e tryEnv) production() bool { return config.Production(e.Label) }

// tryScope is the repository a request is about, with its options and
// environments.
type tryScope struct {
	opt  Options
	envs []tryEnv
	// project is the manifest as read for this request (project mode).
	project *config.Project
	// configErr is why the repository's graphvein.yaml could not be read;
	// its environments are then absent, never guessed.
	configErr error
}

// scope resolves the repository of a request: the server's repository, or
// in project mode the named member (or the only one), and reads its
// environments fresh from disk, so an edit by hand applies at once.
func (t *tryItState) scope(repository string) (tryScope, error) {
	if t.opt.Project == nil {
		if repository != "" && repository != t.opt.Repository {
			return tryScope{}, badRequest("this server serves repository %s, not %s", t.opt.Repository, repository)
		}
		s := tryScope{opt: t.opt}
		cfg, err := t.opt.loadConfig()
		if err != nil {
			return tryScope{}, &apiError{http.StatusConflict, "invalid_config", "graphvein.yaml does not validate, so no environment is available: " + err.Error()}
		}
		s.envs = repositoryEnvs(cfg, t.opt.Repository, t.opt.Root)
		s.loadCAs()
		return s, nil
	}
	dir := t.opt.Project.current().Dir
	p, err := config.LoadProject(dir)
	if err != nil {
		return tryScope{}, &apiError{http.StatusConflict, "invalid_manifest", "graphvein-project.yaml does not validate, so no environment is available: " + err.Error()}
	}
	var member *config.Member
	var names []string
	for i := range p.Members {
		names = append(names, p.Members[i].Repository)
		if p.Members[i].Repository == repository {
			member = &p.Members[i]
		}
	}
	switch {
	case repository == "" && len(p.Members) == 1:
		member = &p.Members[0]
	case repository == "":
		return tryScope{}, &apiError{http.StatusBadRequest, "repository_required", fmt.Sprintf("this server serves project %s with members %s; name one in repository", p.Name, strings.Join(names, ", "))}
	case member == nil:
		return tryScope{}, badRequest("repository %q is not a member of project %s (members: %s)", repository, p.Name, strings.Join(names, ", "))
	}
	mo := t.opt
	mo.Project, mo.Repository, mo.Root, mo.noCheckout = nil, member.Repository, "", !member.Checkout
	if member.Checkout {
		mo.Root = member.Path
	}
	s := tryScope{opt: mo, project: &p}
	for _, pe := range p.TryIt {
		if b, ok := pe.For(member.Repository); ok {
			s.envs = append(s.envs, tryEnv{ID: sourceProject + ":" + pe.Name, Name: pe.Name, Label: pe.Label, Source: sourceProject,
				File: config.ProjectFile, Line: pe.Line, Repository: member.Repository, Base: b, Auth: pe.Auth, Options: pe.Options, Dir: p.Dir})
		}
	}
	if member.Checkout {
		cfg, err := config.Load(member.Path)
		if err != nil {
			s.configErr = err
		} else {
			s.envs = append(s.envs, repositoryEnvs(cfg, member.Repository, member.Path)...)
		}
	}
	s.loadCAs()
	return s, nil
}

func repositoryEnvs(cfg config.Config, repository, dir string) []tryEnv {
	var out []tryEnv
	for _, e := range cfg.TryIt.Environments {
		out = append(out, tryEnv{ID: sourceRepository + ":" + e.Name, Name: e.Name, Label: e.Label, Source: sourceRepository,
			File: "graphvein.yaml", Line: e.Line, Repository: repository, Base: e.BaseURL, Auth: e.Auth, Options: e.Options, Dir: dir})
	}
	return out
}

// loadCAs reads every environment's CA file (row TI-42).
func (s tryScope) loadCAs() {
	for i := range s.envs {
		loadCA(&s.envs[i])
	}
}

// env finds the environment id in s (row TI-4).
func (s tryScope) env(id string) (tryEnv, error) {
	for _, e := range s.envs {
		if e.ID == id {
			return e, nil
		}
	}
	var ids []string
	for _, e := range s.envs {
		ids = append(ids, e.ID)
	}
	if len(ids) == 0 {
		return tryEnv{}, &apiError{http.StatusBadRequest, "unknown_environment", fmt.Sprintf("no environment %q: %s has no try-it environment configured", id, s.opt.Repository)}
	}
	return tryEnv{}, &apiError{http.StatusBadRequest, "unknown_environment", fmt.Sprintf("no environment %q for %s (environments: %s)", id, s.opt.Repository, strings.Join(ids, ", "))}
}

// GET and POST /api/v0/tryit/environments

type environmentsResponse struct {
	APIVersion int    `json:"api_version"`
	Repository string `json:"repository"`
	// Token goes in the X-Graphvein-Token header of every try-it POST.
	Token string `json:"token"`
	// Selected is the id of the environment selected for this repository,
	// or null.
	Selected     *string          `json:"selected"`
	Environments []environmentDoc `json:"environments"`
	// Files are the files an added environment can be written to, with the
	// hash an edit is based on.
	Files []editFileDoc `json:"files"`
	// ConfigError says why the repository's own graphvein.yaml was not
	// read (project mode); its environments are then not listed.
	ConfigError *string   `json:"config_error"`
	Limits      limitsDoc `json:"limits"`
	// LimitCeilings are the highest values an environment's limits may
	// take.
	LimitCeilings envLimitsDoc `json:"limit_ceilings"`
}

// optionsDoc is an environment's options as its file states them; the
// options edit takes the same shape.
type optionsDoc struct {
	TLS            *tlsDoc       `json:"tls"`
	UseSystemProxy bool          `json:"use_system_proxy"`
	Limits         *limitsSetDoc `json:"limits"`
}

type tlsDoc struct {
	Pins   []pinDoc `json:"pins"`
	CAFile *string  `json:"ca_file"`
}

// pinDoc is one pinned certificate: SHA256 is the lower-case hex SHA-256
// of its DER; Origin the origin it is accepted for (null in a repository
// environment, which has one origin; required in a project environment).
type pinDoc struct {
	Origin *string `json:"origin"`
	SHA256 string  `json:"sha256"`
}

// limitsSetDoc are an environment's own limits; null where the default
// applies.
type limitsSetDoc struct {
	RequestBytes   *int64 `json:"request_bytes"`
	ResponseBytes  *int64 `json:"response_bytes"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

// envLimitsDoc are limits in effect (or ceilings).
type envLimitsDoc struct {
	RequestBytes   int64 `json:"request_bytes"`
	ResponseBytes  int64 `json:"response_bytes"`
	TimeoutSeconds int   `json:"timeout_seconds"`
}

func newOptionsDoc(o config.EnvironmentOptions) optionsDoc {
	d := optionsDoc{UseSystemProxy: o.UseSystemProxy}
	if o.TLS != nil {
		d.TLS = &tlsDoc{Pins: []pinDoc{}}
		for _, p := range o.TLS.Pins {
			pd := pinDoc{SHA256: p.SHA256}
			if p.Origin != "" {
				pd.Origin = newPtr(p.Origin)
			}
			d.TLS.Pins = append(d.TLS.Pins, pd)
		}
		if o.TLS.CAFile != "" {
			d.TLS.CAFile = newPtr(o.TLS.CAFile)
		}
	}
	if l := o.Limits; l != (config.Limits{}) {
		d.Limits = &limitsSetDoc{}
		if l.RequestBytes != 0 {
			d.Limits.RequestBytes = newPtr(l.RequestBytes)
		}
		if l.ResponseBytes != 0 {
			d.Limits.ResponseBytes = newPtr(l.ResponseBytes)
		}
		if l.TimeoutSeconds != 0 {
			d.Limits.TimeoutSeconds = newPtr(l.TimeoutSeconds)
		}
	}
	return d
}

func (t *tryItState) effectiveLimits(e tryEnv) envLimitsDoc {
	req, resp, timeout := t.limits(e)
	return envLimitsDoc{req, resp, int(timeout / time.Second)}
}

type environmentDoc struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Label      string  `json:"label"`
	Production bool    `json:"production"`
	Source     string  `json:"source"`
	File       string  `json:"file"`
	Line       int     `json:"line"`
	BaseURL    string  `json:"base_url"`
	Origin     string  `json:"origin"`
	Auth       authDoc `json:"auth"`
	// Selected, Acknowledged and Remembered are the session state:
	// selected now, production acknowledged since selected, and "don't
	// ask again until I switch" chosen.
	Selected     bool `json:"selected"`
	Acknowledged bool `json:"acknowledged"`
	Remembered   bool `json:"remembered"`
	// Options are TLS trust, proxy use and limits as the file states
	// them; EffectiveLimits the limits a send uses.
	Options         optionsDoc   `json:"options"`
	EffectiveLimits envLimitsDoc `json:"effective_limits"`
}

// authDoc says where an environment's auth header comes from, never its
// value.
type authDoc struct {
	// Header is the configured header, or the header of a typed value.
	Header *string `json:"header"`
	// Env is the configured variable name and EnvSet whether the server's
	// environment has it.
	Env    *string `json:"env"`
	EnvSet bool    `json:"env_set"`
	Prefix *string `json:"prefix"`
	// SessionValue is true when a value typed in the UI is held for this
	// environment; it wins over the variable.
	SessionValue bool `json:"session_value"`
}

type editFileDoc struct {
	Target string `json:"target"`
	File   string `json:"file"`
	Exists bool   `json:"exists"`
	// Hash is the base_hash of an edit: "sha256:..." or "absent".
	Hash string `json:"hash"`
	// Writable is false for a member without a checkout here.
	Writable bool `json:"writable"`
}

type limitsDoc struct {
	RequestBodyBytes  int   `json:"request_body_bytes"`
	ResponseBodyBytes int64 `json:"response_body_bytes"`
	TimeoutSeconds    int   `json:"timeout_seconds"`
	ConfirmationTTL   int   `json:"confirmation_ttl_seconds"`
}

func (t *tryItState) environmentsEndpoint(w http.ResponseWriter, r *http.Request) error {
	if r.Method == http.MethodPost {
		return t.editEndpoint(w, r)
	}
	s, err := t.scope(r.URL.Query().Get("repository"))
	if err != nil {
		return err
	}
	out := environmentsResponse{Repository: s.opt.Repository, Token: t.token, Environments: []environmentDoc{}, Files: []editFileDoc{},
		Limits:        limitsDoc{maxTryItBody, t.maxResponse, int(t.timeout / time.Second), int(confirmationTTL / time.Second)},
		LimitCeilings: envLimitsDoc{config.MaxRequestBytes, config.MaxResponseBytes, config.MaxTimeoutSeconds}}
	if s.configErr != nil {
		out.ConfigError = newPtr(s.configErr.Error())
	}
	t.mu.Lock()
	for _, e := range s.envs {
		t.reconcile(e)
		d := environmentDoc{ID: e.ID, Name: e.Name, Label: e.Label, Production: e.production(), Source: e.Source, File: e.File, Line: e.Line,
			BaseURL: e.Base.Text, Origin: e.Base.Origin(), Selected: t.selected == e.key(), Acknowledged: t.acknowledged[e.key()], Remembered: t.granted[e.key()],
			Options: newOptionsDoc(e.Options), EffectiveLimits: t.effectiveLimits(e)}
		if e.Auth != nil {
			_, set := t.getenv(e.Auth.Env)
			d.Auth = authDoc{Header: newPtr(e.Auth.Header), Env: newPtr(e.Auth.Env), EnvSet: set, Prefix: newPtr(e.Auth.Prefix)}
		}
		if sec, ok := t.secrets[e.base()]; ok {
			d.Auth.Header, d.Auth.SessionValue = newPtr(sec.header), true
		}
		if d.Selected {
			out.Selected = newPtr(e.ID)
		}
		out.Environments = append(out.Environments, d)
	}
	t.mu.Unlock()
	for _, target := range []string{sourceRepository, sourceProject} {
		f, ok := t.editFile(s, target)
		if !ok {
			continue
		}
		doc := editFileDoc{Target: target, File: filepath.Base(f.path), Writable: f.writable, Hash: hashAbsent}
		if f.writable {
			data, err := readEditFile(f.path)
			if err != nil {
				return err
			}
			doc.Exists, doc.Hash = data != nil, editHash(data)
		}
		out.Files = append(out.Files, doc)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// POST /api/v0/tryit/select

type selectRequest struct {
	Repository string `json:"repository"`
	// Environment is the id to select; empty clears the selection.
	Environment string `json:"environment"`
	// AcknowledgeProduction is the UI's production dialog: required once
	// per selection of a production environment before any send.
	AcknowledgeProduction bool `json:"acknowledge_production"`
}

type selectResponse struct {
	APIVersion   int     `json:"api_version"`
	Selected     *string `json:"selected"`
	Production   bool    `json:"production"`
	Acknowledged bool    `json:"acknowledged"`
	// AcknowledgementRequired is true for a production environment not
	// acknowledged yet: every send is refused until it is.
	AcknowledgementRequired bool `json:"acknowledgement_required"`
	Remembered              bool `json:"remembered"`
}

// selectEndpoint records the UI's environment switch (rows TI-10, TI-11).
// Selecting another environment revokes every acknowledgement and every
// "don't ask again" grant; selecting the same one keeps them.
func (t *tryItState) selectEndpoint(w http.ResponseWriter, r *http.Request) error {
	var req selectRequest
	if err := t.checkPost(w, r, &req); err != nil {
		return err
	}
	out := selectResponse{}
	if req.Environment == "" {
		if req.AcknowledgeProduction {
			return badRequest("acknowledge_production needs an environment")
		}
		t.mu.Lock()
		t.switchTo("", "")
		t.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	s, err := t.scope(req.Repository)
	if err != nil {
		return err
	}
	e, err := s.env(req.Environment)
	if err != nil {
		return err
	}
	if req.AcknowledgeProduction && !e.production() {
		return badRequest("environment %s is not labelled production; there is nothing to acknowledge", e.ID)
	}
	t.mu.Lock()
	t.reconcile(e)
	t.switchTo(e.key(), e.base())
	if req.AcknowledgeProduction {
		t.acknowledged[e.key()] = true
	}
	out.Selected, out.Production, out.Acknowledged, out.Remembered = newPtr(e.ID), e.production(), t.acknowledged[e.key()], t.granted[e.key()]
	t.mu.Unlock()
	out.AcknowledgementRequired = out.Production && !out.Acknowledged
	t.opt.logger().Info("try-it environment selected", "environment", e.ID, "label", e.Label, "repository", e.Repository)
	writeJSON(w, http.StatusOK, out)
	return nil
}

// switchTo makes key (of base) the selected environment; the caller holds
// mu.
func (t *tryItState) switchTo(key, base string) {
	if t.selected == key {
		return
	}
	t.selected, t.selectedBase = key, base
	t.acknowledged = map[string]bool{}
	t.granted = map[string]bool{}
	t.confirms = map[string]confirmation{}
}

// POST /api/v0/tryit/auth

type authRequest struct {
	Repository  string `json:"repository"`
	Environment string `json:"environment"`
	// Header defaults to the environment's auth header, else
	// Authorization.
	Header string `json:"header"`
	// Value is held in server memory only; Clear removes it.
	Value string `json:"value"`
	Clear bool   `json:"clear"`
}

type authResponse struct {
	APIVersion   int     `json:"api_version"`
	Environment  string  `json:"environment"`
	Header       *string `json:"header"`
	SessionValue bool    `json:"session_value"`
}

// authEndpoint sets or clears a typed auth value (row TI-12). The value
// is never echoed, logged or written.
func (t *tryItState) authEndpoint(w http.ResponseWriter, r *http.Request) error {
	var req authRequest
	if err := t.checkPost(w, r, &req); err != nil {
		return err
	}
	s, err := t.scope(req.Repository)
	if err != nil {
		return err
	}
	e, err := s.env(req.Environment)
	if err != nil {
		return err
	}
	out := authResponse{Environment: e.ID}
	if req.Clear {
		if req.Value != "" || req.Header != "" {
			return badRequest("clear takes no header or value")
		}
		t.mu.Lock()
		delete(t.secrets, e.base())
		t.mu.Unlock()
		t.opt.logger().Info("try-it session auth value cleared", "environment", e.ID)
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	header := req.Header
	if header == "" && e.Auth != nil {
		header = e.Auth.Header
	}
	if header == "" {
		header = "Authorization"
	}
	if p := config.HeaderNameProblem(header); p != "" {
		return &apiError{http.StatusBadRequest, "header_not_allowed", fmt.Sprintf("header %q: %s", header, p)}
	}
	if req.Value == "" || len(req.Value) > maxTryItValue || strings.ContainsAny(req.Value, "\r\n\x00") {
		return badRequest("value must be 1 to %d bytes without line breaks or NUL", maxTryItValue)
	}
	t.mu.Lock()
	t.reconcile(e)
	t.secretGen++
	t.secrets[e.base()] = sessionSecret{key: e.key(), header: header, value: req.Value, gen: t.secretGen}
	t.mu.Unlock()
	t.opt.logger().Info("try-it session auth value set", "environment", e.ID, "header", header)
	out.Header, out.SessionValue = newPtr(header), true
	writeJSON(w, http.StatusOK, out)
	return nil
}

// tryRequest is the body of prepare and send: an operation and values,
// never a URL.
type tryRequest struct {
	Repository  string `json:"repository"`
	Environment string `json:"environment"`
	// Route is the stored route's identity; Path one of its path keys
	// (the template, with the served trailing slash); Method the route's
	// method, repeated so a stale page cannot send another one.
	Route      string            `json:"route"`
	Path       string            `json:"path"`
	Method     string            `json:"method"`
	PathValues map[string]string `json:"path_values"`
	// FreeForm takes Path as a literal path relative to the environment's
	// base URL that need not match the operation (any of the eight
	// methods); such a request always needs a one-time confirmation.
	FreeForm bool      `json:"free_form"`
	Query    []pairDoc `json:"query"`
	Headers  []pairDoc `json:"headers"`
	// Body is sent as written; ContentType, when set, is its
	// Content-Type. Multipart is a multipart/form-data body the server
	// builds instead (fields and files); it takes neither.
	Body        *string   `json:"body"`
	ContentType string    `json:"content_type"`
	Multipart   []partDoc `json:"multipart"`
	// SendWithoutAuth is the user's choice to send although the
	// environment's auth variable is not set (bound into the
	// confirmation).
	SendWithoutAuth bool `json:"send_without_auth"`
	// Confirmation (send only) is the id prepare returned for this exact
	// request; Remember (send only, with a valid confirmation) stops
	// asking for this environment until the UI switches.
	Confirmation string `json:"confirmation"`
	Remember     bool   `json:"remember"`
}

type pairDoc struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// partDoc is one part of a multipart/form-data body: a field (Value) or,
// with Filename, a file whose bytes are DataBase64 (or Value as text).
type partDoc struct {
	Name        string  `json:"name"`
	Value       *string `json:"value"`
	DataBase64  *string `json:"data_base64"`
	Filename    *string `json:"filename"`
	ContentType string  `json:"content_type"`
}

// partSummary describes a built part in prepare's answer: never its
// content.
type partSummary struct {
	Name        string  `json:"name"`
	Filename    *string `json:"filename"`
	ContentType *string `json:"content_type"`
	Bytes       int     `json:"bytes"`
}

// authMissingDoc names the auth a request lacks: the environment's header
// and the variable that is not set.
type authMissingDoc struct {
	Header string `json:"header"`
	Env    string `json:"env"`
}

// Reasons a request needs a confirmation (prepare's confirmation_reasons).
const (
	reasonMethod         = "method"
	reasonMethodOverride = "method_override"
	reasonBodyOnGet      = "body_on_get_or_head"
	reasonFreeForm       = "free_form_path"
)

// built is a request the server built and checked, ready to send.
type built struct {
	env      tryEnv
	method   string
	url      string
	template string
	headers  []pairDoc
	body     []byte
	parts    []partSummary
	freeForm bool
	// reasons say why the request needs a confirmation; mutating is true
	// when a "don't ask again" grant may stand in for it (every reason but
	// a free-form path).
	reasons  []string
	mutating bool
	// auth describes the auth header the server fills: header name and
	// source (the variable and prefix, or the typed value's number); it
	// is part of the hash. authMissing is set when the
	// environment's variable is not set and no value was typed.
	auth            string
	authHeader      string
	authSource      string
	authMissing     *authMissingDoc
	sendWithoutAuth bool
	hash            string
}

// mutating reports whether the method changes state (row TI-9):
// everything but GET and HEAD.
func mutating(method string) bool {
	return method != http.MethodGet && method != http.MethodHead
}

// build resolves the environment and the operation and builds the exact
// request (rows TI-4, TI-7, TI-13); nothing is sent.
func (t *tryItState) build(ctx context.Context, req tryRequest) (built, error) {
	s, err := t.scope(req.Repository)
	if err != nil {
		return built{}, err
	}
	e, err := s.env(req.Environment)
	if err != nil {
		return built{}, err
	}
	var path string
	b := built{env: e, freeForm: req.FreeForm}
	if req.FreeForm {
		// A free-form path (row TI-26): the route only names the operation
		// the user started from; method and path are the user's.
		if _, err := findRoute(ctx, s.opt, req.Route); err != nil {
			return built{}, err
		}
		if !openAPIMethod(req.Method) {
			return built{}, badRequest("a free-form request needs one of the methods GET, PUT, POST, DELETE, OPTIONS, HEAD, PATCH, TRACE")
		}
		if len(req.PathValues) > 0 {
			return built{}, badRequest("a free-form path takes no path_values; write the values into path")
		}
		if path, err = freeFormPath(req.Path); err != nil {
			return built{}, err
		}
		b.method, b.template = req.Method, "(free-form)"
		b.reasons = append(b.reasons, reasonFreeForm)
	} else {
		op, err := findOperation(ctx, s.opt, req.Route, req.Path)
		if err != nil {
			return built{}, err
		}
		if req.Method != op.method {
			return built{}, &apiError{http.StatusBadRequest, "method_mismatch", fmt.Sprintf("the operation's method is %s, not %q; send it as a free-form request to use another method", op.method, req.Method)}
		}
		if path, err = fillPath(op.key, req.PathValues); err != nil {
			return built{}, err
		}
		b.method, b.template = op.method, op.key
	}
	query, err := encodeQuery(req.Query)
	if err != nil {
		return built{}, err
	}
	b.url = e.Base.Origin() + e.Base.Path + path + query
	t.mu.Lock()
	t.reconcile(e)
	b.auth, b.authHeader, b.authSource, b.authMissing = t.authDescriptor(e)
	t.mu.Unlock()
	if req.SendWithoutAuth && b.authMissing == nil {
		return built{}, badRequest("send_without_auth applies only when the environment's auth variable is not set and no value was typed")
	}
	b.sendWithoutAuth = req.SendWithoutAuth
	if len(req.Headers) > maxTryItHeaders {
		return built{}, badRequest("at most %d headers", maxTryItHeaders)
	}
	override := false
	for _, h := range req.Headers {
		if p := config.HeaderNameProblem(h.Name); p != "" {
			return built{}, &apiError{http.StatusBadRequest, "header_not_allowed", fmt.Sprintf("header %q: %s", h.Name, p)}
		}
		if unsupportedHeader(h.Name) {
			msg := "WebSocket endpoints are not supported in try-it yet"
			if strings.EqualFold(h.Name, "HTTP2-Settings") {
				msg = "HTTP/2 cleartext (h2c) upgrades are not made by try-it"
			}
			return built{}, &apiError{http.StatusBadRequest, "not_supported", msg}
		}
		if b.authHeader != "" && strings.EqualFold(h.Name, b.authHeader) {
			return built{}, &apiError{http.StatusBadRequest, "header_not_allowed", fmt.Sprintf("header %s is set by the server from the environment's auth; it cannot be typed per request", h.Name)}
		}
		if (req.ContentType != "" || len(req.Multipart) > 0) && strings.EqualFold(h.Name, "Content-Type") {
			return built{}, badRequest("Content-Type is given twice (content_type or multipart, and headers)")
		}
		if err := valueProblem("header "+h.Name, h.Value); err != nil {
			return built{}, err
		}
		override = override || methodOverrideHeaders[strings.ToLower(h.Name)]
		b.headers = append(b.headers, h)
	}
	for _, q := range req.Query {
		override = override || strings.EqualFold(q.Name, "_method")
	}
	requestBytes, _, _ := t.limits(e)
	tooLarge := func(n int) error {
		return &detailedError{&apiError{http.StatusRequestEntityTooLarge, "body_too_large", fmt.Sprintf("the request body is %d bytes; environment %s sends at most %d (raise limits.request_bytes of the environment, at most %d, through the environment edit)", n, e.ID, requestBytes, config.MaxRequestBytes)},
			limitDetails("request_bytes", requestBytes, config.MaxRequestBytes)}
	}
	switch {
	case len(req.Multipart) > 0:
		if req.Body != nil || req.ContentType != "" {
			return built{}, badRequest("multipart replaces body and content_type; send one or the other")
		}
		body, contentType, parts, err := buildMultipart(req.Multipart, requestBytes)
		if err != nil {
			var size *multipartTooLarge
			if errors.As(err, &size) {
				return built{}, tooLarge(size.n)
			}
			return built{}, err
		}
		b.body, b.parts = body, parts
		b.headers = append(b.headers, pairDoc{"Content-Type", contentType})
	default:
		if req.ContentType != "" {
			if err := valueProblem("content_type", req.ContentType); err != nil {
				return built{}, err
			}
			b.headers = append(b.headers, pairDoc{"Content-Type", req.ContentType})
		}
		if req.Body != nil && (*req.Body != "" || mutating(b.method)) {
			if int64(len(*req.Body)) > requestBytes {
				return built{}, tooLarge(len(*req.Body))
			}
			b.body = []byte(*req.Body)
		}
	}
	// Row TI-27: a GET or HEAD that carries a method override, a body or a
	// content type may change state on many servers, so it needs the
	// confirmation of a mutating method (an empty body counts as none).
	switch {
	case mutating(b.method):
		b.reasons = append(b.reasons, reasonMethod)
	case override:
		b.reasons = append(b.reasons, reasonMethodOverride)
	case len(b.body) > 0 || req.ContentType != "":
		b.reasons = append(b.reasons, reasonBodyOnGet)
	}
	b.mutating = mutating(b.method) || override || len(b.body) > 0 || req.ContentType != ""
	b.hash = b.requestHash()
	return b, nil
}

// requestHash names the exact request a confirmation is bound to:
// environment key, method, URL, headers typed, body, the auth descriptor
// (header, source, prefix, typed value number) and the user's choices
// (free-form path, sending without auth). The auth value itself is not
// part of it.
func (b built) requestHash() string {
	h := sha256.New()
	body := sha256.Sum256(b.body)
	fmt.Fprintf(h, "%q\n%q\n%q\n%x\n%q\nfree_form=%t\nsend_without_auth=%t\n", b.env.key(), b.method, b.url, body, b.auth, b.freeForm, b.sendWithoutAuth)
	for _, p := range b.headers {
		fmt.Fprintf(h, "%q: %q\n", p.Name, p.Value)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func valueProblem(what, v string) error {
	if len(v) > maxTryItValue || strings.ContainsAny(v, "\r\n\x00") {
		return badRequest("%s: a value is at most %d bytes without line breaks or NUL", what, maxTryItValue)
	}
	return nil
}

// authDescriptor describes the auth header the server fills for e (the
// caller holds mu): the descriptor bound into a request's hash, the
// header name and its source ("session" or "env"), all empty without auth.
// A configured variable that is not set (and no typed value) gives no
// header and names what is missing (row TI-47).
func (t *tryItState) authDescriptor(e tryEnv) (desc, header, source string, missing *authMissingDoc) {
	if sec, ok := t.secrets[e.base()]; ok && sec.key == e.key() {
		return fmt.Sprintf("session %q #%d", sec.header, sec.gen), sec.header, "session", nil
	}
	if e.Auth != nil {
		if v, set := t.getenv(e.Auth.Env); !set || v == "" {
			return fmt.Sprintf("missing %q %q", e.Auth.Header, e.Auth.Env), "", "", &authMissingDoc{e.Auth.Header, e.Auth.Env}
		}
		return fmt.Sprintf("env %q %q %q", e.Auth.Header, e.Auth.Env, e.Auth.Prefix), e.Auth.Header, "env", nil
	}
	return "", "", "", nil
}

// methodOverrideHeaders make common servers treat a request as another
// method; on GET and HEAD they make the request need a confirmation (row
// TI-27).
var methodOverrideHeaders = map[string]bool{"x-http-method-override": true, "x-http-method": true, "x-method-override": true}

// unsupportedHeader reports headers of protocol upgrades try-it never
// makes: WebSocket and HTTP/2 cleartext (row TI-28).
func unsupportedHeader(name string) bool {
	l := strings.ToLower(name)
	return strings.HasPrefix(l, "sec-websocket-") || l == "http2-settings"
}

// operation is a stored route's method and one path key try-it may call.
type operation struct {
	method string
	key    string
}

// operationAvailable are the path entry resolutions whose path is known
// (OG-10): literal, a constant, or a settings default.
var operationAvailable = map[string]bool{"literal": true, "constant": true, "settings_default": true}

// findOperation reads route from the store and checks that key is one of
// its callable path keys (row TI-7, "matches the operation"): a production
// path entry with a known template, a method from the eight of OpenAPI,
// one-segment parameters only, no host pattern, not optional. The checks
// follow the OG rows the generated document applies to one route; a
// request for anything else is refused, never approximated.
func findOperation(ctx context.Context, opt Options, route, key string) (operation, error) {
	unavailable := func(format string, args ...any) error {
		return &apiError{http.StatusBadRequest, "operation_unavailable", fmt.Sprintf(format, args...)}
	}
	if route == "" || key == "" {
		return operation{}, badRequest("route and path name the operation")
	}
	n, err := findRoute(ctx, opt, route)
	if err != nil {
		return operation{}, err
	}
	method, _ := n.Attributes["method"].(string)
	switch {
	case openAPIMethod(method):
	case method == "ANY" || method == "":
		return operation{}, unavailable("the route accepts any method; try-it needs one method (send a free-form request)")
	default:
		return operation{}, unavailable("method %s is not one OpenAPI 3.1 describes", method)
	}
	if h, ok := n.Attributes["host"]; ok && h != nil {
		return operation{}, unavailable("the route is restricted to a host pattern")
	}
	if n.Attributes["scope"] == "test" {
		return operation{}, unavailable("the route is registered by test code")
	}
	for _, p := range objects(n.Attributes["paths"]) {
		template, ok := p["template"].(string)
		if !ok {
			continue
		}
		served := template
		if p["trailing_slash"] == true && template != "/" {
			served += "/"
		}
		if served != key {
			continue
		}
		res, _ := p["resolution"].(string)
		switch {
		case p["scope"] == "test":
			return operation{}, unavailable("path %s is registered by test code", key)
		case !operationAvailable[res]:
			return operation{}, unavailable("path %s is not resolved (resolution %q)", key, res)
		case p["optional"] == true:
			return operation{}, unavailable("path %s has an optional parameter", key)
		}
		if err := checkTemplate(key); err != nil {
			return operation{}, unavailable("path %s: %v", key, err)
		}
		return operation{method: method, key: key}, nil
	}
	return operation{}, &apiError{http.StatusBadRequest, "path_mismatch", fmt.Sprintf("%q is not a path of route %s; free-form paths are not sent", key, route)}
}

// findRoute reads the stored route node of identity route.
func findRoute(ctx context.Context, opt Options, route string) (contract.Node, error) {
	if route == "" {
		return contract.Node{}, badRequest("route names the operation")
	}
	rd := &reader{ctx: ctx, s: opt.Store, repo: opt.Repository}
	nodes, err := rd.byIdentity([]string{route})
	if err != nil {
		return contract.Node{}, err
	}
	n, ok := nodes[route]
	if !ok || n.Kind != "route" {
		return contract.Node{}, &apiError{http.StatusNotFound, "unknown_operation", fmt.Sprintf("no stored route %q in %s", route, opt.Repository)}
	}
	return n, nil
}

// openAPIMethod reports the eight methods OpenAPI 3.1 describes.
func openAPIMethod(m string) bool {
	switch m {
	case "GET", "PUT", "POST", "DELETE", "OPTIONS", "HEAD", "PATCH", "TRACE":
		return true
	}
	return false
}

// checkTemplate accepts a path key whose parameters each fill part of one
// segment: {name}, never a wildcard ({*}, {*rest}, {x...}).
func checkTemplate(key string) error {
	if !strings.HasPrefix(key, "/") {
		return errors.New("a path key starts with /")
	}
	if strings.ContainsAny(key, "?#") {
		return errors.New("a path key holds no query or fragment")
	}
	for rest := key; ; {
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			if strings.ContainsRune(rest, '}') {
				return errors.New("unbalanced }")
			}
			return nil
		}
		j := strings.IndexByte(rest[i:], '}')
		if j < 0 {
			return errors.New("unbalanced {")
		}
		name := rest[i+1 : i+j]
		if name == "" || strings.ContainsAny(name, "*/{") || strings.HasSuffix(name, "...") {
			return errors.New("a wildcard or multi-segment parameter")
		}
		rest = rest[i+j+1:]
	}
}

// fillPath substitutes each {name} of key with its value, percent-encoded
// as part of one path segment ("/", "?", "#" and "%" are encoded). A
// missing, empty or extra value is refused, and so is a value that makes
// a "." or ".." segment, which a server could resolve to another path.
func fillPath(key string, values map[string]string) (string, error) {
	// The template's literal parts may not hold an encoded
	// separator; values are encoded here, so a "/" in a value stays
	// inside its segment.
	if encodedSeparator(key) {
		return "", &apiError{http.StatusBadRequest, "invalid_path_value", "the operation's path holds an encoded / or \\ (%2F, %5C); it is not sent"}
	}
	var out strings.Builder
	used := map[string]bool{}
	rest := key
	for {
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			out.WriteString(rest)
			break
		}
		j := strings.IndexByte(rest[i:], '}')
		name := rest[i+1 : i+j]
		v, ok := values[name]
		if !ok || v == "" {
			return "", &apiError{http.StatusBadRequest, "missing_path_value", fmt.Sprintf("path parameter %s needs a value", name)}
		}
		if err := valueProblem("path parameter "+name, v); err != nil {
			return "", err
		}
		used[name] = true
		out.WriteString(rest[:i])
		out.WriteString(escapeSegment(v))
		rest = rest[i+j+1:]
	}
	for name := range values {
		if !used[name] {
			return "", badRequest("path_values names %q, which is not a parameter of %s", name, key)
		}
	}
	path := out.String()
	// Segments are compared decoded, so neither a value nor a literal part
	// of the template makes a dot segment through
	// percent-encoding.
	for _, seg := range strings.Split(path, "/") {
		decoded, err := url.PathUnescape(seg)
		if err != nil || decoded == "." || decoded == ".." {
			return "", &apiError{http.StatusBadRequest, "invalid_path_value", "the filled path may not have a . or .. segment, plain or percent-encoded"}
		}
	}
	return path, nil
}

// escapeSegment percent-encodes everything outside RFC 3986 unreserved
// characters and the sub-delimiters that are safe inside a segment.
func escapeSegment(v string) string {
	const safe = "-._~!$&'()*+,;=:@"
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(safe, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// encodeQuery encodes the pairs as application/x-www-form-urlencoded, in
// the order given.
func encodeQuery(pairs []pairDoc) (string, error) {
	if len(pairs) == 0 {
		return "", nil
	}
	if len(pairs) > maxTryItQuery {
		return "", badRequest("at most %d query parameters", maxTryItQuery)
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p.Name == "" {
			return "", badRequest("a query parameter needs a name")
		}
		if len(p.Name) > maxTryItValue || len(p.Value) > maxTryItValue {
			return "", badRequest("a query name or value is at most %d bytes", maxTryItValue)
		}
		parts = append(parts, queryEscape(p.Name)+"="+queryEscape(p.Value))
	}
	return "?" + strings.Join(parts, "&"), nil
}

func queryEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// POST /api/v0/tryit/prepare

type prepareResponse struct {
	APIVersion  int    `json:"api_version"`
	Environment string `json:"environment"`
	Label       string `json:"label"`
	Production  bool   `json:"production"`
	Method      string `json:"method"`
	// URL is the exact URL that would be sent.
	URL string `json:"url"`
	// FreeForm is true for a free-form path (row TI-26).
	FreeForm bool `json:"free_form"`
	// Headers are the header names that would be sent: the ones typed,
	// then the auth header (its value never shown).
	Headers    []string `json:"headers"`
	AuthHeader *string  `json:"auth_header"`
	// AuthSource is "env", "session" or null.
	AuthSource *string `json:"auth_source"`
	// AuthMissing names the environment's auth header and the variable
	// that is not set (no value typed either); the request is sent only
	// with SendWithoutAuth (row TI-47).
	AuthMissing     *authMissingDoc `json:"auth_missing"`
	SendWithoutAuth bool            `json:"send_without_auth"`
	BodyBytes       int             `json:"body_bytes"`
	BodySHA256      string          `json:"body_sha256"`
	// Multipart lists the parts of a built multipart body: names, file
	// names, types and sizes, never content.
	Multipart []partSummary `json:"multipart"`
	// Limits are the limits this send runs under; Proxy the proxy it
	// goes through (scheme, host and port only), or null.
	Limits envLimitsDoc `json:"limits"`
	Proxy  *string      `json:"proxy"`
	// ProxyCleartext is true when an http:// environment is reached
	// through the proxy: the proxy then sees the whole request, headers
	// and the auth value included, in clear (row TI-43).
	ProxyCleartext bool `json:"proxy_cleartext"`
	// ConfirmationRequired is true when the request needs a one-time
	// confirmation (ConfirmationReasons say why: "method",
	// "method_override", "body_on_get_or_head", "free_form_path");
	// Confirmation is then the id send must carry, valid until ExpiresAt.
	// No id is issued while auth is missing and send_without_auth is not
	// set: prepare again with it.
	ConfirmationRequired bool     `json:"confirmation_required"`
	ConfirmationReasons  []string `json:"confirmation_reasons"`
	Confirmation         *string  `json:"confirmation"`
	ExpiresAt            *string  `json:"expires_at"`
	// AcknowledgementRequired is true for a production environment not
	// acknowledged since it was selected.
	AcknowledgementRequired bool `json:"acknowledgement_required"`
	Selected                bool `json:"selected"`
}

func (t *tryItState) prepareEndpoint(w http.ResponseWriter, r *http.Request) error {
	var req tryRequest
	if err := t.checkPostLimit(w, r, &req, t.maxSendPost); err != nil {
		return err
	}
	if req.Confirmation != "" || req.Remember {
		return badRequest("confirmation and remember belong to send, not prepare")
	}
	b, err := t.build(r.Context(), req)
	if err != nil {
		return err
	}
	body := sha256.Sum256(b.body)
	out := prepareResponse{Environment: b.env.ID, Label: b.env.Label, Production: b.env.production(), Method: b.method, URL: b.url, FreeForm: b.freeForm,
		Headers: []string{}, AuthMissing: b.authMissing, SendWithoutAuth: b.sendWithoutAuth, BodyBytes: len(b.body), BodySHA256: hex.EncodeToString(body[:]),
		Multipart: b.parts, Limits: t.effectiveLimits(b.env), ConfirmationReasons: []string{}}
	for _, h := range b.headers {
		out.Headers = append(out.Headers, h.Name)
	}
	if b.authHeader != "" {
		out.AuthHeader, out.AuthSource = newPtr(b.authHeader), newPtr(b.authSource)
	}
	if proxy, err := t.proxyFor(b); err != nil {
		return err
	} else if proxy != nil {
		out.Proxy = newPtr(proxy.Scheme + "://" + proxy.Host)
		out.ProxyCleartext = b.env.Base.Scheme == "http"
	}
	key := b.env.key()
	t.mu.Lock()
	out.Selected = t.selected == key
	out.AcknowledgementRequired = out.Production && !t.acknowledged[key]
	if b.freeForm || (b.mutating && !t.granted[key]) {
		out.ConfirmationRequired = true
		out.ConfirmationReasons = append(out.ConfirmationReasons, b.reasons...)
		if b.authMissing == nil || b.sendWithoutAuth {
			id := t.issue(b)
			out.Confirmation = &id
			out.ExpiresAt = newPtr(t.confirms[id].expires.UTC().Format(time.RFC3339))
		}
	}
	t.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
	return nil
}

// issue stores a one-time confirmation for b; the caller holds mu.
func (t *tryItState) issue(b built) string {
	now := t.opt.now()
	var oldest string
	for id, c := range t.confirms {
		if !now.Before(c.expires) {
			delete(t.confirms, id)
			continue
		}
		if oldest == "" || c.expires.Before(t.confirms[oldest].expires) {
			oldest = id
		}
	}
	if len(t.confirms) >= maxConfirmations {
		delete(t.confirms, oldest)
	}
	id := rand.Text()
	t.confirms[id] = confirmation{hash: b.hash, key: b.env.key(), expires: now.Add(confirmationTTL)}
	return id
}

// POST /api/v0/tryit/send

type sendResponse struct {
	APIVersion  int    `json:"api_version"`
	Environment string `json:"environment"`
	Method      string `json:"method"`
	URL         string `json:"url"`
	Status      int    `json:"status"`
	StatusText  string `json:"status_text"`
	// Headers are redacted (row TI-19).
	Headers []pairDoc `json:"headers"`
	// Body is the response as text, or for a non-text body the hex of its
	// first 4 KiB (BodyEncoding "hex").
	Body         string `json:"body"`
	BodyEncoding string `json:"body_encoding"`
	BodyBytes    int64  `json:"body_bytes"`
	Truncated    bool   `json:"truncated"`
	// TruncatedReason is "size" (the response limit), "timeout" (the time
	// limit cut the body; the status is the one received) or
	// "read_error", null when the body is complete (row TI-15).
	// TruncatedLimit names the limit that cut it and how to raise
	// it (row TI-44).
	TruncatedReason *string   `json:"truncated_reason"`
	TruncatedLimit  *limitDoc `json:"truncated_limit"`
	// Redirect is the Location of a 3xx answer, never followed;
	// RedirectFollow is how the user can follow it once (row TI-46).
	Redirect       *string            `json:"redirect"`
	RedirectFollow *redirectFollowDoc `json:"redirect_follow"`
	DurationMS     int64              `json:"duration_ms"`
	Remembered     bool               `json:"remembered"`
}

// limitDoc names a limit that cut or refused a request, its value, its
// ceiling and how to raise it.
type limitDoc struct {
	Limit   string `json:"limit"`
	Value   int64  `json:"value"`
	Ceiling int64  `json:"ceiling"`
	Hint    string `json:"hint"`
}

func limitDetails(name string, value, ceiling int64) *limitDoc {
	return &limitDoc{Limit: name, Value: value, Ceiling: ceiling,
		Hint: fmt.Sprintf("raise limits.%s of the environment (at most %d) through the environment edit", name, ceiling)}
}

func (t *tryItState) sendEndpoint(w http.ResponseWriter, r *http.Request) error {
	var req tryRequest
	if err := t.checkPostLimit(w, r, &req, t.maxSendPost); err != nil {
		return err
	}
	b, err := t.build(r.Context(), req)
	if err != nil {
		return err
	}
	t.mu.Lock()
	err = t.ready(b.env)
	t.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case t.inFlight <- struct{}{}:
		defer func() { <-t.inFlight }()
	default:
		return &apiError{http.StatusTooManyRequests, "too_many_requests", fmt.Sprintf("at most %d try-it requests run at once", maxTryItInFlight)}
	}
	authHeader, authValue, remembered, err := t.authorize(b, req)
	if err != nil {
		return err
	}
	return t.send(w, r, b, req.Repository, authHeader, authValue, remembered)
}

// authValue returns the auth header and value for b's environment: a typed
// session value wins over the variable. A configured variable that is not
// set sends no auth header only when the user chose send_without_auth
// (row TI-47); otherwise the send is refused naming the variable.
//
// The caller holds mu.
func (t *tryItState) authValue(b built) (string, string, error) {
	e := b.env
	if sec, ok := t.secrets[e.base()]; ok && sec.key == e.key() {
		return sec.header, sec.value, nil
	}
	if e.Auth == nil {
		return "", "", nil
	}
	v, set := t.getenv(e.Auth.Env)
	if !set || v == "" {
		if b.sendWithoutAuth {
			return "", "", nil
		}
		return "", "", &detailedError{&apiError{http.StatusBadRequest, "auth_missing", fmt.Sprintf("environment %s takes %s from the variable %s, which is not set for this server; set it and restart, type a value for this session, or send without auth (send_without_auth)", e.ID, e.Auth.Header, e.Auth.Env)},
			authMissingDoc{e.Auth.Header, e.Auth.Env}}
	}
	if strings.ContainsAny(v, "\r\n\x00") {
		return "", "", &apiError{http.StatusBadRequest, "secret_invalid", fmt.Sprintf("the variable %s holds a line break or NUL", e.Auth.Env)}
	}
	return e.Auth.Header, e.Auth.Prefix + v, nil
}

// authorize is the last step before a send, in one critical
// section: it checks again that the environment is still the selected
// one, in the form the request was built for, and acknowledged when
// production; that the auth in use is the one the request was built (and
// confirmed) with; reads the auth value; and enforces row TI-9: a request
// that needs a confirmation carries a valid one-time confirmation for
// exactly this request, or (not for a free-form path) a grant for the
// environment. A confirmation is used up by the check, whatever happens
// next.
func (t *tryItState) authorize(b built, req tryRequest) (authHeader, authValue string, remembered bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reconcile(b.env)
	if err := t.ready(b.env); err != nil {
		return "", "", false, err
	}
	if desc, _, _, _ := t.authDescriptor(b.env); desc != b.auth {
		return "", "", false, &apiError{http.StatusConflict, "confirmation_required", "the auth of this environment changed while the request was built; prepare it again"}
	}
	if authHeader, authValue, err = t.authValue(b); err != nil {
		return "", "", false, err
	}
	remembered, err = t.confirm(b, req)
	return authHeader, authValue, remembered, err
}

// ready checks that e is selected and, when production, acknowledged; the
// caller holds mu.
func (t *tryItState) ready(e tryEnv) error {
	if t.selected != e.key() {
		return &apiError{http.StatusConflict, "environment_not_selected", fmt.Sprintf("select environment %s first (POST %s); a send goes only to the selected environment, and an environment changed in its file must be selected again", e.ID, tryItSelectPath)}
	}
	if e.production() && !t.acknowledged[e.key()] {
		return &apiError{http.StatusConflict, "production_acknowledgement_required", fmt.Sprintf("environment %s is labelled %s: acknowledge it once (POST %s with acknowledge_production) before sending", e.ID, e.Label, tryItSelectPath)}
	}
	return nil
}

// confirm enforces row TI-9; the caller holds mu.
func (t *tryItState) confirm(b built, req tryRequest) (remembered bool, err error) {
	key := b.env.key()
	if !b.mutating && !b.freeForm {
		if req.Confirmation != "" || req.Remember {
			return false, badRequest("%s needs no confirmation", b.method)
		}
		return t.granted[key], nil
	}
	if req.Confirmation == "" {
		if req.Remember {
			return false, badRequest("remember needs a confirmation")
		}
		if !b.freeForm && t.granted[key] {
			return true, nil
		}
		why := b.method
		switch b.reasons[0] {
		case reasonFreeForm:
			why = "a free-form path (it does not match the operation)"
		case reasonMethodOverride:
			why = b.method + " with a method override"
		case reasonBodyOnGet:
			why = b.method + " with a body or content type"
		}
		return false, &apiError{http.StatusConflict, "confirmation_required", fmt.Sprintf("%s needs a confirmation: POST %s, show the request, then send with its confirmation", why, tryItPreparePath)}
	}
	c, ok := t.confirms[req.Confirmation]
	delete(t.confirms, req.Confirmation)
	switch {
	case !ok:
		return false, &apiError{http.StatusConflict, "confirmation_required", "the confirmation is unknown or already used; prepare the request again"}
	case !t.opt.now().Before(c.expires):
		return false, &apiError{http.StatusConflict, "confirmation_required", "the confirmation expired; prepare the request again"}
	case c.key != key || subtle.ConstantTimeCompare([]byte(c.hash), []byte(b.hash)) != 1:
		return false, &apiError{http.StatusConflict, "confirmation_required", "the request changed since it was confirmed; prepare it again"}
	}
	if req.Remember {
		t.granted[key] = true
	}
	return t.granted[key], nil
}

// send performs the request through an egress policy holding only the
// environment's origin (and the system proxy when the environment opted
// in), and answers with the redacted response.
func (t *tryItState) send(w http.ResponseWriter, r *http.Request, b built, repository, authHeader, authValue string, remembered bool) error {
	_, maxResponse, timeout := t.limits(b.env)
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	var body io.Reader
	if b.body != nil {
		body = bytes.NewReader(b.body)
	}
	up, err := http.NewRequestWithContext(ctx, b.method, b.url, body)
	if err != nil {
		return badRequest("building the request: %v", err)
	}
	if up.URL.Hostname() != b.env.Base.Host || up.URL.Scheme != b.env.Base.Scheme {
		return &apiError{http.StatusBadRequest, "destination_mismatch", "the built URL left the environment's origin"}
	}
	for _, h := range b.headers {
		up.Header.Add(h.Name, h.Value)
	}
	if authHeader != "" {
		up.Header.Set(authHeader, authValue)
	}
	if _, ok := up.Header["User-Agent"]; !ok {
		up.Header.Set("User-Agent", "graphvein-try-it")
	}
	ca, err := t.caFile(b.env)
	if err != nil {
		return err
	}
	proxy, err := t.proxyFor(b)
	if err != nil {
		return err
	}
	var pins []string
	if b.env.Options.TLS != nil {
		pins = b.env.Options.TLS.PinsFor(b.env.Base.Origin())
	}
	client, err := tryItClient(b.env.Base, egress.TryItOptions{Timeout: timeout, ServerName: b.env.Base.Host, PinnedSHA256: pins, CAPEM: ca, Proxy: proxy, ProxyRootCAs: t.proxyRoots})
	if err != nil {
		if errors.Is(err, egress.ErrNoCertificates) {
			return &apiError{http.StatusConflict, "ca_file_error", fmt.Sprintf("environment %s: tls.ca_file %s holds no PEM certificate", b.env.ID, b.env.Options.TLS.CAFile)}
		}
		return err
	}
	start := time.Now()
	resp, err := client.Do(up)
	logger := t.opt.logger()
	logAttrs := []any{"environment", b.env.ID, "label", b.env.Label, "repository", b.env.Repository, "method", b.method, "path", b.template, "request_bytes", len(b.body)}
	if err != nil {
		code, status := upstreamError(err)
		logger.Info("try-it request failed", append(logAttrs, "reason", code, "duration_ms", time.Since(start).Milliseconds())...)
		if r.Context().Err() != nil {
			return r.Context().Err()
		}
		ae := &apiError{status, code, upstreamMessage(code, b.env)}
		var ce *egress.CertificateError
		switch {
		case errors.As(err, &ce):
			ae.message = certificateMessage(ce, b.env)
			return &detailedError{ae, newCertificateDetails(ce, b.env)}
		case code == "upstream_timeout":
			return &detailedError{ae, limitDetails("timeout_seconds", int64(timeout/time.Second), config.MaxTimeoutSeconds)}
		}
		return ae
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	duration := time.Since(start)
	out := sendResponse{Environment: b.env.ID, Method: b.method, URL: b.url, Status: resp.StatusCode, StatusText: http.StatusText(resp.StatusCode),
		Headers: redactHeaders(resp.Header), DurationMS: duration.Milliseconds(), Remembered: remembered}
	if int64(len(data)) > maxResponse {
		data, out.Truncated, out.TruncatedReason = data[:maxResponse], true, newPtr("size")
		out.TruncatedLimit = limitDetails("response_bytes", maxResponse, config.MaxResponseBytes)
	}
	if readErr != nil {
		// A body cut by the timeout is shown as far as it came, with the
		// status received, and marked.
		reason := "read_error"
		out.TruncatedLimit = nil
		if code, _ := upstreamError(readErr); code == "upstream_timeout" || ctx.Err() != nil {
			reason = "timeout"
			out.TruncatedLimit = limitDetails("timeout_seconds", int64(timeout/time.Second), config.MaxTimeoutSeconds)
		}
		out.Truncated, out.TruncatedReason = true, &reason
	}
	out.BodyBytes = int64(len(data))
	out.Body, out.BodyEncoding = bodyText(resp.Header.Get("Content-Type"), data)
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if loc := resp.Header.Get("Location"); loc != "" {
			out.Redirect = &loc
			out.RedirectFollow = t.redirectFollow(b, repository, resp.StatusCode, loc)
		}
	}
	logger.Info("try-it request sent", append(logAttrs, "status", resp.StatusCode, "response_bytes", out.BodyBytes, "truncated", out.Truncated, "duration_ms", duration.Milliseconds())...)
	writeJSON(w, http.StatusOK, out)
	return nil
}

func upstreamMessage(code string, e tryEnv) string {
	switch code {
	case "destination_blocked":
		return "the request tried to reach a host outside environment " + e.ID + "'s origin " + e.Base.Origin() + "; nothing was sent there"
	case "upstream_timeout":
		return "no complete answer from " + e.Base.Origin() + " in time (raise limits.timeout_seconds of the environment, at most 600, through the environment edit)"
	case "upstream_tls_error":
		return "the TLS connection to " + e.Base.Origin() + " failed"
	case "proxy_tls_error":
		return "the TLS connection to the system proxy failed (its certificate is checked against the system roots for the proxy's own name; environment " + e.ID + "'s pins and CA file never apply to it); nothing was sent"
	}
	return "could not reach " + e.Base.Origin()
}

// redactedHeader reports whether a response header's value is replaced by
// [redacted] (row TI-19): credentials and cookies, and any name that
// reads like a secret.
func redactedHeader(name string) bool {
	l := strings.ToLower(name)
	switch l {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "set-cookie2", "www-authenticate", "proxy-authenticate":
		return true
	}
	for _, word := range []string{"token", "secret", "key", "password", "passwd", "auth", "session", "credential", "signature", "csrf", "xsrf"} {
		if strings.Contains(l, word) {
			return true
		}
	}
	return false
}

func redactHeaders(h http.Header) []pairDoc {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []pairDoc{}
	for _, name := range names {
		for _, v := range h[name] {
			if redactedHeader(name) {
				v = "[redacted]"
			}
			out = append(out, pairDoc{name, v})
		}
	}
	return out
}

// bodyText returns a text body as UTF-8 (invalid bytes replaced), and any
// other body as the hex of its first hexPreview bytes.
func bodyText(contentType string, data []byte) (string, string) {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	text := strings.HasPrefix(mediaType, "text/") || strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
	switch mediaType {
	case "application/json", "application/xml", "application/javascript", "application/x-www-form-urlencoded", "application/problem+json", "application/x-ndjson", "application/yaml", "":
		text = true
	}
	if text && (mediaType != "" || !bytes.ContainsRune(data, 0)) {
		return strings.ToValidUTF8(string(data), "�"), "text"
	}
	if len(data) > hexPreview {
		data = data[:hexPreview]
	}
	return hex.EncodeToString(data), "hex"
}

// tryItClient is the HTTP client of one send: an egress policy holding
// only the environment's origin (and the proxy when one is used), purpose
// try-it (rows TI-5, TI-6), no redirects, bounded by the timeout, with the
// environment's TLS trust (TI-14, TI-15, TI-17, TI-18). The try-it purpose
// is allowed in every egress mode, so the mode does not matter here.
func tryItClient(base config.BaseURL, o egress.TryItOptions) (doer, error) {
	destinations := []egress.Destination{{Host: base.Host, Port: base.Port, Purpose: egress.TryIt}}
	if o.Proxy != nil {
		destinations = append(destinations, egress.ProxyDestination(o.Proxy, egress.TryIt))
	}
	policy, err := egress.NewPolicy(egress.Offline, destinations...)
	if err != nil {
		return nil, fmt.Errorf("building the egress policy: %w", err)
	}
	return policy.TryItClient(o)
}

// doer is the one method of the egress client a send uses.
type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// upstreamError classifies a failed send: a policy refusal, a timeout, a
// certificate that is not trusted (or changed since it was pinned), another
// TLS failure or any other network error.
func upstreamError(err error) (code string, status int) {
	var cert *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var ce *egress.CertificateError
	var pe *egress.ProxyTLSError
	var timeout interface{ Timeout() bool }
	switch {
	case errors.Is(err, egress.ErrBlocked):
		return "destination_blocked", http.StatusBadGateway
	case errors.As(err, &pe):
		return "proxy_tls_error", http.StatusBadGateway
	case errors.As(err, &ce) && len(ce.Pinned) > 0:
		return "upstream_certificate_changed", http.StatusBadGateway
	case errors.As(err, &ce):
		return "upstream_certificate_untrusted", http.StatusBadGateway
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return "upstream_timeout", http.StatusGatewayTimeout
	case errors.As(err, &cert), errors.As(err, &record):
		return "upstream_tls_error", http.StatusBadGateway
	}
	return "upstream_error", http.StatusBadGateway
}
