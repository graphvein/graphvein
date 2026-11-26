package openapi

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
)

func serversAttribute(v *value) []map[string]any {
	out := []map[string]any{}
	for _, server := range v.list() {
		variables := map[string]any{}
		for _, e := range server.get("variables").fields() {
			variables[e.key] = e.value.get("default").nullable()
		}
		out = append(out, map[string]any{"url": server.get("url").nullable(), "variables": variables})
	}
	return out
}

var variablePattern = regexp.MustCompile(`\{([^{}]+)\}`)

func operationPaths(d *document, item, op *value, raw string) ([]map[string]any, int) {
	entries := []map[string]any{}
	if d.format == "swagger" {
		base := d.root.get("basePath").text()
		if base == "" {
			base = "/"
		}
		schemes, exists := op.field("schemes")
		if !exists {
			schemes.value = d.root.get("schemes")
		}
		origins := []string{}
		host := d.root.get("host").text()
		if host != "" {
			for _, scheme := range stringsList(schemes.value) {
				origins = append(origins, scheme+"://"+host)
			}
		}
		entries = append(entries, pathEntry(base, raw, origins, "literal"))
	} else {
		servers := op.get("servers").list()
		if len(servers) == 0 {
			servers = item.get("servers").list()
		}
		if len(servers) == 0 {
			servers = d.root.get("servers").list()
		}
		if len(servers) == 0 {
			entries = append(entries, pathEntry("/", raw, []string{}, "literal"))
		}
		for _, server := range servers {
			text := server.get("url").text()
			resolution := "literal"
			missing := false
			text = variablePattern.ReplaceAllStringFunc(text, func(token string) string {
				resolution = "server_default"
				def := server.get("variables").get(token[1 : len(token)-1]).get("default").nullable()
				if def == nil {
					missing = true
					return token
				}
				return def.(string)
			})
			if strings.ContainsAny(text, "{}") {
				missing = true
			}
			// Server URLs use the served document location, never $self. A relative
			// URL without a leading slash cannot prove that deployment location.
			if i := strings.IndexAny(text, "?#"); i >= 0 {
				text = text[:i]
			}
			base := text
			origins := []string{}
			if strings.HasPrefix(text, "//") || strings.Contains(text, "://") {
				u, err := url.Parse(text)
				if err != nil || u.Host == "" {
					missing = true
				} else {
					// Split the source spelling: URL.EscapedPath would encode
					// literal Unicode, violating path normalization's byte preservation.
					start := 2
					if u.Scheme != "" {
						start = strings.Index(text, "://") + 3
					}
					end := len(text)
					if slash := strings.IndexByte(text[start:], '/'); slash >= 0 {
						end = start + slash
					}
					origin := text[:end]
					origins = append(origins, origin)
					base = text[end:]
				}
			} else if !strings.HasPrefix(text, "/") {
				missing = true
			}
			if missing {
				entries = append(entries, map[string]any{"template": nil, "origins": []string{}, "resolution": "unresolved"})
			} else {
				entries = append(entries, pathEntry(base, raw, origins, resolution))
			}
		}
	}
	byKey := map[string]map[string]any{}
	for _, e := range entries {
		key := "\xff"
		if e["template"] != nil {
			key = e["template"].(string)
		}
		if e["trailing_slash"] == true {
			key += "\x00slash"
		}
		if old := byKey[key]; old != nil {
			old["origins"] = unique(append(old["origins"].([]string), e["origins"].([]string)...))
			if e["resolution"] == "literal" {
				old["resolution"] = "literal"
			}
		} else {
			e["origins"] = unique(e["origins"].([]string))
			byKey[key] = e
		}
	}
	keys := []string{}
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []map[string]any{}
	for _, key := range keys {
		if len(out) < 10 {
			out = append(out, byKey[key])
		}
	}
	return out, len(keys)
}
func pathEntry(base, raw string, origins []string, resolution string) map[string]any {
	joined := strings.TrimRight(base, "/") + "/" + strings.TrimLeft(raw, "/")
	// Route normalization collapses slashes without cleaning dot segments,
	// decoding percent escapes, or converting literal colon segments.
	parts := []string{}
	for _, s := range strings.Split(joined, "/") {
		if s != "" {
			parts = append(parts, s)
		}
	}
	template := "/" + strings.Join(parts, "/")
	e := map[string]any{"template": template, "origins": origins, "resolution": resolution}
	if template != "/" && strings.HasSuffix(joined, "/") {
		e["trailing_slash"] = true
	}
	return e
}
func unique(ss []string) []string {
	sort.Strings(ss)
	out := []string{}
	for _, s := range ss {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}
