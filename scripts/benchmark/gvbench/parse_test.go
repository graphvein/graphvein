package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const finalText = "Done.\n\n```json\n{\"handler\": \"backend/app/api/routes/items.py:76\"}\n```\n"

// canned returns a stand-in transcript with the final message filled in.
func canned(t *testing.T, format, name string) []byte {
	t.Helper()
	data, err := standinFS.ReadFile("testdata/standin/" + format + "/" + name + ".jsonl")
	if err != nil {
		t.Fatal(err)
	}
	quoted, _ := json.Marshal(finalText)
	return []byte(strings.ReplaceAll(string(data), "{{FINAL}}", string(quoted)))
}

func TestParseClaude(t *testing.T) {
	m, err := parseTranscript("claude", canned(t, "claude", "base"))
	if err != nil {
		t.Fatal(err)
	}
	if m.InputTokens != 12 || m.CacheReadTokens != 38100 || m.CacheWriteTokens != 3400 || m.OutputTokens != 510 {
		t.Errorf("tokens: %+v", m)
	}
	if m.ToolCalls != 2 || m.MCPToolCalls != 0 || m.Turns != 3 || m.HarnessCostUSD != 0.04125 || m.CLIDuration != 18450 {
		t.Errorf("tools/turns/cost: %+v", m)
	}
	if m.MCPServerStatus != "absent" || m.FinalText != finalText || m.IsError {
		t.Errorf("status/final: %+v", m)
	}

	m, _ = parseTranscript("claude", canned(t, "claude", "mcp"))
	if m.MCPServerStatus != "connected" || m.MCPToolCalls != 1 || m.MCPResults != 1 || m.MCPBothCopies != 1 {
		t.Errorf("mcp: %+v", m)
	}
	if m.MCPResultStructuredBytes == 0 || m.MCPResultTextBytes == 0 || m.MCPResultBytes != m.MCPResultTextBytes+m.MCPResultStructuredBytes {
		t.Errorf("D5 sizes: %+v", m)
	}

	m, _ = parseTranscript("claude", canned(t, "claude", "ratelimit"))
	if !m.IsError || !m.RateLimited {
		t.Errorf("rate limit: %+v", m)
	}

	turns := []byte(`{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":60,"usage":{"input_tokens":1,"output_tokens":2},"permission_denials":[{"tool_name":"Bash"}]}`)
	m, _ = parseTranscript("claude", turns)
	if !m.TurnLimit || len(m.PermissionDenials) != 1 {
		t.Errorf("turn limit: %+v", m)
	}
	m, _ = parseTranscript("claude", []byte("not json\n"))
	if m.ParseWarnings != 1 || len(m.Unreported) == 0 {
		t.Errorf("garbage must be counted and tokens marked unreported: %+v", m)
	}
}

func TestParseCodex(t *testing.T) {
	m, _ := parseTranscript("codex", canned(t, "codex", "base"))
	if m.InputTokens != 48210-40960 || m.CacheReadTokens != 40960 || m.OutputTokens != 712 || m.ReasoningTokens != 256 {
		t.Errorf("tokens: %+v", m)
	}
	if m.ToolCalls != 2 || m.Turns != 1 || m.FinalText != finalText {
		t.Errorf("tools/final: %+v", m)
	}
	m, _ = parseTranscript("codex", canned(t, "codex", "mcp"))
	if m.MCPToolCalls != 1 || m.MCPBothCopies != 1 || m.MCPResultStructuredBytes == 0 {
		t.Errorf("mcp: %+v", m)
	}
	m, _ = parseTranscript("codex", canned(t, "codex", "ratelimit"))
	if !m.IsError || !m.RateLimited {
		t.Errorf("rate limit: %+v", m)
	}
	// Provisional nesting: usage fields directly on the event also count.
	m, _ = parseTranscript("codex", []byte(`{"type":"turn.completed","input_tokens":10,"cached_input_tokens":4,"output_tokens":3}`))
	if m.InputTokens != 6 || m.CacheReadTokens != 4 || m.OutputTokens != 3 {
		t.Errorf("flat usage: %+v", m)
	}
	m, _ = parseTranscript("codex", []byte(`{"type":"item.completed","item":{"type":"web_search","query":"x"}}`))
	if m.WebCalls != 1 {
		t.Errorf("web search: %+v", m)
	}
}

func TestParseOpencode(t *testing.T) {
	m, _ := parseTranscript("opencode", canned(t, "opencode", "base"))
	if m.InputTokens != 11700 || m.CacheReadTokens != 20200 || m.OutputTokens != 690 || m.ReasoningTokens != 130 {
		t.Errorf("tokens: %+v", m)
	}
	if m.Turns != 3 || m.ToolCalls != 2 || m.FinalText != finalText || m.HarnessCostUSD < 0.0027 {
		t.Errorf("steps/final: %+v", m)
	}
	m, _ = parseTranscript("opencode", canned(t, "opencode", "mcp"))
	if m.MCPToolCalls != 1 || m.MCPResults != 1 || m.MCPResultTextBytes == 0 || m.MCPBothCopies != 0 {
		t.Errorf("mcp: %+v", m)
	}
	m, _ = parseTranscript("opencode", canned(t, "opencode", "ratelimit"))
	if !m.IsError || !m.RateLimited {
		t.Errorf("rate limit: %+v", m)
	}
	m, _ = parseTranscript("opencode", []byte(`{"type":"step_finish","part":{"tokens":{"input":1},"providerMetadata":{"openrouter":{"provider":"DeepInfra"}}},"x":"gen-1234567890abcdef"}`))
	if len(m.ProviderNames) != 1 || m.ProviderNames[0] != "DeepInfra" || len(m.GenerationIDs) != 1 {
		t.Errorf("provider evidence: %+v", m)
	}
}

func TestSplitCopies(t *testing.T) {
	cases := []struct {
		raw              string
		text, structured int64
	}{
		{`"plain"`, 5, 0},
		{`[{"type":"text","text":"abc"}]`, 3, 0},
		{`[{"type":"text","text":"{\"a\":1}"},{"type":"text","text":"abc"}]`, 3, 7},
		{`[{"type":"text","text":"{\"a\":1}"},{"type":"text","text":"{\"a\":1}"}]`, 7, 7},
		{`[{"type":"text","text":"abc"},{"type":"resource","resource":{}}]`, 3, 33},
	}
	for _, c := range cases {
		text, structured := splitCopies(json.RawMessage(c.raw))
		if text != c.text || structured != c.structured {
			t.Errorf("%s: got %d/%d, want %d/%d", c.raw, text, structured, c.text, c.structured)
		}
	}
}
