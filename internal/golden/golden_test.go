package golden

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureValue() any {
	return struct {
		Text  string `json:"text"`
		Paths []Path `json:"paths"`
		ID    uint64 `json:"id"`
	}{`literal\n`, []Path{`src\main.go`, "src/main.go"}, 9007199254740993}
}

func TestFixture(t *testing.T) {
	Check(t, "testdata/portable.golden.json", fixtureValue())
}

func TestCanonical(t *testing.T) {
	got, err := canonical(fixtureValue())
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/portable.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical output:\n%s", got)
	}
	if _, err := canonical(make(chan int)); err == nil {
		t.Fatal("unsupported value accepted")
	}
}

func TestFirstDiff(t *testing.T) {
	for _, tc := range []struct{ name, want, got, diff string }{
		{"nested", `{"nodes":[{"path":"a"}]}`, `{"nodes":[{"path":"b"}]}`, `$["nodes"][0]["path"]`},
		{"missing", `{"a":null}`, `{}`, `$["a"]`},
		{"extra", `{}`, `{"a":null}`, `$["a"]`},
		{"length", `[]`, `[1]`, `$.length`},
		{"type", `[]`, `{}`, `$`},
		{"ordered keys", `{"z":1,"a":1}`, `{"z":2,"a":2}`, `$["a"]`},
		{"large integer", `9007199254740993`, `9007199254740992`, `$`},
		{"escaped key", `{"a.b":1}`, `{"a.b":2}`, `$["a.b"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, err := decode([]byte(tc.want))
			if err != nil {
				t.Fatal(err)
			}
			g, err := decode([]byte(tc.got))
			if err != nil {
				t.Fatal(err)
			}
			diff := firstDiff("$", w, g)
			if !strings.HasPrefix(diff, tc.diff+"\n- expected:") || !strings.Contains(diff, "\n+ actual:") {
				t.Fatalf("diff = %q", diff)
			}
		})
	}
}

// Exercise the real testing failure and the actual -update flag in subprocesses,
// so a deliberate golden failure does not leave the outer suite red.
func TestHarnessSelf(t *testing.T) {
	dir := t.TempDir()
	target, other := filepath.Join(dir, "target.golden.json"), filepath.Join(dir, "other.golden.json")
	wrong := []byte("{\n  \"value\": \"wrong\"\n}\n")
	for _, path := range []string{target, other} {
		if err := os.WriteFile(path, wrong, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runChild := func(rewrite bool) (string, error) {
		t.Helper()
		args := []string{"-test.run=^TestHarnessProcess$"}
		if rewrite {
			args = append(args, "-update")
		}
		cmd := exec.Command(os.Args[0], args...)
		cmd.Env = append(os.Environ(), "GRAPHVEIN_GOLDEN_TARGET="+target)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := runChild(false)
	if err == nil || !strings.Contains(out, `$["value"]`) || !strings.Contains(out, `- expected: "wrong"`) {
		t.Fatalf("wrong golden did not fail readably: %v\n%s", err, out)
	}
	unchanged, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(unchanged, wrong) {
		t.Fatalf("failed comparison rewrote golden: %v", err)
	}
	if out, err := runChild(true); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "{\n  \"value\": \"correct\"\n}\n" {
		t.Fatalf("updated golden = %q, error %v", got, err)
	}
	got, err = os.ReadFile(other)
	if err != nil || !bytes.Equal(got, wrong) {
		t.Fatalf("untargeted golden changed: %v", err)
	}
	if out, err := runChild(false); err != nil {
		t.Fatalf("correct golden: %v\n%s", err, out)
	}
}

func TestHarnessProcess(t *testing.T) {
	path := os.Getenv("GRAPHVEIN_GOLDEN_TARGET")
	if path == "" {
		t.Skip("subprocess helper")
	}
	Check(t, path, map[string]string{"value": "correct"})
}

func TestReadErrorsAndFormatting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case.golden.json")
	if err := check(path, nil, false); err == nil {
		t.Fatal("missing golden accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing golden created automatically")
	}
	for _, contents := range []string{"not JSON", "null\r\n", "null", "null\nnull\n"} {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := check(path, nil, false); err == nil {
			t.Fatalf("accepted %q", contents)
		}
	}
	if err := check(filepath.Join(t.TempDir(), "not-a-golden.json"), nil, true); err == nil {
		t.Fatal("unsafe suffix accepted")
	}
	if err := check(filepath.Join(t.TempDir(), "missing", "case.golden.json"), json.Number("1"), true); err == nil {
		t.Fatal("write error ignored")
	}
}

// TestReplaceRoot covers the {abs_url_path} and {abs_path} link forms for a
// POSIX and a Windows root: all normalise to "/{ROOT}", except the doubled
// slash of an {abs_path} VS Code template on POSIX, which stays visible.
func TestReplaceRoot(t *testing.T) {
	for _, tc := range []struct{ root, link, want string }{
		{"/tmp/r", "vscode://file/tmp/r/a.ts:1:1", "vscode://file/{ROOT}/a.ts:1:1"},
		{"/tmp/r", "zed://file/tmp/r/a.ts:1:1", "zed://file/{ROOT}/a.ts:1:1"},
		{"/tmp/r", "idea://open?file=/tmp/r/a.ts&line=1", "idea://open?file=/{ROOT}/a.ts&line=1"},
		{"/tmp/r", "vscode://file//tmp/r/a.ts:1:1", "vscode://file//{ROOT}/a.ts:1:1"},
		{"D:/r", "vscode://file/D:/r/a.ts:1:1", "vscode://file/{ROOT}/a.ts:1:1"},
		{"D:/r", "zed://fileD:/r/a.ts:1:1", "zed://file/{ROOT}/a.ts:1:1"},
		{"D:/r", "idea://open?file=D:/r/a.ts&line=1", "idea://open?file=/{ROOT}/a.ts&line=1"},
	} {
		if got := ReplaceRoot(tc.link, tc.root); got != tc.want {
			t.Errorf("ReplaceRoot(%q, %q) = %q, want %q", tc.link, tc.root, got, tc.want)
		}
	}
	if got := encodeRoot("D:/my dir"); got != "D:/my%20dir" {
		t.Errorf("encodeRoot = %q", got)
	}
}
