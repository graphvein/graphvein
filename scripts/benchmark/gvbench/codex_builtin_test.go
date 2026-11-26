package main

import "testing"

// TestParseCodexBuiltinServer checks that Codex's own tools, reported as
// mcp_tool_call items of its internal server "codex", count as tool calls
// and never as an MCP server of the run (which would make a base run
// invalid), while a real MCP server still does.
func TestParseCodexBuiltinServer(t *testing.T) {
	tr := []byte(`{"type":"item.completed","item":{"type":"mcp_tool_call","server":"codex","tool":"exec","result":{}}}
{"type":"item.completed","item":{"type":"agent_message","text":"done"}}
`)
	m, _ := parseTranscript("codex", tr)
	if len(m.MCPServers) != 0 || m.MCPToolCalls != 0 || m.ToolCalls != 1 {
		t.Errorf("built-in: servers %v mcp %d tools %d", m.MCPServers, m.MCPToolCalls, m.ToolCalls)
	}
	tr = []byte(`{"type":"item.completed","item":{"type":"mcp_tool_call","server":"codex_apps","tool":"x","result":{}}}
`)
	m, _ = parseTranscript("codex", tr)
	if len(m.MCPServers) != 1 {
		t.Errorf("external server not recorded: %v", m.MCPServers)
	}
}

// TestPlanAgentReps checks the per-agent repetition override.
func TestPlanAgentReps(t *testing.T) {
	o, err := parseAgentReps("codex=1")
	if err != nil {
		t.Fatal(err)
	}
	p := makePlanReps([]string{"claude", "codex"}, []string{"base", "mcp"}, []string{"BQ-1", "BQ-2"}, 3, o, 7, true)
	count := map[string]int{}
	for _, r := range p {
		count[r.Agent]++
	}
	if count["claude"] != 12 || count["codex"] != 4 {
		t.Errorf("counts %v", count)
	}
	if _, err := parseAgentReps("codex=0"); err == nil {
		t.Error("codex=0 accepted")
	}
}
