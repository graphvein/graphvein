package config

import (
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// This file holds the per-environment try-it options (T029, ADR
// 0081): certificate pins and a CA file for servers the system does not
// trust, the opt-in use of the system proxy, and raised limits. Every
// option is written by the user, by hand or through the server's guarded
// environment edit; none has a default other than "off".

// Limit defaults and hard ceilings of one try-it exchange. A value in a
// file above its ceiling is an error, never clamped.
const (
	DefaultRequestBytes   = 1 << 20
	DefaultResponseBytes  = 4 << 20
	DefaultTimeoutSeconds = 30
	MaxRequestBytes       = 64 << 20
	MaxResponseBytes      = 256 << 20
	MaxTimeoutSeconds     = 600
	// MaxPins bounds the pinned certificates of one environment.
	MaxPins = 16
)

// EnvironmentOptions are the optional settings of an environment beyond
// its address and auth.
type EnvironmentOptions struct {
	// TLS is nil when the environment trusts the system roots only.
	TLS *TLSOptions
	// UseSystemProxy makes sends to this environment honour HTTPS_PROXY,
	// HTTP_PROXY and NO_PROXY; off by default.
	UseSystemProxy bool
	// Limits are zero where the default applies.
	Limits Limits
}

// TLSOptions add trust for one environment's origin.
type TLSOptions struct {
	// Pins are leaf certificates (by fingerprint) accepted for one origin
	// whatever their issuer, name or validity.
	Pins []Pin
	// CAFile is a PEM bundle added to the system roots, relative to the
	// file's directory (the repository root, or the project directory for
	// the manifest) or absolute; read at send time.
	CAFile string
}

// Pin is one pinned certificate: the lower-case hex SHA-256 of its DER,
// and the origin it is accepted for. Origin is empty in a repository
// environment, which has one origin; a project environment has a base URL
// per member, so each of its pins names the origin it was pinned for.
type Pin struct {
	Origin string
	SHA256 string
}

// PinsFor returns the fingerprints pinned for origin.
func (t *TLSOptions) PinsFor(origin string) []string {
	var out []string
	if t == nil {
		return out
	}
	for _, p := range t.Pins {
		if p.Origin == "" || p.Origin == origin {
			out = append(out, p.SHA256)
		}
	}
	return out
}

// NormalizeOrigin validates s as an origin (scheme, host, optional port,
// no path) and returns it in BaseURL.Origin form.
func NormalizeOrigin(s string) (string, error) {
	b, err := ParseBaseURL(s)
	if err != nil {
		return "", err
	}
	if b.Path != "" {
		return "", fmt.Errorf("origin %q must not have a path", s)
	}
	return b.Origin(), nil
}

// checkPinOrigins refuses a pin naming an origin the environment does not
// have: a pin is only ever accepted for the origin it names.
func checkPinOrigins(item *yaml.Node, o EnvironmentOptions, origins ...string) error {
	if o.TLS == nil {
		return nil
	}
	for _, p := range o.TLS.Pins {
		if p.Origin == "" {
			continue
		}
		found := false
		for _, origin := range origins {
			found = found || origin == p.Origin
		}
		if !found {
			return errAt(item, "pin %s names origin %s, which is not a base URL origin of this environment", p.SHA256, p.Origin)
		}
	}
	return nil
}

// Limits override the defaults of one environment; zero means default.
type Limits struct {
	RequestBytes   int64
	ResponseBytes  int64
	TimeoutSeconds int
}

// Effective returns the limits with defaults filled in.
func (l Limits) Effective() Limits {
	if l.RequestBytes == 0 {
		l.RequestBytes = DefaultRequestBytes
	}
	if l.ResponseBytes == 0 {
		l.ResponseBytes = DefaultResponseBytes
	}
	if l.TimeoutSeconds == 0 {
		l.TimeoutSeconds = DefaultTimeoutSeconds
	}
	return l
}

// NormalizeFingerprint accepts a SHA-256 fingerprint as 64 hex digits, in
// any case, optionally with ':' separators and a "sha256:" prefix (as
// openssl and browsers show it), and returns 64 lower-case hex digits.
func NormalizeFingerprint(s string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.TrimPrefix(t, "sha256:")
	t = strings.ReplaceAll(t, ":", "")
	if len(t) != 64 {
		return "", fmt.Errorf("fingerprint %q is not a SHA-256 fingerprint (64 hex digits)", s)
	}
	for _, c := range t {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", fmt.Errorf("fingerprint %q is not a SHA-256 fingerprint (64 hex digits)", s)
		}
	}
	return t, nil
}

// CAFileProblem says why s is not a usable ca_file value, or "".
func CAFileProblem(s string) string {
	if strings.TrimSpace(s) == "" || len(s) > 4096 || strings.ContainsAny(s, "\x00\r\n") {
		return "ca_file must be a path of 1 to 4096 characters without control characters"
	}
	return ""
}

// optionKeys are the keys of an environment entry decodeOptions reads.
var optionKeys = []string{"tls", "use_system_proxy", "limits"}

// decodeOption reads one option key of an environment entry into o.
// In a project environment (project) every pin names its origin.
func decodeOption(o *EnvironmentOptions, k, v *yaml.Node, project bool) error {
	switch k.Value {
	case "tls":
		t, err := decodeTLS(v, project)
		if err != nil {
			return err
		}
		o.TLS = &t
	case "use_system_proxy":
		if v.Kind != yaml.ScalarNode || v.Tag != "!!bool" {
			return errAt(v, "use_system_proxy must be true or false")
		}
		o.UseSystemProxy = v.Value == "true"
	case "limits":
		l, err := decodeLimits(v)
		if err != nil {
			return err
		}
		o.Limits = l
	}
	return nil
}

func decodeTLS(n *yaml.Node, project bool) (TLSOptions, error) {
	var t TLSOptions
	fields, err := entries(n, "tls", "pinned_sha256", "ca_file")
	if err != nil {
		return t, err
	}
	for _, f := range fields {
		k, v := f[0], f[1]
		switch k.Value {
		case "pinned_sha256":
			if v.Kind != yaml.SequenceNode {
				return t, errAt(v, "tls.pinned_sha256 must be a list of SHA-256 fingerprints")
			}
			if len(v.Content) > MaxPins {
				return t, errAt(v, "tls.pinned_sha256 holds at most %d fingerprints", MaxPins)
			}
			seen := map[Pin]bool{}
			for _, item := range v.Content {
				var pin Pin
				var raw string
				if item.Kind == yaml.MappingNode {
					pf, err := entries(item, "pin", "origin", "sha256")
					if err != nil {
						return t, err
					}
					for _, f := range pf {
						s, err := scalarString(f[1], "pin "+f[0].Value)
						if err != nil {
							return t, err
						}
						if f[0].Value == "sha256" {
							raw = s
						} else if pin.Origin, err = NormalizeOrigin(s); err != nil {
							return t, errAt(f[1], "%v", err)
						}
					}
					if raw == "" || pin.Origin == "" {
						return t, errAt(item, "a pin with a mapping needs origin and sha256")
					}
				} else {
					s, err := scalarString(item, "tls.pinned_sha256 entry")
					if err != nil {
						return t, err
					}
					if project {
						return t, errAt(item, "a project environment has a base URL per member, so each pin names its origin: {origin: https://host:port, sha256: ...}")
					}
					raw = s
				}
				fp, err := NormalizeFingerprint(raw)
				if err != nil {
					return t, errAt(item, "%v", err)
				}
				pin.SHA256 = fp
				if seen[pin] {
					return t, errAt(item, "fingerprint %s is pinned twice", fp)
				}
				seen[pin] = true
				t.Pins = append(t.Pins, pin)
			}
		case "ca_file":
			s, err := scalarString(v, "tls.ca_file")
			if err != nil {
				return t, err
			}
			if p := CAFileProblem(s); p != "" {
				return t, errAt(v, "%s", p)
			}
			t.CAFile = s
		}
	}
	if len(t.Pins) == 0 && t.CAFile == "" {
		return t, errAt(n, "tls needs pinned_sha256 or ca_file")
	}
	return t, nil
}

func decodeLimits(n *yaml.Node) (Limits, error) {
	var l Limits
	fields, err := entries(n, "limits", "request_bytes", "response_bytes", "timeout_seconds")
	if err != nil {
		return l, err
	}
	for _, f := range fields {
		k, v := f[0], f[1]
		if v.Kind != yaml.ScalarNode || v.Tag != "!!int" {
			return l, errAt(v, "limits.%s must be a whole number", k.Value)
		}
		x, err := strconv.ParseInt(v.Value, 10, 64)
		max := map[string]int64{"request_bytes": MaxRequestBytes, "response_bytes": MaxResponseBytes, "timeout_seconds": MaxTimeoutSeconds}[k.Value]
		if err != nil || x < 1 || x > max {
			return l, errAt(v, "limits.%s must be 1 to %d", k.Value, max)
		}
		switch k.Value {
		case "request_bytes":
			l.RequestBytes = x
		case "response_bytes":
			l.ResponseBytes = x
		case "timeout_seconds":
			l.TimeoutSeconds = int(x)
		}
	}
	return l, nil
}

// optionNodes returns the YAML key and value nodes of o, in key order;
// empty options give none.
func optionNodes(o EnvironmentOptions) []*yaml.Node {
	var out []*yaml.Node
	if o.TLS != nil {
		t := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if len(o.TLS.Pins) > 0 {
			list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for _, p := range o.TLS.Pins {
				if p.Origin == "" {
					list.Content = append(list.Content, str(p.SHA256))
					continue
				}
				list.Content = append(list.Content, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map",
					Content: []*yaml.Node{str("origin"), str(p.Origin), str("sha256"), str(p.SHA256)}})
			}
			t.Content = append(t.Content, str("pinned_sha256"), list)
		}
		if o.TLS.CAFile != "" {
			t.Content = append(t.Content, str("ca_file"), str(o.TLS.CAFile))
		}
		out = append(out, str("tls"), t)
	}
	if o.UseSystemProxy {
		out = append(out, str("use_system_proxy"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	l := o.Limits
	if l != (Limits{}) {
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, kv := range []struct {
			k string
			v int64
		}{{"request_bytes", l.RequestBytes}, {"response_bytes", l.ResponseBytes}, {"timeout_seconds", int64(l.TimeoutSeconds)}} {
			if kv.v != 0 {
				m.Content = append(m.Content, str(kv.k), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(kv.v, 10)})
			}
		}
		out = append(out, str("limits"), m)
	}
	return out
}

// SetTryItEnvironmentOptions returns the file's text with the options of
// environment name (in tryit.environments of graphvein.yaml or of the
// project manifest) replaced by o: tls, use_system_proxy and limits are
// removed, then written again from o where set. The caller validates the
// result with Parse or ParseProject before writing it.
func SetTryItEnvironmentOptions(data []byte, name string, o EnvironmentOptions) ([]byte, error) {
	return editManifest(data, func(root *yaml.Node) error {
		section := mappingValue(root, "tryit")
		var list *yaml.Node
		if section != nil {
			list = mappingValue(section, "environments")
		}
		if list == nil || list.Kind != yaml.SequenceNode {
			return fmt.Errorf("the file has no tryit environments")
		}
		item := findByKey(list, "name", name)
		if item == nil || item.Kind != yaml.MappingNode {
			return fmt.Errorf("the file has no tryit environment %s", name)
		}
		var kept []*yaml.Node
		for i := 0; i+1 < len(item.Content); i += 2 {
			k := item.Content[i].Value
			if k == "tls" || k == "use_system_proxy" || k == "limits" {
				continue
			}
			kept = append(kept, item.Content[i], item.Content[i+1])
		}
		item.Content = append(kept, optionNodes(o)...)
		return nil
	})
}
