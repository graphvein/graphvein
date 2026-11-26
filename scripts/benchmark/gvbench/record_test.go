package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifySignIn(t *testing.T) {
	cases := []struct {
		agent, out    string
		exit          int
		allow, accept bool
		kind          string
		ok, billed    bool
	}{
		{"claude", `{"loggedIn":true,"authMethod":"claude.ai"}`, 0, false, false, "claude.ai", true, false},
		{"claude", `{"loggedIn":true,"authMethod":"oauth_token"}`, 0, false, false, "oauth_token", true, false},
		{"claude", `{"loggedIn":true,"authMethod":"api_key"}`, 0, false, false, "api_key", false, true},
		{"claude", `{"loggedIn":true,"authMethod":"api_key"}`, 0, true, false, "api_key", true, true},
		{"claude", `{"loggedIn":false,"authMethod":"none"}`, 1, false, true, "none", false, false},
		{"claude", `Logged in`, 0, false, false, "unconfirmed", false, false},
		{"claude", `Logged in`, 0, false, true, "unconfirmed", true, false},
		{"codex", "Not logged in", 1, false, true, "none", false, false},
		{"codex", "Logged in using ChatGPT", 0, false, false, "chatgpt", true, false},
		{"codex", "Logged in using an API key - sk-...", 0, false, false, "api_key", false, true},
		{"codex", "Logged in", 0, false, false, "unconfirmed", false, false},
	}
	for _, c := range cases {
		s := classifySignIn(c.agent, []byte(c.out), c.exit, c.allow, c.accept)
		if s.Kind != c.kind || s.OK != c.ok || s.Billed != c.billed {
			t.Errorf("%s %q: %+v", c.agent, c.out, s)
		}
	}
}

func TestAgentConfigs(t *testing.T) {
	if got := claudeMCPConfig(nil); strings.TrimSpace(got) != `{"mcpServers":{}}` {
		t.Errorf("base mode lists no server: %s", got)
	}
	srv := &MCPServer{Command: `C:\gv\graphvein.exe`, Args: []string{"mcp", "C:/ws/a b"}, Env: map[string]string{"GRAPHVEIN_SQLITE_PATH": `C:\s\"x".db`}}
	var cfg map[string]map[string]map[string]any
	if err := json.Unmarshal([]byte(claudeMCPConfig(srv)), &cfg); err != nil || cfg["mcpServers"]["graphvein"]["command"] != `C:\gv\graphvein.exe` {
		t.Errorf("claude mcp config: %v %v", cfg, err)
	}
	ov := codexOverrides(srv, []string{`web_search="disabled"`})
	joined := strings.Join(ov, "\n")
	for _, want := range []string{
		`approval_policy="never"`,
		`web_search="disabled"`,
		`mcp_servers.graphvein.command="C:\\gv\\graphvein.exe"`,
		`mcp_servers.graphvein.args=["mcp", "C:/ws/a b"]`,
		`mcp_servers.graphvein.env={ "GRAPHVEIN_SQLITE_PATH" = "C:\\s\\\"x\".db" }`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("codex overrides lack %s:\n%s", want, joined)
		}
	}
	if base := codexOverrides(nil, nil); len(base) != 1 {
		t.Errorf("base mode adds no server: %v", base)
	}
	if got := tomlString("a\nb"); got != `"a\u000Ab"` {
		t.Errorf("control characters: %s", got)
	}
	var oc map[string]any
	text := opencodeConfig(OpencodeOptions{Model: "a/m", Steps: 60, Preferred: []string{"x/fp8"}, StepOutput: 100, Context: 1000}, nil)
	if err := json.Unmarshal([]byte(text), &oc); err != nil {
		t.Fatal(err)
	}
	perm := oc["permission"].(map[string]any)
	for _, k := range []string{"edit", "bash", "webfetch", "websearch"} {
		if perm[k] != "deny" {
			t.Errorf("opencode must deny %s: %v", k, perm)
		}
	}
	if len(oc["mcp"].(map[string]any)) != 0 || oc["model"] != "openrouter/a/m" {
		t.Errorf("opencode base config: %s", text)
	}
	if !strings.Contains(text, `"allow_fallbacks":false`) || !strings.Contains(text, `"order":["x/fp8"]`) {
		t.Errorf("provider routing: %s", text)
	}
	text = opencodeConfig(OpencodeOptions{Model: "a/m"}, srv)
	if !strings.Contains(text, `"command":["C:\\gv\\graphvein.exe","mcp","C:/ws/a b"]`) || !strings.Contains(text, `"timeout":60000`) {
		t.Errorf("opencode mcp config: %s", text)
	}
	if got := expand("{store}/{corpus}", map[string]string{"store": "/s", "corpus": "c"}); got != "/s/c" {
		t.Errorf("expand: %s", got)
	}
}

func TestPlan(t *testing.T) {
	a := makePlan([]string{"claude", "codex"}, []string{"base", "mcp"}, []string{"BQ-1", "BQ-2", "BQ-3"}, 2, 7, true)
	b := makePlan([]string{"claude", "codex"}, []string{"base", "mcp"}, []string{"BQ-1", "BQ-2", "BQ-3"}, 2, 7, true)
	c := makePlan([]string{"claude", "codex"}, []string{"base", "mcp"}, []string{"BQ-1", "BQ-2", "BQ-3"}, 2, 8, true)
	if len(a) != 24 {
		t.Fatalf("len %d", len(a))
	}
	same := func(x, y []PlannedRun) bool {
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	if !same(a, b) || same(a, c) {
		t.Error("the order must depend on the seed only")
	}
	for i, r := range a {
		if (i < 12) != (r.Agent == "claude") || r.Rep != 1+(i%12)/6 {
			t.Fatalf("agents and repetitions run in blocks: %d %+v", i, r)
		}
	}
	fixed := makePlan([]string{"claude"}, []string{"base", "mcp"}, []string{"BQ-1"}, 1, 7, false)
	if fixed[0].Mode != "base" || fixed[1].Mode != "mcp" {
		t.Errorf("no shuffle keeps the order: %+v", fixed)
	}
}

func TestClassify(t *testing.T) {
	base := RecordInput{Row: Row{Agent: "claude", Mode: "mcp"}, DigestBefore: "d", DigestAfter: "d"}
	connected := Metrics{MCPServerStatus: "connected"}
	cases := []struct {
		name string
		mod  func(*RecordInput, *Metrics)
		want string
		kind string
	}{
		{"answered", func(*RecordInput, *Metrics) {}, outcomeAnswered, ""},
		{"workspace changed beats timeout", func(in *RecordInput, _ *Metrics) { in.DigestAfter = "e"; in.Exec.TimedOut = true }, outcomeInvalid, ""},
		{"timeout", func(in *RecordInput, _ *Metrics) { in.Exec.TimedOut = true }, outcomeTimeout, ""},
		{"cost cap", func(in *RecordInput, _ *Metrics) { in.Exec.CostCapped = true }, outcomeCostCap, ""},
		{"mcp not connected", func(_ *RecordInput, m *Metrics) { m.MCPServerStatus = "failed" }, outcomeInvalid, ""},
		{"mcp in base", func(in *RecordInput, m *Metrics) { in.Row.Mode = "base"; m.MCPServers = []string{"graphvein"} }, outcomeInvalid, ""},
		{"web", func(_ *RecordInput, m *Metrics) { m.WebCalls = 1 }, outcomeInvalid, ""},
		{"denied read", func(_ *RecordInput, m *Metrics) { m.PermissionDenials = []string{"Read"} }, outcomeInvalid, ""},
		{"denied bash is fine", func(_ *RecordInput, m *Metrics) { m.PermissionDenials = []string{"Bash"} }, outcomeAnswered, ""},
		{"turn limit", func(_ *RecordInput, m *Metrics) { m.TurnLimit = true; m.IsError = true }, outcomeTurnLimit, ""},
		{"rate limit", func(_ *RecordInput, m *Metrics) { m.IsError = true; m.RateLimited = true }, outcomeAgentError, "rate_limit"},
		{"exit code", func(in *RecordInput, _ *Metrics) { in.Exec.ExitCode = 2 }, outcomeAgentError, "cli"},
	}
	for _, c := range cases {
		in, m := base, connected
		c.mod(&in, &m)
		got, _, kind := classify(in, m)
		if got != c.want || kind != c.kind {
			t.Errorf("%s: %s/%s, want %s/%s", c.name, got, kind, c.want, c.kind)
		}
	}
}

func TestRowsAndSecret(t *testing.T) {
	dir := t.TempDir()
	qs, err := loadQuestions(publicQuestions)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := findQuestion(qs, "BQ-1")
	in := RecordInput{Row: Row{Agent: "opencode", Mode: "base", Question: "BQ-1", RunID: "r1"}, Question: q,
		Transcript: canned(t, "opencode", "base"), Prices: testPrices(t)}
	in.Exec.WatchedUSD = 0.01
	row, answer, err := buildRow(in)
	if err != nil {
		t.Fatal(err)
	}
	if row.Outcome != outcomeAnswered || !row.Correct || row.CostKind != "worst_case_unsettled" || row.CostUSD < 0.01 {
		t.Errorf("unsettled cost is booked at the worst case: %+v", row)
	}
	if !strings.Contains(answer, "items.py:76") || len(row.TranscriptSHA256) != 64 {
		t.Errorf("answer and hash: %q %s", answer, row.TranscriptSHA256)
	}
	if err := appendRow(dir, row, answer); err != nil {
		t.Fatal(err)
	}
	row.Outcome = outcomeInvalid
	if err := appendRow(dir, row, ""); err != nil {
		t.Fatal(err)
	}
	rows, _ := readRows(filepath.Join(dir, "runs.jsonl"))
	all, _ := readAllRows(filepath.Join(dir, "runs.jsonl"))
	if len(rows) != 1 || rows[0].Outcome != outcomeInvalid || len(all) != 2 {
		t.Errorf("last row per run wins, all rows stay: %d %d", len(rows), len(all))
	}

	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-testsecretvalue")
	if err := appendRow(dir, row, "", []byte("leak sk-or-v1-testsecretvalue")); err != errSecretFound {
		t.Errorf("a transcript holding the key must stop the write: %v", err)
	}
	if got := redact("x sk-or-v1-testsecretvalue y"); got != "x [REDACTED] y" {
		t.Errorf("redact: %s", got)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "runs.jsonl"))
	if bytes.Contains(data, []byte("testsecret")) {
		t.Error("key written")
	}
}

func TestStandinAndReport(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"standin", "--format", "claude", "--", "auth", "status"}, nil, &out, &errb); code != 0 || !strings.Contains(out.String(), "claude.ai") {
		t.Fatalf("auth status: %d %s", code, out.String())
	}
	dir := t.TempDir()
	scen := filepath.Join(dir, "s.txt")
	os.WriteFile(scen, []byte("codex BQ-1 * wrong\n"), 0o644)
	t.Setenv("GV_BENCH_STANDIN_QUESTIONS", publicQuestions)
	t.Setenv("GV_BENCH_STANDIN_SCENARIOS", scen)
	t.Setenv("GV_BENCH_QUESTION_ID", "BQ-1")
	out.Reset()
	if code := run([]string{"standin", "--format", "codex", "--", "exec", "--json", "-c", `mcp_servers.graphvein.command="x"`, "-"}, strings.NewReader("prompt"), &out, &errb); code != 0 {
		t.Fatalf("codex stand-in: %d %s", code, errb.String())
	}
	qs, _ := loadQuestions(publicQuestions)
	q, _ := findQuestion(qs, "BQ-1")
	row, _, err := buildRow(RecordInput{Row: Row{Agent: "codex", Mode: "mcp", Question: "BQ-1", RunID: "x"}, Question: q, Transcript: out.Bytes()})
	if err != nil || row.Outcome != outcomeAnswered || row.Correct || row.MCPToolCalls != 1 {
		t.Fatalf("a wrong stand-in answer must grade wrong: %+v %v", row, err)
	}
	report := writeReport([]Row{row, {RunID: "y", Agent: "codex", Mode: "mcp", Question: "BQ-2", Outcome: outcomeNotRunCap}})
	if !strings.Contains(report, "| codex |") || !strings.Contains(report, "(+1 not run)") {
		t.Errorf("report:\n%s", report)
	}
	if got := medianRange([]float64{3, 1, 2}); got != "2 (1-3)" {
		t.Errorf("median: %s", got)
	}
}

func TestDigestAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "f.txt"), []byte("one"), 0o644)
	d1, n, err := digestTree(dir)
	if err != nil || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
	os.WriteFile(filepath.Join(dir, "a", "f.txt"), []byte("two"), 0o644)
	d2, _, _ := digestTree(dir)
	os.WriteFile(filepath.Join(dir, "a", "g.txt"), nil, 0o644)
	d3, _, _ := digestTree(dir)
	if d1 == d2 || d2 == d3 {
		t.Fatal("content and file list changes must change the digest")
	}
	if err := makeReadOnly(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Chmod(filepath.Join(dir, "a"), 0o755)
		os.Chmod(dir, 0o755)
		os.Chmod(filepath.Join(dir, "a", "f.txt"), 0o644)
		os.Chmod(filepath.Join(dir, "a", "g.txt"), 0o644)
	})
	info, _ := os.Stat(filepath.Join(dir, "a", "f.txt"))
	if info.Mode().Perm()&0o222 != 0 {
		t.Errorf("file still writable: %v", info.Mode())
	}
}
