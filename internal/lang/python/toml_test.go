package python

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// tomlCases are documents the reader must read as Python's tomllib does,
// or refuse where tomllib refuses (the last group).
var tomlCases = []string{
	"[project]\nname = \"lfx\" # comment\n",
	"[tool.uv.workspace]\nmembers = [\n    \"src/a\", # one\n    './b',\n]\n",
	"[tool.uv.sources]\nx = { path = \"../x\", editable = true }\ny = { workspace = true }\n",
	"a.b.c = 1\na.b.d = 'x'\n[e]\nf = 2026-11-18\ng = 1979-05-27 07:32:00Z\nh = [1, 2.5, -3, +inf]\n",
	"[[x]]\nn = 1\n[[x]]\nn = 2\n[x.y]\nz = true\n",
	"s = \"\"\"\nline \\\n   joined \"quote\" \"\"\"\"\nt = '''raw \\n'''\nu = \"\\u00e9\\t\"\n",
	"\"quoted key\".'lit' = false\n[ spaced . header ]\nk=[]\n",
	// Refused by tomllib too.
	"[a]\n[a]\n",
	"a = 1\na = 2\n",
	"a = { b = 1 }\n[a]\n",
	"a = \"x\" b\n",
	"a = \"\\q\"\n",
	"a = [1\n",
	"[a.b]\nc = 1\n[a]\nb.d = 2\n",
	// Scalars and line ends: valid ones are read, every
	// form tomllib refuses is refused.
	"a = 0b101\nb = 1_000\nc = 6.02e+23\nd = -inf\ne = 07:32:00.5\n",
	"a = 0xff\nb = 0o17\nc = 1979-05-27T07:32:00-08:00\nd = 1979-05-27t07:32:00z\ne = +nan\n",
	"a = abc\n",
	"c = truex\n",
	"a = TRUE\n",
	"a = 01\n",
	"a = 1__2\n",
	"a = 0x_1\n",
	"a = 1979-05-27 07:32\n",
	"a = 1\rb = 2\n",
	"a = 1979-13-01\n",
	"a = 1979-02-30\n",
	"a = 24:00:00\n",
	"a = 1e\n",
	"a = 0o8\n",
	"# comment \x01\na = 1\n",
	"a = [1,\r 2]\n",
	"a = 1 # ok\r\nb = 2\r\n",
	// A CR not followed by LF is refused everywhere, also in strings
	// (confirmed in review); CRLF in a multi-line string reads
	// as LF.
	"a = 1\rb=2",
	"a = 1\rb=2\n",
	"a = 1\r",
	"a = \"x\ry\"\n",
	"a = 'x\ry'\n",
	"a = \"\"\"x\ry\"\"\"\n",
	"a = '''x\ry'''\n",
	"a = \"\"\"x\r\ny\"\"\"\n",
	"a = '''x\r\ny'''\n",
	"a = \"\"\"x \\\r\n  y\"\"\"\n",
	"[t]\rb = 1\n",
	"a = { b = 1 }\r",
	"\r\na = 1\r\n",
	"\ra = 1\n",
	"a = \"\"\"x \\\r  y\"\"\"\n",
	"a = \"\"\"x \\\n \r y\"\"\"\n",
	"a = \"\"\"\rx\"\"\"\n",
	"a = '''\r\nx'''\n",
}

// TestTOMLReader checks parseTOML against Python's tomllib (the oracle
// interpreter GRAPHVEIN_PYTHON_ORACLE, else python3) on tomlCases, the
// fixtures' pyproject.toml files and, opt in, every pyproject.toml under
// GRAPHVEIN_PYTHON_TOML_CORPUS. Numbers and dates are compared as present
// (both sides map them to one marker); everything else exactly.
func TestTOMLReader(t *testing.T) {
	interpreter := os.Getenv("GRAPHVEIN_PYTHON_ORACLE")
	if interpreter == "" {
		found, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("no Python interpreter for tomllib")
		}
		interpreter = found
	}
	if err := exec.Command(interpreter, "-I", "-c", "import tomllib").Run(); err != nil {
		t.Skipf("%s has no tomllib: %v", interpreter, err)
	}
	docs := map[string]string{}
	for i, c := range tomlCases {
		docs[fmt.Sprintf("case/%02d", i)] = c
	}
	roots := []string{"../../../testdata"}
	if dir := os.Getenv("GRAPHVEIN_PYTHON_TOML_CORPUS"); dir != "" {
		roots = append(roots, dir)
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if !d.IsDir() && d.Name() == "pyproject.toml" {
				data, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				docs[p] = string(data)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	input, err := json.Marshal(docs)
	if err != nil {
		t.Fatal(err)
	}
	script := `
import datetime, json, sys, tomllib
def norm(v):
    if isinstance(v, bool) or isinstance(v, str):
        return v
    if isinstance(v, dict):
        return {k: norm(x) for k, x in v.items()}
    if isinstance(v, list):
        return [norm(x) for x in v]
    return "\x00scalar"
out = {}
for name, text in json.load(sys.stdin).items():
    try:
        out[name] = norm(tomllib.loads(text))
    except tomllib.TOMLDecodeError:
        out[name] = None
json.dump(out, sys.stdout)
`
	cmd := exec.Command(interpreter, "-I", "-c", script)
	cmd.Stdin = strings.NewReader(string(input))
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("tomllib: %v", err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for name, text := range docs {
		got, err := parseTOML(text)
		if want[name] == nil {
			if err == nil {
				t.Errorf("%s: tomllib refuses it, parseTOML reads it", name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: tomllib reads it, parseTOML: %v", name, err)
			continue
		}
		if g := tomlJSON(got); !reflect.DeepEqual(g, want[name]) {
			t.Errorf("%s: parseTOML %v, tomllib %v", name, g, want[name])
		}
	}
	t.Logf("%d documents compared with tomllib", len(docs))
}

// tomlJSON maps parseTOML's values to the shapes encoding/json gives the
// oracle's output.
func tomlJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = tomlJSON(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = tomlJSON(e)
		}
		return out
	case tomlScalar:
		return "\x00scalar"
	}
	return v
}
