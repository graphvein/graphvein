package main

import (
	"encoding/json"
	"strings"
)

// parseClaude reads `claude -p --output-format stream-json --verbose` output
// (or the single object of `--output-format json`). Token totals and cost
// come from the final result object; tool calls are the tool_use blocks of
// assistant events; MCP result sizes are the tool_result blocks answering a
// graphvein tool call, which is what the model received.
func parseClaude(data []byte) (Metrics, error) {
	m := Metrics{MCPServerStatus: "absent"}
	mcpIDs := map[string]bool{}
	var lastText string
	sawResult := false
	lastMsgID := ""
	m.ParseWarnings = jsonLines(data, func(ev map[string]json.RawMessage) {
		switch str(ev["type"]) {
		case "system":
			if str(ev["subtype"]) != "init" {
				return
			}
			m.ServedModel = str(ev["model"])
			m.APIKeySource = str(ev["apiKeySource"])
			for _, s := range arr(ev["mcp_servers"]) {
				so := obj(s)
				name := str(so["name"])
				m.MCPServers = addUnique(m.MCPServers, name)
				if name == "graphvein" {
					m.MCPServerStatus = str(so["status"])
				}
			}
		case "assistant":
			msg := obj(ev["message"])
			// One API message may arrive as several assistant events (one
			// per content block) that repeat its usage; count it once.
			if id := str(msg["id"]); id == "" || id != lastMsgID {
				lastMsgID = id
				m.CallUsage.add(claudeUsage(msg["usage"]))
			}
			var text []string
			for _, b := range arr(msg["content"]) {
				bo := obj(b)
				switch str(bo["type"]) {
				case "tool_use":
					name := str(bo["name"])
					isMCP := strings.HasPrefix(name, "mcp__graphvein__")
					m.addTool(name, isMCP)
					if isMCP {
						mcpIDs[str(bo["id"])] = true
					}
					if name == "WebFetch" || name == "WebSearch" {
						m.WebCalls++
					}
				case "text":
					text = append(text, str(bo["text"]))
				}
			}
			if len(text) > 0 {
				lastText = strings.Join(text, "")
			}
		case "user":
			msg := obj(ev["message"])
			for _, b := range arr(msg["content"]) {
				bo := obj(b)
				if str(bo["type"]) != "tool_result" || !mcpIDs[str(bo["tool_use_id"])] {
					continue
				}
				t, s := splitCopies(bo["content"])
				m.addMCPResult(t, s)
			}
		case "result":
			sawResult = true
			u := obj(ev["usage"])
			m.InputTokens = num(u["input_tokens"])
			m.OutputTokens = num(u["output_tokens"])
			m.CacheReadTokens = num(u["cache_read_input_tokens"])
			m.CacheWriteTokens = num(u["cache_creation_input_tokens"])
			for _, mu := range obj(ev["modelUsage"]) {
				m.ReasoningTokens += num(obj(mu)["thinkingTokens"])
			}
			if c, ok := ev["total_cost_usd"]; ok && string(c) != "null" {
				m.HarnessCostUSD = fnum(c)
				m.CostReported = true
			}
			m.Turns = int(num(ev["num_turns"]))
			m.CLIDuration = num(ev["duration_ms"])
			sub := str(ev["subtype"])
			var isErr bool
			_ = json.Unmarshal(ev["is_error"], &isErr)
			if sub == "error_max_turns" {
				m.TurnLimit = true
			}
			if isErr || (sub != "" && sub != "success") {
				m.IsError = true
				m.ErrorSubtype = sub
				m.ErrorText = str(ev["result"])
				if m.ErrorText == "" {
					m.ErrorText = sub
				}
			}
			if r := str(ev["result"]); r != "" {
				lastText = r
			}
			for _, d := range arr(ev["permission_denials"]) {
				m.PermissionDenials = append(m.PermissionDenials, str(obj(d)["tool_name"]))
			}
		}
	})
	m.FinalText = lastText
	m.SawResult = sawResult
	if !sawResult {
		m.Unreported = append(m.Unreported, "tokens", "turns", "cost")
	}
	return m, nil
}

// claudeUsage reads the usage object of one Claude API message.
func claudeUsage(raw json.RawMessage) TokenUsage {
	u := obj(raw)
	return TokenUsage{
		Input:      num(u["input_tokens"]),
		CacheRead:  num(u["cache_read_input_tokens"]),
		CacheWrite: num(u["cache_creation_input_tokens"]),
		Output:     num(u["output_tokens"]),
	}
}

// splitCopies measures one MCP tool result as the model saw it. A client
// that forwards both structuredContent and content shows up in a text-only
// transcript as several blocks, one of them a JSON object. So: a result with
// one block is all text; with several blocks, JSON-object text blocks and
// non-text blocks count as the structured copy and plain text as the text
// copy (when every block is JSON, the first one is the text copy).
func splitCopies(raw json.RawMessage) (text, structured int64) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return int64(len(s)), 0
	}
	blocks := arr(raw)
	if len(blocks) == 1 {
		bo := obj(blocks[0])
		if str(bo["type"]) == "text" {
			return int64(len(str(bo["text"]))), 0
		}
		return 0, int64(len(blocks[0]))
	}
	plainSeen := false
	var jsonSizes []int64
	for _, b := range blocks {
		bo := obj(b)
		if str(bo["type"]) != "text" {
			structured += int64(len(b))
			continue
		}
		t := str(bo["text"])
		if isJSONObject(t) {
			jsonSizes = append(jsonSizes, int64(len(t)))
			continue
		}
		plainSeen = true
		text += int64(len(t))
	}
	for i, n := range jsonSizes {
		if i == 0 && !plainSeen {
			text += n
			continue
		}
		structured += n
	}
	return text, structured
}

func isJSONObject(s string) bool {
	var probe map[string]any
	return json.Unmarshal([]byte(strings.TrimSpace(s)), &probe) == nil && probe != nil
}
