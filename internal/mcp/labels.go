package mcp

import "fmt"

// legend holds the meaning of each label value a tool may pass on, by
// family (row MC-5). A response lists only the labels it uses (D11). A
// value the legend lacks is row MC-D: listed as not modelled and reported
// as an unknown, never explained by a guess.
var legend = map[string]map[string]string{
	"freshness": {
		"fresh":       "no file added, changed or deleted since the last index (same scan and ignore rules as indexing), as of checked_at",
		"stale":       "files changed since the last index: facts describe the indexed files, not the current ones",
		"not_indexed": "no index run is stored: no fact is available",
		"unknown":     "the freshness check failed: the index may or may not match the files",
	},
	"link_pass": {
		"none":  "no project link pass is stored: nothing crosses repositories",
		"fresh": "the stored link pass matches the members' current indexes and declarations",
		"stale": "the link pass would link differently if run now: see stale_reasons",
	},
	"provenance": {
		"fact":     "a stored fact: every edge of the path was read from the code",
		"inferred": "the path has an inferred edge: confidence is the weakest edge of the path",
	},
	"confidence": {
		"weakest_edge": "confidence = the lowest stored edge confidence on the shortest path; a fact path has none",
	},
	"claim": {
		"called by":        "a stored call path leads from this node to the target",
		"depends on":       "every edge of the path to the target is a stored fact",
		"may depend on":    "the path to the target has an inferred edge",
		"calls this route": "a stored calls_route edge links this call to the route",
		"no stored caller": "caller_count 0: no stored call edge reaches it in this scope; never 'dead' or 'not called'",
	},
	"precision": {
		"precise":     "no fact outside the claim contract of its scope; not a claim of completeness",
		"best_effort": "no precision rule covers this scope (row D or a refusal): facts are still facts, absence is not proven",
	},
	"file_state": {
		"unchanged": "the file still has the indexed content",
		"changed":   "the file changed since indexing: stored lines may have moved",
		"missing":   "the file cannot be read now (deleted, renamed or unreadable)",
	},
	"match": {
		"exact":  "the call's composed URL path equals the route path",
		"suffix": "the URL's known part matches the end of the route path (unresolved base), or the URL ends with the route's proven path below a prefix the code does not fix (prefix_assumptions UP-V1, UP-V2): confidence 0.6",
	},
	"handler": {
		"resolved":   "the route's handler is a stored function",
		"missing":    "the route names a handler that is not stored",
		"inline":     "the handler is an inline function",
		"unresolved": "no handler was resolved",
	},
	"shape": {
		"not_read":      "this part of the request or response was not read: no claim either way",
		"provably_none": "the code proves this part has no item",
		"partial":       "the items listed are facts; others may exist",
		"complete":      "every item is known (declared by the framework or a committed spec)",
	},
	"near_miss": {
		"ambiguous_candidate": "the call lists this route among equally good candidates: not linked, so not a caller",
		"test_only_candidate": "only a test-scope route matches the call: not linked, so not a caller",
	},
	"source": {
		"exact":        "the lines are the indexed ones: the highlight is the stored span",
		"text_match":   "the file changed but the stored span's text was found once: highlighted there",
		"changed":      "the file changed and the span could not be placed: no highlight",
		"span_invalid": "the stored span does not fit the file: no highlight",
		"missing":      "the file cannot be read now: no lines",
		"not_regular":  "the path is not a regular file: no lines",
		"too_large":    "the file is too large to show: no lines",
		"binary":       "the file is binary: no lines",
		"unreadable":   "the file could not be read: no lines",
		"no_location":  "the node has no stored file or span: no lines",
		"not_indexed":  "the repository is not indexed: no lines",
	},
	"flag": {
		"unmounted":          "no composed path: the path is not known (the route may be unmounted, or a mount or prefix above it is not resolved); never proof that the route is unreachable",
		"path_withheld":      "no composed path: the route is found, but another pattern of the ServeMux, its redirect, a forwarding closure or a route of the router it is mounted on may answer its path, so the path is withheld (couldn't-read route_paths.routes_withheld)",
		"prefix_unresolved":  "no full path: the route is served below a prefix the code does not fix; prefix_unknown shows the known part with \"…\" for the unknown prefix, never a full path",
		"shadowed":           "no composed path: an earlier mount whose prefix Graphvein read takes every path of the route (route_shadowed); the route is mounted, its paths are withheld",
		"path_assumed":       "a composed path rests on an assumption, named in path_assumptions on the endpoint (GR-V5 a wrapper; RO-V1 a mount at an unknown prefix may answer first; RO-V2 a mounted app's catch-all may answer first)",
		"conditional":        "a path is registered conditionally",
		"test_paths":         "some composed paths are in test scope",
		"handler_inline":     "the handler is an inline function",
		"handler_unresolved": "no handler resolved and not inline",
		"cfg_gated":          "the command is behind a cfg gate",
		"test_only":          "the command is test only",
		"unreachable":        "the command's registry is not reached",
		"unproven_registry":  "the command's registry is not proven",
		"plugin_registry":    "the command comes from a plugin registry",
		"unresolved_entry":   "the command name is not resolved",
		"anonymous":          "the server action has no name",
		"inline":             "the server action is inline",
		"unread_use":         "an unread use may reach it: may be called, never 'not called'",
	},
	"path": {
		"prefix_unknown": "a path whose prefix the code does not fix: \"…\" stands for the unknown prefix and the rest is proven; never a full path",
	},
	"verification": {
		"oracle":  "verified by an oracle (the library or engine itself ran the form)",
		"mapped":  "mapped, not verified: the form is recognised by its documented meaning, no oracle ran it",
		"unknown": "no verification recorded for this fact: neither oracle nor mapped is claimed",
	},
	"tier": {
		"fact":     "every edge on the first path from the start is a stored fact",
		"inferred": "an inferred edge is on the first path from the start; the number is the weakest edge confidence",
	},
	"completeness": {
		"complete":           "every column and constraint of the table is read",
		"complete_in_source": "every column and constraint the source declares is read; the database may differ",
		"partial":            "some columns or constraints are not read: no absence is claimed",
		"unknown":            "completeness could not be determined: no absence is claimed",
	},
	"change": {
		"added":          "the item is in the current index, not in the base",
		"removed":        "the item is in the base, not in the current index",
		"changed":        "a compared field of the item differs",
		"body_changed":   "the item's code changed (content hash), its compared fields did not",
		"link_lost":      "a stored link of the base is not in the current index",
		"link_gained":    "a stored link of the current index is not in the base",
		"unknown_now":    "the item's file has a knowledge gap now: its state is unknown, not removed",
		"known_now":      "the item's file had a gap in the base and is read now",
		"became_partial": "the item's facts are partial now",
		"unclassified":   "a difference no change row classifies: listed, not interpreted",
	},
	"cell": {
		"proven":         "the link property is proven by stored facts",
		"as_written":     "the property is as written in the code (a literal), not proven at run time",
		"documented":     "the property comes from documentation or configuration, not code",
		"unknown":        "the property is not proven either way",
		"not_applicable": "the property does not apply to this kind of link (an in-process call has no TLS)",
		"mixed":          "the link's members differ in this property: no single value is claimed",
		"as_configured":  "the property is as a deploy configuration file states it, not proven at run time",
	},
	"component": {
		"read":       "the component's manifest was read",
		"unread":     "the component's manifest was not read: its kinds and counts may be incomplete",
		"unassigned": "files under no package manifest",
	},
	"call_status": {
		"linked":    "the call has a stored link to an endpoint",
		"dynamic":   "the call's URL is not evaluated: not linked",
		"unmatched": "no endpoint matches the call's method and path: not linked",
		"ambiguous": "several endpoints match equally well: not linked, candidates listed",
		"test_only": "only a test-scope endpoint matches: not linked",
		"wrapper":   "the call is inside a client wrapper whose callers make the real requests",
		"generated": "the call is in committed bundler output (ADR 0085): not linked by design, because the bundle repeats the requests of its sources; not a missing link (clients.link_generated_output links them)",
	},
	"item_kind": {
		"fact":            "a typed declaration the framework enforces at run time",
		"read_in_handler": "the name is read in the handler or a function it calls; its type is not read",
		"intended":        "a static type nothing checks at run time",
		"returned":        "the declared type of a returned value, not validated",
		"mapped":          "from a committed spec or a linked typed client; its confidence is that of the link",
	},
	"test_scope": {
		"test_code":     "the item is test code by the code views' test-scope rule (scope attribute test, or a file the repository's test rules classify as test)",
		"via_test_code": "the stored shortest path to the target passes through test code; a longer production-only path may exist, so this is not 'reached only through test code'",
	},
	"link_change": {
		"added":   "the link is in the newer pass and not in the base pass",
		"removed": "the link is in the base pass and not in the newer one",
		"changed": "both passes keep the link (same kind and ends) with a different stored payload (confidence, evidence, assumption or another field; the endpoints' runs left out)",
	},
	"bound": {
		"depth":         "the walk stopped at the depth cap: nodes lie beyond",
		"reached_nodes": "the walk reached its node cap: nodes lie beyond",
		"fan_out":       "one node led to more new nodes in one step than the cap: the rest were not followed",
		"changed_files": "the changed-file list is cut; the count is exact",
		"cut_lines":     "long lines were cut",
		"lines":         "the line window was cut at the source cap",
		"members":       "the chain reached its member cap: members lie beyond",
		"neighbours":    "the foreign-key neighbour list is cut at its cap: tables lie beyond",
	},
}

// label records that the response uses value of family and returns
// whether the legend knows it. A value it does not know is MC-D.
func (e *Envelope) label(family, value string) bool {
	if value == "" {
		return true
	}
	tagRow("MC-5")
	key := family + ":" + value
	if text, ok := legend[family][value]; ok {
		e.labelSet[key] = text
		return true
	}
	e.labelSet[key] = "not modelled by the MCP server (row MC-D): passed on as served, with no meaning claimed"
	e.notModelled(fmt.Sprintf("label %s", key), value)
	return false
}
