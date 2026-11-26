package routes

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// WithheldCode is the diagnostic code the Go linker writes, among others,
// for a route registration whose path it withholds (couldn't-read row
// CR-44). Mounts whose router is not resolved share the code (row CR-42).
const WithheldCode = "route_mount_unresolved"

// withheldMessage is the message form the Go linker writes for a route
// registration whose path it withholds (gohttp_register.go): the method,
// the pattern quoted as Go writes it and the reason in brackets, then the
// registration's span ("ANY \"/*\" (another pattern of the ServeMux, ...)
// at 12:2-12:30"). No mount expression of any linker has this form: an
// upper-case word, a space and a quoted string is not an expression.
var withheldMessage = regexp.MustCompile(`^[A-Z]+ "(?:[^"\\]|\\.)*" \(`)

// withheldSpan reads the span the linker appends to the message.
var withheldSpan = regexp.MustCompile(`(?s) at (\d+):(\d+)-(\d+):(\d+)$`)

// IsWithheldRegistration says whether a route_mount_unresolved diagnostic
// is a route registration whose path the Go linker withholds (row CR-44)
// rather than a mount whose router is not resolved (row CR-42). It is the
// one classification every view uses: the couldn't-read lists, the
// endpoint flag path_withheld and the change views. Only Go files have the
// writer, so a message of another file is a mount.
func IsWithheldRegistration(d contract.Diagnostic) bool {
	return d.Code == WithheldCode && strings.HasSuffix(d.File, ".go") && IsWithheldForm(d.Message)
}

// IsWithheldForm says whether a message has the registration form of row
// CR-44, whatever its file (a check that the form appears only in Go
// files uses it).
func IsWithheldForm(message string) bool {
	return withheldMessage.MatchString(message)
}

// RouteSite is a route registration's place: its file and the span the
// route node and its diagnostics share (the registration call's span).
type RouteSite struct {
	File string
	Span contract.Span
}

// WithheldSites are the sites of the route registrations whose path the Go
// linker withholds, read from stored diagnostics (row CR-44). A message
// without a readable span names no site, so no route is marked from it.
func WithheldSites(diagnostics []contract.Diagnostic) map[RouteSite]bool {
	out := map[RouteSite]bool{}
	for _, d := range diagnostics {
		if !IsWithheldRegistration(d) {
			continue
		}
		m := withheldSpan.FindStringSubmatch(d.Message)
		if m == nil {
			continue
		}
		var n [4]int
		ok := true
		for i := range n {
			v, err := strconv.Atoi(m[i+1])
			if err != nil {
				ok = false
				break
			}
			n[i] = v
		}
		if ok {
			out[RouteSite{File: d.File, Span: contract.Span{StartLine: n[0], StartColumn: n[1], EndLine: n[2], EndColumn: n[3]}}] = true
		}
	}
	return out
}

// MayBeWithheld says whether a route node can be a withheld registration:
// a route with a span and no full path. Callers read the diagnostics only
// when some node may be one.
func MayBeWithheld(n contract.Node) bool {
	if n.Kind != "route" || n.Span == nil {
		return false
	}
	paths, _ := n.Attributes["paths"].([]any)
	for _, p := range paths {
		if m, ok := p.(map[string]any); ok && FullPathEntry(m) {
			return false
		}
	}
	return true
}

// PathWithheld says whether a route node is a registration whose path the
// Go linker withholds: it may be one (MayBeWithheld) and its site is one
// of sites. A view then says "path withheld" rather than "path not known"
// (ADR 0089 Revision 5); neither is "not mounted".
func PathWithheld(n contract.Node, sites map[RouteSite]bool) bool {
	return len(sites) > 0 && MayBeWithheld(n) && sites[RouteSite{File: n.File, Span: *n.Span}]
}
