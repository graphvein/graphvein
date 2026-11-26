package clients

import (
	"regexp"
	"strings"
)

// generatedIgnorePattern mirrors the scanner's private glob spelling for
// diagnostic provenance only. Visibility is still decided by the scanner.
// Keeping this local avoids expanding the scanner's public interface in T012e.
func generatedIgnorePattern(line string) (*regexp.Regexp, error) {
	if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
		line = line[1:]
	}
	line = strings.TrimSuffix(line, "/")
	anchored := strings.HasPrefix(line, "/") || strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	var b strings.Builder
	if anchored {
		b.WriteString("^")
	} else {
		b.WriteString("(^|/)")
	}
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '*':
			if i+1 < len(line) && line[i+1] == '*' {
				i++
				if i+1 < len(line) && line[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(line[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			segment := line[i : i+end+2]
			if strings.HasPrefix(segment, "[!") {
				segment = "[^" + segment[2:]
			}
			b.WriteString(segment)
			i += end + 1
		case '\\':
			if i+1 < len(line) {
				i++
				b.WriteString(regexp.QuoteMeta(line[i : i+1]))
			}
		default:
			b.WriteString(regexp.QuoteMeta(line[i : i+1]))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
