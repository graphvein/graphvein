package server

import (
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// applyUnified applies a diff made by unifiedDiff to old, so the test can
// check that the shown diff is exactly the change written.
func applyUnified(t *testing.T, old, diff string) string {
	t.Helper()
	a := splitLines(old)
	var out []string
	pos := 0
	lines := splitLines(diff)
	for i := 2; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "@@ -") {
			r := strings.Fields(l)[1][1:]
			start, _ := strconv.Atoi(strings.Split(r, ",")[0])
			if !strings.Contains(r, ",") || !strings.HasSuffix(r, ",0") {
				start--
			}
			out = append(out, a[pos:start]...)
			pos = start
			continue
		}
		noEOL := i+1 < len(lines) && strings.HasPrefix(lines[i+1], `\ No newline`)
		text := l[1:]
		if noEOL {
			text = strings.TrimSuffix(text, "\n")
		}
		switch l[0] {
		case ' ':
			if a[pos] != text {
				t.Fatalf("context %q does not match %q", text, a[pos])
			}
			out = append(out, text)
			pos++
		case '-':
			if a[pos] != text {
				t.Fatalf("removed %q does not match %q", text, a[pos])
			}
			pos++
		case '+':
			out = append(out, text)
		case '\\':
		}
	}
	out = append(out, a[pos:]...)
	return strings.Join(out, "")
}

// TestUnifiedDiff checks the diff shown before a manifest edit: the known
// shape of a small edit, agreement with diff(1) when it is installed, and
// that applying it gives the new text on random edits.
func TestUnifiedDiff(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nm\nn\n"
	new := "a\nb\nc\nd\nE\nf\ng\nh\ni\nj\nk\nm\nn\nl\n"
	want := "--- a/m.yaml\n+++ b/m.yaml\n@@ -2,7 +2,7 @@\n b\n c\n d\n-e\n+E\n f\n g\n h\n@@ -11,3 +11,4 @@\n k\n m\n n\n+l\n"
	if got := unifiedDiff("m.yaml", old, new); got != want {
		t.Errorf("diff:\n%s\nwant\n%s", got, want)
	}
	if unifiedDiff("m.yaml", old, old) != "" {
		t.Error("equal texts give a diff")
	}
	if diff, err := exec.LookPath("diff"); err == nil {
		dir := t.TempDir()
		for i, pair := range [][2]string{{old, new}, {"a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\n", "a\nb\nc\nd\nE\nf\ng\nh\ni\nj\nk\nl\n"}, {"x\n", "x"}, {"", "a\nb\n"}, {"a\nb\nc\n", "c\nb\na\n"}} {
			pa, pb := filepath.Join(dir, "a"), filepath.Join(dir, "b")
			os.WriteFile(pa, []byte(pair[0]), 0o644)
			os.WriteFile(pb, []byte(pair[1]), 0o644)
			cmd := exec.Command(diff, "-u", "--label", "a/m.yaml", "--label", "b/m.yaml", pa, pb)
			ref, _ := cmd.Output()
			if got := unifiedDiff("m.yaml", pair[0], pair[1]); got != string(ref) {
				t.Errorf("case %d differs from diff -u:\n%s\nwant\n%s", i, got, ref)
			}
		}
	}
	r := rand.New(rand.NewPCG(1, 2))
	for n := 0; n < 300; n++ {
		var a []string
		for range r.IntN(30) {
			a = append(a, string(rune('a'+r.IntN(5)))+"\n")
		}
		b := append([]string{}, a...)
		for range r.IntN(5) {
			at := r.IntN(len(b) + 1)
			switch r.IntN(2) {
			case 0:
				b = append(b[:at], append([]string{"new\n"}, b[at:]...)...)
			default:
				if at < len(b) {
					b = append(b[:at], b[at+1:]...)
				}
			}
		}
		oldText, newText := strings.Join(a, ""), strings.Join(b, "")
		if r.IntN(4) == 0 {
			newText = strings.TrimSuffix(newText, "\n")
		}
		d := unifiedDiff("m", oldText, newText)
		if oldText == newText {
			continue
		}
		if got := applyUnified(t, oldText, d); got != newText {
			t.Fatalf("applying\n%s\nto %q gives %q, want %q", d, oldText, got, newText)
		}
	}
}
