package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
)

// This file holds the guarded environment edit (T029 row TI-30): a URL
// typed in the UI becomes an environment written to graphvein.yaml (or,
// in project mode, to the project manifest) the way the manifest edit of
// ADR 0075 writes: a preview with the unified diff, a write only of the
// previewed result onto the file it was based on (hash precondition), the
// session token and loopback peer, an atomic replace. An environment is
// dialable only once it is in a file; nothing typed is kept in memory as a
// destination.

// hashAbsent is the base hash of a file that does not exist yet.
const hashAbsent = "absent"

func editHash(data []byte) string {
	if data == nil {
		return hashAbsent
	}
	return manifestHash(data)
}

// readEditFile reads a file an edit may write; nil when it does not exist.
func readEditFile(path string) ([]byte, error) {
	data, err := readBounded(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if data == nil {
		data = []byte{}
	}
	return data, nil
}

// editTarget is the one file an edit for a target may write.
type editTarget struct {
	path     string
	writable bool
}

// editFile names the file of target for the request's repository:
// graphvein.yaml of the repository (the member's checkout in project
// mode), or the project manifest. No request names a path.
func (t *tryItState) editFile(s tryScope, target string) (editTarget, bool) {
	switch target {
	case sourceRepository:
		if s.opt.noCheckout || s.opt.Root == "" {
			return editTarget{path: "graphvein.yaml"}, true
		}
		return editTarget{path: filepath.Join(s.opt.Root, "graphvein.yaml"), writable: true}, true
	case sourceProject:
		if s.project == nil {
			return editTarget{}, false
		}
		return editTarget{path: filepath.Join(s.project.Dir, config.ProjectFile), writable: true}, true
	}
	return editTarget{}, false
}

type envEditRequest struct {
	Repository string `json:"repository"`
	// Target is "repository" (graphvein.yaml) or "project" (the manifest,
	// project mode only).
	Target      string     `json:"target"`
	BaseHash    string     `json:"base_hash"`
	Environment envEditDoc `json:"environment"`
	// Options, instead of Environment, replaces the options of the
	// existing environment Options.Name in the target file (row TI-48).
	Options *optionsEditDoc `json:"options"`
	// Apply writes the result; without it the answer is a preview.
	Apply bool `json:"apply"`
	// ResultHash is the preview's result_hash, required with apply.
	ResultHash string `json:"result_hash"`
}

type envEditDoc struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	BaseURL string `json:"base_url"`
	// Auth names a header and the variable holding its value; a value is
	// never written to a file.
	Auth *authEditDoc `json:"auth"`
}

// optionsEditDoc names an environment and the options it gets: tls,
// use_system_proxy and limits are replaced as a whole (null, false or a
// null limit removes them).
type optionsEditDoc struct {
	Name           string        `json:"name"`
	TLS            *tlsDoc       `json:"tls"`
	UseSystemProxy bool          `json:"use_system_proxy"`
	Limits         *limitsSetDoc `json:"limits"`
}

// environmentOptions validates d and returns the options it describes.
func (d optionsEditDoc) environmentOptions() (config.EnvironmentOptions, error) {
	o := config.EnvironmentOptions{UseSystemProxy: d.UseSystemProxy}
	if d.TLS != nil {
		o.TLS = &config.TLSOptions{}
		seen := map[config.Pin]bool{}
		for _, pd := range d.TLS.Pins {
			n, err := config.NormalizeFingerprint(pd.SHA256)
			if err != nil {
				return o, badRequest("%v", err)
			}
			pin := config.Pin{SHA256: n}
			if pd.Origin != nil {
				if pin.Origin, err = config.NormalizeOrigin(*pd.Origin); err != nil {
					return o, badRequest("pin origin: %v", err)
				}
			}
			if !seen[pin] {
				seen[pin] = true
				o.TLS.Pins = append(o.TLS.Pins, pin)
			}
		}
		if len(o.TLS.Pins) > config.MaxPins {
			return o, badRequest("at most %d pinned certificates", config.MaxPins)
		}
		if d.TLS.CAFile != nil {
			if p := config.CAFileProblem(*d.TLS.CAFile); p != "" {
				return o, badRequest("%s", p)
			}
			o.TLS.CAFile = *d.TLS.CAFile
		}
		if len(o.TLS.Pins) == 0 && o.TLS.CAFile == "" {
			o.TLS = nil
		}
	}
	if l := d.Limits; l != nil {
		check := func(name string, v, max int64) (int64, error) {
			if v < 1 || v > max {
				return 0, badRequest("limits.%s must be 1 to %d", name, max)
			}
			return v, nil
		}
		var err error
		if l.RequestBytes != nil {
			if o.Limits.RequestBytes, err = check("request_bytes", *l.RequestBytes, config.MaxRequestBytes); err != nil {
				return o, err
			}
		}
		if l.ResponseBytes != nil {
			if o.Limits.ResponseBytes, err = check("response_bytes", *l.ResponseBytes, config.MaxResponseBytes); err != nil {
				return o, err
			}
		}
		if l.TimeoutSeconds != nil {
			v, err := check("timeout_seconds", int64(*l.TimeoutSeconds), config.MaxTimeoutSeconds)
			if err != nil {
				return o, err
			}
			o.Limits.TimeoutSeconds = int(v)
		}
	}
	return o, nil
}

type authEditDoc struct {
	Header string `json:"header"`
	Env    string `json:"env"`
	Prefix string `json:"prefix"`
}

type envEditResponse struct {
	APIVersion int    `json:"api_version"`
	Applied    bool   `json:"applied"`
	Target     string `json:"target"`
	File       string `json:"file"`
	BaseHash   string `json:"base_hash"`
	ResultHash string `json:"result_hash"`
	// Diff is the unified diff of the file; Valid and Problems the
	// validation of the result, which is never written when invalid.
	Diff     string   `json:"diff"`
	Valid    bool     `json:"valid"`
	Problems []string `json:"problems"`
	Change   string   `json:"change"`
	// Environment is the id the new or changed environment has.
	Environment string `json:"environment"`
}

func (t *tryItState) editEndpoint(w http.ResponseWriter, r *http.Request) error {
	var req envEditRequest
	if err := t.checkPost(w, r, &req); err != nil {
		return err
	}
	if req.Apply != (req.ResultHash != "") {
		return badRequest("apply needs result_hash (the result_hash of the preview being confirmed), and only apply takes it")
	}
	s, err := t.scope(req.Repository)
	if err != nil {
		return err
	}
	f, ok := t.editFile(s, req.Target)
	if !ok {
		return badRequest("target must be %q, or %q on a project server", sourceRepository, sourceProject)
	}
	if !f.writable {
		return s.opt.noCheckoutError("writing its graphvein.yaml")
	}
	if req.Options != nil {
		if req.Environment != (envEditDoc{}) {
			return badRequest("an edit takes environment (add) or options (change an environment's options), not both")
		}
		return t.optionsEdit(w, s, f, req)
	}
	e := req.Environment
	if p := config.EnvironmentNameProblem(e.Name); p != "" {
		return badRequest("environment name %s", p)
	}
	base, err := config.ParseBaseURL(e.BaseURL)
	if err != nil {
		return badRequest("%v", err)
	}
	var auth *config.Auth
	if e.Auth != nil {
		auth = &config.Auth{Header: e.Auth.Header, Env: e.Auth.Env, Prefix: e.Auth.Prefix}
	}

	t.editMu.Lock()
	defer t.editMu.Unlock()
	if req.Target == sourceRepository {
		// graphvein.yaml is also written by the path value and Settings
		// edits, which hold explorerEditMu (ADR 0088).
		explorerEditMu.Lock()
		defer explorerEditMu.Unlock()
	}
	data, err := readEditFile(f.path)
	if err != nil {
		return err
	}
	baseHash := editHash(data)
	if req.BaseHash != baseHash {
		return &apiError{http.StatusConflict, "file_changed", filepath.Base(f.path) + " changed on disk since this edit was prepared; reload the environments and add it again"}
	}
	out := envEditResponse{Target: req.Target, File: filepath.Base(f.path), BaseHash: baseHash, Problems: []string{}}
	var next []byte
	var verr error
	switch req.Target {
	case sourceRepository:
		if p := config.LabelProblem(e.Label); p != "" {
			return badRequest("environment label %s", p)
		}
		next, err = config.AddTryItEnvironment(data, config.Environment{Name: e.Name, Label: e.Label, BaseURL: base, Auth: auth})
		if err != nil {
			return &apiError{http.StatusBadRequest, "edit_refused", err.Error()}
		}
		out.Environment = sourceRepository + ":" + e.Name
		out.Change = fmt.Sprintf("adds environment %s (label %s, %s) to graphvein.yaml of %s", e.Name, e.Label, base.Text, s.opt.Repository)
		_, verr = config.Parse(f.path, next)
	case sourceProject:
		var existing *config.ProjectEnvironment
		for i := range s.project.TryIt {
			if s.project.TryIt[i].Name == e.Name {
				existing = &s.project.TryIt[i]
			}
		}
		if existing != nil {
			if (e.Label != "" && e.Label != existing.Label) || auth != nil {
				return &apiError{http.StatusConflict, "environment_exists", fmt.Sprintf("project environment %s exists (label %s); adding a base URL for %s keeps its label and auth, so send neither", e.Name, existing.Label, s.opt.Repository)}
			}
			next, err = config.SetProjectTryItBaseURL(data, e.Name, s.opt.Repository, base)
			out.Change = fmt.Sprintf("sets the base URL of %s in project environment %s to %s", s.opt.Repository, e.Name, base.Text)
		} else {
			if p := config.LabelProblem(e.Label); p != "" {
				return badRequest("environment label %s", p)
			}
			next, err = config.AddProjectTryItEnvironment(data, config.ProjectEnvironment{Name: e.Name, Label: e.Label, Auth: auth,
				BaseURLs: []config.MemberBaseURL{{Repository: s.opt.Repository, BaseURL: base}}})
			out.Change = fmt.Sprintf("adds project environment %s (label %s) with %s at %s", e.Name, e.Label, s.opt.Repository, base.Text)
		}
		if err != nil {
			return &apiError{http.StatusBadRequest, "edit_refused", err.Error()}
		}
		out.Environment = sourceProject + ":" + e.Name
		local, lerr := readEditFile(filepath.Join(s.project.Dir, config.ProjectLocalFile))
		if lerr != nil {
			return lerr
		}
		_, verr = config.ParseProject(s.project.Dir, next, local)
	}
	return t.finishEdit(w, s, f, req, data, next, verr, out)
}

// finishEdit answers an edit with the diff and validation of next, and
// with apply writes it atomically when it is the previewed, valid result.
func (t *tryItState) finishEdit(w http.ResponseWriter, s tryScope, f editTarget, req envEditRequest, data, next []byte, verr error, out envEditResponse) error {
	out.ResultHash = editHash(next)
	old := ""
	if data != nil {
		old = string(data)
	}
	out.Diff = unifiedDiff(out.File, old, string(next))
	out.Valid = verr == nil
	if verr != nil {
		out.Problems = append(out.Problems, strings.ReplaceAll(verr.Error(), filepath.Dir(f.path)+string(filepath.Separator), ""))
	}
	if !req.Apply {
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	switch {
	case req.ResultHash != out.ResultHash:
		return &apiError{http.StatusConflict, "preview_changed", "the edit no longer gives the previewed file; preview it again"}
	case !out.Valid:
		return &apiError{http.StatusUnprocessableEntity, "invalid_config", "the edited file does not validate, so nothing was written: " + out.Problems[0]}
	}
	if err := config.WriteFileAtomic(f.path, next); err != nil {
		return err
	}
	out.Applied = true
	t.opt.logger().Info("try-it environment written", "file", out.File, "environment", out.Environment, "repository", s.opt.Repository)
	writeJSON(w, http.StatusOK, out)
	return nil
}

// optionsEdit replaces the options of an existing environment in the
// target file (row TI-48): certificate pins, CA file, system proxy and
// limits, through the same preview, hash precondition and atomic write as
// adding one. The environment's key changes with its TLS or proxy
// options, so it must be selected again (row TI-25).
func (t *tryItState) optionsEdit(w http.ResponseWriter, s tryScope, f editTarget, req envEditRequest) error {
	name := req.Options.Name
	if p := config.EnvironmentNameProblem(name); p != "" {
		return badRequest("environment name %s", p)
	}
	o, err := req.Options.environmentOptions()
	if err != nil {
		return err
	}
	t.editMu.Lock()
	defer t.editMu.Unlock()
	if req.Target == sourceRepository {
		// graphvein.yaml is also written by the path value and Settings
		// edits, which hold explorerEditMu (ADR 0088).
		explorerEditMu.Lock()
		defer explorerEditMu.Unlock()
	}
	data, err := readEditFile(f.path)
	if err != nil {
		return err
	}
	baseHash := editHash(data)
	if req.BaseHash != baseHash {
		return &apiError{http.StatusConflict, "file_changed", filepath.Base(f.path) + " changed on disk since this edit was prepared; reload the environments and edit again"}
	}
	if data == nil {
		return &apiError{http.StatusNotFound, "unknown_environment", fmt.Sprintf("%s does not exist, so it has no environment %s", filepath.Base(f.path), name)}
	}
	next, err := config.SetTryItEnvironmentOptions(data, name, o)
	if err != nil {
		return &apiError{http.StatusBadRequest, "edit_refused", err.Error()}
	}
	out := envEditResponse{Target: req.Target, File: filepath.Base(f.path), BaseHash: baseHash, Problems: []string{},
		Environment: req.Target + ":" + name, Change: fmt.Sprintf("sets the try-it options of environment %s in %s", name, filepath.Base(f.path))}
	var verr error
	switch req.Target {
	case sourceRepository:
		_, verr = config.Parse(f.path, next)
	case sourceProject:
		local, lerr := readEditFile(filepath.Join(s.project.Dir, config.ProjectLocalFile))
		if lerr != nil {
			return lerr
		}
		_, verr = config.ParseProject(s.project.Dir, next, local)
	}
	return t.finishEdit(w, s, f, req, data, next, verr, out)
}
