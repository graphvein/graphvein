package main

import (
	"math"
	"os"
	"testing"
	"time"
)

func testPrices(t *testing.T) *PriceFile {
	t.Helper()
	p, err := loadPrices("../prices/deepseek-v4.1-flash-1017.json")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCommittedPriceFile(t *testing.T) {
	p := testPrices(t)
	if p.Model != "deepseek/deepseek-v4.1-flash-1017" || len(p.Sources) == 0 || p.ReadAt == "" {
		t.Fatalf("price file identity: %+v", p)
	}
	for _, tag := range []string{"deepinfra/fp8", "pearl-research/fp8"} {
		if p.endpoint(tag) == nil {
			t.Errorf("preferred endpoint %s missing", tag)
		}
	}
	// The bound must be at least every endpoint's highest price.
	for _, e := range p.Endpoints {
		if e.Tools && (e.maxPrompt() > p.MaxPrompt || e.maxCompletion() > p.MaxCompletion) {
			t.Errorf("%s is above the bound", e.Tag)
		}
	}
	if p.Listed.maxPrompt() > p.MaxPrompt || p.Listed.maxCompletion() > p.MaxCompletion {
		t.Errorf("listed price above the bound")
	}
}

func f64(v float64) *float64 { return &v }

func TestDecideBudget(t *testing.T) {
	p := &PriceFile{Model: "m", MaxPrompt: 0.6, MaxCompletion: 2.4}
	base := BudgetInput{CapUSD: 8, RunCeilingUSD: 0.45, StepContext: 1048576, StepOutput: 32768, UsageStart: 10}
	over := (1048576*0.6 + 32768*2.4) / 1e6
	cases := []struct {
		name   string
		mod    func(*BudgetInput)
		run    bool
		remain float64
	}{
		{"fresh", func(in *BudgetInput) { in.Key.Usage = 10 }, true, 8},
		{"spent by key", func(in *BudgetInput) { in.Key.Usage = 16.2 }, false, 1.8},
		{"ledger ahead of key", func(in *BudgetInput) { in.Key.Usage = 10; in.LedgerUSD = 6.2 }, false, 1.8},
		{"just fits", func(in *BudgetInput) { in.Key.Usage = 10 + 8 - (0.45 + 2*over) }, true, 0.45 + 2*over},
		{"key limit lower", func(in *BudgetInput) { in.Key.Usage = 10; in.Key.LimitRemaining = f64(1) }, false, 1},
		{"zero ceiling", func(in *BudgetInput) { in.Key.Usage = 10; in.RunCeilingUSD = 0 }, false, 8},
	}
	for _, c := range cases {
		in := base
		c.mod(&in)
		d := decideBudget(in, p)
		if d.Run != c.run || math.Abs(d.RemainingUSD-c.remain) > 1e-5 {
			t.Errorf("%s: %+v, want run=%v remaining=%.4f", c.name, d, c.run, c.remain)
		}
		if math.Abs(d.RequiredUSD-round6(in.RunCeilingUSD+2*over)) > 1e-6 {
			t.Errorf("%s: required %.6f", c.name, d.RequiredUSD)
		}
	}
}

func TestParseKeyInfo(t *testing.T) {
	k, err := parseKeyInfo([]byte(`{"data":{"label":"x","usage":1.25,"limit":8,"limit_remaining":6.75}}`))
	if err != nil || k.Usage != 1.25 || k.LimitRemaining == nil || *k.LimitRemaining != 6.75 {
		t.Fatalf("%+v %v", k, err)
	}
	if _, err := parseKeyInfo([]byte(`{"error":{"code":401}}`)); err == nil {
		t.Fatal("a response without data must fail")
	}
}

const endpointsFixture = `{"data":{"id":"a/m","endpoints":[
 {"tag":"cheap/fp8","provider_name":"Cheap","quantization":"fp8","context_length":1000,"pricing":{"prompt":"0.0000001","completion":"0.0000003","input_cache_read":"0.00000001","discount":0},"supported_parameters":["tools"]},
 {"tag":"peak","provider_name":"Peak","quantization":"unknown","context_length":2000,"pricing":{"prompt":"0.0000002","completion":"0.0000004","overrides":[{"prompt":"0.0000005","completion":"0.000002"}]},"supported_parameters":["tools"]},
 {"tag":"notools","provider_name":"NoTools","quantization":"fp4","context_length":9000,"pricing":{"prompt":"0.00001","completion":"0.00001"},"supported_parameters":[]}
]}}`

func TestImportAndCheckPrices(t *testing.T) {
	p, err := importPrices([]byte(endpointsFixture), nil, "a/m", []string{"cheap/fp8"}, "2026-11-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxPrompt != 0.5 || p.MaxCompletion != 2 || p.ContextLength != 2000 {
		t.Fatalf("overrides must raise and tool-less endpoints must not: %+v", p)
	}
	if _, err := importPrices([]byte(endpointsFixture), nil, "a/m", []string{"missing"}, ""); err == nil {
		t.Error("an unknown preferred endpoint must fail")
	}
	if _, err := importPrices([]byte(endpointsFixture), nil, "b/m", nil, ""); err == nil {
		t.Error("a response for another model must fail")
	}
	now, _ := time.Parse(time.RFC3339, "2026-11-05T00:00:00Z")
	if err := checkLive(p, []byte(endpointsFixture), now, 7*24*time.Hour); err != nil {
		t.Errorf("same prices: %v", err)
	}
	higher := []byte(`{"data":{"id":"a/m","endpoints":[{"tag":"x","provider_name":"X","pricing":{"prompt":"0.000001","completion":"0.000001"},"supported_parameters":["tools"]}]}}`)
	if err := checkLive(p, higher, now, 7*24*time.Hour); err == nil {
		t.Error("a live price above the bound must stop the run")
	}
	if err := checkLive(p, nil, now.Add(10*24*time.Hour), 7*24*time.Hour); err == nil {
		t.Error("an old file without a live check must stop the run")
	}
	if err := checkLive(p, nil, now, 7*24*time.Hour); err != nil {
		t.Errorf("a recent file without a live check is accepted: %v", err)
	}
	bad := []byte(`{"data":{"id":"a/m","endpoints":[{"tag":"x","pricing":{"prompt":"-1","completion":"0.1"},"supported_parameters":["tools"]}]}}`)
	if _, err := importPrices(bad, nil, "a/m", nil, ""); err == nil {
		t.Error("an unparsable price must fail, never lower the bound")
	}
}

func TestIdentifyServing(t *testing.T) {
	p, _ := importPrices([]byte(endpointsFixture), nil, "a/m", []string{"cheap/fp8"}, "2026-11-01T00:00:00Z")
	m := Metrics{InputTokens: 1_000_000, OutputTokens: 100_000}
	cheap := endpointCost(p.endpoint("cheap/fp8").PriceSet, m)
	s := identifyServing(p, nil, m, cheap)
	if s.Elsewhere || !s.Unverified || s.Endpoint != "cheap/fp8" || s.Evidence != "price_match" {
		t.Errorf("price match on the preferred endpoint: %+v", s)
	}
	s = identifyServing(p, nil, m, endpointCost(p.endpoint("peak").PriceSet, m))
	if !s.Elsewhere {
		t.Errorf("price match elsewhere: %+v", s)
	}
	s = identifyServing(p, []string{"Peak"}, m, 0)
	if !s.Elsewhere || s.Provider != "Peak" || s.Endpoint != "peak" || s.Unverified {
		t.Errorf("reported provider elsewhere: %+v", s)
	}
	s = identifyServing(p, []string{"Cheap"}, m, 0)
	if s.Elsewhere || s.Quantization != "fp8" {
		t.Errorf("reported preferred provider: %+v", s)
	}
	s = identifyServing(p, nil, m, 0)
	if !s.Unverified || s.Elsewhere || s.Provider != "unconfirmed" {
		t.Errorf("no evidence: %+v", s)
	}
}

func TestWatchPoll(t *testing.T) {
	dir := t.TempDir()
	tr, state := dir+"/t.jsonl", dir+"/t.watch"
	p := &PriceFile{Model: "m", MaxPrompt: 1, MaxCompletion: 2}
	step := `{"type":"step_finish","part":{"tokens":{"input":100000,"output":10000,"reasoning":0,"cache":{"read":100000,"write":0}}}}` + "\n"
	// 200k prompt at 1/M + 10k output at 2/M = 0.22 per step.
	if err := os.WriteFile(tr, []byte(step+`{"type":"text"}`+"\n"+step[:20]), 0o644); err != nil {
		t.Fatal(err)
	}
	v, w, err := watchPoll("opencode", tr, state, p, 0.4, 0, time.Now())
	if err != nil || v != "ok" || math.Abs(w-0.22) > 1e-9 {
		t.Fatalf("first poll: %s %f %v", v, w, err)
	}
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(step[20:])
	f.Close()
	v, w, _ = watchPoll("opencode", tr, state, p, 0.4, 0, time.Now())
	if v != "cost" || math.Abs(w-0.44) > 1e-9 {
		t.Fatalf("the completed partial line must be priced once: %s %f", v, w)
	}
	v, _, _ = watchPoll("opencode", "", dir+"/none", nil, 0, time.Now().Add(-time.Second).UnixMilli(), time.Now())
	if v != "timeout" {
		t.Fatalf("deadline: %s", v)
	}
}
