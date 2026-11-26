package main

import (
	"encoding/json"
	"regexp"
	"strings"
)

// SignIn is the preflight verdict for one agent CLI. Kind is what the CLI
// reported; OK says whether runs may start. A kind the parser cannot place is
// "unconfirmed" and stops the agent unless the developer accepts it, since
// it might be an API key that bills every run.
type SignIn struct {
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	OK     bool   `json:"ok"`
	Billed bool   `json:"billed"`
	Detail string `json:"detail"`
}

// classifySignIn reads the output and exit code of the sign-in command
// (`claude auth status`, `codex login status`).
func classifySignIn(agent string, output []byte, exitCode int, allowAPIBilling, acceptUnconfirmed bool) SignIn {
	s := SignIn{Agent: agent, Kind: "unconfirmed"}
	text := strings.TrimSpace(string(output))
	switch agent {
	case "claude":
		var st struct {
			LoggedIn   *bool  `json:"loggedIn"`
			AuthMethod string `json:"authMethod"`
		}
		if json.Unmarshal([]byte(text), &st) != nil || st.AuthMethod == "" {
			s.Detail = "auth status output is not the documented JSON"
			break
		}
		s.Kind = st.AuthMethod
		switch st.AuthMethod {
		case "none":
			s.Detail = "not signed in"
			return s
		case "claude.ai", "oauth_token":
			s.OK = exitCode == 0
			s.Detail = "subscription sign-in"
			if !s.OK {
				s.Detail = "auth status exited non-zero"
			}
			return s
		case "api_key", "api_key_helper", "third_party":
			s.Billed = true
			s.OK = exitCode == 0 && allowAPIBilling
			s.Detail = "API billing; needs --allow-api-billing"
			return s
		default:
			s.Detail = "unknown authMethod " + st.AuthMethod
		}
	case "codex":
		low := strings.ToLower(text)
		switch {
		case exitCode != 0 || strings.Contains(low, "not logged in"):
			s.Kind = "none"
			s.Detail = "not signed in"
			return s
		case regexp.MustCompile(`(?i)chatgpt`).MatchString(text):
			s.Kind = "chatgpt"
			s.OK = true
			s.Detail = "ChatGPT sign-in (signed-in wording matched; confirm on the first real run)"
			return s
		case regexp.MustCompile(`(?i)api[ _-]?key`).MatchString(text):
			s.Kind = "api_key"
			s.Billed = true
			s.OK = allowAPIBilling
			s.Detail = "API key sign-in; needs --allow-api-billing"
			return s
		default:
			s.Detail = "signed in, but the kind is not recognised: " + firstLine(text)
		}
	default:
		s.Detail = "no sign-in check for " + agent
	}
	if acceptUnconfirmed && exitCode == 0 {
		s.OK = true
		s.Detail += " (accepted with --accept-unconfirmed-signin)"
	}
	return s
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}
