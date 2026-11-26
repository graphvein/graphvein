package contract

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

func TestContractFixtures(t *testing.T) {
	paths, err := filepath.Glob("../../testdata/contract/*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 5 {
		t.Fatal("missing required contract fixtures")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			report := struct {
				Records     []string     `json:"records"`
				Diagnostics []Diagnostic `json:"diagnostics"`
				Error       string       `json:"error"`
			}{Records: []string{}, Diagnostics: []Diagnostic{}}
			reader := NewReader(file)
			for {
				event, err := reader.Next()
				if err != nil {
					if err != io.EOF {
						report.Error = err.Error()
					}
					break
				}
				report.Diagnostics = append(report.Diagnostics, event.Diagnostics...)
				if event.Record != nil {
					data, _ := json.Marshal(event.Record)
					var typ struct {
						Type string `json:"type"`
					}
					_ = json.Unmarshal(data, &typ)
					report.Records = append(report.Records, typ.Type)
				}
			}
			golden.Check(t, strings.TrimSuffix(path, ".ndjson")+".golden.json", report)
		})
	}
}

func TestDuplicateKeysAndUnknownRecords(t *testing.T) {
	for _, data := range []string{`{"type":"file","type":"node"}`, `{"type":"header","indexer":{"name":"a","name":"b"}}`} {
		if _, _, err := Decode([]byte(data)); err == nil || !strings.Contains(err.Error(), "duplicate key") {
			t.Fatalf("duplicate JSON accepted: %v", err)
		}
	}
	header := strings.Split(string(mustReadFixture(t, "valid")), "\n")[0]
	data := header + "\n" + `{"type":"future","x":1}` + "\n" + `{"type":"footer","counts":{"records":1,"file":0,"node":0,"edge":0,"diagnostic":0}}` + "\n"
	reader := NewReader(strings.NewReader(data))
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	event, err := reader.Next()
	if err != nil || event.Record != nil || len(event.Diagnostics) != 1 {
		t.Fatal("unknown record not diagnosed", err)
	}
	if _, err := reader.Next(); err != nil {
		t.Fatal("unknown record was not counted", err)
	}
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/contract/" + name + ".ndjson")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func TestWriterIOFailure(t *testing.T) {
	record, _, err := Decode(bytes.Split(mustReadFixture(t, "valid"), []byte("\n"))[0])
	if err != nil {
		t.Fatal(err)
	}
	writer := NewWriter(shortWriter{})
	if err := writer.Write(record); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write = %v", err)
	}
	if err := writer.Close(); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("footer after failure = %v", err)
	}
}

func TestSchemasRejectBadFixtureRecords(t *testing.T) {
	schemas, err := loadSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		line int
		typ  string
	}{{"unknown-version", 1, "header"}, {"invalid-identity", 3, "node"}} {
		line := bytes.Split(mustReadFixture(t, tc.name), []byte("\n"))[tc.line-1]
		var value any
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if err := schemas[tc.typ].Validate(value); err == nil {
			t.Fatalf("published schema accepted %s line %d", tc.name, tc.line)
		}
	}
}

func TestSchemasAndWriterRoundTrip(t *testing.T) {
	schemas, err := loadSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"valid", "diagnostic", "version-b", "unknown-fields"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("../../testdata/contract/" + name + ".ndjson")
			if err != nil {
				t.Fatal(err)
			}
			scan := bufio.NewScanner(bytes.NewReader(data))
			var output bytes.Buffer
			writer := NewWriter(&output)
			records := []any{}
			for scan.Scan() {
				var object map[string]any
				decoder := json.NewDecoder(bytes.NewReader(scan.Bytes()))
				decoder.UseNumber()
				if err := decoder.Decode(&object); err != nil {
					t.Fatal(err)
				}
				typ := object["type"].(string)
				if err := schemas[typ].Validate(object); err != nil {
					t.Fatalf("published %s schema: %v", typ, err)
				}
				record, _, err := Decode(scan.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				records = append(records, record)
				if typ != "footer" {
					if err := writer.Write(record); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := scan.Err(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			reader := NewReader(&output)
			for _, want := range records {
				event, err := reader.Next()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(want, event.Record) {
					t.Fatalf("round trip differs: %#v != %#v", want, event.Record)
				}
			}
			if _, err := reader.Next(); err != io.EOF {
				t.Fatalf("EOF = %v", err)
			}
		})
	}
}

func TestIdentity(t *testing.T) {
	cases := []struct{ kind, file, name, want string }{
		{"method", "src/user.ts", "User.save", "method:src/user.ts#User.save"},
		{"method", "src/user.rs", "User.save", "method:src/user.rs#User.save"},
		{"function", "src/jobs.py", "run.validate", "function:src/jobs.py#run.validate"},
		{"route", "src/api.ts", "GET /users/:id", "route:src/api.ts#GET%20%2Fusers%2F%3Aid"},
		{"table", "migrations/001.sql", "public.users", "table:migrations/001.sql#public.users"},
		{"method", "src/my #file.ts", "C.méthod:1", "method:src/my%20%23file.ts#C.m%C3%A9thod%3A1"},
		{"external", ".", "npm:react", "external:.#npm%3Areact"},
		{"function", "src/100%.ts", "#%", "function:src/100%25.ts#%23%25"},
	}
	for _, tc := range cases {
		id, err := Identity(tc.kind, tc.file, tc.name)
		if err != nil || id != tc.want {
			t.Fatalf("Identity = %q, %v; want %q", id, err, tc.want)
		}
		k, p, q, err := ParseIdentity(id)
		if err != nil || k != tc.kind || p != tc.file || q != tc.name {
			t.Fatal("identity does not round trip")
		}
	}
	for _, bad := range []string{"function:../x#f", "function:/x#f", "function:C%3A/x#f", "function:x%5Cy#f", "function:x#", "function:x#f#z", "function:x#%66", "function:x#%c3%a9", "function:x#%FF", "function:x#%00", "function:x%2Fy#f", "Function:x#f"} {
		if _, _, _, err := ParseIdentity(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, bad := range []string{"../x", "x/../y", "./x", "x//y", "x/", "C:/x", "/x", `x\y`, "."} {
		if ValidPath(bad, false) {
			t.Errorf("accepted path %q", bad)
		}
	}
	composed, _ := Identity("function", "é.ts", "f")
	decomposed, _ := Identity("function", "e\u0301.ts", "f")
	if composed == decomposed {
		t.Fatal("Unicode spelling was folded")
	}
	upper, _ := Identity("function", "X.ts", "f")
	lower, _ := Identity("function", "x.ts", "f")
	if upper == lower {
		t.Fatal("path case was folded")
	}
}

func TestContentNormalization(t *testing.T) {
	want := "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := ContentHash([]byte("abc")); got != want {
		t.Fatal(got)
	}
	for _, source := range []string{"a\nb\n", "a\r\nb\r\n", "a\rb\r"} {
		if ContentHash([]byte(source)) != ContentHash([]byte("a\nb\n")) {
			t.Error("line ending hashes differ")
		}
	}
	for _, changed := range []string{" a\nb\n", "a \nb\n", "a\nb", "a\nc\n", "\ufeffa\nb\n"} {
		if ContentHash([]byte(changed)) == ContentHash([]byte("a\nb\n")) {
			t.Errorf("lost significant bytes: %q", changed)
		}
	}
	if ContentHash([]byte(`"a b"`)) == ContentHash([]byte(`"ab"`)) {
		t.Fatal("literal whitespace lost")
	}
}

func TestSyntheticNormalization(t *testing.T) {
	empty, err := SyntheticHash(map[string]any{})
	if err != nil || empty != "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
		t.Fatal(empty, err)
	}
	attrs := map[string]any{"z": []any{true, nil}, "é": "<&\u2028\n", "a": json.Number("9007199254740991")}
	var canonical bytes.Buffer
	if err := canonicalJSON(&canonical, attrs); err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":9007199254740991,\"z\":[true,null],\"é\":\"<&\u2028\\n\"}"
	if canonical.String() != want {
		t.Fatalf("canonical = %q", canonical.String())
	}
	hash, err := SyntheticHash(attrs)
	if err != nil || hash != ContentHash([]byte(want)) {
		t.Fatal(hash, err)
	}
	for _, n := range []any{1.2, json.Number("9007199254740992"), json.Number("-9007199254740992")} {
		if _, err := SyntheticHash(map[string]any{"n": n}); err == nil {
			t.Errorf("accepted synthetic number %v", n)
		}
	}
}

func TestFramingAndValidation(t *testing.T) {
	data, err := os.ReadFile("../../testdata/contract/valid.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"CRLF":             strings.ReplaceAll(string(data), "\n", "\r\n"),
		"no final LF":      strings.TrimSuffix(string(data), "\n"),
		"after footer":     string(data) + "{}\n",
		"counts":           strings.Replace(string(data), `"records":7`, `"records":8`, 1),
		"duplicate header": strings.Split(string(data), "\n")[0] + "\n" + string(data),
		"no header":        "{}\n",
		"overlong":         strings.Repeat("x", MaxRecordBytes+1) + "\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			reader := NewReader(strings.NewReader(data))
			for {
				_, err := reader.Next()
				if err != nil {
					if err == io.EOF {
						t.Fatal("accepted invalid framing")
					}
					break
				}
			}
		})
	}
	node := Node{Type: "node", Identity: "function:x#f", Kind: "function", Level: 4, Name: "f", QualifiedName: "f", File: "x", Span: &Span{1, 0, 1, 1}, ContentHash: ContentHash(nil), Attributes: map[string]any{}}
	for _, mutate := range []func(*Node){func(n *Node) { n.Level = 6 }, func(n *Node) { n.ContentHash = "abc" }, func(n *Node) { n.QualifiedName = "different" }, func(n *Node) { n.Span = &Span{2, 0, 1, 0} }, func(n *Node) { n.Span = nil }, func(n *Node) { n.Attributes = nil }} {
		copy := node
		mutate(&copy)
		if err := Validate(copy); err == nil {
			t.Errorf("accepted invalid node %#v", copy)
		}
	}
	edge := Edge{Type: "edge", Kind: "calls", Source: node.Identity, Target: node.Identity, Provenance: "fact", Confidence: .5, Evidence: []Evidence{}}
	if err := Validate(edge); err == nil {
		t.Fatal("fact confidence below 1 accepted")
	}
	var output bytes.Buffer
	writer := NewWriter(&output)
	if err := writer.Write(node); err == nil {
		t.Fatal("writer accepted node before header")
	}
	if err := writer.Close(); err == nil {
		t.Fatal("writer closed without header")
	}
}
