package parse

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDiskCacheRebuild(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	run := func() {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestCacheWorker$")
		cmd.Env = append(os.Environ(), "GRAPHVEIN_CACHE_CHILD="+cache)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cache worker: %v\n%s", err, output)
		}
	}
	run()
	files := 0
	if err := filepath.WalkDir(cache, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files++
			return os.WriteFile(p, []byte("corrupted compilation entry"), 0600)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("disk cache did not create entries")
	}
	run()
	rebuilt := false
	filepath.WalkDir(cache, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			info, _ := d.Info()
			rebuilt = rebuilt || info.Size() > 100
		}
		return err
	})
	if !rebuilt {
		t.Fatal("corrupt cache was not rebuilt")
	}
}
func TestCacheWorker(t *testing.T) {
	cache := os.Getenv("GRAPHVEIN_CACHE_CHILD")
	if cache == "" {
		t.Skip("cache subprocess only")
	}
	results, err := Batch(context.Background(), []Job{{Language: "typescript", Source: []byte("function f() {}")}}, Options{CacheDir: cache})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
}

func TestAssetsAndWorkers(t *testing.T) {
	for name, checksum := range checksums {
		data, err := assets.ReadFile("grammars/wasitter-" + name + ".wasm")
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != checksum {
			t.Fatal(name, err)
		}
	}
	jobs := []Job{{"typescript", []byte("export const a = () => 1;")}, {"tsx", []byte("export const A = () => <div/>;")}, {"javascript", []byte("function f() { return 1; }")}, {"typescript", []byte("function broken( {")}}
	jobs = append(jobs, Job{"go", []byte("package main\nfunc main() {}")}, Job{"python", []byte("def f():\n    pass\n")}, Job{"rust", []byte("fn main() {}")})
	options := Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "cache")}
	results, err := Batch(context.Background(), jobs, options)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range results {
		if r.Err != nil || r.Root == nil || r.SyntaxError != (i == 3) {
			t.Fatalf("result %d: %+v", i, r)
		}
	}
	engine, err := getEngine(context.Background(), options.CacheDir)
	if err != nil || len(engine.compiled) != 6 {
		t.Fatal("grammars not shared", err)
	}
}

// TestTimeoutRetiresWorker pins that a parse past its per-file deadline fails
// and retires the worker's instance, and that the worker then recovers. The
// hook holds the parse until its deadline has passed: a tiny timeout alone
// raced the clock and lost on a fast Windows runner once grammar setup moved
// outside the deadline (T018g2 G2W2).
func TestTimeoutRetiresWorker(t *testing.T) {
	parseHook = func(ctx context.Context, _ []byte) { <-ctx.Done() }
	t.Cleanup(func() { parseHook = nil })
	options := Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache"), Timeout: 20 * time.Millisecond}
	results, err := Batch(context.Background(), []Job{{"typescript", []byte("function f() {}")}}, options)
	if err != nil || results[0].Err == nil || !errors.Is(results[0].Err, context.DeadlineExceeded) {
		t.Fatal(results, err)
	}
	parseHook = nil
	options.Timeout = 5 * time.Second
	results, err = Batch(context.Background(), []Job{{"typescript", []byte("function ok() {}")}}, options)
	if err != nil || results[0].Err != nil {
		t.Fatal("worker did not recover", results, err)
	}
}

func TestFailureDoesNotStopNextFile(t *testing.T) {
	results, err := Batch(context.Background(), []Job{{Language: "unavailable", Source: []byte("bad")}, {Language: "javascript", Source: []byte("function healthy() {}")}}, Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err == nil || results[1].Err != nil || results[1].Root == nil {
		t.Fatal(results, err)
	}
	// A malformed internal adapter handle exercises the panic boundary and retirement.
	e, err := getEngine(context.Background(), filepath.Join(t.TempDir(), "panic-cache"))
	if err != nil {
		t.Fatal(err)
	}
	w := worker{nil, map[string]*instance{}}
	_, _, err = w.parse(context.Background(), 0, "javascript", []byte("function f() {}"))
	if err == nil || len(w.instances) != 0 {
		t.Fatal("panic was not isolated", err)
	}
	w.engine = e
	root, _, err := w.parse(context.Background(), 0, "javascript", []byte("function healthy() {}"))
	if err != nil || root == nil {
		t.Fatal("worker did not recover", err)
	}
	for _, i := range w.instances {
		i.close()
	}
}

// TestSetupOutsideFileTimeout pins that the per-file timeout covers only
// parsing and traversal. Grammar compilation and module instantiation are
// one-time setup: on a cold compilation cache and a loaded CI machine they
// took longer than the 5s default (T018g2 Windows CI), and a worker waiting
// on another worker's compilation lost a valid file to parse_failed.
func TestSetupOutsideFileTimeout(t *testing.T) {
	compileHook = func(string) { time.Sleep(300 * time.Millisecond) }
	t.Cleanup(func() { compileHook = nil })
	jobs := []Job{{"typescript", []byte("export function greet() {}")}, {"tsx", []byte("export const App = () => <div/>;")}, {"javascript", []byte("function f() {}")}, {"tsx", []byte("const Y = 1;")}}
	options := Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "cache"), Timeout: 200 * time.Millisecond}
	results, err := Batch(context.Background(), jobs, options)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range results {
		if r.Err != nil || r.Root == nil {
			t.Fatalf("Batch result %d lost to setup time: %+v", i, r)
		}
	}
	options.CacheDir = filepath.Join(t.TempDir(), "consume")
	if err := Consume(context.Background(), jobs, options, func(i int, r Result) {
		if r.Err != nil || r.Root == nil {
			t.Errorf("Consume result %d lost to setup time: %+v", i, r)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFileTimeout pins the size-scaled deadline of ADR 0090: the base alone
// up to one DeadlineUnit, proportional above it, capped at MaxDeadlineUnits
// base timeouts, and no deadline when the base disables it.
func TestFileTimeout(t *testing.T) {
	base := 5 * time.Second
	for _, c := range []struct {
		base time.Duration
		size int
		want time.Duration
	}{
		{base, 0, base},
		{base, 100, base},
		{base, DeadlineUnit, base},
		{base, 2 * DeadlineUnit, 2 * base},
		{base, 722537, time.Duration(float64(base) * 722537 / DeadlineUnit)},
		{base, MaxDeadlineUnits * DeadlineUnit, MaxDeadlineUnits * base},
		{base, 2 << 20, MaxDeadlineUnits * base},
		{base, 1 << 30, 60 * time.Second},
		{0, 1 << 20, 0},
		{-1, 1 << 20, -1},
	} {
		if got := FileTimeout(c.base, c.size); got != c.want {
			t.Errorf("FileTimeout(%v, %d) = %v, want %v", c.base, c.size, got, c.want)
		}
	}
	// owncast's largest minified chunk (722,537 bytes) took up to 8.2 s
	// with four workers busy; its deadline must leave a margin over that.
	if got := FileTimeout(base, 722537); got < 2*8200*time.Millisecond {
		t.Errorf("owncast chunk deadline %v leaves no margin", got)
	}
}

// TestDeepTreeTextIsLinear pins that detaching a tree costs the file's bytes
// once, not once per level. A long union type is a left-nested tree as deep
// as the union is long; a copy of each node's text made t3code's 19,307-member
// IAM action union allocate more than 7 GB (T058). Here 4,000 members
// allocated 330 MB that way and allocate about 55 MB (mostly the parser's
// own per-call cost) with one shared copy of the file. Every node's text
// must still be exactly its source bytes.
func TestDeepTreeTextIsLinear(t *testing.T) {
	var b strings.Builder
	b.WriteString("export type Action =\n")
	const members = 4000
	for i := 0; i < members; i++ {
		fmt.Fprintf(&b, "  | \"service-%04d:OperationName\"\n", i)
	}
	b.WriteString(";\n")
	source := []byte(b.String())
	e, err := getEngine(context.Background(), filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	w := worker{e, map[string]*instance{}}
	t.Cleanup(func() {
		for _, i := range w.instances {
			i.close()
		}
	})
	// The first parse instantiates the grammar module; only the second is measured.
	if _, _, err := w.parse(context.Background(), 0, "typescript", []byte("type A = 1;")); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	root, syntax, err := w.parse(context.Background(), 0, "typescript", source)
	runtime.ReadMemStats(&after)
	if err != nil || syntax || root == nil {
		t.Fatal(syntax, err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 128<<20 {
		t.Fatalf("detaching a %d-byte file allocated %d bytes", len(source), allocated)
	}
	depth := 0
	var walk func(n *Node, d int)
	walk = func(n *Node, d int) {
		depth = max(depth, d)
		if n.Text != string(source[n.Start:n.End]) {
			t.Fatalf("%s at %d..%d: text %q", n.Kind, n.Start, n.End, n.Text)
		}
		for _, c := range n.Children {
			walk(c, d+1)
		}
	}
	walk(root, 0)
	if depth < members {
		t.Fatalf("tree depth %d: the union is not nested as assumed", depth)
	}
}
