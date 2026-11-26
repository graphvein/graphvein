package deploy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"go.yaml.in/yaml/v3"
)

// readCompose reads one compose file (row DC-S1). It returns one node per
// service of its `services` mapping, or an error for YAML the reader
// refuses (syntax, an unknown anchor, several documents, duplicate keys,
// a services value that is not a mapping or a service that is not one).
// A file without a `services` key states nothing (DCD).
func readCompose(file string, source []byte) ([]contract.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(source))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			tag("DCD/empty")
			return nil, nil
		}
		return nil, fmt.Errorf("YAML refused: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("YAML refused: %w", err)
		}
		return nil, errors.New("several YAML documents")
	}
	root := resolve(&doc)
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = resolve(root.Content[0])
	}
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("the document is not a mapping")
	}
	top, err := pairs(root)
	if err != nil {
		return nil, err
	}
	var services *yaml.Node
	var after []int // lines of the top-level keys, to bound service spans
	for _, p := range top {
		after = append(after, p.key.Line)
		switch p.key.Value {
		case "services":
			services = p.value
		case "include":
			tag("DCD/include")
		}
	}
	if services == nil || isNull(services) {
		tag("DCD/no-services")
		return nil, nil
	}
	if services.Kind != yaml.MappingNode {
		return nil, errors.New("services is not a mapping")
	}
	list, err := pairs(services)
	if err != nil {
		return nil, err
	}
	lines := sourceLines(source)
	var bounds []int
	bounds = append(bounds, after...)
	for _, p := range list {
		bounds = append(bounds, p.key.Line)
	}
	sort.Ints(bounds)
	var nodes []contract.Node
	for _, p := range list {
		n, err := service(file, p, lines, bounds)
		if err != nil {
			return nil, fmt.Errorf("service %q: %w", p.key.Value, err)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// service builds one deploy_service node.
func service(file string, p pair, lines []string, bounds []int) (contract.Node, error) {
	name := p.key.Value
	body := p.value
	if isNull(body) {
		body = &yaml.Node{Kind: yaml.MappingNode}
	}
	if body.Kind != yaml.MappingNode {
		return contract.Node{}, errors.New("not a mapping")
	}
	fields, err := pairs(body)
	if err != nil {
		return contract.Node{}, err
	}
	tag("DC-S1")
	attrs := map[string]any{"service": name}
	var interp []string
	attrs["image"], attrs["image_name"], attrs["image_tag"], attrs["image_digest"] = nil, nil, nil, nil
	attrs["build_context_as_written"], attrs["build_dockerfile_as_written"] = nil, nil
	attrs["depends_on"] = []any{}
	attrs["ports"] = []any{}
	attrs["environment_urls"] = []any{}
	attrs["traefik_routers"] = []any{}
	attrs["traefik_enable"], attrs["traefik_enabled"] = nil, nil
	var command *yaml.Node
	for _, f := range fields {
		switch f.key.Value {
		case "image":
			if s, ok := scalar(f.value); ok {
				tag("DC-S1/image")
				imageAttrs(attrs, s, &interp)
			}
		case "build":
			if err := buildAttrs(attrs, file, f.value, &interp); err != nil {
				return contract.Node{}, err
			}
		case "depends_on":
			names, err := dependsOn(f.value)
			if err != nil {
				return contract.Node{}, err
			}
			tag("DC-S1/depends_on")
			attrs["depends_on"] = anyList(names)
		case "ports":
			ports, err := portList(f.value)
			if err != nil {
				return contract.Node{}, err
			}
			tag("DC-S1/ports")
			attrs["ports"] = ports
		case "environment":
			env, err := keyValues(f.value)
			if err != nil {
				return contract.Node{}, fmt.Errorf("environment: %w", err)
			}
			attrs["environment_urls"] = environmentURLs(env)
		case "labels":
			labels, err := keyValues(f.value)
			if err != nil {
				return contract.Node{}, fmt.Errorf("labels: %w", err)
			}
			attrs["traefik_routers"] = routers(labels)
			attrs["traefik_enable"], attrs["traefik_enabled"] = traefikEnable(labels)
		case "command":
			command = f.value
		case "extends":
			tag("DCD/extends")
		}
	}
	if name, _ := attrs["image_name"].(string); traefikImage(name) {
		args, err := commandArgs(command)
		if err != nil {
			return contract.Node{}, fmt.Errorf("command: %w", err)
		}
		attrs["traefik_entrypoints"] = entrypoints(args)
		attrs["traefik_exposed_by_default"] = exposedByDefault(args)
	}
	sort.Strings(interp)
	attrs["interpolated"] = anyList(interp)
	start := p.key.Line
	end := len(lines)
	for _, b := range bounds {
		if b > start {
			end = b - 1
			break
		}
	}
	for end > start {
		trimmed := strings.TrimSpace(lines[end-1])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		end--
	}
	text := spanText(lines, start, end)
	identity, err := contract.Identity("deploy_service", file, name)
	if err != nil {
		return contract.Node{}, errors.New("the service name cannot form an identity")
	}
	span := contract.Span{StartLine: start, StartColumn: 0, EndLine: end, EndColumn: len(lines[end-1])}
	return contract.Node{Type: "node", Identity: identity, Kind: "deploy_service", Level: 2, Name: name, QualifiedName: name,
		File: file, Span: &span, ContentHash: contract.ContentHash([]byte(text)), Attributes: attrs}, nil
}

// buildAttrs states `build` as written and resolved (row DC-S1). The
// context resolves against the compose file's directory, the Dockerfile
// against the context; a resolved path is repository-relative and absent
// when not given, interpolated, absolute, remote or outside the repository.
// `build_dockerfile` is absent when no dockerfile is written (the default
// `Dockerfile` of the context is not assumed).
func buildAttrs(attrs map[string]any, file string, value *yaml.Node, interp *[]string) error {
	var context, dockerfile string
	contextGiven, dockerfileGiven := false, false
	switch {
	case isNull(value):
		return nil
	case value.Kind == yaml.ScalarNode:
		context, contextGiven = value.Value, true
	case value.Kind == yaml.MappingNode:
		fields, err := pairs(value)
		if err != nil {
			return fmt.Errorf("build: %w", err)
		}
		for _, f := range fields {
			s, ok := scalar(f.value)
			if !ok {
				continue
			}
			switch f.key.Value {
			case "context":
				context, contextGiven = s, true
			case "dockerfile":
				dockerfile, dockerfileGiven = s, true
			}
		}
	default:
		return errors.New("build is neither a string nor a mapping")
	}
	tag("DC-S1/build")
	if contextGiven {
		attrs["build_context_as_written"] = context
	}
	if dockerfileGiven {
		attrs["build_dockerfile_as_written"] = dockerfile
	}
	if !contextGiven {
		// The compose specification's default context is the project
		// directory; read per file, that is the file's own directory.
		context = "."
	}
	if interpolated(context) {
		*interp = append(*interp, "build.context")
	}
	if dockerfileGiven && interpolated(dockerfile) {
		*interp = append(*interp, "build.dockerfile")
	}
	resolved, ok := resolvePath(path.Dir(file), context)
	if !ok {
		tag("DC-S1/build-unresolved")
		return nil
	}
	attrs["build_context"] = resolved
	if dockerfileGiven {
		if df, ok := resolvePath(resolved, dockerfile); ok {
			attrs["build_dockerfile"] = df
		} else {
			tag("DC-S1/build-unresolved")
		}
	}
	return nil
}

// resolvePath joins a relative path to a repository-relative base. It
// refuses interpolated, absolute, remote (URL or git) and empty paths and
// paths that leave the repository.
func resolvePath(base, p string) (string, bool) {
	if p == "" || interpolated(p) || strings.HasPrefix(p, "/") || strings.Contains(p, "://") || strings.HasPrefix(p, "git@") || strings.Contains(p, "\\") || (len(p) > 1 && p[1] == ':') {
		return "", false
	}
	joined := path.Clean(path.Join(base, p))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", false
	}
	return joined, true
}

// dependsOn reads `depends_on` in its short (list) or long (mapping) form.
func dependsOn(value *yaml.Node) ([]string, error) {
	var out []string
	switch {
	case isNull(value):
	case value.Kind == yaml.SequenceNode:
		for _, item := range value.Content {
			s, ok := scalar(item)
			if !ok {
				return nil, errors.New("depends_on entry is not a string")
			}
			out = append(out, s)
		}
	case value.Kind == yaml.MappingNode:
		fields, err := pairs(value)
		if err != nil {
			return nil, err
		}
		for _, f := range fields {
			out = append(out, f.key.Value)
		}
	default:
		return nil, errors.New("depends_on is neither a list nor a mapping")
	}
	return out, nil
}

// portList reads `ports`: short entries as written, long entries by their
// target, published and protocol keys.
func portList(value *yaml.Node) ([]any, error) {
	out := []any{}
	if isNull(value) {
		return out, nil
	}
	if value.Kind != yaml.SequenceNode {
		return nil, errors.New("ports is not a list")
	}
	for _, item := range value.Content {
		item = resolve(item)
		if s, ok := scalar(item); ok {
			entry := map[string]any{"as_written": s, "interpolated": interpolated(s)}
			out = append(out, entry)
			continue
		}
		if item.Kind != yaml.MappingNode {
			return nil, errors.New("port entry is neither a string nor a mapping")
		}
		fields, err := pairs(item)
		if err != nil {
			return nil, err
		}
		entry := map[string]any{}
		written := false
		for _, f := range fields {
			switch f.key.Value {
			case "target", "published", "protocol":
				if s, ok := scalar(f.value); ok {
					entry[f.key.Value] = s
					written = written || interpolated(s)
				}
			}
		}
		entry["interpolated"] = written
		out = append(out, entry)
	}
	return out, nil
}

// keyValue is one entry of a list-or-mapping of strings (environment,
// labels); hasValue is false for `KEY` alone or a null value.
type keyValue struct {
	key, value string
	hasValue   bool
}

// keyValues reads the list (`KEY=VALUE`) or mapping form.
func keyValues(value *yaml.Node) ([]keyValue, error) {
	var out []keyValue
	switch {
	case isNull(value):
	case value.Kind == yaml.SequenceNode:
		for _, item := range value.Content {
			s, ok := scalar(item)
			if !ok {
				return nil, errors.New("entry is not a string")
			}
			k, v, found := strings.Cut(s, "=")
			out = append(out, keyValue{key: k, value: v, hasValue: found})
		}
	case value.Kind == yaml.MappingNode:
		fields, err := pairs(value)
		if err != nil {
			return nil, err
		}
		for _, f := range fields {
			s, ok := scalar(f.value)
			out = append(out, keyValue{key: f.key.Value, value: s, hasValue: ok})
		}
	default:
		return nil, errors.New("neither a list nor a mapping")
	}
	return out, nil
}

// environmentURLs is row DC-S2: an environment value that is a URL with a
// literal scheme keeps its name, scheme and host only; user info,
// password, port, path and query are never stored. An interpolated host
// is null and named in `interpolated`. Every other value is not stored.
func environmentURLs(env []keyValue) []any {
	out := []any{}
	for _, kv := range env {
		if !kv.hasValue {
			continue
		}
		scheme, host, hostInterpolated, ok := urlSchemeHost(kv.value)
		if !ok {
			tag("DC-S2/other")
			continue
		}
		tag("DC-S2")
		entry := map[string]any{"name": kv.key, "scheme": scheme, "host": nil, "interpolated": []any{}}
		switch {
		case hostInterpolated:
			entry["interpolated"] = []any{"host"}
		case host != "":
			entry["host"] = host
		}
		out = append(out, entry)
	}
	return out
}

// urlSchemeHost reads `scheme://[userinfo@]host[:port][/...]`. The scheme
// must be literal. An `@` after the authority (a password with a raw `/`)
// makes the host unreadable: not a URL for DC-S2.
func urlSchemeHost(v string) (scheme, host string, hostInterpolated, ok bool) {
	sep := strings.Index(v, "://")
	if sep <= 0 {
		return "", "", false, false
	}
	scheme = v[:sep]
	for i, c := range scheme {
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !(letter || (i > 0 && ((c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.'))) {
			return "", "", false, false
		}
	}
	rest := v[sep+3:]
	end := len(rest)
	for i := 0; i < len(rest); i++ {
		if strings.HasPrefix(rest[i:], "${") {
			close := strings.Index(rest[i:], "}")
			if close < 0 {
				return "", "", false, false
			}
			i += close
			continue
		}
		if rest[i] == '/' || rest[i] == '?' || rest[i] == '#' {
			end = i
			break
		}
	}
	authority, tail := rest[:end], rest[end:]
	if strings.Contains(tail, "@") {
		return "", "", false, false
	}
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = authority[at+1:]
	}
	host = authority
	if strings.HasPrefix(host, "[") {
		if close := strings.Index(host, "]"); close >= 0 {
			host = host[:close+1]
		}
	} else if colon := strings.Index(host, ":"); colon >= 0 && !strings.HasPrefix(host, "${") {
		if !validPort(host[colon+1:]) {
			return "", "", false, false
		}
		host = host[:colon]
	}
	if interpolated(host) {
		return scheme, "", true, true
	}
	if strings.ContainsAny(host, " \t") {
		return "", "", false, false
	}
	return scheme, host, false, true
}

// validPort accepts an empty port, digits, or one interpolation.
func validPort(p string) bool {
	if strings.HasPrefix(p, "${") && strings.HasSuffix(p, "}") && strings.Count(p, "}") == 1 {
		return true
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// routerOptions are the router keys of row DC-T1 and their attribute names.
var routerOptions = map[string]string{"rule": "rule", "entrypoints": "entrypoints", "tls": "tls", "tls.certresolver": "tls_certresolver"}

// routers is row DC-T1: `traefik.http.routers.<r>.{rule,entrypoints,tls,
// tls.certresolver}` labels grouped per router, values as written; `tls`
// also as a boolean when it parses as one. Every other label is DCD.
func routers(labels []keyValue) []any {
	const prefix = "traefik.http.routers."
	type router struct {
		name   string
		values map[string]string
		order  int
	}
	byName := map[string]*router{}
	for _, kv := range labels {
		if len(kv.key) <= len(prefix) || !strings.EqualFold(kv.key[:len(prefix)], prefix) {
			continue
		}
		rest := kv.key[len(prefix):]
		name, option := splitRouter(rest)
		attr, known := routerOptions[strings.ToLower(option)]
		if name == "" || !known {
			tag("DCD/label")
			continue
		}
		tag("DC-T1")
		r := byName[name]
		if r == nil {
			r = &router{name: name, values: map[string]string{}, order: len(byName)}
			byName[name] = r
		}
		r.values[attr] = kv.value
	}
	list := make([]*router, 0, len(byName))
	for _, r := range byName {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].order < list[j].order })
	out := []any{}
	for _, r := range list {
		var interp []string
		if interpolated(r.name) {
			tag("DC-T1/interpolated")
			interp = append(interp, "router")
		}
		entry := map[string]any{"router": r.name, "rule": nil, "entrypoints": nil, "tls": nil, "tls_enabled": nil, "tls_certresolver": nil}
		for _, attr := range []string{"rule", "entrypoints", "tls", "tls_certresolver"} {
			v, ok := r.values[attr]
			if !ok {
				continue
			}
			entry[attr] = v
			if interpolated(v) {
				interp = append(interp, attr)
			}
		}
		if v, ok := r.values["tls"]; ok && !interpolated(v) {
			if b, err := strconv.ParseBool(v); err == nil {
				entry["tls_enabled"] = b
			}
		}
		sort.Strings(interp)
		entry["interpolated"] = anyList(interp)
		out = append(out, entry)
	}
	return out
}

// traefikEnable is row DC-T1's `traefik.enable` label (review
// fix): the value as written and, when it parses as a boolean
// (strconv.ParseBool, as Traefik's label decoder), that boolean; nil, nil
// when absent. Traefik's Docker provider ignores a container whose value
// is false.
func traefikEnable(labels []keyValue) (any, any) {
	var written, enabled any
	for _, kv := range labels {
		if !strings.EqualFold(kv.key, "traefik.enable") {
			continue
		}
		tag("DC-T1/enable")
		written, enabled = kv.value, nil
		if !interpolated(kv.value) {
			if b, err := strconv.ParseBool(kv.value); err == nil {
				enabled = b
			}
		}
	}
	return written, enabled
}

// exposedByDefault is row DC-T2's `--providers.docker.exposedbydefault`
// argument of a Traefik service (review fix): true or false when
// written and parsed (a bare flag is true), "unknown" when written and
// not parsed, nil when absent. With false, Traefik's Docker provider
// ignores containers without `traefik.enable=true`.
func exposedByDefault(args []string) any {
	const flag = "--providers.docker.exposedbydefault"
	var out any
	for _, a := range args {
		key, value, found := strings.Cut(a, "=")
		if !strings.EqualFold(key, flag) {
			continue
		}
		tag("DC-T2/exposedbydefault")
		if !found {
			out = true
			continue
		}
		if b, err := strconv.ParseBool(value); err == nil && !interpolated(value) {
			out = b
		} else {
			out = "unknown"
		}
	}
	return out
}

// splitRouter splits "<router>.<option>" at the first dot outside an
// interpolation.
func splitRouter(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if strings.HasPrefix(s[i:], "${") {
			close := strings.Index(s[i:], "}")
			if close < 0 {
				return "", ""
			}
			i += close
			continue
		}
		if s[i] == '.' {
			return s[:i], s[i+1:]
		}
	}
	return "", ""
}

// traefikImage reports whether a parsed image name is Traefik's official
// image (`traefik`, `library/traefik`, `docker.io/library/traefik`).
func traefikImage(name string) bool {
	return officialImage(name) == "traefik"
}

// officialImage strips the Docker Hub registry and `library/` namespace.
func officialImage(name string) string {
	for _, p := range []string{"docker.io/", "index.docker.io/", "registry-1.docker.io/"} {
		name = strings.TrimPrefix(name, p)
	}
	return strings.TrimPrefix(name, "library/")
}

// commandArgs reads `command` as a list, or a string split like a shell
// word list (quotes group, no expansion). A string it cannot split is an
// error for the file (DC-S1 refusal).
func commandArgs(value *yaml.Node) ([]string, error) {
	if value == nil || isNull(value) {
		return nil, nil
	}
	value = resolve(value)
	switch value.Kind {
	case yaml.SequenceNode:
		var out []string
		for _, item := range value.Content {
			s, ok := scalar(item)
			if !ok {
				return nil, errors.New("argument is not a string")
			}
			out = append(out, s)
		}
		return out, nil
	case yaml.ScalarNode:
		return shellWords(value.Value)
	}
	return nil, errors.New("neither a list nor a string")
}

// shellWords splits on unquoted whitespace, honouring single and double
// quotes and backslash escapes.
func shellWords(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inWord := false
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}

// entrypointOptions are the entrypoint arguments of row DC-T2.
var entrypointOptions = map[string]string{"address": "address", "http.redirections.entrypoint.to": "redirect_to", "http.redirections.entrypoint.scheme": "redirect_scheme"}

// entrypoints is row DC-T2: `--entrypoints.<e>.address=<v>` and the HTTP
// redirection arguments of a Traefik service, values as written. Other
// arguments, and these without `=`, are DCD.
func entrypoints(args []string) []any {
	const prefix = "--entrypoints."
	type ep struct {
		name   string
		values map[string]string
		order  int
	}
	byName := map[string]*ep{}
	for _, a := range args {
		if len(a) <= len(prefix) || !strings.EqualFold(a[:len(prefix)], prefix) {
			continue
		}
		key, value, found := strings.Cut(a[len(prefix):], "=")
		name, option := splitRouter(key)
		attr, known := entrypointOptions[strings.ToLower(option)]
		if !found || name == "" || !known {
			tag("DCD/argument")
			continue
		}
		tag("DC-T2")
		e := byName[name]
		if e == nil {
			e = &ep{name: name, values: map[string]string{}, order: len(byName)}
			byName[name] = e
		}
		e.values[attr] = value
	}
	list := make([]*ep, 0, len(byName))
	for _, e := range byName {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].order < list[j].order })
	out := []any{}
	for _, e := range list {
		entry := map[string]any{"entrypoint": e.name, "address": nil, "redirect_to": nil, "redirect_scheme": nil}
		var interp []string
		if interpolated(e.name) {
			interp = append(interp, "entrypoint")
		}
		for _, attr := range []string{"address", "redirect_to", "redirect_scheme"} {
			if v, ok := e.values[attr]; ok {
				entry[attr] = v
				if interpolated(v) {
					interp = append(interp, attr)
				}
			}
		}
		sort.Strings(interp)
		entry["interpolated"] = anyList(interp)
		out = append(out, entry)
	}
	return out
}

// pair is one key and value of a mapping, aliases resolved.
type pair struct {
	key, value *yaml.Node
}

// resolve follows aliases to their anchored node.
func resolve(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// isNull reports an absent or null value.
func isNull(n *yaml.Node) bool {
	n = resolve(n)
	return n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}

// scalar returns a non-null scalar's text as written.
func scalar(n *yaml.Node) (string, bool) {
	n = resolve(n)
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return "", false
	}
	return n.Value, true
}

// pairs returns a mapping's entries with YAML merge keys (`<<: *anchor`,
// `<<: [*a, *b]`) applied: explicit keys override merged ones, earlier
// merged mappings override later ones. Duplicate explicit keys and
// non-scalar keys are refused, as compose refuses them.
func pairs(n *yaml.Node) ([]pair, error) {
	return pairsDepth(resolve(n), 0)
}

func pairsDepth(n *yaml.Node, depth int) ([]pair, error) {
	if depth > 32 {
		return nil, errors.New("merge keys nest too deeply")
	}
	if n.Kind != yaml.MappingNode {
		return nil, errors.New("not a mapping")
	}
	var explicit []pair
	var merged []pair
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := resolve(n.Content[i]), n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			return nil, errors.New("a mapping key is not a string")
		}
		if k.Tag == "!!merge" {
			sources := []*yaml.Node{resolve(v)}
			if sources[0].Kind == yaml.SequenceNode {
				sources = nil
				for _, item := range resolve(v).Content {
					sources = append(sources, resolve(item))
				}
			}
			for _, src := range sources {
				list, err := pairsDepth(src, depth+1)
				if err != nil {
					return nil, fmt.Errorf("merge key: %w", err)
				}
				merged = append(merged, list...)
			}
			continue
		}
		if seen[k.Value] {
			return nil, fmt.Errorf("duplicate key %q on line %d", k.Value, k.Line)
		}
		seen[k.Value] = true
		explicit = append(explicit, pair{key: k, value: resolve(v)})
	}
	out := explicit
	for _, m := range merged {
		if !seen[m.key.Value] {
			seen[m.key.Value] = true
			out = append(out, m)
		}
	}
	return out, nil
}
