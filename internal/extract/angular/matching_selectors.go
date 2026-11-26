package angular

import (
	"regexp"
	"strings"
	"unicode"
)

type selectorAttribute struct{ name, value string }
type selectorCompound struct {
	element string
	attrs   []selectorAttribute
}

var selectorElement = regexp.MustCompile(`^[-a-z0-9_]+`)
var selectorAttr = regexp.MustCompile(`^\[([-.\w*]+)(?:=(?:"([^\]'"]*)"|'([^\]'"]*)'|([^\]'"]*)))?\]`)

// parseSelector deliberately rejects the characters Angular's permissive
// tokenizer skips, rather than claiming facts for an unverified interpretation.
func parseSelector(text string) ([]selectorCompound, bool) {
	out := []selectorCompound{}
	// Commas inside quoted attribute values belong to the value.
	parts := []string{}
	start, bracket := 0, false
	var quote rune
	for i, c := range text {
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if bracket && (c == '\'' || c == '"') {
			quote = c
			continue
		}
		if c == '[' {
			bracket = true
		}
		if c == ']' {
			bracket = false
		}
		if c == ',' && !bracket {
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	parts = append(parts, text[start:])
	for i, part := range parts {
		// Only separator whitespace is accepted; leading/trailing whitespace at
		// the outside of the entire selector is outside S1.
		if i > 0 {
			part = strings.TrimLeftFunc(part, angularWhitespace)
		}
		if i < len(parts)-1 {
			part = strings.TrimRightFunc(part, angularWhitespace)
		}
		c := selectorCompound{}
		c.element = selectorElement.FindString(part)
		part = part[len(c.element):]
		for part != "" {
			match := selectorAttr.FindStringSubmatch(part)
			if match == nil {
				return nil, false
			}
			value := match[2] + match[3] + match[4]
			c.attrs = append(c.attrs, selectorAttribute{match[1], selectorValue(value)})
			part = part[len(match[0]):]
		}
		if c.element == "" && len(c.attrs) == 0 {
			return nil, false
		}
		out = append(out, c)
	}
	return out, true
}

func selectorValue(value string) string {
	// JavaScript's full lowercase mapping expands dotted capital I. Go's
	// simple mapping would incorrectly let that selector match ASCII "i".
	// Other non-ASCII comparisons remain uncertain under N3; retain this
	// expansion so those values cannot accidentally become a definite ASCII
	// match. Contextual Greek sigma differences cannot match ASCII values.
	return strings.ToLower(strings.ReplaceAll(value, "\u0130", "i\u0307"))
}

func angularWhitespace(r rune) bool {
	// Angular's JavaScript whitespace set contains Unicode space separators,
	// its four line terminators, TAB/VT/FF and BOM. Go's IsSpace additionally
	// accepts NEL and omits BOM, so it would broaden the verified subset.
	return unicode.Is(unicode.Zs, r) || r == '\t' || r == '\v' || r == '\f' ||
		r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' || r == '\uFEFF'
}
