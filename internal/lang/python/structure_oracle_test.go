package python_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const pythonOracle = "../../../scripts/oracle/python_structure/oracle.py"

// TestPythonStructureOracle compares every fact the Python structural claim
// contract (docs/lang/python-structure.md) claims with
// scripts/oracle/python_structure/oracle.py, which reads each file's raw
// bytes from disk with the reference CPython's own ast and tokenize modules
// and re-derives imports from importlib's PathFinder/FileFinder rules over
// a listing of the whole checkout (never the scanner's inventory or text).
// The indexer's stream (index.Emit) is the input. Pass is 0 records with
// gate=true (a claimed fact that disagrees); omissions the contract
// explains are logged.
//
// The interpreter is GRAPHVEIN_PYTHON_ORACLE, else python3 on PATH; the
// test skips when it is older than the reference Python 3.14. By default
// it runs on the fixture testdata/fixtures/python-structure. Opt in:
// GRAPHVEIN_PYTHON_STRUCTURE_ORACLE_CORPUS=<directory holding fastapi and
// langflow> and GRAPHVEIN_PYTHON_STRUCTURE_ORACLE_SELF=1 (this
// repository); GRAPHVEIN_PYTHON_STRUCTURE_ORACLE_OUT=<directory> keeps
// each run's records and summary.
func TestPythonStructureOracle(t *testing.T) {
	interpreter := oracleInterpreter(t)
	t.Run("fixture", func(t *testing.T) {
		checkPythonOracle(t, interpreter, "fixture", copyStructureFixture(t), 0)
	})
	// Files over the parse limit keep their record but claim nothing; the
	// oracle reads the file_too_large diagnostic.
	t.Run("fixture-oversize", func(t *testing.T) {
		checkPythonOracle(t, interpreter, "fixture-oversize", copyStructureFixture(t), 64)
	})
	if dir := os.Getenv("GRAPHVEIN_PYTHON_STRUCTURE_ORACLE_CORPUS"); dir != "" {
		for _, name := range []string{"fastapi", "langflow"} {
			t.Run(name, func(t *testing.T) { checkPythonOracle(t, interpreter, name, filepath.Join(dir, name), 0) })
		}
	}
	if os.Getenv("GRAPHVEIN_PYTHON_STRUCTURE_ORACLE_SELF") == "1" {
		t.Run("self", func(t *testing.T) {
			root, err := filepath.Abs(filepath.Join("..", "..", ".."))
			if err != nil {
				t.Fatal(err)
			}
			checkPythonOracle(t, interpreter, "self", root, 0)
		})
	}
}

// oracleInterpreter finds a Python at least as new as the reference 3.14,
// or skips.
func oracleInterpreter(t *testing.T) string {
	t.Helper()
	interpreter := os.Getenv("GRAPHVEIN_PYTHON_ORACLE")
	if interpreter == "" {
		found, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("no Python interpreter: set GRAPHVEIN_PYTHON_ORACLE to a CPython 3.14 or put python3 on PATH")
		}
		interpreter = found
	}
	out, err := exec.Command(interpreter, "-I", "-c", "import sys; print('%d %d' % sys.version_info[:2])").Output()
	if err != nil {
		t.Skipf("%s does not run: %v", interpreter, err)
	}
	fields := strings.Fields(string(out))
	major, _ := strconv.Atoi(fields[0])
	minor, _ := strconv.Atoi(fields[1])
	if major < 3 || major == 3 && minor < 14 {
		t.Skipf("%s is Python %d.%d; the oracle needs the contract's reference Python 3.14 (set GRAPHVEIN_PYTHON_ORACLE)", interpreter, major, minor)
	}
	return interpreter
}

// checkPythonOracle indexes root (maxFileSize: the parse limit, 0 for the
// default) and runs the oracle over the stream.
func checkPythonOracle(t *testing.T, interpreter, corpus, root string, maxFileSize int64) {
	ctx := context.Background()
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("%s: %v", corpus, err)
	}
	work := t.TempDir()
	s, err := sqlite.Open(ctx, filepath.Join(work, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var stream bytes.Buffer
	opt := index.Options{Repository: corpus, GeneratedAt: "2026-11-17T00:00:00Z", MaxFileSize: maxFileSize, Parser: parse.Options{Workers: 4, CacheDir: filepath.Join(work, "cache")}}
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(work, "snapshot.ndjson")
	if err := os.WriteFile(snapshot, stream.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := work
	if dir := os.Getenv("GRAPHVEIN_PYTHON_STRUCTURE_ORACLE_OUT"); dir != "" {
		outDir = dir
	}
	records := filepath.Join(outDir, corpus+".oracle.jsonl")
	summaryPath := filepath.Join(outDir, corpus+".summary.json")
	cmd := exec.Command(interpreter, "-I", pythonOracle, "--snapshot", snapshot, "--root", root, "--corpus", corpus, "--out", records, "--summary", summaryPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("oracle: %v\n%s", err, stderr.String())
	}
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Gate          int            `json:"gate"`
		Python        string         `json:"python"`
		Disagreements map[string]int `json:"disagreements"`
		Census        map[string]int `json:"census"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	var classes []string
	for k, v := range summary.Disagreements {
		classes = append(classes, k+"="+strconv.Itoa(v))
	}
	sort.Strings(classes)
	t.Logf("%s (Python %s): %d Python files, %d import statements, %d gate records; %s", corpus, summary.Python, summary.Census["snapshot_python_files"], summary.Census["import_statements"], summary.Gate, strings.Join(classes, " "))
	if summary.Gate == 0 {
		return
	}
	f, err := os.Open(records)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	shown := 0
	for scanner.Scan() {
		var r struct {
			Gate bool `json:"gate"`
		}
		if json.Unmarshal(scanner.Bytes(), &r) == nil && r.Gate {
			if shown == 60 {
				t.Errorf("... %d more", summary.Gate-shown)
				break
			}
			t.Errorf("%s", scanner.Text())
			shown++
		}
	}
}
