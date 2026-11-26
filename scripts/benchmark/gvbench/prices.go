package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PriceFile is the committed price snapshot the OpenRouter cap is computed
// from. Prices are USD per million tokens. The Max* fields are the highest
// price over every endpoint and every time-of-day override, so the cap holds
// whichever endpoint OpenRouter routes a call to.
type PriceFile struct {
	Model              string     `json:"model"`
	ReadAt             string     `json:"read_at"`
	Sources            []string   `json:"sources"`
	Unit               string     `json:"unit"`
	Listed             PriceSet   `json:"listed"`
	PreferredEndpoints []string   `json:"preferred_endpoints"`
	MaxPrompt          float64    `json:"max_prompt"`
	MaxCompletion      float64    `json:"max_completion"`
	MaxCacheWrite      float64    `json:"max_cache_write"`
	ContextLength      int64      `json:"context_length"`
	Endpoints          []Endpoint `json:"endpoints"`
	Note               string     `json:"note,omitempty"`
}

// PriceSet is one set of per-million prices.
type PriceSet struct {
	Prompt     float64 `json:"prompt"`
	Completion float64 `json:"completion"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write,omitempty"`
	// Peak is the highest time-of-day override, when the page lists any.
	PeakPrompt     float64 `json:"peak_prompt,omitempty"`
	PeakCompletion float64 `json:"peak_completion,omitempty"`
}

// Endpoint is one provider endpoint of the model on OpenRouter.
type Endpoint struct {
	Tag          string  `json:"tag"`
	Provider     string  `json:"provider"`
	Quantization string  `json:"quantization"`
	Tools        bool    `json:"tools"`
	Discount     float64 `json:"discount,omitempty"`
	PriceSet
}

// worstCost prices tokens with no caching at the highest endpoint price.
func (p *PriceFile) worstCost(promptTokens, outputTokens int64) float64 {
	in := math.Max(p.MaxPrompt, p.MaxCacheWrite)
	return (float64(promptTokens)*in + float64(outputTokens)*p.MaxCompletion) / 1e6
}

// maxCacheRead is the highest cache-read price over the tool-capable
// endpoints, raised by the endpoint's time-of-day surcharge where it lists
// one (peak prompt / prompt). An endpoint without a cache-read price does
// not cache, so it reports no cache-read tokens; if no endpoint lists one,
// cache reads are priced as uncached input.
func (p *PriceFile) maxCacheRead() float64 {
	best := 0.0
	for _, e := range p.Endpoints {
		if !e.Tools || e.CacheRead <= 0 {
			continue
		}
		r := e.CacheRead
		if e.PeakPrompt > e.Prompt && e.Prompt > 0 {
			r *= e.PeakPrompt / e.Prompt
		}
		best = math.Max(best, r)
	}
	if best == 0 {
		return math.Max(p.MaxPrompt, p.MaxCacheWrite)
	}
	return best
}

// worstCostCached prices one call's tokens at the highest endpoint prices,
// with cache-read tokens at the highest cache-read price: the per-run
// ceiling's bound. A run's re-sent conversation is mostly cache reads, which
// every endpoint bills at a small fraction of input, so pricing them as
// uncached input (worstCost) stopped real runs at a hundredth of the
// ceiling. Uncached input and cache writes stay at the highest input price.
func (p *PriceFile) worstCostCached(inputTokens, cacheReadTokens, outputTokens int64) float64 {
	in := math.Max(p.MaxPrompt, p.MaxCacheWrite)
	return (float64(inputTokens)*in + float64(cacheReadTokens)*p.maxCacheRead() + float64(outputTokens)*p.MaxCompletion) / 1e6
}

func round6(x float64) float64 {
	return math.Round(x*1e6) / 1e6
}

func loadPrices(path string) (*PriceFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading price file %s: %w", path, err)
	}
	var p PriceFile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing price file %s: %w", path, err)
	}
	if p.Model == "" || p.MaxPrompt <= 0 || p.MaxCompletion <= 0 || p.ReadAt == "" {
		return nil, fmt.Errorf("price file %s needs model, read_at and positive max prices", path)
	}
	return &p, nil
}

// rawPricing is OpenRouter's per-token price strings.
type rawPricing struct {
	Prompt     string  `json:"prompt"`
	Completion string  `json:"completion"`
	CacheRead  string  `json:"input_cache_read"`
	CacheWrite string  `json:"input_cache_write"`
	Discount   float64 `json:"discount"`
	Overrides  []struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
		CacheRead  string `json:"input_cache_read"`
		CacheWrite string `json:"input_cache_write"`
	} `json:"overrides"`
}

func perMillion(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		// A price that does not parse (OpenRouter uses "-1" for "varies")
		// must never lower the bound; callers treat NaN as unusable.
		return math.NaN()
	}
	return math.Round(v*1e6*1e9) / 1e9
}

// priceSetOf returns the base prices and the highest override of a pricing
// object, so time-of-day surcharges are part of the bound.
func priceSetOf(r rawPricing) (PriceSet, error) {
	ps := PriceSet{
		Prompt:     perMillion(r.Prompt),
		Completion: perMillion(r.Completion),
		CacheRead:  perMillion(r.CacheRead),
		CacheWrite: perMillion(r.CacheWrite),
	}
	for _, o := range r.Overrides {
		ps.PeakPrompt = math.Max(ps.PeakPrompt, perMillion(o.Prompt))
		ps.PeakCompletion = math.Max(ps.PeakCompletion, perMillion(o.Completion))
	}
	for _, v := range []float64{ps.Prompt, ps.Completion, ps.CacheRead, ps.CacheWrite, ps.PeakPrompt, ps.PeakCompletion} {
		if math.IsNaN(v) {
			return ps, fmt.Errorf("unparsable price in %+v", r)
		}
	}
	return ps, nil
}

func (ps PriceSet) maxPrompt() float64     { return math.Max(ps.Prompt, ps.PeakPrompt) }
func (ps PriceSet) maxCompletion() float64 { return math.Max(ps.Completion, ps.PeakCompletion) }

// importPrices builds a PriceFile from the raw endpoints response and the
// models list (for the listed price). Only endpoints that support tool calls
// count, since the agent cannot run on the others.
func importPrices(endpointsJSON, modelsJSON []byte, model string, preferred []string, readAt string) (*PriceFile, error) {
	var ep struct {
		Data struct {
			ID        string `json:"id"`
			Endpoints []struct {
				Tag           string     `json:"tag"`
				ProviderName  string     `json:"provider_name"`
				Quantization  string     `json:"quantization"`
				ContextLength int64      `json:"context_length"`
				Pricing       rawPricing `json:"pricing"`
				Supported     []string   `json:"supported_parameters"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.Unmarshal(endpointsJSON, &ep); err != nil {
		return nil, fmt.Errorf("parsing endpoints response: %w", err)
	}
	if ep.Data.ID != model {
		return nil, fmt.Errorf("endpoints response is for %q, not %q", ep.Data.ID, model)
	}
	p := &PriceFile{
		Model:  model,
		ReadAt: readAt,
		Sources: []string{
			"https://openrouter.ai/api/v1/models/" + model + "/endpoints",
			"https://openrouter.ai/api/v1/models",
		},
		Unit:               "USD per million tokens",
		PreferredEndpoints: preferred,
	}
	for _, e := range ep.Data.Endpoints {
		ps, err := priceSetOf(e.Pricing)
		if err != nil {
			return nil, fmt.Errorf("endpoint %s: %w", e.Tag, err)
		}
		tools := false
		for _, s := range e.Supported {
			if s == "tools" {
				tools = true
			}
		}
		p.Endpoints = append(p.Endpoints, Endpoint{Tag: e.Tag, Provider: e.ProviderName, Quantization: e.Quantization, Tools: tools, Discount: e.Pricing.Discount, PriceSet: ps})
		if !tools {
			continue
		}
		p.MaxPrompt = math.Max(p.MaxPrompt, ps.maxPrompt())
		p.MaxCompletion = math.Max(p.MaxCompletion, ps.maxCompletion())
		p.MaxCacheWrite = math.Max(p.MaxCacheWrite, ps.CacheWrite)
		if e.ContextLength > p.ContextLength {
			p.ContextLength = e.ContextLength
		}
	}
	sort.SliceStable(p.Endpoints, func(i, j int) bool { return p.Endpoints[i].Tag < p.Endpoints[j].Tag })
	for _, want := range preferred {
		if p.endpoint(want) == nil {
			return nil, fmt.Errorf("preferred endpoint %s is not listed for %s", want, model)
		}
	}
	if len(modelsJSON) > 0 {
		var ml struct {
			Data []struct {
				ID      string     `json:"id"`
				Pricing rawPricing `json:"pricing"`
			} `json:"data"`
		}
		if err := json.Unmarshal(modelsJSON, &ml); err != nil {
			return nil, fmt.Errorf("parsing models list: %w", err)
		}
		for _, m := range ml.Data {
			if m.ID == model {
				ps, err := priceSetOf(m.Pricing)
				if err != nil {
					return nil, fmt.Errorf("listed price: %w", err)
				}
				p.Listed = ps
			}
		}
	}
	if p.MaxPrompt <= 0 {
		return nil, fmt.Errorf("no tool-capable endpoint for %s", model)
	}
	return p, nil
}

func (p *PriceFile) endpoint(tag string) *Endpoint {
	for i := range p.Endpoints {
		if p.Endpoints[i].Tag == tag {
			return &p.Endpoints[i]
		}
	}
	return nil
}

// checkLive compares a fresh endpoints response with the committed file. The
// run must not start when any live price is above the bound the cap uses, or
// when the committed file is older than maxAge and no live check succeeded.
func checkLive(p *PriceFile, live []byte, now time.Time, maxAge time.Duration) error {
	readAt, err := time.Parse(time.RFC3339, p.ReadAt)
	if err != nil {
		return fmt.Errorf("price file read_at %q: %w", p.ReadAt, err)
	}
	if len(live) == 0 {
		if now.Sub(readAt) > maxAge {
			return fmt.Errorf("price file is from %s (older than %s) and no live price check succeeded", p.ReadAt, maxAge)
		}
		return nil
	}
	fresh, err := importPrices(live, nil, p.Model, nil, now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("live price check: %w", err)
	}
	var over []string
	if fresh.MaxPrompt > p.MaxPrompt+1e-9 {
		over = append(over, fmt.Sprintf("prompt %.4f > %.4f", fresh.MaxPrompt, p.MaxPrompt))
	}
	if fresh.MaxCompletion > p.MaxCompletion+1e-9 {
		over = append(over, fmt.Sprintf("completion %.4f > %.4f", fresh.MaxCompletion, p.MaxCompletion))
	}
	if fresh.MaxCacheWrite > p.MaxCacheWrite+1e-9 {
		over = append(over, fmt.Sprintf("cache write %.4f > %.4f", fresh.MaxCacheWrite, p.MaxCacheWrite))
	}
	if len(over) > 0 {
		return fmt.Errorf("live endpoint prices exceed the committed bound (%s); refresh the price file", strings.Join(over, ", "))
	}
	return nil
}

// Serving records which endpoint served a run, as far as the evidence goes.
type Serving struct {
	Provider     string   `json:"provider"`
	Endpoint     string   `json:"endpoint"`
	Quantization string   `json:"quantization"`
	Evidence     string   `json:"evidence"`
	PriceMatches []string `json:"price_matches,omitempty"`
	// Elsewhere is true when the evidence shows a provider outside the
	// preferred endpoints; Unverified when there is no evidence either way.
	Elsewhere  bool `json:"served_elsewhere"`
	Unverified bool `json:"provider_unverified"`
}

// identifyServing decides the serving provider from provider names reported
// for the run (transcript fields or generation lookups) and, failing that,
// from which endpoint's prices reproduce the billed cost within 3%.
func identifyServing(p *PriceFile, providers []string, m Metrics, billed float64) Serving {
	s := Serving{Provider: "unconfirmed", Endpoint: "unconfirmed", Quantization: "unconfirmed"}
	if p == nil {
		s.Unverified = true
		s.Evidence = "none"
		return s
	}
	preferredProvider := map[string]bool{}
	for _, tag := range p.PreferredEndpoints {
		if e := p.endpoint(tag); e != nil {
			preferredProvider[strings.ToLower(e.Provider)] = true
		}
	}
	if billed > 0 && m.InputTokens+m.CacheReadTokens+m.OutputTokens > 0 {
		for _, e := range p.Endpoints {
			for _, factor := range []float64{1, 1 - e.Discount} {
				if factor <= 0 || (factor != 1 && e.Discount == 0) {
					continue
				}
				c := endpointCost(e.PriceSet, m) * factor
				if c > 0 && math.Abs(c-billed)/billed <= 0.03 {
					s.PriceMatches = addUnique(s.PriceMatches, e.Tag)
				}
			}
		}
	}
	if len(providers) > 0 {
		s.Provider = strings.Join(providers, ",")
		s.Evidence = "provider_reported"
		for _, name := range providers {
			if !preferredProvider[strings.ToLower(name)] {
				s.Elsewhere = true
			}
		}
		if len(providers) == 1 {
			var tags []string
			quant := map[string]bool{}
			for _, e := range p.Endpoints {
				if strings.EqualFold(e.Provider, providers[0]) {
					tags = append(tags, e.Tag)
					quant[e.Quantization] = true
				}
			}
			if len(tags) == 1 {
				s.Endpoint = tags[0]
			} else if len(tags) > 1 {
				// Several endpoints of one provider: narrow by price.
				for _, t := range tags {
					for _, pm := range s.PriceMatches {
						if t == pm {
							s.Endpoint = t
						}
					}
				}
			}
			if len(quant) == 1 {
				for q := range quant {
					s.Quantization = q
				}
			}
			if !s.Elsewhere && s.Endpoint != "unconfirmed" {
				pref := false
				for _, t := range p.PreferredEndpoints {
					if t == s.Endpoint {
						pref = true
					}
				}
				s.Elsewhere = !pref
			}
		}
		return s
	}
	if len(s.PriceMatches) > 0 {
		s.Evidence = "price_match"
		pref := false
		for _, pm := range s.PriceMatches {
			for _, t := range p.PreferredEndpoints {
				if pm == t {
					pref = true
				}
			}
		}
		if len(s.PriceMatches) == 1 {
			e := p.endpoint(s.PriceMatches[0])
			s.Provider, s.Endpoint, s.Quantization = e.Provider, e.Tag, e.Quantization
		}
		// A price match is circumstantial: several endpoints can share a
		// price, so only "no preferred endpoint matches" counts as elsewhere.
		s.Elsewhere = !pref
		s.Unverified = true
		return s
	}
	s.Evidence = "none"
	s.Unverified = true
	return s
}

func endpointCost(ps PriceSet, m Metrics) float64 {
	cw := ps.CacheWrite
	if cw == 0 {
		cw = ps.Prompt
	}
	return (float64(m.InputTokens)*ps.Prompt + float64(m.CacheReadTokens)*ps.CacheRead +
		float64(m.CacheWriteTokens)*cw + float64(m.OutputTokens+m.ReasoningTokens)*ps.Completion) / 1e6
}
