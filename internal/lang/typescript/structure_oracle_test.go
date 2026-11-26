package typescript_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// The oracle is scripts/oracle/ts_structure/oracle.cjs: the TypeScript
// 6.0.3 compiler (installed into a scratch directory as its README says)
// reading the raw bytes, parsing configurations, resolving every import
// site and naming every declaration by identity.md, against the stream
// Graphvein emits. GRAPHVEIN_TS_ORACLE_DIR names the directory holding
// node_modules/typescript; node comes from PATH or GRAPHVEIN_NODE.
const oracleScript = "../../../scripts/oracle/ts_structure/oracle.cjs"

// TestTypeScriptStructureOracle compares every claimed fact of the fixture
// with the compiler (and, opt-in, of each checkout listed in
// GRAPHVEIN_TS_ORACLE_CORPORA, separated by the OS path list separator):
// zero disagreements on claimed facts is the gate. It skips without the
// pinned compiler.
func TestTypeScriptStructureOracle(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_TS_ORACLE_DIR")
	if dir == "" {
		t.Skip("GRAPHVEIN_TS_ORACLE_DIR is not set: install typescript@6.0.3 as scripts/oracle/ts_structure/README.md says")
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "typescript", "package.json")); err != nil {
		t.Skipf("no typescript under %s: %v", dir, err)
	}
	node := os.Getenv("GRAPHVEIN_NODE")
	if node == "" {
		found, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node is not on PATH and GRAPHVEIN_NODE is not set")
		}
		node = found
	}
	roots := map[string]string{"fixture": copyFixture(t)}
	if list := os.Getenv("GRAPHVEIN_TS_ORACLE_CORPORA"); list != "" {
		for _, p := range filepath.SplitList(list) {
			roots[filepath.Base(p)] = p
		}
	}
	for name, root := range roots {
		t.Run(name, func(t *testing.T) {
			summary := runOracle(t, node, dir, name, root)
			stats := summary.Stats
			t.Logf("%s: files %d, declarations %d agree of %d claimed, import sites %d, edges matched %d", name, stats["files"], stats["decl_agree"], stats["decl_graphvein"], stats["sites_oracle"], stats["sites_matched"])
			if stats["claimed_disagreements"] != 0 || stats["files_hash_mismatch"] != 0 {
				t.Errorf("%s: %d claimed facts disagree with the compiler, %d files differ from the snapshot (see %s)", name, stats["claimed_disagreements"], stats["files_hash_mismatch"], summary.out)
			}
			if name == "fixture" && (stats["decl_agree"] == 0 || stats["sites_matched"] == 0) {
				t.Error("the fixture compared nothing")
			}
		})
	}
}

type oracleSummary struct {
	Stats map[string]int `json:"stats"`
	out   string
}

// runOracle indexes root into a snapshot and runs the oracle over it.
func runOracle(t *testing.T, node, dir, name, root string) oracleSummary {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	s, err := sqlite.Open(ctx, filepath.Join(tmp, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var stream bytes.Buffer
	opt := index.Options{Repository: name, GeneratedAt: "2026-11-17T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(tmp, "cache")}}
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(tmp, "snapshot.ndjson")
	if err := os.WriteFile(snapshot, stream.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "out")
	script, err := filepath.Abs(oracleScript)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--max-old-space-size=6000", script, "--ts-dir", dir, "--snapshot", snapshot, "--root", root, "--out", out, "--name", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if output, err := cmd.Output(); err != nil {
		t.Fatalf("oracle: %v\n%s%s", err, output, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(out, name+".summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary oracleSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(fmt.Errorf("reading the oracle summary: %w", err))
	}
	if rows, err := os.ReadFile(filepath.Join(out, name+".rows.jsonl")); err == nil {
		for _, line := range strings.Split(string(rows), "\n") {
			if strings.Contains(line, `"DISAGREE`) || strings.Contains(line, `"decl_disagreement"`) && !strings.Contains(line, `"oracle_only:`) {
				t.Log(line)
			}
		}
	}
	summary.out = out
	return summary
}
