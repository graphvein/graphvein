package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The stand-in agent replays canned transcripts in each CLI's output format
// so the driver, parsers, grader and cap run end to end with no CLI, no
// sign-in and no spend. Claude and opencode transcripts follow the
// documented fields; Codex's are provisional until a real capture exists.
//
//go:embed testdata/standin
var standinFS embed.FS

// Scenarios a stand-in run can play.
const (
	scCorrect   = "correct"
	scWrong     = "wrong"
	scMalformed = "malformed"
	scHang      = "hang"
	scRateLimit = "ratelimit"
	scCostly    = "costly"
	scElsewhere = "elsewhere"
	// scNoCost drops total_cost_usd from Claude's result object, so the run
	// is booked at its worst case; scLeak puts the ANTHROPIC_API_KEY value
	// into the transcript and stderr, which the driver must refuse to write.
	scNoCost = "nocost"
	scLeak   = "leak"
)

// standinMain implements `gvbench standin --format F [CLI arguments...]`.
func standinMain(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "--format" {
		fmt.Fprintln(stderr, "usage: gvbench standin --format claude|codex|opencode [arguments]")
		return 2
	}
	format, rest := args[1], args[2:]
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	switch {
	case hasArg(rest, "--version"):
		fmt.Fprintf(stdout, "0.0.0-standin (%s stand-in)\n", format)
		return 0
	case format == "claude" && len(rest) >= 2 && rest[0] == "auth" && rest[1] == "status":
		fmt.Fprintln(stdout, `{"loggedIn": true, "authMethod": "claude.ai", "apiProvider": "firstParty"}`)
		return 0
	case format == "codex" && len(rest) >= 2 && rest[0] == "login" && rest[1] == "status":
		fmt.Fprintln(stdout, "Logged in using ChatGPT")
		return 0
	case format == "codex" && len(rest) >= 3 && rest[0] == "debug" && rest[1] == "models" && rest[2] == "--bundled":
		// The shape of Codex 0.171.0's bundled catalog, cut to one model.
		fmt.Fprintln(stdout, `{"models":[{"slug":"gpt-6.2-sol","supports_search_tool":true,"tool_mode":"code_mode_only","context_window":500000}]}`)
		return 0
	}
	// A codex-visible run passes the rewritten catalog; like Codex, the
	// stand-in refuses a catalog it cannot read, and it also refuses one
	// that still offers tool search, which would defer the MCP tools.
	if format == "codex" {
		if err := standinCheckCatalog(rest); err != nil {
			fmt.Fprintln(stderr, "stand-in:", err)
			return 1
		}
	}
	// Real CLIs read the prompt; so does the stand-in, to keep pipes honest.
	if format != "opencode" {
		_, _ = io.Copy(io.Discard, stdin)
	}
	// Like the real CLI, --bare authenticates only with ANTHROPIC_API_KEY.
	bare := format == "claude" && hasArg(rest, "--bare")
	if bare && os.Getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Fprintln(stderr, "stand-in: --bare needs ANTHROPIC_API_KEY (OAuth and keychain are never read)")
		return 1
	}
	// Like the real CLI, opencode prefers a login stored by `opencode auth
	// login` (auth.json in its data directory) over OPENROUTER_API_KEY, so a
	// run that can see the developer's data directory may bill another
	// account. The stand-in refuses to run unless the driver gave it its own
	// config, data and state homes and none holds a stored login.
	if format == "opencode" {
		if err := standinOpencodeIsolated(); err != nil {
			fmt.Fprintln(stderr, "stand-in:", err)
			return 1
		}
	}
	mode := standinMode(format, rest)
	qid := os.Getenv("GV_BENCH_QUESTION_ID")
	scenario := pickScenario(os.Getenv("GV_BENCH_STANDIN_SCENARIOS"), format, qid, mode)
	final, err := standinFinal(qid, scenario)
	if err != nil {
		fmt.Fprintln(stderr, "stand-in:", err)
		return 2
	}
	if scenario == scLeak {
		key := os.Getenv("ANTHROPIC_API_KEY")
		fmt.Fprintln(stderr, "stand-in: debug key", key)
		final = "The key is " + key + "\n\n" + final
	}
	w := bufio.NewWriter(stdout)
	defer w.Flush()
	bill := newStandinBiller(format, scenario)

	name := mode
	switch scenario {
	case scRateLimit:
		name = "ratelimit"
	case scCostly:
		return standinCostly(w, bill)
	}
	tmpl, err := standinFS.ReadFile(path.Join("testdata/standin", format, name+".jsonl"))
	if err != nil {
		fmt.Fprintln(stderr, "stand-in:", err)
		return 2
	}
	finalJSON, _ := json.Marshal(final)
	lines := strings.Split(strings.TrimRight(string(tmpl), "\n"), "\n")
	for i, line := range lines {
		line = strings.ReplaceAll(line, "{{FINAL}}", string(finalJSON))
		if bare {
			line = strings.ReplaceAll(line, `"apiKeySource":"none"`, `"apiKeySource":"ANTHROPIC_API_KEY"`)
		}
		if scenario == scNoCost {
			line = totalCostRE.ReplaceAllString(line, "")
		}
		fmt.Fprintln(w, line)
		bill.line([]byte(line))
		if scenario == scHang && i == 0 {
			w.Flush()
			time.Sleep(time.Hour)
		}
	}
	if scenario == scRateLimit {
		return 1
	}
	return 0
}

var totalCostRE = regexp.MustCompile(`"total_cost_usd":[0-9.eE+-]+,`)

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// standinMode tells base from mcp the way the real CLI would see it: from
// the MCP configuration the driver passed.
func standinMode(format string, args []string) string {
	switch format {
	case "claude":
		for i, a := range args {
			if a == "--mcp-config" && i+1 < len(args) {
				data, _ := os.ReadFile(args[i+1])
				if strings.Contains(string(data), `"graphvein"`) {
					return "mcp"
				}
			}
		}
	case "codex":
		for _, a := range args {
			if strings.HasPrefix(a, "mcp_servers.graphvein.command=") {
				return "mcp"
			}
		}
	case "opencode":
		data, _ := os.ReadFile(os.Getenv("OPENCODE_CONFIG"))
		var cfg struct {
			MCP map[string]any `json:"mcp"`
		}
		if json.Unmarshal(data, &cfg) == nil && cfg.MCP["graphvein"] != nil {
			return "mcp"
		}
	}
	return "base"
}

// pickScenario reads "format question mode scenario" lines ("*" matches
// anything); the first match wins and the default is a correct answer.
func pickScenario(file, format, qid, mode string) string {
	if file == "" {
		return scCorrect
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return scCorrect
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if wild(f[0], format) && wild(f[1], qid) && wild(f[2], mode) {
			return f[3]
		}
	}
	return scCorrect
}

func wild(pattern, v string) bool { return pattern == "*" || pattern == v }

// standinFinal builds the final message: the verified answer, a wrong one,
// or text without a JSON block.
func standinFinal(qid, scenario string) (string, error) {
	if scenario == scMalformed {
		return "I looked through the repository. The answer is probably in the routes folder, but I could not finish.", nil
	}
	qs, err := loadQuestions(os.Getenv("GV_BENCH_STANDIN_QUESTIONS"))
	if err != nil {
		return "", err
	}
	q, err := findQuestion(qs, qid)
	if err != nil {
		return "", err
	}
	answer := map[string]json.RawMessage{}
	for k, v := range q.Expected {
		answer[k] = v
	}
	if scenario == scWrong {
		for k, v := range answer {
			answer[k] = wrongValue(v)
		}
	}
	return "Here is what I found.\n\n```json\n" + marshalPlain(answer) + "\n```\n", nil
}

// wrongValue drops the first element of a list and adds a bogus one, or moves
// a scalar location far away, so the grade is deterministic and not 1.
func wrongValue(v json.RawMessage) json.RawMessage {
	var list []string
	if json.Unmarshal(v, &list) == nil {
		if len(list) > 0 {
			list = list[1:]
		}
		list = append(list, "GET /not/a/route")
		return json.RawMessage(marshalPlain(list))
	}
	var s string
	_ = json.Unmarshal(v, &s)
	if loc, ok := parseLoc(s, ""); ok {
		s = fmt.Sprintf("%s:%d", loc.file, loc.line+50)
	}
	return json.RawMessage(marshalPlain(s))
}

// standinBiller simulates OpenRouter billing for opencode runs: every
// step_finish adds its cost at an endpoint's price to the usage in the key
// file, as the real key's usage would grow while a run streams.
type standinBiller struct {
	file   string
	prices *PriceSet
}

func newStandinBiller(format, scenario string) *standinBiller {
	b := &standinBiller{}
	if format != "opencode" {
		return b
	}
	b.file = os.Getenv("GV_BENCH_STANDIN_KEYFILE")
	p, err := loadPrices(os.Getenv("GV_BENCH_STANDIN_PRICES"))
	if err != nil || b.file == "" {
		b.file = ""
		return b
	}
	tag := "deepinfra/fp8"
	if len(p.PreferredEndpoints) > 0 {
		tag = p.PreferredEndpoints[0]
	}
	if scenario == scElsewhere {
		// Bill at the most expensive endpoint, which is never preferred.
		best := 0.0
		for _, e := range p.Endpoints {
			if e.Tools && e.Prompt > best {
				best, tag = e.Prompt, e.Tag
			}
		}
	}
	if e := p.endpoint(tag); e != nil {
		ps := e.PriceSet
		b.prices = &ps
	}
	return b
}

func (b *standinBiller) line(line []byte) {
	if b.file == "" || b.prices == nil {
		return
	}
	m, _ := parseOpencode(line)
	cost := endpointCost(*b.prices, m)
	if cost == 0 {
		return
	}
	data, err := os.ReadFile(b.file)
	if err != nil {
		return
	}
	var doc map[string]map[string]any
	if json.Unmarshal(data, &doc) != nil || doc["data"] == nil {
		return
	}
	usage, _ := doc["data"]["usage"].(float64)
	doc["data"]["usage"] = round6(usage + cost)
	tmp := b.file + ".tmp"
	if os.WriteFile(tmp, []byte(marshalPlain(doc)+"\n"), 0o644) == nil {
		_ = os.Rename(tmp, b.file)
	}
}

// standinCostly streams large steps until the runner's per-run ceiling
// kills it; it never answers.
func standinCostly(w *bufio.Writer, bill *standinBiller) int {
	for i := 1; i <= 200; i++ {
		lines := []string{
			fmt.Sprintf(`{"type":"step_start","timestamp":%d,"sessionID":"ses_standin","part":{"id":"prt_s%d","sessionID":"ses_standin","messageID":"msg_%d","type":"step-start"}}`, i, i, i),
			fmt.Sprintf(`{"type":"tool_use","timestamp":%d,"sessionID":"ses_standin","part":{"id":"prt_t%d","sessionID":"ses_standin","messageID":"msg_%d","type":"tool","callID":"call_%d","tool":"read","state":{"status":"completed","input":{"filePath":"README.md"},"output":"...","title":"README.md","metadata":{},"time":{"start":%d,"end":%d}}}}`, i, i, i, i, i, i),
			fmt.Sprintf(`{"type":"step_finish","timestamp":%d,"sessionID":"ses_standin","part":{"id":"prt_f%d","sessionID":"ses_standin","messageID":"msg_%d","type":"step-finish","reason":"tool-calls","cost":0.01,"tokens":{"input":200000,"output":2000,"reasoning":0,"cache":{"read":0,"write":0}}}}`, i, i, i),
		}
		for _, l := range lines {
			fmt.Fprintln(w, l)
			bill.line([]byte(l))
		}
		w.Flush()
		time.Sleep(200 * time.Millisecond)
	}
	return 0
}

// standinOpencodeIsolated checks the XDG homes opencode reads its config,
// stored logins and state from: each must be set (unset, opencode falls back
// to the user's home directory) and the data home must hold no auth.json.
func standinOpencodeIsolated() error {
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		if os.Getenv(name) == "" {
			return fmt.Errorf("opencode: %s is not set, so the user's own directory would be read", name)
		}
	}
	auth := filepath.Join(os.Getenv("XDG_DATA_HOME"), "opencode", "auth.json")
	if _, err := os.Stat(auth); err == nil {
		return fmt.Errorf("opencode: a stored login (%s) would be used instead of OPENROUTER_API_KEY", auth)
	}
	return nil
}

// standinCheckCatalog checks a -c model_catalog_json=PATH override, if any.
func standinCheckCatalog(args []string) error {
	for _, a := range args {
		v, ok := strings.CutPrefix(a, "model_catalog_json=")
		if !ok {
			continue
		}
		data, err := os.ReadFile(strings.Trim(v, `"`))
		if err != nil {
			return fmt.Errorf("model_catalog_json: %w", err)
		}
		var cat struct {
			Models []struct {
				SupportsSearchTool bool `json:"supports_search_tool"`
			} `json:"models"`
		}
		if err := json.Unmarshal(data, &cat); err != nil || len(cat.Models) == 0 {
			return fmt.Errorf("model_catalog_json: not a model catalog")
		}
		for _, m := range cat.Models {
			if m.SupportsSearchTool {
				return fmt.Errorf("model_catalog_json: a model still offers tool search")
			}
		}
	}
	return nil
}
