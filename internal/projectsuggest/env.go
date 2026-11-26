package projectsuggest

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
)

// isEnvFile matches `.env` and `.env.*` (for example .env.development and
// .env.example: a committed example still says where the member points).
func isEnvFile(name string) bool {
	return name == ".env" || strings.HasPrefix(name, ".env.")
}

// envPortKeys are the env keys read as the port a member listens on
// (RowEnvPort). Other *_PORT keys usually name a port the member connects
// to (DATABASE_PORT), so they are not read.
var envPortKeys = map[string]bool{"PORT": true, "HTTP_PORT": true, "SERVER_PORT": true, "APP_PORT": true, "API_PORT": true}

// readEnv reads rows SG-E1 and SG-E2 from a dotenv file. Lines that hold
// neither a URL nor a port key are not evidence and are skipped silently;
// a line that holds "://" but is refused is listed as unread.
func (s *state) readEnv(m *config.Member, path string, data []byte) {
	for i, raw := range strings.Split(string(data), "\n") {
		line := i + 1
		text := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		eq := strings.Index(text, "=")
		if eq <= 0 {
			if strings.Contains(text, "://") {
				s.unreadAt(m, path, line, "not a KEY=VALUE line")
			}
			continue
		}
		key := strings.TrimSpace(text[:eq])
		value, problem := envValue(strings.TrimSpace(text[eq+1:]))
		if !validEnvKey(key) {
			if strings.Contains(text, "://") {
				s.unreadAt(m, path, line, "not a KEY=VALUE line")
			}
			continue
		}
		if envPortKeys[key] {
			if problem != "" {
				s.unreadAt(m, path, line, fmt.Sprintf("%s: %s", key, problem))
				continue
			}
			if value == "" {
				continue
			}
			port, ok := parsePort(value)
			if !ok {
				s.unreadAt(m, path, line, fmt.Sprintf("%s: not a port from 1 to 65535", key))
				continue
			}
			s.listens = append(s.listens, listen{
				member: m, scope: scopeOf(m, path), port: port, local: true,
				evidence: s.evidence(m, RowEnvPort, path, line, key, value),
			})
			continue
		}
		if !strings.Contains(text[eq+1:], "://") {
			continue
		}
		if problem != "" {
			s.unreadAt(m, path, line, fmt.Sprintf("%s: %s", key, problem))
			continue
		}
		a, reason, isHTTP := readURL(value)
		if !isHTTP {
			continue
		}
		if reason != "" {
			s.unreadAt(m, path, line, fmt.Sprintf("%s: %s", key, reason))
			continue
		}
		a.consumer, a.scope, a.base, a.dir = m, scopeOf(m, path), key, filepath.Dir(path)
		a.evidence = s.evidence(m, RowEnvURL, path, line, key, value)
		s.addresses = append(s.addresses, a)
	}
}

// validEnvKey is a shell-style variable name, so it can be a base selector.
func validEnvKey(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		ok := r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// envValue unquotes a dotenv value. Interpolation and escapes are refused:
// their result depends on the tool that loads the file.
func envValue(v string) (string, string) {
	if v == "" {
		return "", ""
	}
	if q := v[0]; q == '"' || q == '\'' {
		end := strings.IndexByte(v[1:], q)
		if end < 0 {
			return "", "quoted value not closed on its line"
		}
		inner := v[1 : end+1]
		if q == '"' && (strings.Contains(inner, `\`) || strings.Contains(inner, "$")) {
			return "", "value uses escapes or interpolation"
		}
		return inner, ""
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	if strings.Contains(v, "$") {
		return "", "value uses interpolation"
	}
	return v, ""
}

// readURL reads an absolute http(s) URL as an address. isHTTP is false for
// a value that is not an http(s) URL at all (a database URL, for example),
// which is not evidence. reason is set when an http(s) URL is refused.
func readURL(v string) (a address, reason string, isHTTP bool) {
	lower := strings.ToLower(v)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return address{}, "", false
	}
	if strings.ContainsAny(v, "${}` \t") {
		return address{}, "URL uses interpolation or whitespace", true
	}
	u, err := url.Parse(v)
	if err != nil {
		return address{}, "not a valid URL", true
	}
	switch {
	case u.User != nil:
		return address{}, "URL carries credentials (not read)", true
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return address{}, "URL has a query or fragment (not read: it may hold a secret)", true
	case u.Hostname() == "":
		return address{}, "URL has no host", true
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := 80
	if scheme == "https" {
		port = 443
	}
	origin := scheme + "://" + strings.ToLower(u.Host)
	if p := u.Port(); p != "" {
		n, ok := parsePort(p)
		if !ok {
			return address{}, "URL port is not a number from 1 to 65535", true
		}
		port = n
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return address{scheme: scheme, host: host, port: port, origin: origin}, "", true
}
