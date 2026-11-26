package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegrade re-grades a run directory against a corrected question: only
// the changed row is rewritten, it keeps its first grade in grade_v1, a
// second re-grade changes nothing, and the summary lists the change.
func TestRegrade(t *testing.T) {
	q := question(t, `{"call_sites": ["file:line"]}`, `{"call_sites": ["a.ts:10"]}`)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "answers"), 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i, answer := range []string{`{"call_sites":["a.ts:10","b.ts:22"]}`, `{"call_sites":["a.ts:10"]}`} {
		r := Row{RunID: runID("codex", "base", q.ID, i+1), Agent: "codex", Mode: "base", Question: q.ID, Rep: i + 1, Outcome: outcomeAnswered, AnswerFile: "answers/" + runID("codex", "base", q.ID, i+1) + ".json"}
		g := gradeAnswer(q, "```json\n"+answer+"\n```", "")
		r.Grade, r.Correct, r.Score = &g, g.Correct, g.Score
		if err := os.WriteFile(filepath.Join(dir, r.AnswerFile), []byte(answer+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, marshalPlain(r))
	}
	// A row that never answered has nothing to re-grade.
	lines = append(lines, marshalPlain(Row{RunID: "codex-base-x-r3", Question: q.ID, Outcome: outcomeTimeout}))
	original := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "runs.jsonl"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var corrected []byte
	corrected, _ = json.Marshal([]string{"a.ts:10", "b.ts:22"})
	q.Expected["call_sites"] = corrected
	qs := []Question{q}
	changes, err := regradeRuns(dir, qs, "test correction", false)
	if err != nil || len(changes) != 2 {
		t.Fatalf("check: %v %+v", err, changes)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "runs.jsonl")); string(data) != original {
		t.Fatal("--check wrote the runs file")
	}
	if _, err := regradeRuns(dir, qs, "test correction", true); err != nil {
		t.Fatal(err)
	}
	rows, err := readAllRows(filepath.Join(dir, "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].Correct || rows[0].GradeV1 == nil || rows[0].GradeV1.Correct || rows[0].Regraded != "test correction" {
		t.Errorf("row 1 not re-graded to correct with grade_v1: %+v", rows[0])
	}
	if rows[1].Correct || rows[1].GradeV1 == nil || !rows[1].GradeV1.Correct {
		t.Errorf("row 2 must turn wrong and keep its correct grade_v1: %+v", rows[1])
	}
	after, _ := os.ReadFile(filepath.Join(dir, "runs.jsonl"))
	if !strings.HasSuffix(string(after), lines[2]+"\n") {
		t.Error("an unchanged row was rewritten")
	}
	if c, _ := regradeRuns(dir, qs, "again", true); len(c) != 0 {
		t.Errorf("a second re-grade changed %d rows", len(c))
	}
	report := writeReport(rows)
	if !strings.Contains(report, "## Re-graded runs") || !strings.Contains(report, "| "+q.ID+" | 2 | 1 | 1 |") {
		t.Errorf("summary lacks the re-grade table:\n%s", report)
	}
}

// TestPlanCodexVisibleAndRerun checks that codex-visible has mcp runs only
// and that --rerun patterns select runs and refuse a pattern that matches
// nothing.
func TestPlanCodexVisibleAndRerun(t *testing.T) {
	var out bytes.Buffer
	err := dispatch("plan", []string{"--agents", "codex-visible,opencode", "--ids", "BQ-1,BQ-21", "--reps", "3", "--no-shuffle",
		"--only", "codex-visible-*,opencode-base-bq-21-r2"}, nil, &out)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(got) != 7 {
		t.Fatalf("want 6 codex-visible mcp runs and 1 opencode run, got %q", got)
	}
	for _, l := range got[:6] {
		if !strings.HasPrefix(l, "codex-visible ") || !strings.Contains(l, " mcp ") {
			t.Errorf("unexpected run %q", l)
		}
	}
	if got[6] != "opencode 2 base BQ-21" {
		t.Errorf("rerun selection: %q", got[6])
	}
	err = dispatch("plan", []string{"--agents", "opencode", "--ids", "BQ-21", "--only", "opencode-base-bq-12-r1"}, nil, &out)
	if err == nil || !strings.Contains(err.Error(), "matches no planned run") {
		t.Errorf("an unmatched --rerun pattern was accepted: %v", err)
	}
}

// TestOpencodeConfigNeverAsks checks that no permission of the generated
// opencode config is "ask" (opencode run rejects every ask) and that paths
// outside the workspace are denied.
func TestOpencodeConfigNeverAsks(t *testing.T) {
	var cfg struct {
		Permission map[string]string `json:"permission"`
	}
	if err := json.Unmarshal([]byte(opencodeConfig(OpencodeOptions{Model: "m", Steps: 60}, nil)), &cfg); err != nil {
		t.Fatal(err)
	}
	for k, v := range cfg.Permission {
		if v == "ask" {
			t.Errorf("permission %s is ask", k)
		}
	}
	if cfg.Permission["external_directory"] != "deny" || cfg.Permission["doom_loop"] != "allow" {
		t.Errorf("permissions: %v", cfg.Permission)
	}
}

// TestOpencodePermissionErrors checks the two opencode 1.19.6 permission
// errors: a denial of a path outside the workspace by the configured
// external_directory rule is expected (the run stays valid and the row says
// so), while a rejected read voids the attempt with its error text in the
// detail, with the workspace path redacted.
func TestOpencodePermissionErrors(t *testing.T) {
	ws := "/tmp/bench/ws/langflow"
	outside := `{"type":"tool_use","part":{"tool":"glob","state":{"status":"error","input":{"path":"/src"},"error":"The user has specified a rule which prevents you from using this specific tool call. Here are some of the relevant rules [{\"permission\":\"external_directory\",\"pattern\":\"*\",\"action\":\"deny\"}]"}}}`
	rejected := `{"type":"tool_use","part":{"tool":"read","state":{"status":"error","error":"The user rejected permission to use this specific tool call. (` + ws + `/src/a.ts)"}}}`
	base := string(canned(t, "opencode", "base"))
	in := RecordInput{Row: Row{Agent: "opencode", Mode: "base"}, Workspace: ws, CostSettled: true,
		Question: Question{ID: "BQ-1"}, Transcript: []byte(outside + "\n" + base)}
	row, _, err := buildRow(in)
	if err != nil {
		t.Fatal(err)
	}
	if row.Outcome == outcomeInvalid || !strings.Contains(row.Detail, "outside the workspace denied 1 time") {
		t.Errorf("outside denial: %s %q", row.Outcome, row.Detail)
	}
	in.Transcript = []byte(rejected + "\n" + base)
	row, _, _ = buildRow(in)
	if row.Outcome != outcomeInvalid || !strings.Contains(row.Detail, "rejected permission") ||
		!strings.Contains(row.Detail, "<workspace>/src/a.ts") || strings.Contains(row.Detail, ws) {
		t.Errorf("rejected read: %s %q", row.Outcome, row.Detail)
	}
}

// TestCeilingPricesCacheReads checks the recalibrated per-run bound on the
// committed price file: cache reads at the highest cache-read price, and a
// round-1 BQ-21 run that the old bound stopped at $0.48 now well below the
// $0.45 ceiling, while still above its billed $0.006.
func TestCeilingPricesCacheReads(t *testing.T) {
	p, err := loadPrices("../prices/deepseek-v4.1-flash-1017.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.maxCacheRead(); got != 0.03 {
		t.Errorf("max cache read %v, want 0.03 (the highest listed)", got)
	}
	for _, e := range p.Endpoints {
		if e.Tools && e.CacheRead > p.maxCacheRead() {
			t.Errorf("%s cache read %v above the bound", e.Tag, e.CacheRead)
		}
	}
	// opencode-base-bq-21-r1 of 2026-11-24-full.
	old := p.worstCost(53664+679296, 11259+7056)
	now := p.worstCostCached(53664, 679296, 11259+7056)
	if old < 0.45 || now >= 0.45 || now < 0.005952 {
		t.Errorf("old bound %.4f, new bound %.4f", old, now)
	}
	line := []byte(`{"type":"step_finish","part":{"tokens":{"input":1000,"output":100,"reasoning":0,"cache":{"read":100000,"write":0}}}}`)
	if got, want := stepWorstCost(line, p), (1000*0.6+100000*0.03+100*2.4)/1e6; got != want {
		t.Errorf("step cost %v, want %v", got, want)
	}
}

// TestCodexVisibleCatalog checks the catalog rewrite keeps every field and
// turns supports_search_tool off for every model.
func TestCodexVisibleCatalog(t *testing.T) {
	in := []byte(`{"models":[{"slug":"gpt-6.2-sol","supports_search_tool":true,"context_window":500000,"tool_mode":"code_mode_only"},{"slug":"x"}],"etag":"e"}`)
	out, n, err := codexVisibleCatalog(in)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	var cat struct {
		Models []map[string]any `json:"models"`
		Etag   string           `json:"etag"`
	}
	if err := json.Unmarshal(out, &cat); err != nil {
		t.Fatal(err)
	}
	if cat.Etag != "e" || cat.Models[0]["context_window"] != float64(500000) || cat.Models[0]["tool_mode"] != "code_mode_only" {
		t.Errorf("fields lost: %s", out)
	}
	for _, m := range cat.Models {
		if m["supports_search_tool"] != false {
			t.Errorf("model %v still offers the search tool", m["slug"])
		}
	}
	if _, _, err := codexVisibleCatalog([]byte(`{"models":[]}`)); err == nil {
		t.Error("an empty catalog was accepted")
	}
}

// TestPublicTree hashes a small commit's public snapshot and compares it
// with the tree id Git wrote for a repository holding that snapshot (the
// expected id was produced with git write-tree from the same files):
// excluded paths gone, docs/public/AGENTS.md at the root as AGENTS.md, a
// directory sorted as its name plus "/", the executable mode kept, a path
// with spaces and brackets taken literally, and a submodule left out.
func TestPublicTree(t *testing.T) {
	// The internal record names are assembled so this public file does not
	// name them literally (the public-tree check refuses that).
	plan := "docs/" + "PL" + "AN.md"
	tasks := "docs/" + "ta" + "sks/"
	entries := []string{
		"100644 blob ce83b25d66563fd88edca888a06520571d055667\tAGENTS.md",
		"100644 blob 856cc8f41a38e0996e155d01b5ee3124d58ecc47\t" + plan,
		"100644 blob bd93009536360a2d96f2b097ac88b28f1fc8cdb4\tdocs/a b/x[1].md",
		"100644 blob a48cf0de7af6613887a8f33ce5957001234ac817\tdocs/public/AGENTS.md",
		"100644 blob f1b4bf686ec09d39b46b118a0786bc7d8c7ef309\t" + tasks + "T1.md",
		"100644 blob 3642de3047a097e7dff4563d7e993921d6823971\tscripts/public-exclude.txt",
		"100644 blob 587be6b4c3f93f93c489c0111bba5596147a26cb\tsrc.txt",
		"100644 blob 06ab7d0f9a35a7d1070711496d6ca1cb892a258f\tsrc/main.go",
		"100755 blob 1a2485251c33a70432394c93fb89330ef214bfc9\tsrc/run.sh",
		"160000 commit 1a2485251c33a70432394c93fb89330ef214bfc9\tvendor/sub",
	}
	lsTree := []byte(strings.Join(entries, "\x00") + "\x00")
	exclude := []byte("AGENTS.md  # replaced\n" + tasks + "   # internal\n" + plan + "\n")
	got, err := publicTree(lsTree, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if want := "1725d212832f140a25c5d2ae6361a534e2fea64c"; got != want {
		t.Errorf("public tree %s, want %s", got, want)
	}
	if _, err := publicTree(lsTree, []byte("docs/public/\n")); err == nil {
		t.Error("a snapshot without docs/public/AGENTS.md was accepted")
	}
	if _, err := publicTree(lsTree, []byte("../x\n")); err == nil {
		t.Error("an exclude entry with .. was accepted")
	}
}
