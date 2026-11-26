package main

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func claudeTestPrices(t *testing.T) *PriceFile {
	t.Helper()
	p, err := loadPrices("../prices/claude-opus-5-5.json")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The committed Anthropic price file holds the developer's pinned prices,
// and cache writes are bounded by the 2x (1-hour TTL) multiplier.
func TestCommittedClaudePriceFile(t *testing.T) {
	p := claudeTestPrices(t)
	if p.Model != "claude-opus-5-5" || p.MaxPrompt != 4 || p.MaxCompletion != 20 || p.Listed.CacheRead != 0.2 {
		t.Fatalf("prices: %+v", p)
	}
	if p.MaxCacheWrite != 2*p.MaxPrompt || p.Listed.CacheWrite != p.MaxCacheWrite {
		t.Errorf("cache writes must be booked at 2x input: %+v", p)
	}
	if p.ContextLength != 1000000 || p.ReadAt == "" || len(p.Sources) == 0 {
		t.Errorf("identity: %+v", p)
	}
}

func TestClaudeBookedCost(t *testing.T) {
	p := claudeTestPrices(t)
	// One call of the worst case at 1,000 prompt and 100 output tokens:
	// 1000 x 8 (cache-write bound) + 100 x 20 = 0.01 USD.
	worst := p.worstCost(1000, 100)
	if math.Abs(worst-0.01) > 1e-12 {
		t.Fatalf("worst call %f", worst)
	}
	reported, _ := parseTranscript("claude", canned(t, "claude", "base"))
	noCost := reported
	noCost.CostReported = false
	noResult := Metrics{CallUsage: TokenUsage{Input: 100, CacheRead: 1000, CacheWrite: 100, Output: 10}}
	cases := []struct {
		name    string
		m       Metrics
		watched float64
		cost    float64
		kind    string
	}{
		{"reported", reported, 0, 0.04125, costAPIBilled},
		// 12 x 4 + 38100 x 0.2 + 3400 x 8 + 510 x 20 = 45068 per million.
		{"missing cost, result tokens", noCost, 0, 0.045068, costAPIBilledWorstCase},
		{"missing cost, watch higher", noCost, 0.05, 0.05, costAPIBilledWorstCase},
		// 100 x 4 + 1000 x 0.2 + 100 x 8 + 10 x 20 = 1600, plus two worst calls.
		{"no result object", noResult, 0, 0.0016 + 2*worst, costAPIBilledWorstCase},
		{"no result object, nothing seen", Metrics{}, 0, 2 * worst, costAPIBilledWorstCase},
	}
	for _, c := range cases {
		cost, kind, err := claudeBookedCost(p, c.m, c.watched, 1000, 100)
		if err != nil || kind != c.kind || math.Abs(cost-c.cost) > 1e-6 {
			t.Errorf("%s: %f %s %v, want %f %s", c.name, cost, kind, err, c.cost, c.kind)
		}
	}
	if _, _, err := claudeBookedCost(nil, noCost, 0, 1000, 100); err == nil {
		t.Error("a missing cost without a price file must fail, never book zero")
	}
}

func TestClaudeRowBilling(t *testing.T) {
	qs, err := loadQuestions(publicQuestions)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := findQuestion(qs, "BQ-1")
	in := RecordInput{Row: Row{Agent: "claude", Mode: "base", Question: "BQ-1", RunID: "r"}, Question: q,
		Transcript: canned(t, "claude", "base"), AuthPath: authSubscription}
	row, _, err := buildRow(in)
	if err != nil || row.Billing != "subscription" || row.CostKind != "estimate_not_billed" || row.AuthPath != authSubscription {
		t.Errorf("subscription: %+v %v", row, err)
	}
	in.AuthPath = authAPIKeyBare
	in.Prices = claudeTestPrices(t)
	row, _, err = buildRow(in)
	if err != nil || row.Billing != "API billing" || row.CostKind != costAPIBilled || row.CostUSD != 0.04125 {
		t.Errorf("API key path: %+v %v", row, err)
	}
	in.Prices = nil
	in.Transcript = []byte(strings.Replace(string(in.Transcript), `"total_cost_usd":0.04125,`, "", 1))
	if _, _, err := buildRow(in); err == nil {
		t.Error("an API-billed run with a missing cost and no price file must fail")
	}
}

func TestClaudeLedgerAndBudget(t *testing.T) {
	rows := []Row{
		{Agent: "claude", CostKind: costAPIBilled, CostUSD: 1},
		{Agent: "claude", CostKind: costAPIBilledWorstCase, CostUSD: 2},
		{Agent: "claude", CostKind: "estimate_not_billed", CostUSD: 5}, // subscription: not spend
		{Agent: "opencode", CostKind: "billed", CostUSD: 7},
	}
	var claude, opencode float64
	for _, r := range rows {
		if ledgerRow("claude", r) {
			claude += r.CostUSD
		}
		if ledgerRow("opencode", r) {
			opencode += r.CostUSD
		}
	}
	if claude != 3 || opencode != 7 {
		t.Errorf("ledgers: claude %f opencode %f", claude, opencode)
	}
	p := claudeTestPrices(t)
	// Defaults: cap 60, ceiling 3, one call 1,000,000 x 8 + 32768 x 20 per
	// million = 8.65536, so an attempt needs 3 + 2 x 8.65536 = 20.31072 left.
	in := BudgetInput{CapUSD: 60, RunCeilingUSD: 3, StepContext: 1000000, StepOutput: 32768}
	in.LedgerUSD = 60 - 20.31072
	if d := decideBudget(in, p); !d.Run || math.Abs(d.RequiredUSD-20.31072) > 1e-6 {
		t.Errorf("just fits: %+v", d)
	}
	in.LedgerUSD += 0.001
	if d := decideBudget(in, p); d.Run {
		t.Errorf("must stop: %+v", d)
	}
}

func TestClaudeWatch(t *testing.T) {
	dir := t.TempDir()
	tr, state := dir+"/t.jsonl", dir+"/t.watch"
	p := claudeTestPrices(t)
	// Two events of message m1 (one per content block) and one of m2; each
	// call costs 1000 x 4 + 1000 x 20 = 0.024.
	ev := func(id string) string {
		return `{"type":"assistant","message":{"id":"` + id + `","usage":{"input_tokens":1000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1000}}}` + "\n"
	}
	if err := os.WriteFile(tr, []byte(ev("m1")+ev("m1")), 0o644); err != nil {
		t.Fatal(err)
	}
	v, w, err := watchPoll("claude", tr, state, p, 0.04, 0, time.Now())
	if err != nil || v != "ok" || math.Abs(w-0.024) > 1e-9 {
		t.Fatalf("repeated message id counted once: %s %f %v", v, w, err)
	}
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(ev("m1") + ev("m2"))
	f.Close()
	v, w, _ = watchPoll("claude", tr, state, p, 0.04, 0, time.Now())
	if v != "cost" || math.Abs(w-0.048) > 1e-9 {
		t.Fatalf("the id survives between polls; the second call crosses the ceiling: %s %f", v, w)
	}
}

func TestParseClaudeAuthAndCost(t *testing.T) {
	m, _ := parseTranscript("claude", canned(t, "claude", "base"))
	if !m.SawResult || !m.CostReported || m.APIKeySource != "none" {
		t.Errorf("result, cost and key source: %+v", m)
	}
	// Three distinct messages: 4+6+2 input, 2100+900+400 writes.
	if m.CallUsage.Input != 12 || m.CallUsage.CacheWrite != 3400 || m.CallUsage.Output != 510 {
		t.Errorf("call usage: %+v", m.CallUsage)
	}
	cut := strings.Replace(string(canned(t, "claude", "base")), `"total_cost_usd":0.04125,`, "", 1)
	m, _ = parseTranscript("claude", []byte(cut))
	if m.CostReported || m.HarnessCostUSD != 0 {
		t.Errorf("a missing total_cost_usd is not reported: %+v", m)
	}
}

func TestRedactBothKeys(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-firstsecret")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-secondsecret")
	if got := redact("a sk-or-v1-firstsecret b sk-ant-api03-secondsecret c"); got != "a [REDACTED] b [REDACTED] c" {
		t.Errorf("redact: %s", got)
	}
	if err := checkNoSecret([]byte("ok"), []byte("x sk-ant-api03-secondsecret")); err != errSecretFound {
		t.Errorf("the Anthropic key must stop a write: %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "short")
	if got := redact("short text"); got != "short text" {
		t.Errorf("values under 8 bytes are not keys: %s", got)
	}
}
