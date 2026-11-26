package main

import (
	"encoding/json"
)

// parseCodex reads `codex exec --json` JSON Lines. The event and item names
// come from the released 0.171.0 binary; the nesting of usage on
// turn.completed and the shape of mcp_tool_call results are provisional until
// a signed-in capture confirms them, so the parser accepts both a "usage"
// object and the fields at the top level of the event.
func parseCodex(data []byte) (Metrics, error) {
	m := Metrics{MCPServerStatus: "unreported"}
	var lastText string
	sawUsage := false
	m.ParseWarnings = jsonLines(data, func(ev map[string]json.RawMessage) {
		switch str(ev["type"]) {
		case "turn.completed":
			m.Turns++
			u := obj(ev["usage"])
			if u == nil {
				u = ev
			}
			if _, ok := u["input_tokens"]; ok {
				sawUsage = true
			}
			in := num(u["input_tokens"])
			cached := num(u["cached_input_tokens"])
			// OpenAI usage counts cached tokens inside input_tokens.
			m.InputTokens += max(in-cached, 0)
			m.CacheReadTokens += cached
			m.OutputTokens += num(u["output_tokens"])
			m.ReasoningTokens += num(u["reasoning_output_tokens"])
		case "turn.failed":
			m.IsError = true
			m.ErrorSubtype = "turn.failed"
			m.ErrorText = errorMessage(ev["error"])
		case "error":
			m.IsError = true
			if m.ErrorSubtype == "" {
				m.ErrorSubtype = "error"
			}
			if t := str(ev["message"]); t != "" {
				m.ErrorText = t
			}
		case "item.completed":
			item := obj(ev["item"])
			switch str(item["type"]) {
			case "agent_message":
				lastText = str(item["text"])
			case "command_execution", "file_change":
				m.addTool(str(item["type"]), false)
			case "web_search":
				m.addTool("web_search", false)
				m.WebCalls++
			case "mcp_tool_call":
				server := str(item["server"])
				if server == "codex" {
					// Codex reports its own built-in tools (code mode) as
					// calls to its internal server "codex": a tool call,
					// not an MCP server of the run.
					m.addTool("codex/"+str(item["tool"]), false)
					break
				}
				isGV := server == "graphvein"
				m.addTool("mcp:"+server+"/"+str(item["tool"]), isGV)
				m.MCPServers = addUnique(m.MCPServers, server)
				if isGV {
					res := obj(item["result"])
					t, s := splitCopies(res["content"])
					if sc, ok := res["structured_content"]; ok && string(sc) != "null" {
						s += int64(len(sc))
					}
					m.addMCPResult(t, s)
				}
			}
		}
	})
	m.FinalText = lastText
	if !sawUsage {
		m.Unreported = append(m.Unreported, "tokens")
	}
	m.Unreported = append(m.Unreported, "cost", "cli_duration")
	return m, nil
}

func errorMessage(raw json.RawMessage) string {
	if s := str(raw); s != "" {
		return s
	}
	return str(obj(raw)["message"])
}
