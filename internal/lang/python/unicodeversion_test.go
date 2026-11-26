package python

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/graphvein/graphvein/internal/parse"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/unicode/runenames"
)

// TestPythonUnicodeVersion checks the generated lists of unicodeversion.go
// (row PS-F2, form unicode_version): that they were generated for the
// Unicode version of the tables in use (golang.org/x/text's NFKC and rune
// names, Go's unicode package), that they are sorted, disjoint ranges of
// the stated size, that an identifier holding a listed character and a
// `\N{...}` name of one are refused, and that the grammar reports an error
// for every listed identifier character today (so tree_sitter_error comes
// first). With an interpreter (GRAPHVEIN_PYTHON_ORACLE, else python3) it
// compares Go's tables with the interpreter's unicodedata code point by code
// point, as read by scripts/oracle/python_structure/unicode_tables.py dump:
// every code point when the interpreter's Unicode version is unicodeTables,
// every code point outside the lists when it is unicodeReference (the
// reference Python); it skips that part for another version.
func TestPythonUnicodeVersion(t *testing.T) {
	for name, version := range map[string]string{"x/text norm": norm.Version, "x/text runenames": runenames.UnicodeVersion, "Go unicode": unicode.Version} {
		if version != unicodeTables {
			t.Errorf("%s is Unicode %s; unicodeversion.go was generated for %s: regenerate it (command in unicode_tables.py)", name, version, unicodeTables)
		}
	}
	for name, list := range map[string]struct {
		ranges [][2]rune
		count  int
	}{"identifierChanged": {identifierChanged, identifierChangedCount}, "nameChanged": {nameChanged, nameChangedCount}} {
		total := 0
		for i, r := range list.ranges {
			if r[0] > r[1] || i > 0 && r[0] <= list.ranges[i-1][1]+1 {
				t.Errorf("%s: range %d %X-%X is not sorted and disjoint", name, i, r[0], r[1])
			}
			total += int(r[1]-r[0]) + 1
		}
		if total != list.count {
			t.Errorf("%s holds %d code points, want %d", name, total, list.count)
		}
	}

	// The identifier branch: a listed character refuses, ASCII and an
	// unlisted character do not.
	for _, r := range identifierChanged {
		for _, c := range []rune{r[0], r[1]} {
			if got := nodeRejection(&parse.Node{Kind: "identifier", Text: "a" + string(c)}); got != rejectUnicodeVersion {
				t.Errorf("identifier with U+%04X: rejection %q, want %q", c, got, rejectUnicodeVersion)
			}
		}
	}
	for _, text := range []string{"abc", "café", "ʕ"} {
		if got := nodeRejection(&parse.Node{Kind: "identifier", Text: text}); got != "" {
			t.Errorf("identifier %q: rejection %q, want none", text, got)
		}
	}

	// The grammar does not read a listed character as part of an
	// identifier (they are new characters, not XID in the reference
	// version): `a<c> = 1` has a syntax error for every one.
	var jobs []parse.Job
	var runes []rune
	for _, r := range identifierChanged {
		for c := r[0]; c <= r[1]; c++ {
			runes = append(runes, c)
			jobs = append(jobs, parse.Job{Language: "python", Source: []byte("a" + string(c) + " = 1\n")})
		}
	}
	err := parse.Consume(context.Background(), jobs, parse.Options{}, func(i int, res parse.Result) {
		if res.Err != nil {
			t.Errorf("U+%04X: %v", runes[i], res.Err)
			return
		}
		if rejection(res.Root, jobs[i].Source) == "" && !res.SyntaxError {
			t.Errorf("U+%04X: the grammar reads `a<U+%04X> = 1` without an error", runes[i], runes[i])
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	interpreter := os.Getenv("GRAPHVEIN_PYTHON_ORACLE")
	if interpreter == "" {
		found, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("no Python interpreter on PATH")
		}
		interpreter = found
	}
	out, err := exec.Command(interpreter, "-I", "../../../scripts/oracle/python_structure/unicode_tables.py", "dump").Output()
	if err != nil {
		t.Skipf("%s does not run the table script: %v", interpreter, err)
	}
	compareUnicodeDump(t, interpreter, out)
}

// compareUnicodeDump compares a unicode_tables.py dump with Go's tables:
// the NFKC form of each code point, its letter, upper-case letter or
// decimal digit class (what the extractor reads through unicode.IsLetter,
// IsUpper and IsDigit), and every name runenames holds that unicodeName
// can confirm.
func compareUnicodeDump(t *testing.T, interpreter string, out []byte) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	if !sc.Scan() {
		t.Fatal("empty dump")
	}
	header := strings.Fields(strings.TrimPrefix(sc.Text(), "#"))
	if len(header) != 2 {
		t.Fatalf("dump header %q", sc.Text())
	}
	var all bool
	switch header[0] {
	case unicodeTables:
		all = true
	case unicodeReference:
	default:
		t.Skipf("%s (Python %s) has Unicode %s; the lists compare %s with %s", interpreter, header[1], header[0], unicodeReference, unicodeTables)
	}
	compared, mismatches := 0, 0
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) != 7 {
			t.Fatalf("dump line %q", sc.Text())
		}
		cp, err := strconv.ParseUint(f[0], 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		r := rune(cp)
		if r >= 0xD800 && r <= 0xDFFF {
			continue // surrogates are not UTF-8 text
		}
		compared++
		report := func(format string, args ...any) {
			mismatches++
			if mismatches <= 20 {
				t.Errorf("U+%04X: %s", r, fmt.Sprintf(format, args...))
			}
		}
		if all || !runeInRanges(r, identifierChanged) {
			var nfkc []string
			for _, c := range norm.NFKC.String(string(r)) {
				nfkc = append(nfkc, fmt.Sprintf("%X", c))
			}
			if got := strings.Join(nfkc, " "); got != f[5] {
				report("NFKC %s, %s has %s", got, interpreter, f[5])
			}
			if got, want := goCategoryClass(r), pythonCategoryClass(f[3]); got != want {
				report("class %s, %s has %s (category %s)", got, interpreter, want, f[3])
			}
		}
		if all || !runeInRanges(r, nameChanged) {
			if n := runenames.Name(r); n != "" && !strings.HasPrefix(n, "<") && n != f[6] {
				report("name %q, %s has %q", n, interpreter, f[6])
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if mismatches > 0 {
		t.Errorf("%d differences between Go's tables and %s (Unicode %s)", mismatches, interpreter, header[0])
	}
	t.Logf("compared %d code points with %s (Python %s, Unicode %s, all=%v)", compared, interpreter, header[1], header[0], all)
}

func goCategoryClass(r rune) string {
	switch {
	case unicode.IsUpper(r):
		return "Lu"
	case unicode.IsLetter(r):
		return "L"
	case unicode.IsDigit(r):
		return "Nd"
	}
	return "-"
}

func pythonCategoryClass(category string) string {
	switch {
	case category == "Lu" || category == "Nd":
		return category
	case strings.HasPrefix(category, "L"):
		return "L"
	}
	return "-"
}
