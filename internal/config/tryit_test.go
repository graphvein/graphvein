package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTryItEnvironments reads valid tryit sections and checks the decoded
// environments, base URL normalisation included.
func TestTryItEnvironments(t *testing.T) {
	root := t.TempDir()
	input := `version: 1
tryit:
  environments:
    - name: local
      label: local
      base_url: HTTP://LocalHost:8000/api/
    - name: prod
      label: Production
      base_url: https://api.example.com
      auth:
        header: Authorization
        env: API_TOKEN
        prefix: "Bearer "
`
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	envs := c.TryIt.Environments
	if len(envs) != 2 {
		t.Fatalf("environments %+v", envs)
	}
	if e := envs[0]; e.Name != "local" || e.BaseURL.Text != "http://localhost:8000/api" || e.BaseURL.Port != 8000 || e.BaseURL.Path != "/api" || e.Auth != nil || Production(e.Label) || e.Line != 4 {
		t.Errorf("local: %+v", e)
	}
	if e := envs[1]; e.BaseURL.Text != "https://api.example.com" || e.BaseURL.Port != 443 || !Production(e.Label) ||
		e.Auth == nil || *e.Auth != (Auth{Header: "Authorization", Env: "API_TOKEN", Prefix: "Bearer "}) {
		t.Errorf("prod: %+v", e)
	}
}

// TestTryItErrors checks that every invalid form fails to load with the
// file and the line of the offending value.
func TestTryItErrors(t *testing.T) {
	root := t.TempDir()
	head := "version: 1\ntryit:\n  environments:\n"
	env := func(lines ...string) string {
		return head + "    - " + strings.Join(lines, "\n      ") + "\n"
	}
	for _, tc := range []struct{ input, msg string }{
		{"version: 1\ntryit: []\n", "line 2: tryit must be a mapping"},
		{"version: 1\ntryit:\n  envs: []\n", "line 3: unknown tryit key envs"},
		{"version: 1\ntryit:\n  environments: {}\n", "line 3: tryit.environments must be a list"},
		{env("name: a", "label: local"), "line 4: a tryit environment needs base_url"},
		{env("name: a", "base_url: http://x"), "needs label"},
		{env("label: l", "base_url: http://x"), "needs name"},
		{env("name: a b", "label: l", "base_url: http://x"), "line 4: environment name may contain only"},
		{env("name: a", "label: ''", "base_url: http://x"), "environment label must be"},
		{env("name: a", "label: l", "base_url: ftp://x"), "line 6: base_url \"ftp://x\" must start with http:// or https://"},
		{env("name: a", "label: l", "base_url: http://u:p@x"), "must not contain user info"},
		{env("name: a", "label: l", "base_url: http://x/?q=1"), "must not have a query or fragment"},
		{env("name: a", "label: l", "base_url: http://x#f"), "must not have a query or fragment"},
		{env("name: a", "label: l", "base_url: http://x:0"), "port must be 1-65535"},
		{env("name: a", "label: l", "base_url: http://x:99999"), "port must be 1-65535"},
		{env("name: a", "label: l", "base_url: http://x/a/../b"), "must not contain . or .."},
		{env("name: a", "label: l", "base_url: http://"), "has no host"},
		{env("name: a", "label: l", "base_url: 8000"), "line 6: base_url must be a string"},
		{env("name: a", "label: l", "base_url: http://x", "port: 1"), "unknown tryit environment key port"},
		{env("name: a", "label: l", "base_url: http://x", "auth: {header: Authorization}"), "auth needs header and env"},
		{env("name: a", "label: l", "base_url: http://x", "auth: {header: Host, env: X}"), "the HTTP client sets Host itself"},
		{env("name: a", "label: l", "base_url: http://x", "auth: {header: 'a b', env: X}"), "RFC 9110 token"},
		{env("name: a", "label: l", "base_url: http://x", "auth: {header: X-Key, env: 'sk-123 value'}"), "auth.env must name an environment variable"},
		{env("name: a", "label: l", "base_url: http://x", "auth: {header: X-Key, env: K, value: v}"), "unknown auth key value"},
		{head + "    - {name: a, label: l, base_url: 'http://x'}\n    - {name: a, label: l, base_url: 'http://y'}\n", "line 5: environment a is defined twice"},
		{env("name: &n a", "label: l", "base_url: http://x"), "aliases and anchors"},
	} {
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(tc.input), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(root)
		if err == nil || !strings.Contains(err.Error(), tc.msg) || !strings.Contains(err.Error(), "graphvein.yaml: line ") {
			t.Errorf("%q: err %v, want %q", tc.input, err, tc.msg)
		}
	}
}

// TestParseBaseURL checks normalisation and the origin of accepted URLs.
func TestParseBaseURL(t *testing.T) {
	if _, err := ParseBaseURL("http://[fe80::1%25eth0]:8000"); err == nil || !strings.Contains(err.Error(), "IPv6 zones") {
		t.Errorf("zone: %v", err)
	}
	for _, tc := range []struct{ in, text, origin string }{
		{"http://127.0.0.1:8000", "http://127.0.0.1:8000", "http://127.0.0.1:8000"},
		{"http://[::1]:8080/v1/", "http://[::1]:8080/v1", "http://[::1]:8080"},
		{"https://api.example.com:443/", "https://api.example.com", "https://api.example.com"},
		{"http://example.com:80", "http://example.com", "http://example.com"},
		{"https://Example.COM/Base", "https://example.com/Base", "https://example.com"},
	} {
		b, err := ParseBaseURL(tc.in)
		if err != nil || b.Text != tc.text || b.Origin() != tc.origin {
			t.Errorf("%s: %+v %v", tc.in, b, err)
		}
	}
	for _, in := range []string{"", "http://exa mple.com", "http://ex!ample.com", "http://x:", "mailto:a@b", "http:///path", "http://[fe80::1%25eth0]:8000", "http://x/a/%2e%2e/b", "http://x/%2E"} {
		if _, err := ParseBaseURL(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

// TestAddTryItEnvironment checks the writer: a new file, and an append that
// keeps comments and the rest of the file.
func TestAddTryItEnvironment(t *testing.T) {
	b, _ := ParseBaseURL("http://localhost:8000")
	out, err := AddTryItEnvironment(nil, Environment{Name: "local", Label: "local", BaseURL: b})
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\ntryit:\n  environments:\n    - name: local\n      label: local\n      base_url: http://localhost:8000\n"
	if string(out) != want {
		t.Fatalf("new file:\n%s\nwant:\n%s", out, want)
	}
	in := "# team settings\nversion: 1\nrepository: r # the id\ntryit:\n  environments:\n    - name: local\n      label: local\n      base_url: http://localhost:8000\n"
	b2, _ := ParseBaseURL("https://staging.example.com")
	out, err = AddTryItEnvironment([]byte(in), Environment{Name: "staging", Label: "staging", BaseURL: b2, Auth: &Auth{Header: "X-Api-Key", Env: "STAGING_KEY"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), in) || !strings.Contains(string(out), "    - name: staging\n      label: staging\n      base_url: https://staging.example.com\n      auth:\n        header: X-Api-Key\n        env: STAGING_KEY\n") {
		t.Fatalf("append:\n%s", out)
	}
	c, err := Parse("graphvein.yaml", out)
	if err != nil || len(c.TryIt.Environments) != 2 || c.Repository != "r" {
		t.Fatalf("result does not parse: %+v %v", c.TryIt, err)
	}
}

// TestProjectTryIt checks project environments: parsing, member checks and
// the two manifest writers.
func TestProjectTryIt(t *testing.T) {
	dir := t.TempDir()
	for _, m := range []string{"api", "web"} {
		if err := os.Mkdir(filepath.Join(dir, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := "version: 1\nname: p\nmembers:\n  - {repository: api, path: api}\n  - {repository: web, path: web}\n"
	withTryIt := manifest + "tryit:\n  environments:\n    - name: staging\n      label: staging\n      base_urls:\n        api: https://api.staging.example.com\n"
	p, err := ParseProject(dir, []byte(withTryIt), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.TryIt) != 1 {
		t.Fatalf("%+v", p.TryIt)
	}
	if b, ok := p.TryIt[0].For("api"); !ok || b.Text != "https://api.staging.example.com" {
		t.Errorf("api: %+v %v", b, ok)
	}
	if _, ok := p.TryIt[0].For("web"); ok {
		t.Error("web has no base URL in staging")
	}
	for _, tc := range []struct{ input, msg string }{
		{manifest + "tryit:\n  environments:\n    - name: s\n      label: s\n      base_urls:\n        other: http://x\n", "line 8: tryit environment s names other, which is not a member"},
		{manifest + "tryit:\n  environments:\n    - name: s\n      label: s\n      base_urls: {}\n", "line 8: a project tryit environment needs name, label and base_urls"},
		{manifest + "tryit:\n  environments:\n    - name: s\n      label: s\n      base_urls:\n        api: http://u:p@x\n", "line 11: base_url must not contain user info"},
	} {
		if _, err := ParseProject(dir, []byte(tc.input), nil); err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%q: err %v, want %q", tc.input, err, tc.msg)
		}
	}
	web, _ := ParseBaseURL("https://staging.example.com")
	out, err := SetProjectTryItBaseURL([]byte(withTryIt), "staging", "web", web)
	if err != nil {
		t.Fatal(err)
	}
	if p, err = ParseProject(dir, out, nil); err != nil {
		t.Fatal(err)
	}
	if b, ok := p.TryIt[0].For("web"); !ok || b.Text != web.Text {
		t.Errorf("set web: %s", out)
	}
	api, _ := ParseBaseURL("http://localhost:8000")
	out, err = AddProjectTryItEnvironment(out, ProjectEnvironment{Name: "local", Label: "local", BaseURLs: []MemberBaseURL{{"api", api}}})
	if err != nil {
		t.Fatal(err)
	}
	if p, err = ParseProject(dir, out, nil); err != nil || len(p.TryIt) != 2 || p.TryIt[1].Name != "local" {
		t.Fatalf("add: %s %v", out, err)
	}
	if _, err := SetProjectTryItBaseURL(out, "missing", "api", api); err == nil {
		t.Error("set on a missing environment accepted")
	}
	// Options on a project environment, written and read back.
	out, err = SetTryItEnvironmentOptions(out, "local", EnvironmentOptions{TLS: &TLSOptions{CAFile: "ca.pem"}, UseSystemProxy: true})
	if err != nil {
		t.Fatal(err)
	}
	if p, err = ParseProject(dir, out, nil); err != nil || p.TryIt[1].Options.TLS == nil || p.TryIt[1].Options.TLS.CAFile != "ca.pem" || !p.TryIt[1].Options.UseSystemProxy {
		t.Fatalf("project options: %s %v", out, err)
	}
	// A project environment's pin names its origin (one of its members').
	fp := strings.Repeat("ab", 32)
	pinned, err := SetTryItEnvironmentOptions(out, "local", EnvironmentOptions{TLS: &TLSOptions{Pins: []Pin{{Origin: "http://localhost:8000", SHA256: fp}}}})
	if err != nil {
		t.Fatal(err)
	}
	if p, err = ParseProject(dir, pinned, nil); err != nil || len(p.TryIt[1].Options.TLS.PinsFor("http://localhost:8000")) != 1 || len(p.TryIt[1].Options.TLS.PinsFor("https://other:1")) != 0 {
		t.Fatalf("project pin: %s %v", pinned, err)
	}
	for _, tc := range []struct {
		opts EnvironmentOptions
		msg  string
	}{
		{EnvironmentOptions{TLS: &TLSOptions{Pins: []Pin{{SHA256: fp}}}}, "each pin names its origin"},
		{EnvironmentOptions{TLS: &TLSOptions{Pins: []Pin{{Origin: "https://other.test", SHA256: fp}}}}, "not a base URL origin of this environment"},
	} {
		bad, err := SetTryItEnvironmentOptions(out, "local", tc.opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseProject(dir, bad, nil); err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%v, want %q", err, tc.msg)
		}
	}
}

// TestTryItOptions checks the per-environment options (tls, use_system_proxy,
// limits): decoding with normalisation, every invalid form with its line,
// and the writer that replaces them.
func TestTryItOptions(t *testing.T) {
	fp := strings.Repeat("ab", 32)
	colons := strings.ToUpper(strings.TrimSuffix(strings.Repeat("AB:", 32), ":"))
	in := "version: 1\ntryit:\n  environments:\n    - name: local\n      label: local\n      base_url: https://127.0.0.1:8443\n" +
		"      tls:\n        pinned_sha256: ['sha256:" + colons + "']\n        ca_file: certs/root.pem\n" +
		"      use_system_proxy: true\n      limits: {request_bytes: 10, response_bytes: 268435456, timeout_seconds: 600}\n"
	c, err := Parse("graphvein.yaml", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	o := c.TryIt.Environments[0].Options
	if o.TLS == nil || strings.Join(o.TLS.PinsFor("https://127.0.0.1:8443"), ",") != fp || o.TLS.CAFile != "certs/root.pem" || !o.UseSystemProxy ||
		o.Limits != (Limits{RequestBytes: 10, ResponseBytes: MaxResponseBytes, TimeoutSeconds: MaxTimeoutSeconds}) {
		t.Fatalf("%+v %+v", o, o.TLS)
	}
	if e := (Limits{RequestBytes: 10}).Effective(); e != (Limits{10, DefaultResponseBytes, DefaultTimeoutSeconds}) {
		t.Errorf("effective %+v", e)
	}
	head := "version: 1\ntryit:\n  environments:\n    - name: a\n      label: l\n      base_url: http://x\n      "
	for _, tc := range []struct{ input, msg string }{
		{head + "tls: {}\n", "line 7: tls needs pinned_sha256 or ca_file"},
		{head + "tls: {pinned_sha256: [abc]}\n", "line 7: fingerprint \"abc\" is not a SHA-256 fingerprint"},
		{head + "tls: {pinned_sha256: " + fp + "}\n", "tls.pinned_sha256 must be a list"},
		{head + "tls: {pinned_sha256: [" + fp + ", '" + colons + "']}\n", "is pinned twice"},
		{head + "tls: {ca_file: ''}\n", "ca_file must be a path"},
		{head + "tls: {pinned_sha256: [{origin: 'https://other.test', sha256: " + fp + "}]}\n", "not a base URL origin of this environment"},
		{head + "tls: {pinned_sha256: [{origin: 'http://x/p', sha256: " + fp + "}]}\n", "must not have a path"},
		{head + "tls: {insecure: true}\n", "unknown tls key insecure"},
		{head + "use_system_proxy: yes please\n", "use_system_proxy must be true or false"},
		{head + "limits: {request_bytes: 67108865}\n", "limits.request_bytes must be 1 to 67108864"},
		{head + "limits: {response_bytes: 0}\n", "limits.response_bytes must be 1 to 268435456"},
		{head + "limits: {timeout_seconds: 601}\n", "limits.timeout_seconds must be 1 to 600"},
		{head + "limits: {timeout_seconds: '30'}\n", "limits.timeout_seconds must be a whole number"},
		{head + "limits: {in_flight: 9}\n", "unknown limits key in_flight"},
	} {
		_, err := Parse("graphvein.yaml", []byte(tc.input))
		if err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%q: err %v, want %q", tc.input, err, tc.msg)
		}
	}
	// The writer replaces the options and keeps everything else.
	out, err := SetTryItEnvironmentOptions([]byte(in), "local", EnvironmentOptions{TLS: &TLSOptions{Pins: []Pin{{SHA256: fp}}}, Limits: Limits{TimeoutSeconds: 90}})
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\ntryit:\n  environments:\n    - name: local\n      label: local\n      base_url: https://127.0.0.1:8443\n" +
		"      tls:\n        pinned_sha256:\n          - " + fp + "\n      limits:\n        timeout_seconds: 90\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	if out, err = SetTryItEnvironmentOptions(out, "local", EnvironmentOptions{}); err != nil || strings.Contains(string(out), "tls") || strings.Contains(string(out), "limits") {
		t.Fatalf("clear: %s %v", out, err)
	}
	if _, err := SetTryItEnvironmentOptions(out, "nope", EnvironmentOptions{}); err == nil {
		t.Fatal("an unknown environment accepted")
	}
	// A new environment is written with its options.
	b, _ := ParseBaseURL("https://127.0.0.1:9443")
	pm, err := AddTryItEnvironment(nil, Environment{Name: "n", Label: "l", BaseURL: b, Options: EnvironmentOptions{UseSystemProxy: true}})
	if err != nil || !strings.Contains(string(pm), "      use_system_proxy: true\n") {
		t.Fatalf("%s %v", pm, err)
	}
}
