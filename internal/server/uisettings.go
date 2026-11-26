package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// GET /api/v0/ui

// uiResponse is the body of GET /api/v0/ui: the web UI defaults the
// repository sets in graphvein.yaml (T025b). Field order is the documented
// order (docs/cli/serve.md).
type uiResponse struct {
	APIVersion int `json:"api_version"`
	// Labels is ui.labels ("plain" or "technical"), or null when
	// graphvein.yaml sets none or fails to load; the UI then uses plain.
	// A browser's own choice wins either way.
	Labels *string `json:"labels"`
	// ImpactDepth is ui.impact_depth (1 to 10), or null when graphvein.yaml
	// sets none or fails to load; the Impact tab then starts at 3 (T042).
	ImpactDepth *int `json:"impact_depth"`
	// ChainDepth is ui.chain_depth (1 to 10), or null; the architecture
	// chain then starts at 6 (T042).
	ChainDepth *int `json:"chain_depth"`
	// Error is the load error of graphvein.yaml, else null.
	Error *string `json:"error"`
	// ReportURL is the public wrong-fact issue form set when the binary
	// was built (the public-repository switch, ADR 0089), or null: the UI
	// then renders no "Report a wrong fact" flag. No repository setting
	// can change it.
	ReportURL *string `json:"report_url"`
}

// ReportURL checks a build-time wrong-fact issue form URL: an absolute
// https URL with a host, no user information, query or fragment (the UI
// adds the form's own query). It returns the URL as given.
func ReportURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return "", fmt.Errorf("report URL %q: %w", raw, err)
	case u.Scheme != "https" || u.Host == "" || u.Opaque != "":
		return "", fmt.Errorf("report URL %q is not an absolute https URL", raw)
	case u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, " \t\n\"'<>"):
		return "", errors.New("report URL must have no user information, query, fragment, spaces or quotes")
	}
	return raw, nil
}

// uiHandler answers GET /api/v0/ui. Like GET /api/v0/ide it reads
// graphvein.yaml on every request and never the index, so it has no
// consistency check.
type uiHandler struct {
	opt Options
}

func (h uiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	out := uiResponse{APIVersion: 0}
	if report, err := ReportURL(h.opt.ReportURL); h.opt.ReportURL != "" && err == nil {
		out.ReportURL = &report
	}
	cfg, err := h.opt.loadConfig()
	if err != nil {
		msg := err.Error()
		out.Error = &msg
	} else {
		if cfg.UI.Labels != "" {
			labels := cfg.UI.Labels
			out.Labels = &labels
		}
		if cfg.UI.ImpactDepth != 0 {
			d := cfg.UI.ImpactDepth
			out.ImpactDepth = &d
		}
		if cfg.UI.ChainDepth != 0 {
			d := cfg.UI.ChainDepth
			out.ChainDepth = &d
		}
	}
	writeJSON(w, http.StatusOK, out)
}
