package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Outcomes of a run. Only "invalid" runs are retried; "not_run_*" rows mark
// planned runs that never started and are never counted as failures.
const (
	outcomeAnswered        = "answered"
	outcomeNoAnswer        = "no_answer"
	outcomeTimeout         = "timeout"
	outcomeTurnLimit       = "turn_limit"
	outcomeCostCap         = "cost_cap"
	outcomeAgentError      = "agent_error"
	outcomeInvalid         = "invalid"
	outcomeNotRunCap       = "not_run_cap"
	outcomeNotRunRate      = "not_run_rate_limit"
	outcomeNotRunPreflight = "not_run_preflight"
)

// Row is one line of runs.jsonl: the identity of the run, its outcome and
// every metric. It is committed, so it holds no transcript text.
type Row struct {
	RunID          string `json:"run_id"`
	Agent          string `json:"agent"`
	Harness        string `json:"harness"`
	HarnessVersion string `json:"harness_version"`
	Model          string `json:"model"`
	ServedModel    string `json:"served_model,omitempty"`
	// Variant describes a non-default harness setting of the agent id, such
	// as codex-visible (Codex with its MCP tools shown to the model instead
	// of deferred behind tool search).
	Variant   string `json:"variant,omitempty"`
	Mode      string `json:"mode"`
	Question  string `json:"question"`
	Corpus    string `json:"corpus"`
	Rep       int    `json:"rep"`
	Attempt   int    `json:"attempt"`
	StandIn   bool   `json:"stand_in,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	// AuthPath (Claude) is how the run authenticated: api_key_bare,
	// subscription or api_signin. Billing names who pays: "API billing"
	// (real spend, counted against the Claude cap) or "subscription".
	AuthPath     string `json:"auth_path,omitempty"`
	Billing      string `json:"billing,omitempty"`
	APIKeySource string `json:"api_key_source,omitempty"`

	Outcome   string  `json:"outcome"`
	Detail    string  `json:"detail,omitempty"`
	ErrorKind string  `json:"error_kind,omitempty"`
	Correct   bool    `json:"correct"`
	Score     float64 `json:"score"`
	Grade     *Grade  `json:"grade,omitempty"`
	// GradeV1 keeps the grade a run had before `gvbench regrade` re-graded
	// it against a corrected question file; Regraded says when and why.
	GradeV1     *GradeRecord `json:"grade_v1,omitempty"`
	Regraded    string       `json:"regraded,omitempty"`
	AnswerFile  string       `json:"answer_file,omitempty"`
	ExitCode    int          `json:"exit_code"`
	WallMS      int64        `json:"wall_ms"`
	CLIDuration int64        `json:"cli_duration_ms,omitempty"`

	InputTokens      int64 `json:"input_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`

	CostUSD        float64 `json:"cost_usd"`
	CostKind       string  `json:"cost_kind"`
	HarnessCostUSD float64 `json:"harness_cost_usd"`
	WatchedUSD     float64 `json:"watched_worst_usd,omitempty"`

	ToolCalls    int            `json:"tool_calls"`
	MCPToolCalls int            `json:"mcp_tool_calls"`
	ToolsByName  map[string]int `json:"tools_by_name,omitempty"`
	Turns        int            `json:"turns"`

	MCPServerStatus          string `json:"mcp_server_status"`
	MCPResults               int    `json:"mcp_results"`
	MCPResultBytes           int64  `json:"mcp_result_bytes"`
	MCPResultTextBytes       int64  `json:"mcp_result_text_bytes"`
	MCPResultStructuredBytes int64  `json:"mcp_result_structured_bytes"`
	MCPBothCopies            int    `json:"mcp_both_copies"`

	Serving *Serving `json:"serving,omitempty"`

	TranscriptSHA256 string   `json:"transcript_sha256,omitempty"`
	TranscriptBytes  int64    `json:"transcript_bytes,omitempty"`
	Unreported       []string `json:"unreported,omitempty"`
	WorkspaceDigest  string   `json:"workspace_digest,omitempty"`
}

// RecordInput gathers what the driver knows about one finished run.
type RecordInput struct {
	Row          Row // identity fields filled by the caller
	Question     Question
	Transcript   []byte
	Stderr       []byte
	Exec         ExecResult
	Workspace    string
	DigestBefore string
	DigestAfter  string
	SignInBilled bool
	// AuthPath is the Claude auth path the driver chose; StepContext and
	// StepOutput bound one model call for a Claude worst-case booking.
	AuthPath    string
	StepContext int64
	StepOutput  int64
	BilledUSD   float64
	CostSettled bool
	Prices      *PriceFile
	Providers   []string
}

// buildRow classifies the run and fills the metrics; the answer object, if
// any, is returned for the answers/ file.
func buildRow(in RecordInput) (Row, string, error) {
	row := in.Row
	m, err := parseTranscript(formatOf(in.Row.Agent), in.Transcript)
	if err != nil {
		return row, "", err
	}
	sum := sha256.Sum256(in.Transcript)
	row.TranscriptSHA256 = hex.EncodeToString(sum[:])
	row.TranscriptBytes = int64(len(in.Transcript))
	row.StartedAt = in.Exec.StartedAt
	row.ExitCode = in.Exec.ExitCode
	row.WallMS = in.Exec.WallMS
	row.WatchedUSD = in.Exec.WatchedUSD
	row.CLIDuration = m.CLIDuration
	row.ServedModel = m.ServedModel
	row.InputTokens, row.CacheReadTokens, row.CacheWriteTokens = m.InputTokens, m.CacheReadTokens, m.CacheWriteTokens
	row.OutputTokens, row.ReasoningTokens = m.OutputTokens, m.ReasoningTokens
	row.HarnessCostUSD = round6(m.HarnessCostUSD)
	row.ToolCalls, row.MCPToolCalls, row.ToolsByName, row.Turns = m.ToolCalls, m.MCPToolCalls, m.ToolsByName, m.Turns
	row.MCPServerStatus = m.MCPServerStatus
	row.MCPResults, row.MCPResultBytes = m.MCPResults, m.MCPResultBytes
	row.MCPResultTextBytes, row.MCPResultStructuredBytes, row.MCPBothCopies = m.MCPResultTextBytes, m.MCPResultStructuredBytes, m.MCPBothCopies
	row.Unreported = m.Unreported
	row.WorkspaceDigest = in.DigestAfter

	switch formatOf(row.Agent) {
	case "claude":
		row.AuthPath = in.AuthPath
		row.APIKeySource = m.APIKeySource
		if claudeBilled(in.AuthPath, in.SignInBilled) {
			row.Billing = "API billing"
			cost, kind, err := claudeBookedCost(in.Prices, m, in.Exec.WatchedUSD, in.StepContext, in.StepOutput)
			if err != nil {
				return row, "", err
			}
			row.CostUSD, row.CostKind = cost, kind
		} else {
			row.Billing = "subscription"
			row.CostUSD = row.HarnessCostUSD
			row.CostKind = "estimate_not_billed"
		}
	case "codex":
		row.CostKind = "not_reported"
	case "opencode":
		if in.CostSettled {
			// OpenRouter's key usage can lag behind a run and read 0; the
			// harness's own per-step cost is then the better record, so the
			// larger of the two is booked.
			row.CostUSD = round6(maxf(in.BilledUSD, m.HarnessCostUSD))
			row.CostKind = "billed"
		} else {
			// The key usage did not settle: book the worst case so the cap
			// stays conservative.
			row.CostUSD = round6(maxf(in.BilledUSD, in.Exec.WatchedUSD, worstOf(in.Prices, m)))
			row.CostKind = "worst_case_unsettled"
		}
		s := identifyServing(in.Prices, append(append([]string{}, in.Providers...), m.ProviderNames...), m, in.BilledUSD)
		row.Serving = &s
	}

	row.Outcome, row.Detail, row.ErrorKind = classify(in, m)
	if row.Agent == "opencode" && len(m.OutsideDenials) > 0 && row.Detail == "" {
		row.Detail = fmt.Sprintf("access outside the workspace denied %d time(s), as configured: %s", len(m.OutsideDenials), redactPaths(m.OutsideDenials[0], in.Workspace))
	}
	answer := ""
	if row.Outcome == outcomeAnswered || row.Outcome == outcomeNoAnswer {
		g := gradeAnswer(in.Question, m.FinalText, in.Workspace)
		row.Grade = &g
		row.Correct, row.Score = g.Correct, g.Score
		if !g.Answered {
			row.Outcome = outcomeNoAnswer
			row.Detail = g.Problem
		}
	}
	if obj, block, problem := extractAnswer(m.FinalText); problem == "" {
		answer = marshalPlain(obj) + "\n"
	} else if block != "" {
		answer = marshalPlain(map[string]string{"problem": problem, "block": block}) + "\n"
	} else if m.FinalText != "" || row.Outcome == outcomeNoAnswer {
		answer = marshalPlain(map[string]string{"problem": problem}) + "\n"
	}
	return row, answer, nil
}

func worstOf(p *PriceFile, m Metrics) float64 {
	if p == nil {
		return 0
	}
	return p.worstCost(m.InputTokens+m.CacheReadTokens+m.CacheWriteTokens, m.OutputTokens+m.ReasoningTokens)
}

func maxf(xs ...float64) float64 {
	best := 0.0
	for _, x := range xs {
		if x > best {
			best = x
		}
	}
	return best
}

// classify applies the outcome rules in a fixed order: a changed workspace
// voids everything; then the runner's own kills; then conditions that make
// the comparison invalid; then CLI-reported failures.
func classify(in RecordInput, m Metrics) (outcome, detail, errorKind string) {
	mode := in.Row.Mode
	switch {
	case in.DigestBefore != "" && in.DigestAfter != in.DigestBefore:
		return outcomeInvalid, "workspace modified", ""
	case in.Exec.StartError != "":
		return outcomeAgentError, "start failed: " + in.Exec.StartError, "start"
	case in.Exec.TimedOut:
		return outcomeTimeout, "", ""
	case in.Exec.CostCapped:
		return outcomeCostCap, fmt.Sprintf("per-run ceiling reached (worst case %.4f USD)", in.Exec.WatchedUSD), ""
	case mode == "mcp" && in.Row.Agent == "claude" && m.MCPServerStatus != "connected":
		return outcomeInvalid, "graphvein MCP server not connected: " + m.MCPServerStatus, ""
	case mode == "base" && (len(m.MCPServers) > 0 || m.MCPToolCalls > 0):
		return outcomeInvalid, "an MCP server was present in base mode", ""
	case m.WebCalls > 0:
		return outcomeInvalid, "a web tool was used", ""
	case deniedConfigured(in.Row.Agent, m.PermissionDenials):
		d := "a configured tool was denied: " + strings.Join(m.PermissionDenials, ",")
		if len(m.PermissionErrors) > 0 {
			// The first denial's own error text says which rule refused it.
			d += "; first error: " + redactPaths(m.PermissionErrors[0], in.Workspace)
		}
		return outcomeInvalid, d, ""
	case m.TurnLimit:
		return outcomeTurnLimit, m.ErrorSubtype, ""
	case m.RateLimited:
		return outcomeAgentError, m.ErrorText, "rate_limit"
	case in.Exec.ExitCode != 0 || m.IsError:
		d := m.ErrorText
		if d == "" {
			d = fmt.Sprintf("exit code %d", in.Exec.ExitCode)
			if s := firstLine(strings.TrimSpace(string(in.Stderr))); s != "" {
				d += ": " + s
			}
		}
		return outcomeAgentError, d, "cli"
	}
	return outcomeAnswered, "", ""
}

// deniedConfigured reports a permission denial of a tool the benchmark
// allows; denials of tools outside the read-only set are expected.
func deniedConfigured(agent string, denials []string) bool {
	for _, d := range denials {
		switch agent {
		case "claude":
			if d == "Read" || d == "Grep" || d == "Glob" || strings.HasPrefix(d, "mcp__graphvein__") {
				return true
			}
		case "opencode":
			if d == "read" || d == "grep" || d == "glob" || d == "list" || strings.HasPrefix(d, "graphvein_") {
				return true
			}
		}
	}
	return false
}

// formatOf maps an agent id to the CLI whose transcript format, sign-in
// check and cost rules it shares: codex-visible is Codex with one setting
// changed.
func formatOf(agent string) string {
	if agent == "codex-visible" {
		return "codex"
	}
	return agent
}

// GradeRecord is a grade as it stood before a re-grade.
type GradeRecord struct {
	Correct bool    `json:"correct"`
	Score   float64 `json:"score"`
	Grade   *Grade  `json:"grade,omitempty"`
}

// maxErrorText bounds the error text a row keeps.
const maxErrorText = 300

// redactPaths shortens an error text for a committed row: the workspace
// path becomes <workspace>, the home directory ~, and the text is cut to
// maxErrorText bytes, so no machine-specific path is published.
func redactPaths(text, workspace string) string {
	text = strings.Join(strings.Fields(text), " ")
	repl := []string{}
	if workspace != "" {
		repl = append(repl, workspace, "<workspace>", strings.ReplaceAll(workspace, "/", `\\`), "<workspace>", strings.ReplaceAll(workspace, "/", `\`), "<workspace>")
	}
	for _, name := range []string{"HOME", "USERPROFILE"} {
		if h := os.Getenv(name); len(h) > 1 {
			repl = append(repl, h, "~", strings.ReplaceAll(h, `\`, "/"), "~", strings.ReplaceAll(h, `\`, `\\`), "~")
		}
	}
	if len(repl) > 0 {
		text = strings.NewReplacer(repl...).Replace(text)
	}
	if len(text) > maxErrorText {
		text = strings.ToValidUTF8(text[:maxErrorText], "") + "..."
	}
	return text
}

// errSecretFound stops a write that would put an API key on disk in the
// results.
var errSecretFound = errors.New("an API key value (OPENROUTER_API_KEY or ANTHROPIC_API_KEY) appears in data about to be written; nothing was written")

// secretEnv names the environment variables whose values are API keys. Keys
// come only from the environment, never from a flag or a file.
var secretEnv = []string{"OPENROUTER_API_KEY", "ANTHROPIC_API_KEY"}

// secretValues returns the key values set in the environment; values shorter
// than 8 bytes are not keys and would match ordinary text.
func secretValues() []string {
	var out []string
	for _, name := range secretEnv {
		if v := os.Getenv(name); len(v) >= 8 {
			out = append(out, v)
		}
	}
	return out
}

// checkNoSecret fails if any key occurs in any of the blobs.
func checkNoSecret(blobs ...[]byte) error {
	for _, key := range secretValues() {
		for _, b := range blobs {
			if bytes.Contains(b, []byte(key)) {
				return errSecretFound
			}
		}
	}
	return nil
}

// redact replaces every key value in text meant for a terminal or log.
func redact(s string) string {
	for _, key := range secretValues() {
		s = strings.ReplaceAll(s, key, "[REDACTED]")
	}
	return s
}

// appendRow writes the row (and answer file) to the results directory.
func appendRow(outDir string, row Row, answer string, extra ...[]byte) error {
	line := marshalPlain(row) + "\n"
	if err := checkNoSecret(append(extra, []byte(line), []byte(answer))...); err != nil {
		return err
	}
	if answer != "" {
		dir := filepath.Join(outDir, "answers")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating answers dir: %w", err)
		}
		name := row.RunID + ".json"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(answer), 0o644); err != nil {
			return fmt.Errorf("writing answer: %w", err)
		}
	}
	f, err := os.OpenFile(filepath.Join(outDir, "runs.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening runs.jsonl: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("appending to runs.jsonl: %w", err)
	}
	return nil
}

// readRows returns the rows of runs.jsonl, keeping only the last row of each
// run id (a resumed or retried run supersedes earlier rows).
func readRows(path string) ([]Row, error) {
	all, err := readAllRows(path)
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	var rows []Row
	for _, r := range all {
		if i, ok := index[r.RunID]; ok {
			rows[i] = r
			continue
		}
		index[r.RunID] = len(rows)
		rows = append(rows, r)
	}
	return rows, nil
}

// readAllRows returns every row of runs.jsonl in file order.
func readAllRows(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()
	var rows []Row
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var r Row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}

// runID names a run the same way in rows, answer files and transcripts.
func runID(agent, mode, question string, rep int) string {
	return fmt.Sprintf("%s-%s-%s-r%d", agent, mode, strings.ToLower(question), rep)
}
