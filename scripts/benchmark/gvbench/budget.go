package main

import (
	"encoding/json"
	"fmt"
)

// KeyInfo is the part of OpenRouter's GET /api/v1/key response the cap
// needs: credits used so far on the key, and its own limit if one is set.
type KeyInfo struct {
	Usage          float64  `json:"usage"`
	Limit          *float64 `json:"limit"`
	LimitRemaining *float64 `json:"limit_remaining"`
}

func parseKeyInfo(data []byte) (KeyInfo, error) {
	var wrap struct {
		Data *KeyInfo `json:"data"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return KeyInfo{}, fmt.Errorf("parsing key response: %w", err)
	}
	if wrap.Data == nil {
		return KeyInfo{}, fmt.Errorf("key response has no data object")
	}
	return *wrap.Data, nil
}

// BudgetInput is everything the go/stop decision for the next OpenRouter
// run depends on.
type BudgetInput struct {
	CapUSD        float64
	RunCeilingUSD float64
	// StepContext and StepOutput bound one model call: the prompt can not
	// exceed the context window and the harness caps output per call.
	StepContext int64
	StepOutput  int64
	UsageStart  float64
	Key         KeyInfo
	// LedgerUSD is the sum of the costs recorded for finished runs; it
	// covers usage the key endpoint has not caught up with yet.
	LedgerUSD float64
}

// BudgetDecision explains the decision so the log and manifest can show it.
type BudgetDecision struct {
	Run          bool    `json:"run"`
	SpentUSD     float64 `json:"spent_usd"`
	RemainingUSD float64 `json:"remaining_usd"`
	RequiredUSD  float64 `json:"required_usd"`
	OvershootUSD float64 `json:"one_call_overshoot_usd"`
	Reason       string  `json:"reason"`
}

// decideBudget allows a run only if its worst case fits in what is left.
// The worst case of one run is the per-run ceiling plus two model calls at
// the highest endpoint price with no caching: the call whose step_finish
// crosses the ceiling, and one more that may already be in flight when the
// runner kills the process.
func decideBudget(in BudgetInput, p *PriceFile) BudgetDecision {
	over := p.worstCost(in.StepContext, in.StepOutput)
	d := BudgetDecision{OvershootUSD: round6(over)}
	spent := in.Key.Usage - in.UsageStart
	if in.LedgerUSD > spent {
		spent = in.LedgerUSD
	}
	if spent < 0 {
		spent = 0
	}
	d.SpentUSD = round6(spent)
	remaining := in.CapUSD - spent
	if in.Key.LimitRemaining != nil && *in.Key.LimitRemaining < remaining {
		remaining = *in.Key.LimitRemaining
	}
	d.RemainingUSD = round6(remaining)
	d.RequiredUSD = round6(in.RunCeilingUSD + 2*over)
	switch {
	case in.RunCeilingUSD <= 0 || in.CapUSD <= 0:
		d.Reason = "cap and per-run ceiling must be positive"
	case d.RequiredUSD > d.RemainingUSD:
		d.Reason = fmt.Sprintf("worst case %.4f exceeds remaining %.4f", d.RequiredUSD, d.RemainingUSD)
	default:
		d.Run = true
		d.Reason = "fits"
	}
	return d
}

// ledgerRow says whether a row's cost counts against an agent's cap: every
// OpenRouter attempt, and every Claude attempt billed to API credits
// (subscription runs cost nothing). Superseded invalid attempts count too.
func ledgerRow(agent string, r Row) bool {
	switch agent {
	case "opencode":
		return r.Agent == "opencode"
	case "claude":
		return r.Agent == "claude" && (r.CostKind == costAPIBilled || r.CostKind == costAPIBilledWorstCase)
	}
	return false
}
