package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Metrics is what one transcript says about a run, in the same units for
// every CLI. Fields a CLI does not report stay zero and are named in
// Unreported, so a zero is never mistaken for a measurement.
type Metrics struct {
	Format string `json:"format"`
	// FinalText is the agent's last message; the grader reads it.
	FinalText string `json:"-"`

	InputTokens      int64 `json:"input_tokens"` // uncached prompt tokens
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`

	// HarnessCostUSD is the cost the CLI itself reports: Claude's list-price
	// estimate, opencode's catalogue price. Billed cost is recorded apart.
	HarnessCostUSD float64 `json:"harness_cost_usd"`

	ToolCalls    int            `json:"tool_calls"`
	MCPToolCalls int            `json:"mcp_tool_calls"`
	ToolsByName  map[string]int `json:"tools_by_name,omitempty"`
	WebCalls     int            `json:"web_calls"`
	Turns        int            `json:"turns"`
	CLIDuration  int64          `json:"cli_duration_ms"`

	ServedModel string `json:"served_model,omitempty"`
	// MCPServerStatus is the graphvein server state the CLI reported at
	// start ("connected", "failed", ...), "absent" when it listed none, or
	// "unreported" when the CLI does not say.
	MCPServerStatus string   `json:"mcp_server_status"`
	MCPServers      []string `json:"mcp_servers,omitempty"`

	// D5 measurement: sizes of the MCP tool results the agent received.
	MCPResults               int   `json:"mcp_results"`
	MCPResultBytes           int64 `json:"mcp_result_bytes"`
	MCPResultTextBytes       int64 `json:"mcp_result_text_bytes"`
	MCPResultStructuredBytes int64 `json:"mcp_result_structured_bytes"`
	// MCPBothCopies counts results that carried both a text and a
	// structured copy of the same payload.
	MCPBothCopies int `json:"mcp_both_copies"`

	IsError           bool     `json:"is_error"`
	ErrorSubtype      string   `json:"error_subtype,omitempty"`
	ErrorText         string   `json:"error_text,omitempty"`
	RateLimited       bool     `json:"rate_limited"`
	TurnLimit         bool     `json:"turn_limit"`
	PermissionDenials []string `json:"permission_denials,omitempty"`
	// PermissionErrors holds the error text of each denial in
	// PermissionDenials, where the transcript gives one (opencode).
	PermissionErrors []string `json:"permission_errors,omitempty"`
	// OutsideDenials holds the error texts of opencode tool calls refused by
	// the configured external_directory rule (a path outside the
	// workspace). They are expected and do not void the run.
	OutsideDenials []string `json:"outside_denials,omitempty"`
	ProviderNames  []string `json:"provider_names,omitempty"`
	GenerationIDs  []string `json:"generation_ids,omitempty"`

	// Claude only. SawResult says the final result object was printed;
	// CostReported says it carried total_cost_usd (a missing field is
	// booked at a worst case, never as zero). APIKeySource is what the init
	// event names as the credential ("none" for a subscription sign-in,
	// "ANTHROPIC_API_KEY" for the environment key). CallUsage sums the usage
	// of the distinct assistant messages, for runs that end without a result.
	SawResult     bool       `json:"saw_result"`
	CostReported  bool       `json:"cost_reported"`
	APIKeySource  string     `json:"api_key_source,omitempty"`
	CallUsage     TokenUsage `json:"call_usage"`
	Unreported    []string   `json:"unreported,omitempty"`
	ParseWarnings int        `json:"parse_warnings"`
}

// TokenUsage is one set of token counts of a model call or a sum of calls.
type TokenUsage struct {
	Input      int64 `json:"input"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Output     int64 `json:"output"`
}

func (u *TokenUsage) add(o TokenUsage) {
	u.Input += o.Input
	u.CacheRead += o.CacheRead
	u.CacheWrite += o.CacheWrite
	u.Output += o.Output
}

func (m *Metrics) addTool(name string, mcp bool) {
	m.ToolCalls++
	if mcp {
		m.MCPToolCalls++
	}
	if m.ToolsByName == nil {
		m.ToolsByName = map[string]int{}
	}
	m.ToolsByName[name]++
}

func (m *Metrics) addMCPResult(textBytes, structuredBytes int64) {
	m.MCPResults++
	m.MCPResultTextBytes += textBytes
	m.MCPResultStructuredBytes += structuredBytes
	m.MCPResultBytes += textBytes + structuredBytes
	if textBytes > 0 && structuredBytes > 0 {
		m.MCPBothCopies++
	}
}

func addUnique(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

var rateLimitRE = regexp.MustCompile(`(?i)rate[ _-]?limit|usage limit|too many requests|\b429\b|quota exceeded`)

// parseTranscript dispatches on the CLI's output format.
func parseTranscript(format string, data []byte) (Metrics, error) {
	var m Metrics
	var err error
	switch format {
	case "claude":
		m, err = parseClaude(data)
	case "codex":
		m, err = parseCodex(data)
	case "opencode":
		m, err = parseOpencode(data)
	default:
		return Metrics{}, fmt.Errorf("unknown transcript format %q", format)
	}
	m.Format = format
	if m.ErrorText != "" && rateLimitRE.MatchString(m.ErrorText) {
		m.RateLimited = true
	}
	if len(m.ErrorText) > 300 {
		m.ErrorText = m.ErrorText[:300]
	}
	return m, err
}

// jsonLines calls fn for every line that decodes as a JSON object and counts
// the others; a CLI may print non-JSON warnings on stdout.
func jsonLines(data []byte, fn func(obj map[string]json.RawMessage)) int {
	bad := 0
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
			bad++
			continue
		}
		fn(obj)
	}
	return bad
}

func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func num(raw json.RawMessage) int64 {
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		return 0
	}
	return int64(f)
}

func fnum(raw json.RawMessage) float64 {
	var f float64
	_ = json.Unmarshal(raw, &f)
	return f
}

func obj(raw json.RawMessage) map[string]json.RawMessage {
	var o map[string]json.RawMessage
	_ = json.Unmarshal(raw, &o)
	return o
}

func arr(raw json.RawMessage) []json.RawMessage {
	var a []json.RawMessage
	_ = json.Unmarshal(raw, &a)
	return a
}

// genIDRE finds OpenRouter generation ids anywhere in a transcript; when a
// harness passes them through, the driver can ask OpenRouter which provider
// served each call.
var genIDRE = regexp.MustCompile(`"(gen-[0-9A-Za-z_-]{8,})"`)

func findGenerationIDs(data []byte) []string {
	var out []string
	for _, m := range genIDRE.FindAllSubmatch(data, -1) {
		out = addUnique(out, string(m[1]))
	}
	return out
}

func trimmed(s string) string { return strings.TrimSpace(s) }
