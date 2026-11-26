package main

import (
	"fmt"
	"os"
	"strings"
)

// MCPServer is the stdio command each agent starts in mcp mode. The runner
// only passes it on; nothing here depends on what the command does.
type MCPServer struct {
	Command string
	Args    []string
	Env     map[string]string
}

// expand replaces {name} placeholders (workspace, store, corpus) in an MCP
// argument or environment value.
func expand(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// claudeMCPConfig is the --mcp-config file. In base mode it lists no server;
// together with --strict-mcp-config that keeps every other MCP source out.
func claudeMCPConfig(srv *MCPServer) string {
	servers := map[string]any{}
	if srv != nil {
		servers["graphvein"] = map[string]any{
			"type":    "stdio",
			"command": srv.Command,
			"args":    nonNil(srv.Args),
			"env":     nonNilMap(srv.Env),
		}
	}
	return marshalPlain(map[string]any{"mcpServers": servers}) + "\n"
}

// codexOverrides returns the `-c key=value` overrides (TOML values) for
// codex exec. --ignore-user-config already drops the developer's own MCP
// servers, so base mode needs only the shared settings.
func codexOverrides(srv *MCPServer, extra []string) []string {
	out := []string{`approval_policy="never"`}
	out = append(out, extra...)
	if srv != nil {
		out = append(out,
			"mcp_servers.graphvein.command="+tomlString(srv.Command),
			"mcp_servers.graphvein.args="+tomlArray(srv.Args),
			"mcp_servers.graphvein.startup_timeout_sec=60",
			"mcp_servers.graphvein.tool_timeout_sec=60",
		)
		if len(srv.Env) > 0 {
			keys := sortedKeys(srv.Env)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, tomlString(k)+" = "+tomlString(srv.Env[k]))
			}
			out = append(out, "mcp_servers.graphvein.env={ "+strings.Join(parts, ", ")+" }")
		}
	}
	return out
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlArray(items []string) string {
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = tomlString(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// OpencodeOptions are the benchmark settings of the opencode config.
type OpencodeOptions struct {
	Model      string // OpenRouter model id, without the "openrouter/" prefix
	Steps      int
	Preferred  []string
	StepOutput int64
	Context    int64
}

// opencodeConfig is the OPENCODE_CONFIG file: the model through OpenRouter
// with the preferred endpoints first and no fallback, file tools only
// (edit, bash and web fetch denied), the turn limit, and the graphvein MCP
// server in mcp mode.
//
// No permission may be left at "ask": `opencode run` rejects every ask, and
// the rejected tool call fails with a permission error. opencode 1.19.6's
// built-in agent asks for two things a read-only run meets: a path outside
// the workspace (external_directory, "*": "ask") and a third identical tool
// call in a row (doom_loop). In the first full run these rejections voided
// ten base-mode attempts. external_directory is therefore denied outright
// (the run stays in the workspace; opencode still allows its own tool-output
// directory) and doom_loop is allowed (the turn limit, timeout and cost
// ceiling bound a loop, as for the other agents, which have no such guard).
func opencodeConfig(o OpencodeOptions, srv *MCPServer) string {
	cfg := map[string]any{
		"$schema":    "https://opencode.ai/config.json",
		"autoupdate": false,
		"share":      "disabled",
		"model":      "openrouter/" + o.Model,
		"provider": map[string]any{
			"openrouter": map[string]any{
				"models": map[string]any{
					o.Model: map[string]any{
						"limit": map[string]any{"context": o.Context, "output": o.StepOutput},
						"options": map[string]any{
							"provider": map[string]any{
								"order":           nonNil(o.Preferred),
								"allow_fallbacks": false,
							},
						},
					},
				},
			},
		},
		"permission": map[string]any{
			"edit":               "deny",
			"bash":               "deny",
			"webfetch":           "deny",
			"websearch":          "deny",
			"read":               "allow",
			"grep":               "allow",
			"glob":               "allow",
			"list":               "allow",
			"graphvein_*":        "allow",
			"external_directory": "deny",
			"doom_loop":          "allow",
		},
		"agent": map[string]any{
			"build": map[string]any{"steps": o.Steps},
		},
	}
	mcp := map[string]any{}
	if srv != nil {
		mcp["graphvein"] = map[string]any{
			"type":        "local",
			"command":     append([]string{srv.Command}, srv.Args...),
			"environment": nonNilMap(srv.Env),
			"enabled":     true,
			"timeout":     60000,
		}
	}
	cfg["mcp"] = mcp
	return marshalPlain(cfg) + "\n"
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// parseEnvPairs turns K=V arguments into a map, keeping the last value.
func parseEnvPairs(pairs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("environment entry %q is not KEY=VALUE", p)
		}
		out[k] = v
	}
	return out, nil
}

func writeFile(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
