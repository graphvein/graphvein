package main

import (
	"encoding/json"
	"regexp"
	"strings"
)

// parseOpencode reads `opencode run --format json` output: one event per
// line, {"type","timestamp","sessionID","part"}. Tokens are summed over
// step_finish parts; tool calls are tool_use events, and graphvein MCP tools
// are the ones named "graphvein_*". The format follows the 1.19.6 event code;
// where the served provider appears is not confirmed, so the parser only
// picks up explicit provider fields if a transcript carries them.
func parseOpencode(data []byte) (Metrics, error) {
	m := Metrics{MCPServerStatus: "unreported"}
	var stepText []string
	var lastText string
	sawStep := false
	m.ParseWarnings = jsonLines(data, func(ev map[string]json.RawMessage) {
		part := obj(ev["part"])
		switch str(ev["type"]) {
		case "step_start":
			stepText = nil
		case "step_finish":
			sawStep = true
			m.Turns++
			t := obj(part["tokens"])
			c := obj(t["cache"])
			m.InputTokens += num(t["input"])
			m.OutputTokens += num(t["output"])
			m.ReasoningTokens += num(t["reasoning"])
			m.CacheReadTokens += num(c["read"])
			m.CacheWriteTokens += num(c["write"])
			m.HarnessCostUSD += fnum(part["cost"])
			if len(stepText) > 0 {
				lastText = strings.Join(stepText, "")
			}
			if str(part["reason"]) == "length" {
				m.ErrorSubtype = "length"
			}
		case "text":
			stepText = append(stepText, str(part["text"]))
		case "tool_use":
			name := str(part["tool"])
			isMCP := strings.HasPrefix(name, "graphvein_")
			m.addTool(name, isMCP)
			if name == "webfetch" || name == "websearch" {
				m.WebCalls++
			}
			state := obj(part["state"])
			if errText := str(state["error"]); str(state["status"]) == "error" && isPermissionError(errText) {
				if strings.Contains(errText, "external_directory") {
					m.OutsideDenials = append(m.OutsideDenials, errText)
				} else {
					m.PermissionDenials = append(m.PermissionDenials, name)
					m.PermissionErrors = append(m.PermissionErrors, errText)
				}
			}
			if isMCP {
				text := int64(len(str(state["output"])))
				var structured int64
				md := obj(state["metadata"])
				for _, k := range []string{"structuredContent", "structured_content"} {
					if v, ok := md[k]; ok && string(v) != "null" {
						structured += int64(len(v))
					}
				}
				m.addMCPResult(text, structured)
			}
		case "error":
			m.IsError = true
			m.ErrorSubtype = "error"
			e := obj(ev["error"])
			msg := str(obj(e["data"])["message"])
			if msg == "" {
				msg = errorMessage(ev["error"])
			}
			if msg == "" {
				msg = str(e["name"])
			}
			if code := num(obj(e["data"])["statusCode"]); code == 429 {
				msg += " (429)"
			}
			m.ErrorText = msg
		}
	})
	if len(stepText) > 0 {
		lastText = strings.Join(stepText, "")
	}
	m.FinalText = lastText
	for _, p := range providerRE.FindAllSubmatch(data, -1) {
		if v := string(p[1]); !strings.EqualFold(v, "openrouter") {
			m.ProviderNames = addUnique(m.ProviderNames, v)
		}
	}
	m.GenerationIDs = findGenerationIDs(data)
	if !sawStep {
		m.Unreported = append(m.Unreported, "tokens", "turns")
	}
	m.Unreported = append(m.Unreported, "cli_duration")
	return m, nil
}

// isPermissionError recognises opencode 1.19.6's permission errors: a
// rejected ask ("The user rejected permission to use this specific tool
// call", also what `opencode run` answers to every ask) and a deny rule
// ("The user has specified a rule which prevents you from using this
// specific tool call", followed by the matching rules as JSON, which name
// the permission, e.g. "external_directory").
func isPermissionError(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "permission") || strings.Contains(low, "specified a rule")
}

var providerRE = regexp.MustCompile(`"provider(?:_name)?"\s*:\s*"([^"]+)"`)
