// Package parse owns the pinned, offline tree-sitter runtime and worker lifetimes.
package parse

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/zema1/wasitter"

	"github.com/graphvein/graphvein/internal/contract"
)

//go:embed grammars/*
var assets embed.FS
var checksums = map[string]string{
	"angular":    "343253fe664ecd1c20d49ccfe19991fc5f75a6fbe2834d5642c90da20755d96a",
	"typescript": "520278cf9f41dbb6d591e99889f25ea1faa80da4063050548eda5803d7b459e2",
	"tsx":        "454fe6226f8c2c2a6ef0813d49e5c9471a6b419450ddb7f1d47e8f7dc79b01be",
	"javascript": "3cdf36463561d9fc4d2719a9fd3a52b8b8a1ffd2fd61a1d23ec5acbbf97f3fc3",
	"go":         "2812c281eeee4a59ab27ae71595a625c1e50619507431a8f85f03edd361ae508",
	"python":     "0b4396e688a08f9d2c6a11de2b018bdda63eef7b59497ee1fd56481cc96f5423",
	"rust":       "2cdd61ffe54efabf991b04f8ef9915e60822e8bd61e5f0fd677aa84061346ade",
	"java":       "bdafeee7af6963461de9498f22f9e0a1893a2196bb410f20941a55fab0e24e88",
	"c_sharp":    "9075c0442ff28ddd2d0c368994aad2b26de4d79f08df5a22383290ef46208bcf",
	"dart":       "ff84ee67832448ba4187f05adb9c991c3bbd7e1fc4e8f5761f94c6daa22e302b",
}

// Node detaches extraction from WASM handles, which must never outlive their tree.
type Node struct {
	Kind, Text                     string
	Start, End                     uint32
	Row, Column, EndRow, EndColumn uint32
	Fields                         map[string]*Node
	Children                       []*Node
	Tokens                         []string
	// ErrorSpans is populated on Angular roots only, including unnamed MISSING
	// nodes that structural extraction normally omits from detached children.
	ErrorSpans []contract.Span
}

// Options bounds concurrency and the time spent parsing and traversing one file.
type Options struct {
	Workers int
	// Timeout is the base per-file deadline; FileTimeout scales it with the
	// file's size (ADR 0090).
	Timeout  time.Duration
	CacheDir string
}

// Size-scaled parse deadline (ADR 0090). Parsing and detaching a tree costs
// time linear in the file's bytes (owncast's minified Next.js chunks: about
// 7 µs per byte alone, up to 17.5 µs per byte with four workers busy), so a
// fixed deadline loses large files that parse normally whenever the machine
// is loaded. A file gets the base timeout per DeadlineUnit of bytes,
// proportionally (the base alone up to one unit), at most MaxDeadlineUnits
// base timeouts: with the 5 s default that is about 38 µs per byte, 60 s at
// most.
const (
	// DeadlineUnit is the file size one base timeout covers. Small files,
	// where a slow parse means a pathological input rather than size, keep
	// the base timeout exactly.
	DeadlineUnit = 128 << 10
	// MaxDeadlineUnits caps the scaled deadline at this many base timeouts,
	// so no single file can hold a worker longer than that.
	MaxDeadlineUnits = 12
)

// FileTimeout is the deadline for parsing and traversing one file of size
// bytes: base times size/DeadlineUnit, never less than base and never more
// than MaxDeadlineUnits times base. A zero or negative base disables the
// deadline, as before.
func FileTimeout(base time.Duration, size int) time.Duration {
	if base <= 0 || size <= DeadlineUnit {
		return base
	}
	if size >= MaxDeadlineUnits*DeadlineUnit {
		return MaxDeadlineUnits * base
	}
	return time.Duration(float64(base) * float64(size) / DeadlineUnit)
}

// RetryTimeout is the deadline of Consume's serial retry (T074): the cap
// FileTimeout never exceeds, MaxDeadlineUnits base timeouts (60 s with the
// 5 s default), whatever the file's size.
func RetryTimeout(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	return MaxDeadlineUnits * base
}

// TimeoutError is a parse that ran past its own per-file deadline while the
// run itself was still live. Its message is the underlying error's, so a
// parse_failed diagnostic reads as before ("context deadline exceeded").
// Consume retries such a file once, serially (T074); no other failure is
// retried.
type TimeoutError struct {
	// Deadline is the per-file deadline the parse was given.
	Deadline time.Duration
	Err      error
}

func (e *TimeoutError) Error() string { return e.Err.Error() }

func (e *TimeoutError) Unwrap() error { return e.Err }

// IsTimeout reports whether err is a per-file parse deadline (TimeoutError).
func IsTimeout(err error) bool {
	var t *TimeoutError
	return errors.As(err, &t)
}

type compiledGrammar struct {
	rt     wazero.Runtime
	module wazero.CompiledModule
}
type engine struct {
	mu       sync.Mutex
	rt       wazero.Runtime
	fallback wazero.Runtime
	cacheDir string
	compiled map[string]*compiledGrammar
}

// compileHook is a test-only hook called before each grammar compilation,
// so a test can make the one-time setup slower than the per-file timeout.
var compileHook func(language string)

// parseHook is a test-only hook called once the per-file deadline is set and
// before parsing starts, with the file's context and source, so a test can
// make the parse itself outlast its deadline on any machine speed instead of
// relying on a tiny timeout racing the clock.
var parseHook func(ctx context.Context, source []byte)

var engines = struct {
	sync.Mutex
	items map[string]*engine
}{items: map[string]*engine{}}

func getEngine(ctx context.Context, dir string) (*engine, error) {
	engines.Lock()
	defer engines.Unlock()
	if e := engines.items[dir]; e != nil {
		return e, nil
	}
	config := wazero.NewRuntimeConfig().WithCloseOnContextDone(true)
	// Wazero internally keys entries by runtime version and WASM digest. The
	// additional namespace pins our adapter/assets and allows safe future upgrades.
	cacheDir := ""
	if dir != "" {
		cacheDir = filepath.Join(dir, "wasitter-0.1.0-assets-v5")
		if cache, err := wazero.NewCompilationCacheWithDir(cacheDir); err == nil {
			config = config.WithCompilationCache(cache)
		} else {
			slog.Warn("WASM compilation cache unavailable; using memory", "error", err)
			cacheDir = ""
		}
	}
	rt := wazero.NewRuntimeWithConfig(context.Background(), config)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		rt.Close(context.Background())
		return nil, fmt.Errorf("loading WASI: %w", err)
	}
	e := &engine{rt: rt, cacheDir: cacheDir, compiled: map[string]*compiledGrammar{}}
	engines.items[dir] = e
	return e, nil
}
func safeCompile(ctx context.Context, rt wazero.Runtime, data []byte) (compiled wazero.CompiledModule, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("compilation cache panic: %v", v)
		}
	}()
	return rt.CompileModule(ctx, data)
}
func (e *engine) grammar(ctx context.Context, language string) (*compiledGrammar, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c := e.compiled[language]; c != nil {
		return c, nil
	}
	expected, ok := checksums[language]
	if !ok {
		return nil, fmt.Errorf("unsupported grammar %s", language)
	}
	data, err := assets.ReadFile("grammars/wasitter-" + language + ".wasm")
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != expected {
		return nil, fmt.Errorf("%s grammar checksum mismatch", language)
	}
	if compileHook != nil {
		compileHook(language)
	}
	rt := e.rt
	c, err := safeCompile(ctx, rt, data)
	if err != nil && ctx.Err() == nil {
		// Disk cache failure must not make a valid embedded grammar unusable.
		if e.fallback == nil {
			config := wazero.NewRuntimeConfig().WithCloseOnContextDone(true)
			// Only this fixed, Graphvein-owned namespace is ever discarded. Wazero
			// recreates version/digest keys; unrelated user-cache files are untouched.
			if e.cacheDir != "" {
				if removeErr := os.RemoveAll(e.cacheDir); removeErr == nil {
					if cache, cacheErr := wazero.NewCompilationCacheWithDir(e.cacheDir); cacheErr == nil {
						config = config.WithCompilationCache(cache)
					}
				}
			}
			slog.Warn("rebuilding WASM compilation cache", "error", err)
			e.fallback = wazero.NewRuntimeWithConfig(context.Background(), config)
			if _, e2 := wasi_snapshot_preview1.Instantiate(ctx, e.fallback); e2 != nil {
				return nil, e2
			}
		}
		rt = e.fallback
		c, err = safeCompile(ctx, rt, data)
	}
	if err != nil {
		return nil, fmt.Errorf("compiling %s: %w", language, err)
	}
	g := &compiledGrammar{rt, c}
	e.compiled[language] = g
	return g, nil
}

type instance struct {
	runtime *wasitter.Runtime
	parser  *wasitter.Parser
}

func (i *instance) close() (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("parser cleanup panic: %v", v)
		}
	}()
	if i.parser != nil {
		err = i.parser.Close()
	}
	if i.runtime != nil {
		if e := i.runtime.Close(); err == nil {
			err = e
		}
	}
	return
}

type worker struct {
	engine    *engine
	instances map[string]*instance
}

// parse parses one file. The per-file timeout starts only after the one-time
// setup of a worker's first file in a language (grammar compilation, which
// may wait on another worker's compilation, and module instantiation), so it
// bounds parsing and traversal alone. Setup cost depends on the compilation
// cache and machine load, not on the file, so it must not turn a valid file
// into parse_failed; ctx still cancels it. timeout is the file's own
// deadline (the caller applies FileTimeout or RetryTimeout); a timeout <= 0
// adds no deadline. A failure after that deadline passed, while ctx is still
// live, is returned as a *TimeoutError.
func (w *worker) parse(ctx context.Context, timeout time.Duration, language string, source []byte) (root *Node, syntax bool, err error) {
	var fileCtx context.Context
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("parser panic: %v", v)
		}
		if err != nil && fileCtx != nil && ctx.Err() == nil && errors.Is(fileCtx.Err(), context.DeadlineExceeded) {
			err = &TimeoutError{Deadline: timeout, Err: err}
		}
		if err != nil {
			if i := w.instances[language]; i != nil {
				if e := i.close(); e != nil {
					err = fmt.Errorf("%w; cleaning worker: %v", err, e)
				}
				delete(w.instances, language)
			}
		}
	}()
	i := w.instances[language]
	if i == nil {
		c, e := w.engine.grammar(ctx, language)
		if e != nil {
			return nil, false, e
		}
		mod, e := c.rt.InstantiateModule(ctx, c.module, wazero.NewModuleConfig().WithName("").WithStartFunctions())
		if e != nil {
			return nil, false, e
		}
		r, e := wasitter.NewRuntimeFromModule(context.Background(), mod)
		if e != nil {
			mod.Close(context.Background())
			return nil, false, e
		}
		p, e := wasitter.NewParserWithRuntime(r)
		if e != nil {
			r.Close()
			return nil, false, e
		}
		l, e := r.LoadLanguage("")
		if e == nil {
			e = p.SetLanguage(l)
		}
		if e != nil {
			p.Close()
			r.Close()
			return nil, false, e
		}
		i = &instance{r, p}
		w.instances[language] = i
	}
	fileCtx = ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		fileCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if parseHook != nil {
		parseHook(fileCtx, source)
	}
	tree, err := i.parser.ParseContext(fileCtx, source, nil)
	if err != nil {
		return nil, false, err
	}
	defer tree.Close()
	syntax = tree.RootNode().HasError()
	// Every node's Text is a substring of one copy of the file, so the
	// detached tree costs the file's bytes once. A copy per node cost the
	// bytes times the tree's depth: a generated 19,307-member union type
	// (717 KB, a left-nested tree as deep as the union is long) took more
	// than 7 GB (T058).
	text := string(source)
	var errorSpans []contract.Span
	var detach func(wasitter.Node) (*Node, error)
	detach = func(n wasitter.Node) (*Node, error) {
		if err := fileCtx.Err(); err != nil {
			return nil, err
		}
		start, end := n.StartPoint(), n.EndPoint()
		if language == "angular" && (n.Type() == "ERROR" || n.IsMissing()) {
			errorSpans = append(errorSpans, contract.Span{StartLine: int(start.Row) + 1, StartColumn: int(start.Column), EndLine: int(end.Row) + 1, EndColumn: int(end.Column)})
		}
		out := &Node{Kind: n.Type(), Start: n.StartByte(), End: n.EndByte(), Row: start.Row, Column: start.Column, EndRow: end.Row, EndColumn: end.Column, Fields: map[string]*Node{}}
		if out.End <= uint32(len(source)) {
			out.Text = text[out.Start:out.End]
		}
		for j := 0; j < n.ChildCount(); j++ {
			child := n.Child(j)
			if !child.IsNamed() {
				if language == "angular" && child.IsMissing() {
					a, b := child.StartPoint(), child.EndPoint()
					errorSpans = append(errorSpans, contract.Span{StartLine: int(a.Row) + 1, StartColumn: int(a.Column), EndLine: int(b.Row) + 1, EndColumn: int(b.Column)})
				}
				out.Tokens = append(out.Tokens, child.Type())
				continue
			}
			v, e := detach(child)
			if e != nil {
				return nil, e
			}
			out.Children = append(out.Children, v)
			if field := n.FieldNameForChild(j); field != "" {
				out.Fields[field] = v
			}
		}
		return out, nil
	}
	root, err = detach(tree.RootNode())
	if root != nil {
		root.ErrorSpans = errorSpans
	}
	return
}

// Job gives a worker only one file, so faults cannot contaminate another tree.
type Job struct {
	Language string
	Source   []byte
}

// Result returns recoverable syntax and runtime failures without aborting a batch.
type Result struct {
	Root        *Node
	SyntaxError bool
	Err         error
	// Retried is true when the file timed out in Consume's parallel pass
	// and this is the result of its serial retry (T074).
	Retried bool
}

// Consume bounds detached-tree lifetime while one caller extracts results in
// arrival order. The callback must not retain roots after returning. A file
// whose parse ran past its own deadline is not consumed from the parallel
// pass: once every parallel worker has stopped, it is parsed again, alone,
// with RetryTimeout, and that result (Retried set) is consumed instead.
func Consume(ctx context.Context, jobs []Job, options Options, consume func(int, Result)) error {
	if options.Workers <= 0 {
		options.Workers = min(4, runtime.NumCPU())
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	if options.CacheDir == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			options.CacheDir = filepath.Join(dir, "graphvein", "wazero")
		}
	}
	e, err := getEngine(ctx, options.CacheDir)
	if err != nil {
		return err
	}
	type indexedResult struct {
		index  int
		result Result
	}
	results := make(chan indexedResult, options.Workers)
	workers := min(options.Workers, len(jobs))
	var wg sync.WaitGroup
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func(first int) {
			defer wg.Done()
			w := worker{e, map[string]*instance{}}
			closeWorker := func() error {
				var err error
				for language, i := range w.instances {
					if e := i.close(); e != nil {
						err = fmt.Errorf("cleaning parser: %w", e)
					}
					delete(w.instances, language)
				}
				return err
			}
			defer closeWorker()
			// Fixed lanes let cleanup errors accompany the worker's final result,
			// without retaining an already consumed tree or revising emitted facts.
			for j := first; j < len(jobs); j += workers {
				if ctx.Err() != nil {
					return
				}
				root, syntax, err := w.parse(ctx, FileTimeout(options.Timeout, len(jobs[j].Source)), jobs[j].Language, jobs[j].Source)
				if j+workers >= len(jobs) {
					if e := closeWorker(); e != nil {
						err = e
					}
				}
				if root != nil {
					profileDetachedTree(1)
				}
				select {
				case results <- indexedResult{j, Result{Root: root, SyntaxError: syntax, Err: err}}:
					root = nil
				case <-ctx.Done():
					if root != nil {
						root = nil
						profileDetachedTree(-1)
					}
					return
				}
			}
		}(n)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	// This calling goroutine is the sole consumer. Drain on cancellation to
	// release queued roots and join all workers before returning the context error.
	// A file that ran past its own deadline is held back for the retry below
	// instead of being consumed.
	var timedOut []int
	for item := range results {
		if ctx.Err() == nil {
			if item.result.Root == nil && IsTimeout(item.result.Err) {
				timedOut = append(timedOut, item.index)
			} else {
				consume(item.index, item.result)
			}
		}
		if item.result.Root != nil {
			item.result.Root = nil
			profileDetachedTree(-1)
		}
	}
	if ctx.Err() == nil && len(timedOut) > 0 {
		retrySerially(ctx, e, jobs, timedOut, options.Timeout, consume)
	}
	return ctx.Err()
}

// retrySerially parses again, once each, the files of the parallel pass that
// ran past their deadline (T074). Under CPU load a file of normal size can
// outlast a deadline sized for an idle machine and lose every fact. The
// retry runs after every parallel worker has stopped, one file at a time on
// a single worker, with RetryTimeout: the least contention and the longest
// deadline any file gets. A file that succeeds yields exactly the tree an
// unloaded run parses, since parsing does not depend on timing; one that
// times out again is consumed with that failure. Files are retried in input
// order so the work done does not depend on arrival order.
func retrySerially(ctx context.Context, e *engine, jobs []Job, indexes []int, base time.Duration, consume func(int, Result)) {
	sort.Ints(indexes)
	w := worker{e, map[string]*instance{}}
	closeWorker := func() error {
		var err error
		for language, i := range w.instances {
			if e := i.close(); e != nil {
				err = fmt.Errorf("cleaning parser: %w", e)
			}
			delete(w.instances, language)
		}
		return err
	}
	defer closeWorker()
	deadline := RetryTimeout(base)
	for n, j := range indexes {
		if ctx.Err() != nil {
			return
		}
		slog.Debug("retrying timed-out parse serially", "job", j, "deadline", deadline)
		root, syntax, err := w.parse(ctx, deadline, jobs[j].Language, jobs[j].Source)
		if n == len(indexes)-1 {
			if e := closeWorker(); e != nil {
				err = e
			}
		}
		if root != nil {
			profileDetachedTree(1)
		}
		if ctx.Err() == nil {
			consume(j, Result{Root: root, SyntaxError: syntax, Err: err, Retried: true})
		}
		if root != nil {
			profileDetachedTree(-1)
		}
	}
}

// Batch parses independent files in stable input order and closes every worker module.
func Batch(ctx context.Context, jobs []Job, options Options) ([]Result, error) {
	if options.Workers <= 0 {
		options.Workers = min(4, runtime.NumCPU())
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	if options.CacheDir == "" {
		dir, err := os.UserCacheDir()
		if err == nil {
			options.CacheDir = filepath.Join(dir, "graphvein", "wazero")
		}
	}
	e, err := getEngine(ctx, options.CacheDir)
	if err != nil {
		return nil, err
	}
	out := make([]Result, len(jobs))
	queue := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < min(options.Workers, len(jobs)); n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := worker{e, map[string]*instance{}}
			last := -1
			defer func() {
				for _, i := range w.instances {
					if err := i.close(); err != nil && last >= 0 {
						out[last].Err = fmt.Errorf("cleaning parser: %w", err)
					}
				}
			}()
			for j := range queue {
				last = j
				root, syntax, err := w.parse(ctx, FileTimeout(options.Timeout, len(jobs[j].Source)), jobs[j].Language, jobs[j].Source)
				out[j] = Result{Root: root, SyntaxError: syntax, Err: err}
			}
		}()
	}
	for i := range jobs {
		queue <- i
	}
	close(queue)
	wg.Wait()
	return out, ctx.Err()
}
