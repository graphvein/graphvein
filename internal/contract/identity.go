package contract

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

var kindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidPath rejects checkout-specific and ambiguous paths before they reach storage.
func ValidPath(p string, root bool) bool {
	if root && p == "." {
		return true
	}
	if p == "" || !utf8.ValidString(p) || strings.ContainsAny(p, "\\\x00\r\n") || strings.HasPrefix(p, "/") || strings.Contains(p, ":") || path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func escape(s string, slash bool) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", rune(c)) || slash && c == '/' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// Identity builds the canonical stable key without body hashes or commit metadata.
func Identity(kind, file, qualified string) (string, error) {
	if !kindPattern.MatchString(kind) || !ValidPath(file, true) || qualified == "" || !utf8.ValidString(qualified) || strings.ContainsRune(qualified, 0) {
		return "", fmt.Errorf("invalid identity components")
	}
	return kind + ":" + escape(file, true) + "#" + escape(qualified, false), nil
}

// ParseIdentity requires canonical spelling to avoid duplicate identities.
func ParseIdentity(id string) (kind, file, qualified string, err error) {
	kind, rest, ok := strings.Cut(id, ":")
	if !ok {
		return "", "", "", fmt.Errorf("invalid identity %q", id)
	}
	p, q, ok := strings.Cut(rest, "#")
	if !ok {
		return "", "", "", fmt.Errorf("invalid identity %q", id)
	}
	file, e1 := url.PathUnescape(p)
	qualified, e2 := url.PathUnescape(q)
	canonical, e3 := Identity(kind, file, qualified)
	if e1 != nil || e2 != nil || e3 != nil || canonical != id {
		return "", "", "", fmt.Errorf("invalid identity %q", id)
	}
	return kind, file, qualified, nil
}

// ContentHash preserves potentially meaningful bytes while removing checkout line endings.
func ContentHash(source []byte) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(source), "\r\n", "\n"), "\r", "\n")
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(normalized)))
}
