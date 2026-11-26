package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/traverse"
)

// TestUIEndpoint checks GET /api/v0/ui (T025b): labels is null without a
// setting, the configured style with one, and null with the load error
// when graphvein.yaml fails to load; the usual headers, Host and Origin
// checks and GET only.
func TestUIEndpoint(t *testing.T) {
	root := t.TempDir()
	opt := testOptions(root, nil)
	decode := func(t *testing.T) uiResponse {
		t.Helper()
		rec := get(t, opt, http.MethodGet, "/api/v0/ui", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d %s", rec.Code, rec.Body.String())
		}
		for _, h := range securityHeaders {
			if got := rec.Header().Get(h[0]); got != h[1] {
				t.Errorf("header %s = %q", h[0], got)
			}
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
		}
		if body := rec.Body.String(); !strings.Contains(body, `"api_version":0`) || !strings.Contains(body, `"labels":`) || !strings.Contains(body, `"error":`) {
			t.Errorf("body %s", body)
		}
		var out uiResponse
		dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&out); err != nil {
			t.Fatal(err, rec.Body.String())
		}
		return out
	}
	if got := decode(t); got.Labels != nil || got.Error != nil {
		t.Errorf("no file: %+v", got)
	}
	writeConfig(t, root, "version: 1\n")
	if got := decode(t); got.Labels != nil || got.Error != nil {
		t.Errorf("no ui: %+v", got)
	}
	for _, style := range []string{"plain", "technical"} {
		writeConfig(t, root, "version: 1\nui: {labels: "+style+"}\n")
		if got := decode(t); got.Labels == nil || *got.Labels != style || got.Error != nil {
			t.Errorf("%s: %+v", style, got)
		}
	}
	writeConfig(t, root, "version: 1\nui: {labels: jargon}\n")
	if got := decode(t); got.Labels != nil || got.Error == nil || !strings.Contains(*got.Error, "ui.labels must be plain or technical") {
		t.Errorf("load error: %+v", got)
	}
	// T042: the depth defaults, null when unset, out of range a load error
	// that nulls every field.
	writeConfig(t, root, "version: 1\nui: {labels: plain}\n")
	if got := decode(t); got.ImpactDepth != nil || got.ChainDepth != nil {
		t.Errorf("no depths: %+v", got)
	}
	writeConfig(t, root, "version: 1\nui: {impact_depth: 5, chain_depth: 9}\n")
	if got := decode(t); got.Labels != nil || got.ImpactDepth == nil || *got.ImpactDepth != 5 || got.ChainDepth == nil || *got.ChainDepth != 9 || got.Error != nil {
		t.Errorf("depths: %+v", got)
	}
	if body := get(t, opt, http.MethodGet, "/api/v0/ui", nil).Body.String(); !strings.Contains(body, `"labels":null,"impact_depth":5,"chain_depth":9,"error":null`) {
		t.Errorf("field order: %s", body)
	}
	writeConfig(t, root, "version: 1\nui: {labels: plain, chain_depth: 11}\n")
	if got := decode(t); got.Labels != nil || got.ImpactDepth != nil || got.ChainDepth != nil || got.Error == nil || !strings.Contains(*got.Error, "ui.chain_depth must be an integer from 1 to 10") {
		t.Errorf("depth load error: %+v", got)
	}

	for _, tc := range []struct {
		header map[string]string
		code   string
	}{
		{map[string]string{"Host": "evil.example:7420"}, "host_not_allowed"},
		{map[string]string{"Origin": "http://evil.example"}, "cross_site_request"},
	} {
		rec := get(t, opt, http.MethodGet, "/api/v0/ui", tc.header)
		if rec.Code != http.StatusForbidden || errorCode(t, rec) != tc.code {
			t.Errorf("%v: status %d %s", tc.header, rec.Code, rec.Body.String())
		}
	}
	if rec := get(t, opt, http.MethodPost, "/api/v0/ui", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d", rec.Code)
	}
}

// TestUIDepthDefaultsAgree keeps the depth bounds and defaults of
// internal/config (which imports neither package) equal to the walks'
// own (T042).
func TestUIDepthDefaultsAgree(t *testing.T) {
	if config.MaxUIDepth != changes.MaxDepth {
		t.Errorf("max: config %d, changes %d", config.MaxUIDepth, changes.MaxDepth)
	}
	if config.DefaultImpactDepth != changes.DefaultDepth {
		t.Errorf("impact default: config %d, changes %d", config.DefaultImpactDepth, changes.DefaultDepth)
	}
	if config.DefaultChainDepth != traverse.ChainDefaultDepth {
		t.Errorf("chain default: config %d, traverse %d", config.DefaultChainDepth, traverse.ChainDefaultDepth)
	}
}

// TestUIReportURL checks the public-repository switch (ADR 0089):
// report_url is null without a build-time URL, the URL with one, and null
// for a URL ReportURL refuses; no graphvein.yaml key changes it.
func TestUIReportURL(t *testing.T) {
	root := t.TempDir()
	opt := testOptions(root, nil)
	report := func(opt Options) *string {
		t.Helper()
		rec := get(t, opt, http.MethodGet, "/api/v0/ui", nil)
		var out uiResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatal(err, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"report_url":`) {
			t.Errorf("report_url missing: %s", rec.Body.String())
		}
		return out.ReportURL
	}
	if got := report(opt); got != nil {
		t.Errorf("switch off: %v", *got)
	}
	writeConfig(t, root, "version: 1\nui: {labels: plain}\n")
	const form = "https://github.com/example/project/issues/new"
	opt.ReportURL = form
	if got := report(opt); got == nil || *got != form {
		t.Errorf("switch on: %v", got)
	}
	for _, bad := range []string{"http://github.com/x/issues/new", "https://github.com/x/issues/new?template=a", "https://u:p@github.com/x", "https:///x", "javascript:alert(1)", "https://github.com/x#y", "https://github.com/x y", "/relative"} {
		if _, err := ReportURL(bad); err == nil {
			t.Errorf("ReportURL(%q) accepted", bad)
		}
		opt.ReportURL = bad
		if got := report(opt); got != nil {
			t.Errorf("%q served as %v", bad, *got)
		}
	}
}
