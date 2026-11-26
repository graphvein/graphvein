package routes

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// Axum security (rows SH-S13..SH-S17): typed Authorization extractors of
// the handler and the route's layers (attribute layers, DA-X1..DA-X4)
// read for the closed list. Semantics read in axum 0.8.8, axum-extra
// 0.10.3 (typed_header.rs: a missing or undecodable header answers 400),
// headers 0.4.2 (authorization.rs) and tower-http 0.6.8
// (auth/require_authorization.rs: 401 unless the header equals the
// configured value), checked by scripts/oracle/route_security. The section
// is never complete: layers outside the router (a service wrapping the
// served router) are not read.
func (b *shapeBuild) security(f *axumFn, params []handlerParam, attrs map[string]any) {
	col := &secCollector{}
	if unknown, ok := attrs["layers_unknown"].([]map[string]any); ok {
		for _, u := range unknown {
			reason, _ := u["reason"].(string)
			col.reason(fmt.Sprintf("a layer may wrap the route (%s): not read", reason))
		}
	}
	if layers, ok := attrs["layers"].([]map[string]any); ok {
		for _, l := range layers {
			expression, _ := l["expression"].(string)
			file, _ := l["file"].(string)
			span, _ := l["span"].(contract.Span)
			line := span.StartLine
			d, _ := l["declares"].(map[string]any)
			library, _ := d["library"].(string)
			detail, _ := d["detail"].(string)
			switch {
			case library == "tower-http" && detail == "ValidateRequestHeaderLayer::bearer":
				tagShape("SH-S15/bearer")
				col.sources = append(col.sources, secSource{rule: "SH-S15", kind: routeshape.Fact, alts: one(bearerScheme("SH-S15", file, line)), file: file, line: line})
			case library == "tower-http" && detail == "ValidateRequestHeaderLayer::basic":
				tagShape("SH-S15/basic")
				col.sources = append(col.sources, secSource{rule: "SH-S15", kind: routeshape.Fact, alts: one(basicScheme("SH-S15", file, line)), file: file, line: line})
			case library == "axum-login":
				tagShape("SH-S16/login_required")
				col.sources = append(col.sources, secSource{rule: "SH-S16", kind: routeshape.Fact, alts: one(unknownScheme("axum-login", "SH-S16", file, line)), file: file, line: line,
					reasons: []string{"axum-login: a session cookie whose name the session layer sets"}})
			default:
				col.reason(fmt.Sprintf("layer %s (%s:%d) is not read for auth", expression, file, line))
			}
		}
	}
	for _, p := range params {
		switch p.role {
		case roleTyped:
			name, _ := typedHeaderName(p.inner)
			cred := p.inner.arg(0)
			if name != "authorization" || cred == nil || cred.kind != "ext" {
				continue
			}
			var sc routeshape.Scheme
			switch cred.name {
			case "Bearer":
				tagShape("SH-S13/TypedHeader-Bearer")
				sc = bearerScheme("SH-S13", f.file, p.p.Line)
			case "Basic":
				tagShape("SH-S14/TypedHeader-Basic")
				sc = basicScheme("SH-S14", f.file, p.p.Line)
			default:
				col.reason("typed Authorization header " + p.p.Type.Text + " is not read for auth")
				continue
			}
			src := secSource{rule: "SH-S13", kind: routeshape.Fact, alts: one(sc), file: f.file, line: p.p.Line}
			if cred.name == "Basic" {
				src.rule = "SH-S14"
			}
			if p.opt {
				tagShape("SH-S13/Option")
				src.kind = routeshape.Intended
				src.reasons = []string{"an Option<TypedHeader<..>> extractor lets requests without the header reach the handler"}
			}
			col.sources = append(col.sources, src)
		case roleCustom:
			col.reason("extractor " + p.p.Type.Text + " (a repository extractor) is not read for auth")
		case roleWrapped, roleUnknown:
			col.reason("extractor " + p.p.Type.Text + " is not read for auth")
		}
	}
	if len(col.sources) > 0 {
		tagShape("SH-S17/partial")
	}
	col.reason("layers outside the router are not read")
	col.apply(b.sh)
}
