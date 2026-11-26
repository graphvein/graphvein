package projectsuggest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/graphvein/graphvein/internal/config"
)

// isComposeFile matches compose.yaml, docker-compose.yml and their
// variants (docker-compose.override.yml, dev.docker-compose.yml,
// cdk-docker-compose.yml).
func isComposeFile(name string) bool {
	lower := strings.ToLower(name)
	var stem string
	switch {
	case strings.HasSuffix(lower, ".yml"):
		stem = strings.TrimSuffix(lower, ".yml")
	case strings.HasSuffix(lower, ".yaml"):
		stem = strings.TrimSuffix(lower, ".yaml")
	default:
		return false
	}
	for _, base := range []string{"docker-compose", "compose"} {
		if stem == base || strings.HasPrefix(stem, base+".") || strings.HasSuffix(stem, "."+base) || strings.HasSuffix(stem, "-"+base) {
			return true
		}
	}
	return false
}

// readCompose reads rows SG-C1 to SG-C3. owner is the member holding the
// file, or nil for the project folder. A service is attributed to the
// member its literal build context lies in; a service without one (an
// image) is not a member, so it proposes nothing.
func (s *state) readCompose(owner *config.Member, path string, data []byte) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		s.unreadAt(owner, path, 0, fmt.Sprintf("not valid YAML: %v", err))
		return
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		s.unreadAt(owner, path, 0, "not a Compose mapping")
		return
	}
	services := yamlValue(doc.Content[0], "services")
	if services == nil {
		s.unreadAt(owner, path, 0, "no services mapping (Compose file format 1 is not read)")
		return
	}
	if services.Kind != yaml.MappingNode {
		s.unreadAt(owner, path, services.Line, "services is not a literal mapping")
		return
	}
	composeID := path
	for i := 0; i+1 < len(services.Content); i += 2 {
		name, svc := services.Content[i].Value, services.Content[i+1]
		// Every service name is recorded, read or not, so an address naming
		// it from outside the file (row RowMatchServiceName) sees every
		// service that may answer to it.
		known := service{owner: owner, file: path, name: name, line: services.Content[i].Line}
		if svc.Kind != yaml.MappingNode {
			known.reason = "is not a literal mapping"
			s.services = append(s.services, known)
			s.unreadAt(owner, path, svc.Line, fmt.Sprintf("service %s is not a literal mapping", name))
			continue
		}
		if k := firstKey(svc, "<<", "extends"); k != nil {
			known.reason = "uses " + k.Value
			s.services = append(s.services, known)
			s.unreadAt(owner, path, k.Line, fmt.Sprintf("service %s uses %s (its fields come from elsewhere)", name, k.Value))
			continue
		}
		member, reason := s.buildMember(path, svc)
		switch {
		case member != nil:
			known.member, known.scope = member, scopeOf(member, s.buildDir(path, svc))
		case yamlValue(svc, "build") == nil && yamlValue(svc, "image") == nil:
			known.reason = "has neither build nor image in this file (its fields come from another Compose file)"
		default:
			known.reason = reason
		}
		s.services = append(s.services, known)
		ports, expose, env := yamlValue(svc, "ports"), yamlValue(svc, "expose"), yamlValue(svc, "environment")
		if ports == nil && expose == nil && env == nil {
			continue
		}
		if member == nil {
			s.unreadAt(owner, path, svc.Line, fmt.Sprintf("service %s: %s", name, reason))
			continue
		}
		scope := known.scope
		if ports != nil {
			s.composePorts(owner, member, scope, path, composeID, name, ports)
		}
		if expose != nil {
			s.composeExpose(owner, member, scope, path, composeID, name, expose)
		}
		if env != nil {
			s.composeEnvironment(owner, member, scope, path, composeID, name, env)
		}
	}
}

// service is one Compose service as row RowMatchServiceName sees it: its
// file, the member holding the file (nil for the project folder), and the
// member (and scope) it is built from, or why it is attributed to none.
type service struct {
	owner  *config.Member
	file   string
	name   string
	line   int
	member *config.Member
	scope  string
	reason string
}

// buildMember returns the member a service's literal build context lies in.
func (s *state) buildMember(path string, svc *yaml.Node) (*config.Member, string) {
	build := yamlValue(svc, "build")
	if build == nil {
		return nil, "runs an image, not built from a member (no build context)"
	}
	ctx, ok := buildContext(build)
	if !ok {
		return nil, "build context is not a literal local path"
	}
	dir := filepath.Join(filepath.Dir(path), filepath.FromSlash(ctx))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Sprintf("build context %s is not a folder here (Compose resolves it from the file's folder)", ctx)
	}
	m := s.memberFor(dir)
	if m == nil {
		return nil, fmt.Sprintf("build context %s is in no member checkout", ctx)
	}
	return m, ""
}

func (s *state) buildDir(path string, svc *yaml.Node) string {
	ctx, _ := buildContext(yamlValue(svc, "build"))
	return filepath.Join(filepath.Dir(path), filepath.FromSlash(ctx))
}

// buildContext reads `build: DIR` or `build: {context: DIR}`; a context
// that is interpolated or remote is not a literal local path.
func buildContext(build *yaml.Node) (string, bool) {
	n := build
	if build.Kind == yaml.MappingNode {
		n = yamlValue(build, "context")
		if n == nil {
			return ".", true
		}
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Value == "" {
		return "", false
	}
	v := n.Value
	if strings.ContainsAny(v, "$") || strings.Contains(v, "://") || strings.HasPrefix(v, "git@") {
		return "", false
	}
	return v, true
}

// composePorts reads row SG-C2: short ("8080", "3000:80",
// "127.0.0.1:3000:80", optional "/tcp") and long syntax.
func (s *state) composePorts(owner, member *config.Member, scope, path, composeID, service string, ports *yaml.Node) {
	if ports.Kind != yaml.SequenceNode {
		s.unreadAt(owner, path, ports.Line, fmt.Sprintf("service %s: ports is not a literal list", service))
		return
	}
	for _, item := range ports.Content {
		published, target, reason := composePort(item)
		if reason != "" {
			s.unreadAt(owner, path, item.Line, fmt.Sprintf("service %s: ports entry %s", service, reason))
		}
		if target == 0 {
			continue
		}
		key := service + ".ports"
		if published != 0 {
			s.listens = append(s.listens, listen{member: member, scope: scope, port: published, local: true,
				evidence: s.evidence(owner, RowComposePorts, path, item.Line, key, portText(item))})
		}
		s.listens = append(s.listens, listen{member: member, scope: scope, port: target, service: service, composeFile: composeID,
			evidence: s.evidence(owner, RowComposePorts, path, item.Line, key, portText(item))})
	}
}

func portText(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	var parts []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		parts = append(parts, n.Content[i].Value+": "+n.Content[i+1].Value)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func composePort(n *yaml.Node) (published, target int, reason string) {
	switch n.Kind {
	case yaml.ScalarNode:
		v := n.Value
		if i := strings.LastIndex(v, "/"); i >= 0 {
			v = v[:i]
		}
		// A published part may use interpolation (`${PORT:-8080}:8080`);
		// it is cut at the last colon outside braces, and only a literal
		// container port is kept.
		parts := splitPort(v)
		if len(parts) > 3 {
			return 0, 0, "has more than three parts"
		}
		var ok bool
		if target, ok = parsePort(parts[len(parts)-1]); !ok {
			return 0, 0, "has a container port that is not one number (range or interpolation)"
		}
		if len(parts) == 1 {
			return 0, target, ""
		}
		if published, ok = parsePort(parts[len(parts)-2]); !ok {
			// The container port still names the service's port.
			return 0, target, "has a published port that is not one number (interpolation, range or empty); only its container port is read"
		}
		return published, target, ""
	case yaml.MappingNode:
		t := yamlValue(n, "target")
		if t == nil || t.Kind != yaml.ScalarNode {
			return 0, 0, "has no literal target"
		}
		var ok bool
		if target, ok = parsePort(t.Value); !ok {
			return 0, 0, "has a target that is not one number"
		}
		if p := yamlValue(n, "published"); p != nil {
			if p.Kind != yaml.ScalarNode {
				return 0, 0, "has a published port that is not literal"
			}
			if published, ok = parsePort(p.Value); !ok {
				return 0, 0, "has a published port that is not one number"
			}
		}
		return published, target, ""
	}
	return 0, 0, "is not a string, number or mapping"
}

// composeExpose reads row SG-C3.
func (s *state) composeExpose(owner, member *config.Member, scope, path, composeID, service string, expose *yaml.Node) {
	if expose.Kind != yaml.SequenceNode {
		s.unreadAt(owner, path, expose.Line, fmt.Sprintf("service %s: expose is not a literal list", service))
		return
	}
	for _, item := range expose.Content {
		v := item.Value
		if i := strings.LastIndex(v, "/"); i >= 0 {
			v = v[:i]
		}
		port, ok := parsePort(v)
		if item.Kind != yaml.ScalarNode || !ok {
			s.unreadAt(owner, path, item.Line, fmt.Sprintf("service %s: expose entry is not one port", service))
			continue
		}
		s.listens = append(s.listens, listen{member: member, scope: scope, port: port, service: service, composeFile: composeID,
			evidence: s.evidence(owner, RowComposeExpose, path, item.Line, service+".expose", item.Value)})
	}
}

// composeEnvironment reads row SG-C1: list (`- KEY=VALUE`) or mapping
// form. Compose interpolates `$`, so such values are refused.
func (s *state) composeEnvironment(owner, member *config.Member, scope, path, composeID, service string, env *yaml.Node) {
	type entry struct {
		key, value string
		line       int
	}
	var entries []entry
	switch env.Kind {
	case yaml.SequenceNode:
		for _, item := range env.Content {
			if item.Kind != yaml.ScalarNode {
				s.unreadAt(owner, path, item.Line, fmt.Sprintf("service %s: environment entry is not a string", service))
				continue
			}
			k, v, _ := strings.Cut(item.Value, "=")
			entries = append(entries, entry{k, v, item.Line})
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(env.Content); i += 2 {
			v := env.Content[i+1]
			if v.Kind != yaml.ScalarNode {
				s.unreadAt(owner, path, v.Line, fmt.Sprintf("service %s: environment value is not literal", service))
				continue
			}
			entries = append(entries, entry{env.Content[i].Value, v.Value, env.Content[i].Line})
		}
	default:
		s.unreadAt(owner, path, env.Line, fmt.Sprintf("service %s: environment is not a literal list or mapping", service))
		return
	}
	for _, e := range entries {
		if !strings.Contains(e.value, "://") {
			continue
		}
		key := strings.TrimSpace(e.key)
		if !validEnvKey(key) {
			s.unreadAt(owner, path, e.line, fmt.Sprintf("service %s: environment key is not a variable name", service))
			continue
		}
		a, reason, isHTTP := readURL(strings.TrimSpace(e.value))
		if !isHTTP {
			continue
		}
		if reason != "" {
			s.unreadAt(owner, path, e.line, fmt.Sprintf("service %s: %s: %s", service, key, reason))
			continue
		}
		a.consumer, a.scope, a.base, a.composeFile = member, scope, key, composeID
		a.evidence = s.evidence(owner, RowComposeEnvironment, path, e.line, service+".environment."+key, strings.TrimSpace(e.value))
		s.addresses = append(s.addresses, a)
	}
}

func yamlValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func firstKey(m *yaml.Node, keys ...string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		for _, k := range keys {
			if m.Content[i].Value == k {
				return m.Content[i]
			}
		}
	}
	return nil
}

// splitPort splits a short ports entry at colons outside ${...}.
func splitPort(v string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ':':
			if depth == 0 {
				parts = append(parts, v[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, v[start:])
}
