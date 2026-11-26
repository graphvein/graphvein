// Command gvbench is the Go half of the agent benchmark driver
// (scripts/benchmark/run.sh). It parses agent transcripts, grades answers,
// runs each agent CLI under a timeout and cost watch, decides whether the
// OpenRouter budget allows the next run, and writes the results files.
//
// It uses only the standard library and opens no network connection: the
// driver's curl calls are the only network access of the benchmark.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// listFlag collects a repeatable flag.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gvbench <prompt|question|plan|agentconfig|watch|now-ms|duration-ms|record|notrun|budget|keyusage|signin|prices|digest|readonly|manifest|report|regrade|public-tree|codex-catalog|standin|redact|genids|provider>")
		return 2
	}
	if args[0] == "standin" {
		return standinMain(args[1:], stdin, stdout, stderr)
	}
	err := dispatch(args[0], args[1:], stdin, stdout)
	var ex exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ex):
		if ex.msg != "" {
			fmt.Fprintln(stderr, "gvbench:", redact(ex.msg))
		}
		return ex.code
	default:
		fmt.Fprintln(stderr, "gvbench:", redact(err.Error()))
		return 1
	}
}

// exitError carries a specific exit status for the driver (3 = budget stop,
// 4 = secret found, 5 = sign-in refused).
type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }

func dispatch(cmd string, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	switch cmd {
	case "prompt", "question":
		questions := fs.String("questions", "", "question file")
		id := fs.String("id", "", "question id")
		field := fs.String("field", "", "corpus or commit (question only)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		qs, err := loadQuestions(*questions)
		if err != nil {
			return err
		}
		if cmd == "question" && *id == "" {
			for _, q := range qs {
				fmt.Fprintln(stdout, q.ID)
			}
			return nil
		}
		q, err := findQuestion(qs, *id)
		if err != nil {
			return err
		}
		if cmd == "prompt" {
			_, err = io.WriteString(stdout, buildPrompt(q))
			return err
		}
		switch *field {
		case "corpus":
			fmt.Fprintln(stdout, q.Corpus)
		case "commit":
			fmt.Fprintln(stdout, q.Commit)
		default:
			return fmt.Errorf("unknown field %q", *field)
		}
		return nil

	case "plan":
		agents := fs.String("agents", "", "comma-separated agents")
		modes := fs.String("modes", "base,mcp", "comma-separated modes")
		ids := fs.String("ids", "", "comma-separated question ids")
		reps := fs.Int("reps", 3, "repetitions")
		agentReps := fs.String("agent-reps", "", "per-agent repetitions, e.g. codex=1 (overrides --reps for that agent)")
		seed := fs.Uint64("seed", 1, "shuffle seed")
		noShuffle := fs.Bool("no-shuffle", false, "keep the listed order")
		done := fs.String("done", "", "runs.jsonl whose finished runs are skipped")
		only := fs.String("only", "", "comma-separated run ids or patterns (e.g. codex-visible-mcp-*); other runs are left out")
		if err := fs.Parse(args); err != nil {
			return err
		}
		finished := map[string]bool{}
		if *done != "" {
			rows, err := readRows(*done)
			if err != nil {
				return err
			}
			for _, r := range rows {
				if !strings.HasPrefix(r.Outcome, "not_run") && r.Outcome != outcomeInvalid {
					finished[r.RunID] = true
				}
			}
		}
		override, err := parseAgentReps(*agentReps)
		if err != nil {
			return err
		}
		pats := splitList(*only)
		used := map[string]bool{}
		var lines []string
		for _, p := range makePlanReps(splitList(*agents), splitList(*modes), splitList(*ids), *reps, override, *seed, !*noShuffle) {
			id := runID(p.Agent, p.Mode, p.Question, p.Rep)
			keep := len(pats) == 0
			for _, pat := range pats {
				ok, err := matchesAny(id, []string{pat})
				if err != nil {
					return err
				}
				if ok {
					used[pat], keep = true, true
				}
			}
			if !keep || finished[id] {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s %d %s %s", p.Agent, p.Rep, p.Mode, p.Question))
		}
		for _, pat := range pats {
			if !used[pat] {
				return fmt.Errorf("--rerun %q matches no planned run (check --agents, --questions, --modes and --reps)", pat)
			}
		}
		for _, l := range lines {
			fmt.Fprintln(stdout, l)
		}
		return nil

	case "agentconfig":
		format := fs.String("format", "", "claude, codex or opencode")
		mode := fs.String("mode", "base", "base or mcp")
		out := fs.String("out", "", "file to write (claude, opencode)")
		command := fs.String("mcp-command", "", "MCP server command")
		var mcpArgs, mcpEnv, extra listFlag
		fs.Var(&mcpArgs, "mcp-arg", "MCP server argument (repeatable; {workspace} {store} {corpus} expand)")
		fs.Var(&mcpEnv, "mcp-env", "MCP server environment KEY=VALUE (repeatable)")
		fs.Var(&extra, "codex-config", "extra codex -c override (repeatable)")
		var vars listFlag
		fs.Var(&vars, "var", "placeholder NAME=VALUE (repeatable)")
		model := fs.String("model", "", "OpenRouter model id (opencode)")
		steps := fs.Int("steps", 60, "turn limit (opencode)")
		preferred := fs.String("preferred", "", "comma-separated preferred endpoint tags (opencode)")
		stepOutput := fs.Int64("step-output", 32768, "output tokens per call (opencode)")
		context := fs.Int64("context", 1048576, "context window (opencode)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		var srv *MCPServer
		if *mode == "mcp" {
			if *command == "" {
				return errors.New("mcp mode needs --mcp-command")
			}
			v, err := parseEnvPairs(vars)
			if err != nil {
				return err
			}
			env, err := parseEnvPairs(mcpEnv)
			if err != nil {
				return err
			}
			srv = &MCPServer{Command: expand(*command, v), Env: map[string]string{}}
			for _, a := range mcpArgs {
				srv.Args = append(srv.Args, expand(a, v))
			}
			for k, val := range env {
				srv.Env[k] = expand(val, v)
			}
		}
		switch *format {
		case "claude":
			return writeFile(*out, claudeMCPConfig(srv))
		case "codex":
			for _, o := range codexOverrides(srv, extra) {
				fmt.Fprintln(stdout, o)
			}
			return nil
		case "opencode":
			return writeFile(*out, opencodeConfig(OpencodeOptions{Model: *model, Steps: *steps, Preferred: splitList(*preferred), StepOutput: *stepOutput, Context: *context}, srv))
		}
		return fmt.Errorf("unknown format %q", *format)

	case "watch":
		format := fs.String("format", "opencode", "transcript format: opencode or claude")
		transcript := fs.String("transcript", "", "transcript being written")
		state := fs.String("state", "", "watch state file")
		prices := fs.String("prices", "", "price file (enables the cost watch)")
		ceiling := fs.Float64("run-ceiling", 0, "per-run ceiling in USD at worst-case prices")
		deadline := fs.Int64("deadline-ms", 0, "Unix milliseconds after which the run times out")
		if err := fs.Parse(args); err != nil {
			return err
		}
		var p *PriceFile
		if *prices != "" {
			var err error
			if p, err = loadPrices(*prices); err != nil {
				return err
			}
		}
		verdict, watched, err := watchPoll(*format, *transcript, *state, p, *ceiling, *deadline, time.Now())
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s %.6f\n", verdict, watched)
		return nil

	case "now-ms":
		fmt.Fprintln(stdout, time.Now().UnixMilli())
		return nil

	case "duration-ms":
		if len(args) != 1 {
			return errors.New("usage: gvbench duration-ms DURATION")
		}
		d, err := time.ParseDuration(args[0])
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid duration %q (Go syntax, e.g. 10m)", args[0])
		}
		fmt.Fprintln(stdout, d.Milliseconds())
		return nil

	case "record":
		return recordCmd(fs, args, stdout)

	case "notrun":
		out := fs.String("out", "", "results directory")
		var r Row
		fs.StringVar(&r.Agent, "agent", "", "")
		fs.StringVar(&r.Harness, "harness", "", "")
		fs.StringVar(&r.HarnessVersion, "harness-version", "", "")
		fs.StringVar(&r.Model, "model", "", "")
		fs.StringVar(&r.Mode, "mode", "", "")
		fs.StringVar(&r.Question, "question", "", "")
		fs.StringVar(&r.Corpus, "corpus", "", "")
		fs.IntVar(&r.Rep, "rep", 0, "")
		fs.StringVar(&r.Outcome, "outcome", "", "not_run_cap, not_run_rate_limit or not_run_preflight")
		fs.StringVar(&r.Detail, "detail", "", "")
		fs.StringVar(&r.AuthPath, "auth-path", "", "Claude auth path")
		apiBilling := fs.Bool("api-billing", false, "a Claude run billed to API credits")
		fs.BoolVar(&r.StandIn, "standin", false, "")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if !strings.HasPrefix(r.Outcome, "not_run") {
			return fmt.Errorf("notrun needs a not_run_* outcome, got %q", r.Outcome)
		}
		r.RunID = runID(r.Agent, r.Mode, r.Question, r.Rep)
		r.CostKind = "none"
		if r.Agent == "claude" {
			r.Billing = "subscription"
			if *apiBilling {
				r.Billing = "API billing"
			}
		}
		return appendRow(*out, r, "")

	case "budget":
		agent := fs.String("agent", "opencode", "opencode (OpenRouter cap) or claude (Claude API cap)")
		prices := fs.String("prices", "", "price file")
		in := BudgetInput{}
		fs.Float64Var(&in.CapUSD, "cap", 0, "total cap USD")
		fs.Float64Var(&in.RunCeilingUSD, "run-ceiling", 0, "per-run ceiling USD")
		fs.Int64Var(&in.StepContext, "step-context", 1048576, "prompt tokens bound of one call")
		fs.Int64Var(&in.StepOutput, "step-output", 32768, "output tokens bound of one call")
		fs.Float64Var(&in.UsageStart, "usage-start", 0, "key usage when the benchmark started")
		keyJSON := fs.String("key-json", "", "GET /api/v1/key response (opencode only)")
		runs := fs.String("runs", "", "runs.jsonl for the ledger")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *agent != "opencode" && *agent != "claude" {
			return fmt.Errorf("budget: unknown agent %q", *agent)
		}
		p, err := loadPrices(*prices)
		if err != nil {
			return err
		}
		if *agent == "opencode" {
			data, err := os.ReadFile(*keyJSON)
			if err != nil {
				return fmt.Errorf("reading key response: %w", err)
			}
			if in.Key, err = parseKeyInfo(data); err != nil {
				return err
			}
		}
		if *runs != "" {
			// Every attempt costs money, including superseded invalid ones,
			// so the ledger sums all rows, not the last row per run.
			rows, err := readAllRows(*runs)
			if err != nil {
				return err
			}
			for _, r := range rows {
				if ledgerRow(*agent, r) {
					in.LedgerUSD += r.CostUSD
				}
			}
		}
		d := decideBudget(in, p)
		fmt.Fprintln(stdout, marshalPlain(d))
		if !d.Run {
			return exitError{code: 3}
		}
		return nil

	case "keyusage":
		keyJSON := fs.String("key-json", "", "GET /api/v1/key response")
		field := fs.String("field", "usage", "usage or limit_remaining")
		if err := fs.Parse(args); err != nil {
			return err
		}
		data, err := os.ReadFile(*keyJSON)
		if err != nil {
			return fmt.Errorf("reading key response: %w", err)
		}
		k, err := parseKeyInfo(data)
		if err != nil {
			return err
		}
		switch *field {
		case "usage":
			fmt.Fprintln(stdout, strconv.FormatFloat(k.Usage, 'f', -1, 64))
		case "limit_remaining":
			if k.LimitRemaining == nil {
				fmt.Fprintln(stdout, "none")
			} else {
				fmt.Fprintln(stdout, strconv.FormatFloat(*k.LimitRemaining, 'f', -1, 64))
			}
		}
		return nil

	case "signin":
		agent := fs.String("agent", "", "claude or codex")
		output := fs.String("output", "", "file with the sign-in command's output")
		code := fs.Int("exit", 0, "the sign-in command's exit code")
		allow := fs.Bool("allow-api-billing", false, "")
		accept := fs.Bool("accept-unconfirmed-signin", false, "")
		if err := fs.Parse(args); err != nil {
			return err
		}
		data, _ := os.ReadFile(*output)
		s := classifySignIn(*agent, data, *code, *allow, *accept)
		fmt.Fprintln(stdout, marshalPlain(s))
		if !s.OK {
			return exitError{code: 5}
		}
		return nil

	case "prices":
		return pricesCmd(args, stdout)

	case "digest", "readonly":
		if len(args) != 1 {
			return fmt.Errorf("usage: gvbench %s DIR", cmd)
		}
		if cmd == "readonly" {
			return makeReadOnly(args[0])
		}
		d, n, err := digestTree(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s %d\n", d, n)
		return nil

	case "manifest":
		file := fs.String("file", "", "manifest.json")
		key := fs.String("key", "", "dotted key")
		value := fs.String("value", "", "value")
		valueFile := fs.String("value-file", "", "read the value from this file")
		raw := fs.Bool("json", false, "value is JSON")
		if err := fs.Parse(args); err != nil {
			return err
		}
		v := *value
		if *valueFile != "" {
			data, err := os.ReadFile(*valueFile)
			if err != nil {
				return fmt.Errorf("reading value: %w", err)
			}
			v = strings.TrimRight(string(data), "\r\n")
		}
		return setManifest(*file, *key, v, *raw)

	case "report":
		runs := fs.String("runs", "", "runs.jsonl")
		out := fs.String("out", "", "summary.md")
		if err := fs.Parse(args); err != nil {
			return err
		}
		rows, err := readRows(*runs)
		if err != nil {
			return err
		}
		text := writeReport(rows)
		if err := checkNoSecret([]byte(text)); err != nil {
			return exitError{code: 4, msg: err.Error()}
		}
		return writeFile(*out, text)

	case "regrade":
		dir := fs.String("runs-dir", "", "results directory of one label (runs.jsonl, answers/, summary.md)")
		questions := fs.String("questions", "", "question file to grade against")
		note := fs.String("note", "", "why the runs are re-graded, recorded in each changed row (required)")
		check := fs.Bool("check", false, "only list the rows whose grade would change; write nothing")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *dir == "" || *questions == "" || (*note == "" && !*check) {
			return errors.New("usage: gvbench regrade --runs-dir DIR --questions FILE --note TEXT [--check]")
		}
		qs, err := loadQuestions(*questions)
		if err != nil {
			return err
		}
		sum, err := fileSHA256(*questions)
		if err != nil {
			return err
		}
		recorded := fmt.Sprintf("%s (question file sha256 %s)", *note, sum)
		changes, err := regradeRuns(*dir, qs, recorded, !*check)
		if err != nil {
			if errors.Is(err, errSecretFound) {
				return exitError{code: 4, msg: err.Error()}
			}
			return err
		}
		for _, c := range changes {
			fmt.Fprintf(stdout, "%s %s correct %t -> %t, score %.4f -> %.4f\n", c.RunID, c.Question, c.OldCorrect, c.NewCorrect, c.OldScore, c.NewScore)
		}
		fmt.Fprintf(stdout, "%d row(s) %s\n", len(changes), map[bool]string{true: "would change", false: "re-graded"}[*check])
		if *check || len(changes) == 0 {
			return nil
		}
		rows, err := readRows(filepath.Join(*dir, "runs.jsonl"))
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(*dir, "summary.md"), writeReport(rows))

	case "public-tree":
		lsTree := fs.String("ls-tree", "", "output of git ls-tree -r -z COMMIT")
		exclude := fs.String("exclude", "", "the commit's scripts/public-exclude.txt (git show COMMIT:scripts/public-exclude.txt)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		lt, err := os.ReadFile(*lsTree)
		if err != nil {
			return fmt.Errorf("reading ls-tree output: %w", err)
		}
		ex, err := os.ReadFile(*exclude)
		if err != nil {
			return fmt.Errorf("reading the exclude list: %w", err)
		}
		tree, err := publicTree(lt, ex)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, tree)
		return nil

	case "codex-catalog":
		in := fs.String("in", "", "output of `codex debug models --bundled`")
		out := fs.String("out", "", "catalog file for -c model_catalog_json")
		if err := fs.Parse(args); err != nil {
			return err
		}
		data, err := os.ReadFile(*in)
		if err != nil {
			return fmt.Errorf("reading the Codex model catalog: %w", err)
		}
		cat, n, err := codexVisibleCatalog(data)
		if err != nil {
			return err
		}
		if err := writeFile(*out, string(cat)); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%d models, supports_search_tool false\n", n)
		return nil

	case "sha256":
		if len(args) != 1 {
			return errors.New("usage: gvbench sha256 FILE")
		}
		sum, err := fileSHA256(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, sum)
		return nil

	case "redact":
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		_, err = io.WriteString(stdout, redact(string(data)))
		return err

	case "genids":
		if len(args) != 1 {
			return errors.New("usage: gvbench genids TRANSCRIPT")
		}
		data, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		for _, id := range findGenerationIDs(data) {
			fmt.Fprintln(stdout, id)
		}
		return nil

	case "provider":
		// Reads OpenRouter GET /api/v1/generation responses and prints the
		// provider names they report, one per line.
		for _, f := range args {
			data, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			if name := str(obj(obj(data)["data"])["provider_name"]); name != "" {
				fmt.Fprintln(stdout, name)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", cmd)
}

func recordCmd(fs *flag.FlagSet, args []string, stdout io.Writer) error {
	var in RecordInput
	r := &in.Row
	fs.StringVar(&r.Agent, "agent", "", "claude, codex, codex-visible or opencode")
	fs.StringVar(&r.Harness, "harness", "", "harness name")
	fs.StringVar(&r.HarnessVersion, "harness-version", "", "")
	fs.StringVar(&r.Model, "model", "", "requested model")
	fs.StringVar(&r.Mode, "mode", "", "base or mcp")
	fs.StringVar(&r.Variant, "variant", "", "non-default harness setting of the agent id")
	fs.StringVar(&r.Question, "question", "", "question id")
	fs.IntVar(&r.Rep, "rep", 1, "")
	fs.IntVar(&r.Attempt, "attempt", 1, "")
	fs.BoolVar(&r.StandIn, "standin", false, "")
	questions := fs.String("questions", "", "question file")
	transcript := fs.String("transcript", "", "transcript file")
	stderrFile := fs.String("stderr", "", "stderr file")
	fs.IntVar(&in.Exec.ExitCode, "exit-code", 0, "CLI exit status")
	fs.Int64Var(&in.Exec.WallMS, "wall-ms", 0, "wall time")
	fs.BoolVar(&in.Exec.TimedOut, "timed-out", false, "killed at the timeout")
	fs.BoolVar(&in.Exec.CostCapped, "cost-capped", false, "killed at the per-run ceiling")
	fs.Float64Var(&in.Exec.WatchedUSD, "watched-usd", 0, "worst-case cost seen by the watch")
	fs.StringVar(&in.Exec.StartedAt, "started-at", "", "start time, RFC 3339")
	fs.StringVar(&in.Exec.StartError, "start-error", "", "why the CLI did not start")
	fs.StringVar(&in.Workspace, "workspace", "", "workspace directory")
	fs.StringVar(&in.DigestBefore, "digest-before", "", "")
	fs.StringVar(&in.DigestAfter, "digest-after", "", "")
	fs.BoolVar(&in.SignInBilled, "signin-billed", false, "")
	fs.StringVar(&in.AuthPath, "auth-path", "", "Claude: api_key_bare, subscription or api_signin")
	fs.Int64Var(&in.StepContext, "step-context", 1000000, "prompt-token bound of one model call (Claude worst case)")
	fs.Int64Var(&in.StepOutput, "step-output", 32768, "output-token bound of one model call (Claude worst case)")
	fs.Float64Var(&in.BilledUSD, "billed-usd", 0, "OpenRouter usage delta")
	fs.BoolVar(&in.CostSettled, "cost-settled", false, "the usage delta settled")
	prices := fs.String("prices", "", "price file (opencode; Claude when billed)")
	providers := fs.String("providers", "", "comma-separated provider names from generation lookups")
	out := fs.String("out", "", "results directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	qs, err := loadQuestions(*questions)
	if err != nil {
		return err
	}
	if in.Question, err = findQuestion(qs, r.Question); err != nil {
		return err
	}
	r.Corpus = in.Question.Corpus
	r.RunID = runID(r.Agent, r.Mode, r.Question, r.Rep)
	if in.Transcript, err = os.ReadFile(*transcript); err != nil {
		return fmt.Errorf("reading transcript: %w", err)
	}
	in.Stderr, _ = os.ReadFile(*stderrFile)
	if *prices != "" {
		if in.Prices, err = loadPrices(*prices); err != nil {
			return err
		}
	}
	in.Providers = splitList(*providers)
	row, answer, err := buildRow(in)
	if err != nil {
		return err
	}
	if row.AnswerFile = ""; answer != "" {
		row.AnswerFile = "answers/" + row.RunID + ".json"
	}
	if err := appendRow(*out, row, answer, in.Transcript, in.Stderr); err != nil {
		if errors.Is(err, errSecretFound) {
			return exitError{code: 4, msg: err.Error()}
		}
		return err
	}
	fmt.Fprintf(stdout, "%s %s %t %.4f\n", row.Outcome, row.ErrorKind, row.Correct, row.Score)
	return nil
}

func pricesCmd(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: gvbench prices import|check|show ...")
	}
	fs := flag.NewFlagSet("prices", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "price file")
	endpoints := fs.String("endpoints", "", "endpoints response (import) or live response (check)")
	models := fs.String("models", "", "models list response (import)")
	model := fs.String("model", "", "model id (import)")
	preferred := fs.String("preferred", "", "comma-separated preferred endpoint tags (import)")
	readAt := fs.String("read-at", "", "RFC 3339 time the responses were read (import)")
	maxAge := fs.Duration("max-age", 7*24*time.Hour, "oldest acceptable price file without a live check")
	field := fs.String("field", "", "show: model, preferred, max_prompt, max_completion, read_at")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "import":
		ep, err := os.ReadFile(*endpoints)
		if err != nil {
			return fmt.Errorf("reading endpoints response: %w", err)
		}
		var ml []byte
		if *models != "" {
			if ml, err = os.ReadFile(*models); err != nil {
				return fmt.Errorf("reading models list: %w", err)
			}
		}
		p, err := importPrices(ep, ml, *model, splitList(*preferred), *readAt)
		if err != nil {
			return err
		}
		p.Note = "Max* fields are the highest price over every tool-capable endpoint and time-of-day override, before any discount; the cap uses them with no caching."
		var b strings.Builder
		b.WriteString(marshalIndent(p))
		return writeFile(*file, b.String())
	case "check":
		p, err := loadPrices(*file)
		if err != nil {
			return err
		}
		var live []byte
		if *endpoints != "" {
			live, _ = os.ReadFile(*endpoints)
		}
		return checkLive(p, live, time.Now(), *maxAge)
	case "show":
		p, err := loadPrices(*file)
		if err != nil {
			return err
		}
		switch *field {
		case "model":
			fmt.Fprintln(stdout, p.Model)
		case "preferred":
			fmt.Fprintln(stdout, strings.Join(p.PreferredEndpoints, ","))
		case "max_prompt":
			fmt.Fprintln(stdout, p.MaxPrompt)
		case "max_completion":
			fmt.Fprintln(stdout, p.MaxCompletion)
		case "read_at":
			fmt.Fprintln(stdout, p.ReadAt)
		case "context":
			fmt.Fprintln(stdout, p.ContextLength)
		default:
			return fmt.Errorf("unknown field %q", *field)
		}
		return nil
	}
	return fmt.Errorf("unknown prices command %q", args[0])
}
