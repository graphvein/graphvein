package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
)

// reason explains why a call is not (fully) linked. Reasons are derived
// from stored attributes only: the index stores no reason text, and no text
// here claims more than the attribute it reads (ADR 0042). Detail is the
// stored source spelling the reason refers to, or nil.
type reason struct {
	Code   string  `json:"code"`
	Text   string  `json:"text"`
	Detail *string `json:"detail"`
}

func newReason(code, text string, detail *string) reason {
	return reason{Code: code, Text: text, Detail: detail}
}

func strPtr(s string) *string { return &s }

// callReasons derives the ordered reasons of one call node; the first is
// the primary reason. hasEdges reports whether the call has a stored call
// edge. A call whose status is exactly "linked" has no reasons.
func callReasons(t transport, n contract.Node, hasEdges bool) []reason {
	status := statusOf(n)
	if !t.Known[status] {
		return []reason{newReason("status_unknown", fmt.Sprintf("Status %q is not known to this Graphvein version", status), strPtr(status))}
	}
	if status == "linked" {
		return append(originAssumed(n), prefixAssumed(n)...)
	}
	var out []reason
	if t.Name == httpTransport.Name {
		out = clientReasons(n, status)
	} else {
		out = invokeReasons(n, status)
	}
	if hasEdges {
		out = append(out, newReason("alternative_linked", fmt.Sprintf("A stored link exists although the status is %q", status), nil))
		out = append(out, prefixAssumed(n)...)
	}
	if len(out) == 0 {
		out = append(out, newReason("status", fmt.Sprintf("Status is %q", status), strPtr(status)))
	}
	return out
}

// originAssumed is the one reason a linked call can carry (T034 follow-up):
// each stored origin assumption of its linked alternatives, "Origin
// <origin> assumed to be this repository's server", detail the stored text.
func originAssumed(n contract.Node) []reason {
	out := []reason{}
	list, _ := n.Attributes["origin_assumptions"].([]any)
	if typed, ok := n.Attributes["origin_assumptions"].([]string); ok {
		for _, s := range typed {
			list = append(list, s)
		}
	}
	for _, v := range list {
		if text, ok := v.(string); ok && text != "" {
			out = append(out, newReason("origin_assumed", "Assumption: "+text, nil))
		}
	}
	return out
}

// linkAssumptions is the assumptions a stored calls_route link from call
// to route rests on (clients.LinkAssumptions), nil when there is none, so
// a view's omitempty field stays absent.
func linkAssumptions(call contract.Node, route string) []clients.LinkAssumption {
	list := clients.LinkAssumptions(call.Attributes, route)
	if len(list) == 0 {
		return nil
	}
	return list
}

// prefixAssumed lists the assumptions a link of the unknown-prefix tier
// rests on (T033, rows UP-19, UP-V1, UP-V2 of
// docs/contract/claims/unknown-prefix-paths.md): one reason per stored
// UP-V id in the call's assumes, "Assumption: <text>", detail the id. A
// path assumption of the matched route path entry (GR-V5, RO-V1,
// RO-V2) is the reason path_assumed, worded the same way. Other ids
// (NX-V1, ...) are not shown here.
func prefixAssumed(n contract.Node) []reason {
	var ids []string
	switch list := n.Attributes["assumes"].(type) {
	case []any:
		for _, v := range list {
			if id, ok := v.(string); ok {
				ids = append(ids, id)
			}
		}
	case []string:
		ids = list
	}
	var out []reason
	for _, id := range ids {
		if text, ok := clients.UnknownPrefixAssumptions[id]; ok {
			out = append(out, newReason("prefix_assumed", "Assumption: "+text, strPtr(id)))
		} else if clients.PathAssumption(id) {
			out = append(out, newReason("path_assumed", "Assumption: "+clients.AssumptionText(id), strPtr(id)))
		}
	}
	return out
}

// clientReasons applies the client_call table of docs/cli/serve.md.
func clientReasons(n contract.Node, status string) []reason {
	out := []reason{}
	attrs := n.Attributes
	paths := objects(attrs["paths"])
	method := stringAttr(n, "method")
	if status == "wrapper" {
		out = append(out, newReason("wrapper_inner", "Inner request of a wrapper; the wrapper's call sites are listed as calls", nil))
	}
	if status == "generated" {
		// Committed bundler output (ADR 0085): the stored marker row.
		out = append(out, newReason("generated_output", "Committed bundler output; its calls are not linked (clients.link_generated_output in graphvein.yaml links them)", stringAttr(n, "generated_output")))
	}
	if p := firstPathWithStatus(paths, "unmatched"); p != nil {
		var detail *string
		if tmpl, ok := p["template"].(string); ok {
			d := tmpl
			if method != nil {
				d = *method + " " + tmpl
			}
			detail = &d
		}
		out = append(out, newReason("no_matching_route", "No route matches this method and path", detail))
	}
	if p := firstPathWithStatus(paths, "ambiguous"); p != nil {
		count, exact := candidateTie(p)
		text := fmt.Sprintf("%d routes match equally", count)
		if !exact {
			text = fmt.Sprintf("At least %d routes match equally", count)
		}
		out = append(out, newReason("ambiguous", text, templateOf(p)))
	}
	if p := firstPathWithStatus(paths, "test_only"); p != nil {
		out = append(out, newReason("test_only_target", "Only test-scope routes match", templateOf(p)))
	}
	nullTemplates := 0
	for _, p := range paths {
		if _, ok := p["template"].(string); !ok {
			nullTemplates++
		}
	}
	switch {
	case len(paths) > 0 && nullTemplates == len(paths):
		out = append(out, newReason("url_unevaluated", "URL is not evaluated statically", stringAttr(n, "url_expression")))
	case nullTemplates > 0:
		out = append(out, newReason("some_alternatives_unevaluated", "Some URL alternatives are not evaluated", stringAttr(n, "url_expression")))
	}
	if method == nil {
		out = append(out, newReason("method_unknown", "Method is not known statically", stringAttr(n, "method_expression")))
	}
	if attrs["base"] == "unresolved" {
		if expr := stringAttr(n, "base_expression"); expr != nil && *expr != "" {
			out = append(out, newReason("base_unresolved", "Base URL is not resolved", expr))
		}
	}
	if attrs["library"] == "orval" {
		for _, p := range paths {
			if _, ok := p["template"].(string); !ok && p["resolution"] == "generated" {
				out = append(out, newReason("generated_unproven", "Generated client; its transport is not proven", stringAttr(n, "operation")))
				break
			}
		}
	}
	return out
}

// invokeTexts are one sentence per ipc_call status, from
// docs/extract/tauri-commands.md "Matching and statuses".
var invokeTexts = map[string]string{
	"registry_unproven": "A command registry in this repository is not proven; it could be the one that receives this call",
	"acl_possible":      "The app may restrict commands with permissions; whether this call is allowed is not proven",
	"ambiguous":         "More than one Tauri app could receive this call",
	"shadow_possible":   "An unresolved registry entry before the match could take this name",
	"unknown_command":   "No resolved command has this name, but an unresolved registry entry might",
	"test_only":         "Only test-scope or test-only commands have this name",
	"unmatched":         "No registered command has this name",
	"plugin":            "Plugin command; plugin commands are not linked",
	"global_unlinked":   "Calls through the window.__TAURI__ globals are not linked",
}

// invokeReasons applies the ipc_call list of docs/cli/serve.md.
func invokeReasons(n contract.Node, status string) []reason {
	out := []reason{}
	attrs := n.Attributes
	if status == "wrapper" {
		out = append(out, newReason("wrapper_inner", "Inner invoke of a wrapper; the wrapper's call sites are listed as calls", nil))
	}
	unevaluated := stringAttr(n, "command") == nil && status == "dynamic"
	for _, alt := range objects(attrs["alternatives"]) {
		if _, ok := alt["command"].(string); !ok {
			unevaluated = true
		}
	}
	if unevaluated {
		detail := strings.TrimPrefix(n.Name, "INVOKE ")
		out = append(out, newReason("command_unevaluated", "Command name is not evaluated statically", &detail))
	}
	if attrs["form"] == "global" {
		out = append(out, newReason("global_form", "Called through a window.__TAURI__ global, not an imported invoke", nil))
	}
	if text, ok := invokeTexts[status]; ok {
		var detail *string
		switch status {
		case "plugin":
			parts := []string{}
			for _, key := range []string{"plugin", "plugin_command"} {
				if s := stringAttr(n, key); s != nil {
					parts = append(parts, *s)
				}
			}
			if len(parts) > 0 {
				detail = strPtr(strings.Join(parts, "|"))
			}
		case "unmatched":
			detail = stringAttr(n, "command")
			// A conditional call keeps command null; name the alternative
			// that matched no command instead.
			for _, alt := range objects(attrs["alternatives"]) {
				if s, ok := alt["command"].(string); ok && detail == nil && alt["status"] == "unmatched" {
					detail = &s
				}
			}
		}
		out = append(out, newReason(status, text, detail))
	}
	return out
}

func firstPathWithStatus(paths []map[string]any, status string) map[string]any {
	for _, p := range paths {
		if p["status"] == status {
			return p
		}
	}
	return nil
}

func templateOf(p map[string]any) *string {
	if s, ok := p["template"].(string); ok {
		return &s
	}
	return nil
}

// maxCandidates is the cap on a recorded candidate list: the HTTP and Tauri
// extractors keep at most ten candidates (internal/extract/clients,
// internal/extract/tauri).
const maxCandidates = 10

// candidateTie is the size of a path entry's tie and whether that size is
// exact. The extractor stores candidate_count for ties of two or more (zero
// for one candidate), and the list may be cut at maxCandidates. Without a
// stored count the listed candidates are all that is known: exact below the
// cap, a lower bound at it, so a capped list is never presented as the tie.
func candidateTie(p map[string]any) (count int, exact bool) {
	listed := len(stringList(p["candidates"]))
	stored, ok := 0, false
	switch v := p["candidate_count"].(type) {
	case json.Number: // the store decodes numbers with UseNumber
		if i, err := v.Int64(); err == nil && i >= 0 {
			stored, ok = int(i), true
		}
	case float64:
		if v >= 0 && v == float64(int(v)) {
			stored, ok = int(v), true
		}
	}
	if ok && (stored >= listed || stored == 0 && listed == 1) {
		return max(stored, listed), true
	}
	return listed, listed < maxCandidates
}
