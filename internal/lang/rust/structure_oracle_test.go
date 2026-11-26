package rust_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/rust"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// oracleClaimClasses are the comparator's disagreement classes that state a
// fact Graphvein claims and the oracle contradicts
// (scripts/oracle/rust_structure/compare.go). Omissions (D-MISSING,
// I-MISSING), cargo or rustc failures (M-*), evidence no configuration
// reaches (not_compared) and edges for which rustc resolved nothing
// (I-UNPROVEN: a leaf rustc's error recovery leaves without a resolution,
// listed in the log and in ADR 0071) are reported, not failed. R-EXTRA is a
// crate root Graphvein reads that is no cargo target of its package; a
// cargo target Graphvein does not read as a root (R-MISSING) is an
// omission.
var oracleClaimClasses = map[string]bool{
	"D-REJECTED": true, "D-LONECR": true, "D-EXTRA": true, "D-LOCAL": true, "D-ASSOC": true, "D-OTHERPOS": true,
	"D-MISMATCH": true, "D-FOREIGN": true, "D-SUFFIX": true, "I-WRONG": true, "R-EXTRA": true,
}

// TestRustStructureOracle compares every declaration, mod edge and use edge
// Graphvein claims with the Rust toolchain (ADR 0071): syn over the raw
// bytes, cargo metadata and rustc -Zunpretty=hir-tree under the oracle's
// cfg configurations. Opt-in and never run by CI: it needs rustc and cargo
// 1.97.0 with the x86_64-pc-windows-msvc and x86_64-apple-darwin rust-std
// components, and the pinned syn crates (crates.io on the first build).
//
//	GRAPHVEIN_RUST_STRUCTURE_ORACLE=1                  the fixture
//	GRAPHVEIN_RUST_STRUCTURE_ORACLE_CORPUS=dir[:dir]   corpora (checkouts)
//	GRAPHVEIN_RUST_STRUCTURE_ORACLE_SCRATCH=dir        scratch (default: a temp dir)
func TestRustStructureOracle(t *testing.T) {
	fixture := os.Getenv("GRAPHVEIN_RUST_STRUCTURE_ORACLE") == "1"
	corpora := filepath.SplitList(os.Getenv("GRAPHVEIN_RUST_STRUCTURE_ORACLE_CORPUS"))
	if !fixture && len(corpora) == 0 {
		t.Skip("opt-in: set GRAPHVEIN_RUST_STRUCTURE_ORACLE=1 or GRAPHVEIN_RUST_STRUCTURE_ORACLE_CORPUS")
	}
	home, _ := os.UserHomeDir()
	toolchain := os.Getenv("RUST_ORACLE_TOOLCHAIN")
	if toolchain == "" {
		toolchain = filepath.Join(home, ".rustup", "toolchains", "stable-x86_64-unknown-linux-gnu", "bin")
	}
	version, err := exec.Command(filepath.Join(toolchain, "rustc"), "--version").Output()
	if err != nil || !strings.HasPrefix(string(version), "rustc 1.97.0 ") {
		t.Skipf("the oracle is pinned to rustc 1.97.0 (the hir-tree format is unstable); found %q, %v", strings.TrimSpace(string(version)), err)
	}
	for _, target := range []string{"x86_64-pc-windows-msvc", "x86_64-apple-darwin"} {
		if _, err := os.Stat(filepath.Join(toolchain, "..", "lib", "rustlib", target, "lib")); err != nil {
			t.Skipf("rust-std for %s is not installed (rustup target add %s)", target, target)
		}
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is needed to run scripts/oracle/rust_structure/run.sh")
	}
	scratch := os.Getenv("GRAPHVEIN_RUST_STRUCTURE_ORACLE_SCRATCH")
	if scratch == "" {
		scratch = t.TempDir()
	}
	script, err := filepath.Abs("../../../scripts/oracle/rust_structure/run.sh")
	if err != nil {
		t.Fatal(err)
	}
	type target struct{ name, root string }
	var targets []target
	if fixture {
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(structureFixture)); err != nil {
			t.Fatal(err)
		}
		rust.WriteStructureExtraFiles(t, root)
		targets = append(targets, target{"fixture", root})
	}
	for _, c := range corpora {
		if c != "" {
			targets = append(targets, target{filepath.Base(c), c})
		}
	}
	for _, tg := range targets {
		t.Run(tg.name, func(t *testing.T) {
			snapshot := filepath.Join(scratch, tg.name+".ndjson")
			writeSnapshot(t, tg.root, snapshot, tg.name)
			writeRoots(t, tg.root, filepath.Join(scratch, tg.name+".roots.json"))
			cmd := exec.Command("bash", script, tg.root, snapshot, tg.name, scratch)
			cmd.Env = append(os.Environ(), "RUST_ORACLE_TOOLCHAIN="+toolchain)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("oracle: %v\n%s", err, out)
			}
			checkOracleOutput(t, filepath.Join(scratch, tg.name+".jsonl"))
		})
	}
}

// writeSnapshot indexes root into an NDJSON snapshot, as `graphvein index
// -emit` does, with the store and the fact cache in temporary directories.
func writeSnapshot(t *testing.T, root, out, name string) {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var stream bytes.Buffer
	opt := index.Options{Repository: name, GeneratedAt: "2026-11-17T00:00:00Z", Parser: parse.Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, stream.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeRoots writes the crate roots Graphvein reads (row RS-M1) for the
// comparator's comparison with cargo metadata.
func writeRoots(t *testing.T, root, out string) {
	t.Helper()
	b, err := json.Marshal(rust.StructureRoots(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// checkOracleOutput fails on every disagreement about a claimed fact and
// logs the summary counts.
func checkOracleOutput(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	reported := map[string]int{}
	for sc.Scan() {
		var r map[string]any
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		switch r["kind"] {
		case "summary":
			var keys []string
			for k := range r {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				t.Logf("%s = %v", k, r[k])
			}
		case "disagreement":
			class, _ := r["class"].(string)
			if oracleClaimClasses[class] {
				t.Errorf("%s %v: %v", class, r["file"], r["detail"])
			} else {
				reported[class]++
				if class == "I-UNPROVEN" {
					t.Logf("%s %v: %v", class, r["file"], r["detail"])
				}
			}
		case "not_compared":
			reported["not_compared"]++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("omissions and unreached (not failed): %v", reported)
}
