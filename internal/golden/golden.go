// Package golden keeps fixture expectations explicit and portable across hosts.
package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite only golden files reached by the selected tests")

// Path marks filesystem paths so snapshots use forward slashes without changing
// ordinary strings (such as source snippets containing backslashes).
type Path string

// MarshalJSON makes even Windows paths portable when tests run on another OS.
func (p Path) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.ReplaceAll(string(p), `\`, "/"))
}

// Check fails the calling test with the first differing JSON path. Only an
// explicit -update flag permits writing the supplied golden file; use -run to
// select the fixture tests whose expectations should change.
func Check(t testing.TB, path string, value any) {
	t.Helper()
	if err := check(path, value, *update); err != nil {
		t.Errorf("%v", err)
	}
}

// CheckRoot normalizes only IDE link roots in test comparisons and goldens.
// Production responses retain their actual absolute checkout paths.
func CheckRoot(t testing.TB, path string, value any, root string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	v, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded := encodeRoot(root)
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			if ide, ok := x["ide"].(map[string]any); ok {
				if link, ok := ide["link"].(string); ok {
					ide["link"] = ReplaceRoot(link, encoded)
				}
			}
			// shape_links (and a link's call_links and route_links of its
			// shape_differences, and the links of a schema table's
			// model_routes) map "file:line" to an IDE link of the same form.
			linkMaps := []any{x["shape_links"], x["call_links"], x["route_links"]}
			if mr, ok := x["model_routes"].(map[string]any); ok {
				linkMaps = append(linkMaps, mr["links"])
			}
			for _, m := range linkMaps {
				links, ok := m.(map[string]any)
				if !ok {
					continue
				}
				for _, l := range links {
					if ide, ok := l.(map[string]any); ok {
						if link, ok := ide["link"].(string); ok {
							ide["link"] = ReplaceRoot(link, encoded)
						}
					}
				}
			}
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(v)
	Check(t, path, v)
}

// encodeRoot percent-encodes root the way IDE links encode paths, with
// forward slashes.
func encodeRoot(root string) string {
	var encoded strings.Builder
	for _, c := range []byte(filepath.ToSlash(root)) {
		if c == '/' || c == ':' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", rune(c)) {
			encoded.WriteByte(c)
		} else {
			fmt.Fprintf(&encoded, "%%%02X", c)
		}
	}
	return encoded.String()
}

// ReplaceRoot replaces the forward-slash root in s with {ROOT}. {ROOT}
// stands for the root without its leading slash and is always written
// after a slash: "/" + root-without-leading-slash becomes "/{ROOT}" (POSIX
// /tmp/r and Windows /D:/r, the {abs_url_path} forms), then on a root
// without a leading slash (Windows) the remaining plain D:/r (the
// {abs_path} form) becomes "/{ROOT}" too. So vscode://file/{ROOT}/a.ts,
// zed://file/{ROOT}/a.ts and file=/{ROOT}/a.ts read the same on every OS,
// while a doubled slash (vscode://file//tmp/r on POSIX) stays visible.
func ReplaceRoot(s, root string) string {
	bare := strings.TrimPrefix(root, "/")
	s = strings.ReplaceAll(s, "/"+bare, "/{ROOT}")
	if !strings.HasPrefix(root, "/") {
		s = strings.ReplaceAll(s, root, "/{ROOT}")
	}
	return s
}

func canonical(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("serialising golden value: %w", err)
	}
	// Round-trip structs too: encoding/json sorts map keys, but struct fields
	// otherwise retain declaration order. UseNumber avoids rounding large IDs.
	v, err := decode(raw)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("formatting golden value: %w", err)
	}
	return append(data, '\n'), nil
}

func decode(data []byte) (any, error) {
	if !json.Valid(data) {
		return nil, fmt.Errorf("decoding golden JSON: invalid JSON document")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decoding golden JSON: %w", err)
	}
	return value, nil
}

func check(path string, value any, rewrite bool) error {
	if !strings.HasSuffix(path, ".golden.json") {
		return fmt.Errorf("golden path %q must end in .golden.json", path)
	}
	got, err := canonical(value)
	if err != nil {
		return fmt.Errorf("golden %s: %w", path, err)
	}
	if rewrite {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			return fmt.Errorf("writing golden %s: %w", path, err)
		}
		return nil
	}
	want, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading golden %s: %w (create intentionally with -update)", path, err)
	}
	if bytes.Equal(want, got) {
		return nil
	}
	wantValue, err := decode(want)
	if err != nil {
		return fmt.Errorf("golden %s: %w", path, err)
	}
	gotValue, err := decode(got)
	if err != nil {
		return err
	}
	if diff := firstDiff("$", wantValue, gotValue); diff != "" {
		return fmt.Errorf("golden %s differs at %s", path, diff)
	}
	return fmt.Errorf("golden %s differs at $ (formatting): expected sorted keys, two-space indentation and LF with a final newline; rewrite intentionally with -update", path)
}

func firstDiff(path string, want, got any) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	if wm, ok := want.(map[string]any); ok {
		if gm, ok := got.(map[string]any); ok {
			keys := make(map[string]bool)
			for k := range wm {
				keys[k] = true
			}
			for k := range gm {
				keys[k] = true
			}
			ordered := make([]string, 0, len(keys))
			for k := range keys {
				ordered = append(ordered, k)
			}
			sort.Strings(ordered)
			for _, k := range ordered {
				child := path + "[" + quoted(k) + "]"
				w, wok := wm[k]
				g, gok := gm[k]
				if !wok {
					return child + "\n- expected: <missing>\n+ actual: " + quoted(g)
				}
				if !gok {
					return child + "\n- expected: " + quoted(w) + "\n+ actual: <missing>"
				}
				if diff := firstDiff(child, w, g); diff != "" {
					return diff
				}
			}
		}
	}
	if wa, ok := want.([]any); ok {
		if ga, ok := got.([]any); ok {
			for i := 0; i < len(wa) && i < len(ga); i++ {
				if diff := firstDiff(fmt.Sprintf("%s[%d]", path, i), wa[i], ga[i]); diff != "" {
					return diff
				}
			}
			if len(wa) != len(ga) {
				return fmt.Sprintf("%s.length\n- expected: %d\n+ actual: %d", path, len(wa), len(ga))
			}
		}
	}
	return path + "\n- expected: " + quoted(want) + "\n+ actual: " + quoted(got)
}

func quoted(value any) string {
	data, _ := json.Marshal(value) // Values here have already been decoded as JSON.
	return string(data)
}
