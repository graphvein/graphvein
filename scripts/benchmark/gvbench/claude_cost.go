package main

import (
	"errors"
	"math"
)

// Claude auth paths. With ANTHROPIC_API_KEY set the driver runs Claude Code
// with --bare, which reads only that key, so every run bills the key's API
// credits; without it the run uses the CLI's own sign-in (a subscription, or
// an API-key sign-in the developer allowed with --allow-api-billing).
const (
	authAPIKeyBare   = "api_key_bare"
	authSubscription = "subscription"
	authAPISignIn    = "api_signin"
)

// Cost kinds of a Claude run billed to API credits.
const (
	costAPIBilled          = "api_billed"
	costAPIBilledWorstCase = "api_billed_worst_case"
)

// claudeBilled reports whether a Claude run's cost is real API spend.
func claudeBilled(authPath string, signInBilled bool) bool {
	return authPath == authAPIKeyBare || authPath == authAPISignIn || signInBilled
}

// claudeCallCost prices token counts at the Claude price file: uncached
// input at the input price, cache reads at the cache-read price, cache
// writes at the cache-write bound (the 2x input price of the 1-hour TTL, the
// higher of the two documented multipliers) and output at the output price.
func claudeCallCost(p *PriceFile, u TokenUsage) float64 {
	cacheRead := p.Listed.CacheRead
	if cacheRead <= 0 {
		cacheRead = p.MaxPrompt // no documented read price: book it as input
	}
	cacheWrite := math.Max(p.MaxCacheWrite, p.MaxPrompt)
	return (float64(u.Input)*p.MaxPrompt + float64(u.CacheRead)*cacheRead +
		float64(u.CacheWrite)*cacheWrite + float64(u.Output)*p.MaxCompletion) / 1e6
}

// claudeBookedCost is what an API-billed Claude run costs the cap. Claude
// Code's own total_cost_usd is used when the result object carries it. A
// missing cost is booked at a worst case: the larger of the result's token
// totals (thinking tokens added to output, which can only overcount) and the
// per-call usage the stream showed, priced as above; a run that ended
// without a result object (killed at the timeout or the per-run ceiling)
// also books two more model calls at the full bound, the call in flight
// when the kill landed and one more, the same overshoot rule as the cap.
func claudeBookedCost(p *PriceFile, m Metrics, watchedUSD float64, stepContext, stepOutput int64) (float64, string, error) {
	if m.CostReported {
		return round6(m.HarnessCostUSD), costAPIBilled, nil
	}
	if p == nil {
		return 0, "", errors.New("an API-billed Claude run without a reported cost needs the Claude price file (--prices)")
	}
	cost := maxf(watchedUSD, claudeCallCost(p, m.CallUsage))
	if m.SawResult {
		total := TokenUsage{Input: m.InputTokens, CacheRead: m.CacheReadTokens, CacheWrite: m.CacheWriteTokens, Output: m.OutputTokens + m.ReasoningTokens}
		cost = maxf(cost, claudeCallCost(p, total))
	} else {
		cost += 2 * p.worstCost(stepContext, stepOutput)
	}
	return round6(cost), costAPIBilledWorstCase, nil
}

// claudeStepCost prices one line of a Claude stream-json transcript for the
// live watch: an assistant event is one finished API message. Events that
// repeat the message id of the previous one (one event per content block)
// are counted once; lastID carries that id between lines and polls.
func claudeStepCost(line []byte, p *PriceFile, lastID *string) float64 {
	ev := obj(line)
	if p == nil || str(ev["type"]) != "assistant" {
		return 0
	}
	msg := obj(ev["message"])
	id := str(msg["id"])
	if id != "" && id == *lastID {
		return 0
	}
	*lastID = id
	return claudeCallCost(p, claudeUsage(msg["usage"]))
}
