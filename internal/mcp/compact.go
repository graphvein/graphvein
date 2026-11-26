package mcp

import (
	"fmt"
	"sort"
	"strings"
)

// This file holds the compact detail of the endpoints and endpoint tools
// (row MC-29, the token economy of T030 MCP-2): what an agent needs to
// pick a route or call it, with everything left out named in omitted and
// how to get it. Full detail returns the views' objects as MCP-1 did.

// compactRow is one endpoints row without its per-route shape summary
// (the endpoint tool has the shape) and without the name and pattern
// where they repeat the method and path.
func compactRow(e *Envelope, row map[string]any) map[string]any {
	out := map[string]any{}
	served := strings.TrimSpace(str(row["method"]) + " " + str(row["path"]))
	for _, k := range sortedKeys(row) {
		val := row[k]
		switch {
		case k == "shape":
			e.omitHow("rows.shape", "per-route request and response section states: call endpoint with the route, or endpoints with detail full")
		case k == "name" && str(val) == served:
			e.omitHow("rows.name (when it equals method and path)", "it is method and path; endpoints with detail full shows it")
		case k == "pattern" && str(val) == str(row["path"]):
			e.omitHow("rows.pattern (when it equals path)", "it is the path; endpoints with detail full shows it")
		case k == "documented" && val == false, k == "path_count" && str(val) == "1", k == "scope" && val == nil:
			e.omitHow("rows."+k+" (default value: false, 1 or null)", "endpoints with detail full shows it")
		case k == "flags" && len(listOf(val)) == 0:
			e.omitHow("rows.flags (when empty)", "no flag is set; endpoints with detail full shows the empty list")
		default:
			out[k] = val
		}
	}
	return out
}

// compactOpenAPI keeps each documenting operation's node location and its
// stored edge; the operation's attributes (summary, parameters, response
// codes) are full detail.
func compactOpenAPI(e *Envelope, val any) any {
	m, ok := val.(map[string]any)
	if !ok {
		return val
	}
	out := map[string]any{}
	for _, k := range sortedKeys(m) {
		if k != "operations" {
			out[k] = m[k]
			continue
		}
		var ops []any
		for _, raw := range listOf(m[k]) {
			op, _ := raw.(map[string]any)
			entry := map[string]any{}
			for _, ok := range sortedKeys(op) {
				switch ok {
				case "node":
					n, _ := op[ok].(map[string]any)
					entry[ok] = compactNode(e, n)
					e.omitHow("openapi.operations.node.attributes", "the operation's summary, parameters and response codes: call endpoint with detail full")
				case "edge":
					entry[ok] = op[ok]
					if ed, ok := op[ok].(map[string]any); ok {
						e.label("provenance", str(ed["provenance"]))
					}
				default:
					e.notModelled("openapi.operation."+ok, op[ok])
				}
			}
			ops = append(ops, entry)
		}
		if ops == nil {
			ops = []any{}
		}
		out[k] = ops
	}
	return out
}

// shapeSections are the request and response sections in the view's
// order (routeshape.SectionNames).
var shapeSections = []string{"path", "query", "header", "cookie", "body", "responses", "security"}

// compactShape is a route shape as states and items: each section's state
// and reason, each item's name (or status, content type), presence,
// access, type and how it is known (provenance kind, confidence when not
// 1), the named models with their field types, the security schemes by
// name and type, and the call following as stop counts. Types are
// written out ("array<Item>", "object{id: integer, note?: string}",
// "unknown (reason)", "not read"), so an unknown is named, never dropped.
func compactShape(e *Envelope, val any) any {
	m, ok := val.(map[string]any)
	if !ok {
		return val
	}
	out := map[string]any{}
	known := map[string]bool{"version": true, "models": true, "security_schemes": true, "follow": true}
	for _, sec := range shapeSections {
		known[sec] = true
		sm, ok := m[sec].(map[string]any)
		if !ok {
			continue
		}
		cs := map[string]any{"state": sm["state"]}
		if r := str(sm["reason"]); r != "" {
			cs["reason"] = r
		}
		var items []any
		for _, raw := range listOf(sm["items"]) {
			items = append(items, shapeItem(e, sec, raw))
		}
		if items == nil {
			items = []any{}
		}
		cs["items"] = items
		for _, k := range sortedKeys(sm) {
			switch k {
			case "state", "reason", "items":
			case "evidence":
				e.omitHow("shape.*.evidence", "the declaration that proves provably_none or complete: call endpoint with detail full")
			default:
				e.notModelled("shape."+sec+"."+k, sm[k])
			}
		}
		out[sec] = cs
	}
	var models []any
	for _, raw := range listOf(m["models"]) {
		mm, _ := raw.(map[string]any)
		entry := map[string]any{"name": mm["name"], "type": schemaType(mm["schema"])}
		if p, ok := mm["provenance"].(map[string]any); ok {
			entry["kind"] = p["kind"]
		}
		models = append(models, entry)
	}
	if models == nil {
		models = []any{}
	}
	out["models"] = models
	var schemes []any
	for _, raw := range listOf(m["security_schemes"]) {
		sm, _ := raw.(map[string]any)
		schemes = append(schemes, map[string]any{"name": sm["name"], "type": sm["type"]})
		e.omitHow("shape.security_schemes details", "scheme, bearer format, key location and URLs of each scheme: call endpoint with detail full")
	}
	if schemes == nil {
		schemes = []any{}
	}
	out["security_schemes"] = schemes
	if f, ok := m["follow"].(map[string]any); ok {
		stops := map[string]int{}
		for _, raw := range listOf(f["chains"]) {
			c, _ := raw.(map[string]any)
			stops[str(c["stop"])]++
		}
		out["follow"] = map[string]any{"depth_limit": f["depth_limit"], "chains": len(listOf(f["chains"])), "stops": stops}
		if n := stops["depth_cap"]; n > 0 {
			e.unknown("MC-12", "shape_depth_cap", fmt.Sprintf("%d call chains stopped at the shape depth cap (%v, an index setting): items read past it are missing", n, f["depth_limit"]), nil)
		}
		if len(listOf(f["chains"])) > 0 {
			e.omitHow("shape.follow.chains", "each followed chain with its steps and stop site: call endpoint with detail full")
		}
	}
	for _, k := range sortedKeys(m) {
		if !known[k] {
			e.notModelled("shape."+k, m[k])
		}
	}
	e.omitHow("shape items' provenance (rule, file, line, call chain)", "call endpoint with detail full")
	return out
}

// shapeItem is one compact shape item of section sec.
func shapeItem(e *Envelope, sec string, raw any) map[string]any {
	it, _ := raw.(map[string]any)
	out := map[string]any{}
	switch sec {
	case "responses":
		out["status"] = it["status"]
		out["content_type"] = it["content_type"]
		out["type"] = schemaType(it["schema"])
	case "body":
		out["content_type"] = it["content_type"]
		out["type"] = schemaType(it["schema"])
	case "security":
		var names []string
		for _, s := range listOf(it["schemes"]) {
			sm, _ := s.(map[string]any)
			name := str(sm["name"])
			if scopes := listOf(sm["scopes"]); len(scopes) > 0 {
				var ss []string
				for _, sc := range scopes {
					ss = append(ss, str(sc))
				}
				name += " (" + strings.Join(ss, ", ") + ")"
			}
			names = append(names, name)
		}
		if names == nil {
			names = []string{}
		}
		out["schemes"] = names
	default:
		out["name"] = it["name"]
		out["presence"] = it["presence"]
		out["access"] = it["access"]
		out["type"] = schemaType(it["schema"])
	}
	if p, ok := it["provenance"].(map[string]any); ok {
		out["kind"] = p["kind"]
		e.label("item_kind", str(p["kind"]))
		if c := str(p["confidence"]); c != "" && c != "1" {
			out["confidence"] = p["confidence"]
		}
	}
	return out
}

// schemaType writes a shape schema as one type expression. A nil schema
// is "not read"; an unknown schema keeps its reason.
func schemaType(v any) string {
	s, ok := v.(map[string]any)
	if !ok {
		return "not read"
	}
	var t string
	switch typ := str(s["type"]); typ {
	case "ref":
		t = str(s["ref"])
	case "array":
		t = "array<" + schemaType(s["items"]) + ">"
	case "enum":
		var lits []string
		for _, l := range listOf(s["enum"]) {
			lits = append(lits, fmt.Sprint(l))
		}
		t = "enum(" + strings.Join(lits, "|") + ")"
	case "object":
		var fields []string
		for _, raw := range listOf(s["fields"]) {
			f, _ := raw.(map[string]any)
			name := str(f["name"])
			switch str(f["presence"]) {
			case "optional":
				name += "?"
			case "unknown":
				name += "(presence unknown)"
			}
			fields = append(fields, name+": "+schemaType(f["schema"]))
		}
		sort.Strings(fields)
		t = "object{" + strings.Join(fields, ", ") + "}"
		if a := str(s["additional"]); a != "" && a != "forbidden" {
			t += " (additional fields " + a + ")"
		}
	case "unknown":
		t = "unknown (" + str(s["reason"]) + ")"
	default:
		t = typ
	}
	if b, _ := s["nullable"].(bool); b {
		t += "|null"
	}
	return t
}
