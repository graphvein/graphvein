package query

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
)

// Match tiers of a target lookup (T030 review). A store lookup by term
// (TargetFilter) returns every node whose name or qualified name equals the
// term or whose qualified name ends with it; only some of those are what
// the term names.
const (
	// MatchIdentity: the term is the node's canonical identity.
	MatchIdentity = "identity"
	// MatchName: the term is the node's name or its whole qualified name.
	MatchName = "name"
	// MatchQualifiedSuffix: the qualified name ends with the term at a
	// name boundary ("save" in "User.save", "id" in "Post.id").
	MatchQualifiedSuffix = "qualified_suffix"
	// MatchPartial: the qualified name ends with the term inside a name
	// ("Items" in "listItems"). Such a node is offered as a candidate,
	// never resolved: the term does not name it.
	MatchPartial = "partial_suffix"
)

// MatchOf is the tier at which term matches n; "" when it does not match.
func MatchOf(n contract.Node, term string) string {
	switch {
	case term == "":
		return ""
	case n.Identity == term && IsIdentity(term):
		return MatchIdentity
	case n.Name == term || n.QualifiedName == term:
		return MatchName
	case !strings.HasSuffix(n.QualifiedName, term):
		return ""
	case atBoundary(n.QualifiedName, term):
		return MatchQualifiedSuffix
	}
	return MatchPartial
}

// atBoundary reports whether the suffix term of qualified starts at a name
// boundary: the character before it, or its own first character, is not
// part of an identifier (a qualifier separator such as ".", "::", "#" or
// "/", a space, a dash).
func atBoundary(qualified, term string) bool {
	before, _ := utf8.DecodeLastRuneInString(qualified[:len(qualified)-len(term)])
	first, _ := utf8.DecodeRuneInString(term)
	return !identifierRune(before) || !identifierRune(first)
}

func identifierRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Resolve splits the nodes a TargetFilter lookup of term returned into the
// ones the term resolves to (identity, name or a suffix at a name boundary)
// and the partial suffix matches, which a caller offers as candidates only.
// This is the one target rule of graphvein callers and impact (single
// repository and project) and of the MCP server's tools: exactly one
// resolving node is the target; several are ambiguous; none, with partial
// matches, is "no matching target" with those as candidates.
func Resolve(term string, nodes []contract.Node) (resolving, partial []contract.Node) {
	for _, n := range nodes {
		switch MatchOf(n, term) {
		case MatchIdentity, MatchName, MatchQualifiedSuffix:
			resolving = append(resolving, n)
		case MatchPartial:
			partial = append(partial, n)
		}
	}
	return resolving, partial
}
