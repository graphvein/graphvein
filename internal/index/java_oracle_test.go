package index_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/parse"
)

// TestJavaDeclsOracle is the Graphvein side of the javac declaration oracle
// (scripts/oracle/java_decls.sh; docs/lang/best-effort.md "Oracle"). It runs
// only when GRAPHVEIN_JAVA_ORACLE_PHASE is set, never in CI.
//
//   - phase "index" indexes GRAPHVEIN_JAVA_ORACLE_ROOT and writes, under
//     GRAPHVEIN_JAVA_ORACLE_SCRATCH, files.txt (every scanned .java file),
//     refused.txt (the BE-2 files) and graphvein.tsv (every emitted class,
//     interface, enum, record, annotation, method and constructor with its
//     owner path and span, in the helper's format).
//   - phase "compare" compares graphvein.tsv with javac.tsv, which the
//     helper wrote, outside the refused files: kind, owner path, start and
//     end must agree. Overload suffixes, repeated-name suffixes and import
//     resolution are not compared. GRAPHVEIN_JAVA_ORACLE_OUT, when set,
//     receives the counts as JSON.
func TestJavaDeclsOracle(t *testing.T) {
	phase := os.Getenv("GRAPHVEIN_JAVA_ORACLE_PHASE")
	if phase == "" {
		t.Skip("javac oracle: set GRAPHVEIN_JAVA_ORACLE_PHASE (scripts/oracle/java_decls.sh)")
	}
	scratch := os.Getenv("GRAPHVEIN_JAVA_ORACLE_SCRATCH")
	if scratch == "" {
		t.Fatal("GRAPHVEIN_JAVA_ORACLE_SCRATCH is required")
	}
	switch phase {
	case "index":
		javaOracleIndex(t, os.Getenv("GRAPHVEIN_JAVA_ORACLE_ROOT"), scratch)
	case "compare":
		javaOracleCompare(t, scratch, os.Getenv("GRAPHVEIN_JAVA_ORACLE_OUT"))
	default:
		t.Fatalf("unknown phase %q", phase)
	}
}

var oracleKinds = map[string]bool{"class": true, "interface": true, "enum": true, "record": true, "annotation": true, "method": true, "constructor": true}

// javaOracleParseTimeout is the oracle's per-file parse budget. The product
// default (5s, parse.Options.Timeout) bounds a user's index; here it made
// the oracle flaky: commons-lang's 7,081-line ArrayUtilsTest.java parses in
// about 1.5s on an idle 4-core machine but took 6-15s under concurrent CPU
// load, ran past the deadline and was recorded parse_failed, so its 370
// declarations read as javac-only. The oracle compares declarations, not
// parse speed, so it gets a budget no loaded machine reaches while a parser
// hang still ends.
const javaOracleParseTimeout = 5 * time.Minute

// javaOracleOptions are the shared test options with the oracle's parse
// budget; nothing else differs from what the rest of the package indexes with.
func javaOracleOptions(t *testing.T) index.Options {
	opt := options(t)
	opt.Parser.Timeout = javaOracleParseTimeout
	return opt
}

// javaOracleFailures lists every .java file the index recorded as
// parse_failed or extract_failed, one line each, and names an expired
// per-file parse deadline as a timeout so it is never mistaken for a
// declaration disagreement.
func javaOracleFailures(diagnostics []contract.Diagnostic, timeout time.Duration) []string {
	var failures []string
	for _, d := range diagnostics {
		if !strings.HasSuffix(d.File, ".java") || (d.Code != "parse_failed" && d.Code != "extract_failed") {
			continue
		}
		if d.Code == "parse_failed" && strings.Contains(d.Message, context.DeadlineExceeded.Error()) {
			failures = append(failures, fmt.Sprintf("%s: parse_failed: TIMEOUT, the per-file parse deadline (%v) expired (machine load or a parser hang, not a declaration disagreement): %s", d.File, timeout, d.Message))
			continue
		}
		failures = append(failures, fmt.Sprintf("%s: %s: %s", d.File, d.Code, d.Message))
	}
	sort.Strings(failures)
	return failures
}

func javaOracleIndex(t *testing.T, root, scratch string) {
	if root == "" {
		t.Fatal("GRAPHVEIN_JAVA_ORACLE_ROOT is required")
	}
	s := open(t)
	emit(t, root, s, javaOracleOptions(t))
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if failures := javaOracleFailures(snapshot.Diagnostics, javaOracleParseTimeout); len(failures) > 0 {
		t.Fatalf("the index recorded %d Java file(s) as failed; their declarations would read as javac-only disagreements, so the comparison is not run:\n%s", len(failures), strings.Join(failures, "\n"))
	}
	var files, refused []string
	for _, f := range snapshot.Files {
		if f.Language == "java" {
			files = append(files, f.Path)
		}
	}
	for _, d := range snapshot.Diagnostics {
		if d.Code == "syntax_error" && strings.HasSuffix(d.File, ".java") {
			refused = append(refused, d.File)
		}
	}
	nodes := map[string]contract.Node{}
	for _, n := range snapshot.Nodes {
		nodes[n.Identity] = n
	}
	owner := map[string]string{}
	for _, e := range snapshot.Edges {
		if e.Kind == "contains" {
			owner[e.Target] = e.Source
		}
	}
	var rows []string
	for _, n := range snapshot.Nodes {
		if !oracleKinds[n.Kind] || !strings.HasSuffix(n.File, ".java") || n.Span == nil {
			continue
		}
		path := []string{n.Name}
		for up := owner[n.Identity]; up != ""; up = owner[up] {
			parent, ok := nodes[up]
			if !ok || parent.Kind == "file" {
				break
			}
			path = append([]string{parent.Name}, path...)
		}
		s := n.Span
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%d:%d-%d:%d", n.File, n.Kind, strings.Join(path, "."), s.StartLine, s.StartColumn, s.EndLine, s.EndColumn))
	}
	sort.Strings(files)
	sort.Strings(refused)
	sort.Strings(rows)
	for name, lines := range map[string][]string{"files.txt": files, "refused.txt": refused, "graphvein.tsv": rows} {
		if err := os.WriteFile(filepath.Join(scratch, name), []byte(strings.Join(append(lines, ""), "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// javaOracleCompare compares the two listings (phase "compare") and
// returns javac's and Graphvein's declaration counts outside the refused
// files and the number that agree.
func javaOracleCompare(t *testing.T, scratch, out string) (javacTotal, graphveinTotal, agreeing int) {
	refused := map[string]bool{}
	for _, f := range readLines(t, filepath.Join(scratch, "refused.txt")) {
		refused[f] = true
	}
	files := readLines(t, filepath.Join(scratch, "files.txt"))
	count := func(lines []string) (map[string]int, int, map[string]bool) {
		set := map[string]int{}
		skipped := 0
		errors := map[string]bool{}
		for _, line := range lines {
			if strings.HasPrefix(line, "#error\t") {
				errors[strings.TrimPrefix(line, "#error\t")] = true
				continue
			}
			file, _, _ := strings.Cut(line, "\t")
			if refused[file] {
				skipped++
				continue
			}
			set[line]++
		}
		return set, skipped, errors
	}
	javac, javacRefused, javacErrors := count(readLines(t, filepath.Join(scratch, "javac.tsv")))
	graphvein, _, _ := count(readLines(t, filepath.Join(scratch, "graphvein.tsv")))
	var onlyJavac, onlyGraphvein []string
	agree := 0
	for line, n := range javac {
		m := graphvein[line]
		agree += min(n, m)
		for i := m; i < n; i++ {
			onlyJavac = append(onlyJavac, line)
		}
	}
	for line, m := range graphvein {
		for i := javac[line]; i < m; i++ {
			onlyGraphvein = append(onlyGraphvein, line)
		}
	}
	sort.Strings(onlyJavac)
	sort.Strings(onlyGraphvein)
	total := func(set map[string]int) int {
		sum := 0
		for _, n := range set {
			sum += n
		}
		return sum
	}
	var errorFiles []string
	for f := range javacErrors {
		errorFiles = append(errorFiles, f)
	}
	sort.Strings(errorFiles)
	refusedFiles := make([]string, 0, len(refused))
	for f := range refused {
		refusedFiles = append(refusedFiles, f)
	}
	sort.Strings(refusedFiles)
	result := map[string]any{
		"files":                  len(files),
		"refused_files":          refusedFiles,
		"javac_error_files":      errorFiles,
		"javac_declarations":     total(javac),
		"graphvein_declarations": total(graphvein),
		"agreeing":               agree,
		"javac_only":             len(onlyJavac),
		"graphvein_only":         len(onlyGraphvein),
		"javac_in_refused_files": javacRefused,
		"compared":               "kind, owner path, start, end",
		"not_compared":           "overload and repeated-name suffixes, import resolution",
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	t.Logf("%s", data)
	if out != "" {
		if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i, line := range onlyJavac {
		if i == 20 {
			break
		}
		t.Errorf("javac only: %s", line)
	}
	for i, line := range onlyGraphvein {
		if i == 20 {
			break
		}
		t.Errorf("graphvein only: %s", line)
	}
	return total(javac), total(graphvein), agree
}

// javaOracleCorpora are the javac oracle's expected declaration counts on
// the two pinned Java corpora (checkout names of scripts/cloud-corpus.sh):
// the T021-B2 run, all of them agreeing.
var javaOracleCorpora = map[string]int{"spring-modulith": 4279, "commons-lang": 12719}

// TestJavaDeclsOracleCorpora runs the javac declaration oracle
// (scripts/oracle/java_decls.sh, in-process) on spring-modulith and
// commons-lang and requires zero disagreements (kind, owner path, start,
// end) and the expected number of declarations. Never run by CI: set
// GRAPHVEIN_JAVA_ORACLE_CORPUS to the directory holding the checkouts
// (scripts/cloud-corpus.sh spring-modulith commons-lang); it needs a JDK 21
// or later (javac, java). javac only parses the corpus (JavacTask.parse(),
// -proc:none); nothing of it runs.
func TestJavaDeclsOracleCorpora(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_JAVA_ORACLE_CORPUS")
	if dir == "" {
		t.Skip("javac oracle: set GRAPHVEIN_JAVA_ORACLE_CORPUS to the corpus directory")
	}
	javac, err := exec.LookPath("javac")
	if err != nil {
		t.Fatalf("javac not found: %v", err)
	}
	java, err := exec.LookPath("java")
	if err != nil {
		t.Fatalf("java not found: %v", err)
	}
	classes := filepath.Join(t.TempDir(), "classes")
	if out, err := exec.Command(javac, "-d", classes, "../../scripts/oracle/JavaDecls.java").CombinedOutput(); err != nil {
		t.Fatalf("compiling the helper: %v\n%s", err, out)
	}
	for name, want := range javaOracleCorpora {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(dir, name)
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%v (run scripts/cloud-corpus.sh %s)", err, name)
			}
			scratch := t.TempDir()
			javaOracleIndex(t, root, scratch)
			listing, err := os.Create(filepath.Join(scratch, "javac.tsv"))
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(java, "-cp", classes, "JavaDecls", root, filepath.Join(scratch, "files.txt"))
			cmd.Stdout = listing
			var stderr strings.Builder
			cmd.Stderr = &stderr
			err = cmd.Run()
			listing.Close()
			if err != nil {
				t.Fatalf("javac helper: %v\n%s", err, stderr.String())
			}
			javacTotal, graphveinTotal, agreeing := javaOracleCompare(t, scratch, "")
			if javacTotal != want || graphveinTotal != want || agreeing != want {
				t.Errorf("javac %d, graphvein %d, agreeing %d; want %d each", javacTotal, graphveinTotal, agreeing, want)
			}
		})
	}
}

// TestJavaOracleReportsParseTimeout pins that the oracle harness names a
// Java file whose parse ran past its deadline as a timeout instead of
// letting its declarations fall out of the comparison. It runs in CI; the
// hold makes the timeout deterministic on any machine speed.
func TestJavaOracleReportsParseTimeout(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "A.java"), []byte("class A { void m() {} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := parse.HoldParseUntilDeadline()
	s := open(t)
	opt := options(t)
	opt.Parser.Timeout = 20 * time.Millisecond
	emit(t, root, s, opt)
	restore()
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	failures := javaOracleFailures(snapshot.Diagnostics, opt.Parser.Timeout)
	if len(failures) != 1 || !strings.HasPrefix(failures[0], "A.java: parse_failed: TIMEOUT") {
		t.Fatalf("failures = %q, want one timeout for A.java", failures)
	}
	if got := javaOracleOptions(t).Parser.Timeout; got != javaOracleParseTimeout {
		t.Fatalf("oracle parse budget = %v, want %v", got, javaOracleParseTimeout)
	}
}
