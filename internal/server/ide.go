package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
)

// This file holds the editor choice of ADR 0050: the editor parameter of
// the endpoints that present nodes, the precedence that picks the
// editor of a response, and GET /api/v0/ide, which lists the presets for
// the web UI's editor picker.

// Values of ide.source: which precedence rule chose the editor.
const (
	ideSourceRequest    = "request"    // the editor parameter (the browser's choice)
	ideSourceRepository = "repository" // graphvein.yaml's ide setting
	ideSourceBuiltin    = "builtin"    // no ide setting: Graphvein's default
)

// editorChoice is a preset named by the editor parameter.
type editorChoice struct {
	name string
	ide  config.IDE
}

// editorParam parses the optional editor parameter. A present value must
// name a preset: an unknown or empty value is a client error and never
// falls back to another editor, so a stale choice cannot produce a link
// that opens a different editor than the one the client asked for.
func editorParam(q url.Values) (*editorChoice, error) {
	v, present, err := param(q, "editor")
	if err != nil || !present {
		return nil, err
	}
	ide, ok := config.PresetIDE(v)
	if !ok {
		return nil, badRequest("editor must be one of %s; not %q", strings.Join(config.PresetNames(), ", "), v)
	}
	return &editorChoice{name: v, ide: ide}, nil
}

// resolveIDE applies the precedence of ADR 0050 and returns the ide status
// of a response with the settings that build its links. A requested editor
// wins over the repository, including a custom template, and still builds
// links when graphvein.yaml fails to load (the error stays in the status).
// Without one, a load error drops every link; the settings returned then
// only serve the file states.
func resolveIDE(opt Options, editor *editorChoice) (ideStatus, config.IDE) {
	if opt.noCheckout {
		// Editor links open files of the checkout, which this server does
		// not have: none, whatever the requested editor (T022 B9).
		msg := fmt.Sprintf("member %s has no checkout here; editor links need its files", opt.Repository)
		return ideStatus{Available: false, Error: &msg, Source: ideSourceRepository}, config.DefaultIDE()
	}
	cfg, err := config.Load(opt.Root)
	var loadErr *string
	if err != nil {
		msg := err.Error()
		loadErr = &msg
	}
	if editor != nil {
		name, label := editor.name, editor.ide.Label
		return ideStatus{Available: true, Error: loadErr, Label: &label, Editor: &name, Source: ideSourceRequest}, editor.ide
	}
	if err != nil {
		// The file exists but does not load, so whether it sets an editor
		// is unknown; the repository setting is what failed.
		return ideStatus{Available: false, Error: loadErr, Source: ideSourceRepository}, config.DefaultIDE()
	}
	return repositoryStatus(cfg), cfg.IDE
}

// repositoryStatus is the ide status of a loaded repository configuration.
func repositoryStatus(cfg config.Config) ideStatus {
	status := ideStatus{Available: true, Source: ideSourceBuiltin}
	if cfg.IDEGiven {
		status.Source = ideSourceRepository
	}
	if cfg.IDE.Label != "" {
		label := cfg.IDE.Label
		status.Label = &label
	}
	if !cfg.IDECustom {
		editor := cfg.IDE.Editor
		status.Editor = &editor
	}
	return status
}

// GET /api/v0/ide

// ideResponse is the body of GET /api/v0/ide. Field order is the
// documented order (docs/cli/serve.md).
type ideResponse struct {
	APIVersion int             `json:"api_version"`
	Presets    []presetEntry   `json:"presets"`
	Repository repositoryEntry `json:"repository"`
}

// presetEntry is one row of the preset table, in table order.
type presetEntry struct {
	Editor string `json:"editor"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Note   string `json:"note"`
	// ShortNote is the note's caveat in a few words ("macOS only") for a
	// compact row; "" for a verified preset.
	ShortNote string `json:"short_note"`
}

// repositoryEntry is the team default exactly as graphvein.yaml gives it,
// the editor that applies when the browser chose none.
type repositoryEntry struct {
	Available bool    `json:"available"`
	Error     *string `json:"error"`
	Editor    *string `json:"editor"`
	Label     *string `json:"label"`
	Custom    bool    `json:"custom"`
	Source    string  `json:"source"`
}

// ideHandler answers GET /api/v0/ide. It reads graphvein.yaml on every
// request, as the node endpoints do, and never the index, so it has no
// consistency check.
type ideHandler struct {
	opt Options
}

func (h ideHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	out := ideResponse{APIVersion: 0, Presets: []presetEntry{}}
	for _, p := range config.Presets() {
		out.Presets = append(out.Presets, presetEntry{Editor: p.Name, Label: p.Label, Status: p.Status, Note: p.Note, ShortNote: p.ShortNote})
	}
	status, _ := resolveIDE(h.opt, nil)
	cfg, err := h.opt.loadConfig()
	out.Repository = repositoryEntry{
		Available: status.Available, Error: status.Error, Editor: status.Editor,
		Label: status.Label, Custom: err == nil && cfg.IDECustom, Source: status.Source,
	}
	writeJSON(w, http.StatusOK, out)
}
