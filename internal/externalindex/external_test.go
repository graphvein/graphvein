package externalindex

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

const fixtureDir = "../../testdata/fixtures/external-indexer"

// The test binary is the producer; no nested go build and no production bypass.
func TestExternalIndexerHelper(t *testing.T) {
	mode := os.Getenv("GV_TEST_EXTERNAL_INDEXER_MODE")
	if mode == "" {
		return
	}
	data, err := os.ReadFile(os.Getenv("GV_TEST_EXTERNAL_INDEXER_STREAM"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	hang := func() {
		for {
			time.Sleep(time.Hour)
		}
	}
	if mode == "hang" {
		hang()
	}
	if mode == "wrong-repository" {
		data = bytes.Replace(data, []byte(`"repository":"fixture"`), []byte(`"repository":"other"`), 1)
		// Stay alive to prove that rejecting the header kills and waits for us.
		fmt.Fprintln(os.Stdout, strings.SplitN(string(data), "\n", 2)[0])
		hang()
	}
	if mode == "valid-then-exit-1" || mode == "truncated" || mode == "bad-footer" {
		data = bytes.Replace(data, []byte(`"name":"a"`), []byte(`"name":"changed"`), 1)
	}
	if mode == "truncated" {
		data = data[:bytes.LastIndex(data[:len(data)-1], []byte("\n"))+1]
	}
	if mode == "bad-footer" {
		data = bytes.Replace(data, []byte(`"records":7`), []byte(`"records":99`), 1)
	}
	if mode == "echo-args" {
		var visible []string
		for _, entry := range os.Environ() {
			name := strings.SplitN(entry, "=", 2)[0]
			if strings.HasPrefix(strings.ToUpper(name), "GRAPHVEIN_") {
				visible = append(visible, name)
			}
		}
		cwd, _ := os.Getwd()
		message, _ := json.Marshal(struct {
			Args      []string
			Cwd       string
			Variables []string
		}{os.Args[3:], cwd, visible})
		diagnostic, _ := json.Marshal(contract.Diagnostic{Type: "diagnostic", Severity: "info", Code: "invocation", Message: string(message)})
		footer := bytes.LastIndex(data, []byte(`{"type":"footer"`))
		data = append(append(append([]byte{}, data[:footer]...), diagnostic...), '\n')
		data = append(data, []byte("{\"type\":\"footer\",\"counts\":{\"records\":8,\"file\":2,\"node\":3,\"edge\":2,\"diagnostic\":1}}\n")...)
	}
	if mode == "stderr-noise" || mode == "stderr-failure" {
		for i := 0; i < 10000; i++ {
			fmt.Fprintf(os.Stderr, "noise-%05d: %s\n", i, strings.Repeat("x", 40))
		}
	}
	if mode == "malformed-header" {
		fmt.Fprintln(os.Stdout, "{}")
		hang()
	}
	_, _ = os.Stdout.Write(data)
	if mode == "valid-then-exit-1" || mode == "stderr-failure" {
		os.Exit(1)
	}
	os.Exit(0)
}

func backends(t *testing.T, check func(*testing.T, store.Store)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		check(t, s)
	})
	t.Run("postgres", func(t *testing.T) {
		url := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
		unavailable := func() {
			if os.Getenv("GRAPHVEIN_REQUIRE_POSTGRES") == "1" {
				t.Fatal("GRAPHVEIN_REQUIRE_POSTGRES=1 requires a live test database")
			}
			t.Skip("Postgres unavailable: GRAPHVEIN_TEST_POSTGRES_URL unset or unreachable")
		}
		if url == "" {
			unavailable()
		}
		admin, err := egress.OpenPostgres(url, nil)
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = admin.PingContext(ctx)
		cancel()
		if err != nil {
			unavailable()
		}
		schema := "graphvein_external_" + strings.ToLower(rand.Text())
		if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
				t.Error(err)
			}
		}()
		db, err := egress.OpenPostgres(url, map[string]string{"search_path": schema})
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		s, err := sqlstore.Open(context.Background(), db, "postgres")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		check(t, s)
	})
}

func TestExternalSnapshots(t *testing.T) {
	streamPath, err := filepath.Abs(filepath.Join(fixtureDir, "snapshot.golden.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	backends(t, func(t *testing.T, s store.Store) {
		root := filepath.Join(t.TempDir(), "checkout with spaces")
		if err := os.Mkdir(root, 0755); err != nil {
			t.Fatal(err)
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		// Copy the already-built test binary to exercise an executable path with
		// spaces; this never invokes a compiler from a test.
		executable := filepath.Join(root, filepath.Base(os.Args[0]))
		if err := copyExecutable(os.Args[0], executable); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		t.Setenv("GV_TEST_EXTERNAL_INDEXER_STREAM", streamPath)
		t.Setenv("GRAPHVEIN_SQLITE_PATH", "secret")
		t.Setenv("GRAPHVEIN_TOKEN", "secret")
		t.Setenv("GRAPHVEIN_", "secret")
		if err := s.SaveFactCache(ctx, "fixture", "absent.ts", "h", "v", []byte("preserved")); err != nil {
			t.Fatal(err)
		}
		runMode := func(mode string, timeout time.Duration, emit *bytes.Buffer) (store.Writes, error) {
			t.Helper()
			t.Setenv("GV_TEST_EXTERNAL_INDEXER_MODE", mode)
			childCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := command(childCtx, executable, []string{"-test.run=^TestExternalIndexerHelper$", "--"}, root, "fixture")
			writes, err := consume(childCtx, cancel, cmd, "fixture", s, emit)
			if cmd.ProcessState == nil {
				t.Fatal("child Wait did not return")
			}
			return writes, err
		}
		var emit bytes.Buffer
		if _, err := runMode("valid", 10*time.Second, &emit); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(streamPath)
		if !bytes.Equal(emit.Bytes(), data) {
			t.Fatal("emit copy differs")
		}
		before, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, filepath.Join(fixtureDir, "dump.golden.json"), before)
		runs, err := s.Runs(ctx, "fixture")
		if err != nil || len(runs) != 1 || runs[0].Header.Indexer.Name != "fixture" {
			t.Fatal(runs, err)
		}
		if writes, err := runMode("valid", 10*time.Second, &bytes.Buffer{}); err != nil || writes != (store.Writes{}) {
			t.Fatal(writes, err)
		}
		for _, mode := range []string{"valid-then-exit-1", "truncated", "wrong-repository", "hang", "bad-footer", "malformed-header", "stderr-failure"} {
			runsBefore, _ := s.Runs(ctx, "fixture")
			timeout := 10 * time.Second
			if mode == "hang" {
				timeout = 250 * time.Millisecond
			}
			_, err := runMode(mode, timeout, &bytes.Buffer{})
			if err == nil {
				t.Fatalf("%s succeeded", mode)
			}
			if mode == "stderr-failure" && (!strings.Contains(err.Error(), "noise-09999") || strings.Contains(err.Error(), "noise-09979")) {
				t.Fatal("stderr tail", err)
			}
			after, dumpErr := s.Dump(ctx, "fixture")
			if dumpErr != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("%s did not roll back: %v", mode, dumpErr)
			}
			runsAfter, _ := s.Runs(ctx, "fixture")
			if !reflect.DeepEqual(runsBefore, runsAfter) {
				t.Fatalf("%s published run metadata", mode)
			}
			other, err := s.Runs(ctx, "other")
			if err != nil || len(other) != 0 {
				t.Fatal(other, err)
			}
		}
		var logs bytes.Buffer
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
		_, noiseErr := runMode("stderr-noise", 10*time.Second, &bytes.Buffer{})
		slog.SetDefault(oldLogger)
		if noiseErr != nil || !strings.Contains(logs.String(), "noise-09999") || strings.Contains(logs.String(), "noise-00000") {
			t.Fatal("stderr capture failed", noiseErr)
		}
		if _, err := runMode("echo-args", 10*time.Second, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		dump, err := s.Dump(ctx, "fixture")
		if err != nil || len(dump.Diagnostics) != 1 {
			t.Fatal(dump, err)
		}
		var invocation struct {
			Args      []string
			Cwd       string
			Variables []string
		}
		if err := json.Unmarshal([]byte(dump.Diagnostics[0].Message), &invocation); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(invocation.Args, []string{"--repository", "fixture", root}) || invocation.Cwd != root || len(invocation.Variables) != 0 {
			t.Fatalf("invocation: %+v", invocation)
		}
		cache, err := s.FactCache(ctx, "fixture", "absent.ts", "h", "v")
		if err != nil || string(cache) != "preserved" {
			t.Fatal("external run modified cache", string(cache), err)
		}
		// Start failure must likewise leave the already-published graph alone.
		if _, err := Run(ctx, filepath.Join(root, "missing.exe"), root, "fixture", 0, s, nil); err == nil {
			t.Fatal("missing executable succeeded")
		}
		after, _ := s.Dump(ctx, "fixture")
		if !reflect.DeepEqual(dump, after) {
			t.Fatal("start failure changed graph")
		}
	})
}

func copyExecutable(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func TestExecutablePathsAndTail(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "missing")} {
		if _, err := ResolveExecutable(path); err == nil {
			t.Fatal("accepted", path)
		}
	}
	plain := filepath.Join(root, "plain.txt")
	if err := os.WriteFile(plain, []byte("plain"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveExecutable(plain); err == nil {
		t.Fatal("accepted non-executable")
	}
	resolved, err := ResolveExecutable(os.Args[0])
	if err != nil || !filepath.IsAbs(resolved) {
		t.Fatal(resolved, err)
	}
	// Put cwd on the same volume as the test executable on Windows.
	t.Chdir(root)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if path, err := ResolveExecutable(relative); err != nil || path != resolved {
		t.Fatal(path, err)
	}
	// Setting PATH does not make a nonexistent cwd-relative file acceptable.
	pathDir := filepath.Join(root, "path-only")
	if err := os.Mkdir(pathDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(os.Args[0], filepath.Join(pathDir, "producer.exe")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := ResolveExecutable("producer.exe"); err == nil {
		t.Fatal("looked up indexer on PATH")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(plain, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveExecutable(plain); err != nil {
			t.Fatal(err)
		}
	}
	var tail tailBuffer
	_, _ = tail.Write([]byte(strings.Repeat("a", 100000)))
	_, _ = tail.Write([]byte("\nlast\n"))
	if len(tail.data) != 64<<10 || tail.lastLines(1) != "last" {
		t.Fatal("unbounded or wrong stderr tail")
	}
}
