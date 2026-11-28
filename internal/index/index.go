// Package index produces complete contract snapshots from per-file cached facts.
package index

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/actions"
	"github.com/graphvein/graphvein/internal/extract/angular"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/extract/deploy"
	"github.com/graphvein/graphvein/internal/extract/models/diesel"
	"github.com/graphvein/graphvein/internal/extract/models/drizzle"
	"github.com/graphvein/graphvein/internal/extract/models/gorm"
	"github.com/graphvein/graphvein/internal/extract/models/prisma"
	"github.com/graphvein/graphvein/internal/extract/models/sequelize"
	"github.com/graphvein/graphvein/internal/extract/models/sqlmodel"
	"github.com/graphvein/graphvein/internal/extract/models/supabasetypes"
	"github.com/graphvein/graphvein/internal/extract/models/typeorm"
	"github.com/graphvein/graphvein/internal/extract/openapi"
	"github.com/graphvein/graphvein/internal/extract/packages"
	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/extract/schema"
	"github.com/graphvein/graphvein/internal/extract/tableaccess"
	"github.com/graphvein/graphvein/internal/extract/tauri"
	"github.com/graphvein/graphvein/internal/lang/besteffort"
	"github.com/graphvein/graphvein/internal/lang/csharp"
	"github.com/graphvein/graphvein/internal/lang/dart"
	langfacts "github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
	"github.com/graphvein/graphvein/internal/lang/java"
	"github.com/graphvein/graphvein/internal/lang/python"
	"github.com/graphvein/graphvein/internal/lang/rust"
	"github.com/graphvein/graphvein/internal/lang/template"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/precision"
	"github.com/graphvein/graphvein/internal/scan"
	"github.com/graphvein/graphvein/internal/store"
)

// Version invalidates cached facts when any language or asset semantics change.
const Version = "extractors-64"

func supported(language string) bool {
	return language == "typescript" || language == "tsx" || language == "javascript" || language == "go" || language == "python" || language == "rust" || language == "html" || bestEffort(language)
}

// Reads reports whether the built-in indexer parses files of language
// with a reader of its own. The couldn't-read page (ADR 0089) lists a file
// as "no fact stored" only when its language has no reader; other passes
// (packages, deploy files, OpenAPI specs) may still store facts from it.
func Reads(language string) bool { return supported(language) }

// ReadsPath reports whether a file at p would be parsed by a built-in
// reader judging by its name alone (the scanner's extension rule; a file
// without a known extension is not). The couldn't-read page uses it to
// put a source file that was not read first (row CR-36).
func ReadsPath(p string) bool { return supported(scan.Language(p, nil)) }

// CountsUnresolvedCalls reports whether the built-in reader of language
// counts the calls it could not resolve (the unresolved_calls diagnostic
// and the file node attributes unresolved_calls_external and
// unresolved_calls_unknown). Every file node carries both attributes, so a
// zero means "none" only for these languages.
func CountsUnresolvedCalls(language string) bool {
	switch language {
	case "typescript", "tsx", "javascript", "go", "python", "rust":
		return true
	}
	return false
}

// bestEffort reports the languages of the best-effort tier (T021, ADR 0067)
// that are extracted: their recovered trees claim nothing (row BE-2).
func bestEffort(language string) bool {
	return language == "java" || language == "c_sharp" || language == "dart"
}

// Options records resource bounds and a logical identity shared across checkouts.
type Options struct {
	Repository       string
	Parser           parse.Options
	MaxFileSize      int64
	GeneratedAt      string
	ExtractorVersion string
	// CacheFallback, when set, is a repository whose fact cache is read
	// before Repository's own. A ref run (ADR 0056) reads the main
	// repository's cache this way, so only files that differ from the
	// indexed working tree are parsed; it still writes only its own cache.
	CacheFallback string
	// ConfigRoot, when set, is the directory graphvein.yaml is read from
	// instead of root. A ref run whose commit has no graphvein.yaml uses the
	// working tree's file without adding it to the ref's files.
	ConfigRoot string
	// ShapeDepth, when nonzero, overrides graphvein.yaml index.shape_depth
	// (graphvein index --shape-depth). It must be within
	// routeshape.MinDepth..routeshape.MaxDepthLimit. It changes link
	// output only: no cached syntax depends on it.
	ShapeDepth int
	// Progress, when set, is called on the emitting goroutine as Emit enters
	// each phase and, in the countable phases, after each file. It must
	// return quickly. It never changes the output.
	Progress func(Progress)
}

// Emit's phases, in order. Linking runs until Emit returns, so it also
// covers saving the fact cache and writing the snapshot.
const (
	PhaseScanning = "scanning"
	PhaseCache    = "cache"
	PhaseParsing  = "parsing"
	PhaseLinking  = "linking"
)

// Progress is one report from Emit. Done and Total are both counted by the
// run itself, in files, and only when Counted is true (the cache and
// parsing phases); every other phase has no known total, and a consumer
// must not invent one (ADR 0052).
type Progress struct {
	Phase       string
	Done, Total int
	Counted     bool
}

// Summary distinguishes parser work from snapshot size and exposes phase timings.
type Summary struct {
	Languages                                                 map[string]int
	Files, Nodes, Edges, Diagnostics, Parsed, Reused, Dropped int
	Scan, Parse, Total                                        time.Duration
	// OpenAPI measures the link-time spec pass, including discovery and parsing.
	OpenAPI time.Duration
	// Schema measures the link-time migration pass (ADR 0047).
	Schema time.Duration
	// ShapeDepth is the call-following depth limit the run's route shape
	// extractors used (routeshape rows SM-21, SM-23): Options.ShapeDepth
	// when set, else graphvein.yaml index.shape_depth, else the default.
	// The extractors store it on every shape they write.
	ShapeDepth int
	// ParseFailed counts the files this run could not parse (parse_failed
	// diagnostics), after the serial retry; ParseTimedOut is those of them
	// whose parse ran past its deadline even when retried alone. Retried
	// counts the files the serial retry parsed again, Recovered those it
	// read (T074).
	ParseFailed, ParseTimedOut, Retried, Recovered int
	// Settings are the graphvein.yaml index inputs the run read, from the
	// same bytes as its configuration, with ShapeDepth as the shape depth
	// (ADR 0088, row ST-41). The run stores their record with its row.
	Settings config.IndexInputs
}

// Emit replays cache entries and relinks them into the unchanged v1 transport.
func Emit(ctx context.Context, root string, s store.Store, output io.Writer, options Options) (summary Summary, err error) {
	return emit(ctx, root, s, output, options, extractFile)
}

// The extractor argument permits fault injection in tests without a runtime switch.
func emit(ctx context.Context, root string, s store.Store, output io.Writer, options Options, extract func(string, []byte, *parse.Node, string) (typescript.Facts, error)) (summary Summary, err error) {
	return emitWithPipeline(ctx, root, s, output, options, extract, parse.Consume)
}

func emitWithPipeline(ctx context.Context, root string, s store.Store, output io.Writer, options Options, extract func(string, []byte, *parse.Node, string) (typescript.Facts, error), pipeline func(context.Context, []parse.Job, parse.Options, func(int, parse.Result)) error) (summary Summary, err error) {
	started := time.Now()
	configRoot := root
	if options.ConfigRoot != "" {
		configRoot = options.ConfigRoot
	}
	cfg, inputs, err := config.LoadIndexInputs(configRoot)
	if err != nil {
		return summary, err
	}
	summary.Languages = map[string]int{}
	summary.ShapeDepth = cfg.Index.ShapeDepth
	if options.ShapeDepth != 0 {
		if !routeshape.ValidDepth(options.ShapeDepth) {
			return summary, fmt.Errorf("shape depth %d is outside %d..%d", options.ShapeDepth, routeshape.MinDepth, routeshape.MaxDepthLimit)
		}
		summary.ShapeDepth = options.ShapeDepth
	}
	summary.Settings = inputs.WithShapeDepth(summary.ShapeDepth)
	if options.Repository == "" {
		return summary, fmt.Errorf("repository identifier is required")
	}
	if options.MaxFileSize <= 0 {
		options.MaxFileSize = 2 << 20
	}
	if options.ExtractorVersion == "" {
		options.ExtractorVersion = Version
	}
	version := fmt.Sprintf("%s/max=%d", options.ExtractorVersion, options.MaxFileSize)
	report := func(p Progress) {
		if options.Progress != nil {
			options.Progress(p)
		}
	}
	report(Progress{Phase: PhaseScanning})
	profileBoundary("scan")
	scanned, err := scan.Walk(ctx, root)
	summary.Scan = time.Since(started)
	if err != nil {
		return summary, err
	}
	facts := map[string]typescript.Facts{}
	templates := map[string]template.Facts{}
	templateSyntax := map[string]typescript.TemplateSyntax{}
	declarations := map[string]typescript.DeclarationSyntax{}
	injections := map[string]typescript.InjectionSyntax{}
	routers := map[string]typescript.RouterSyntax{}
	modelPayloads := map[string]*typescript.ModelSyntax{}
	diagnostics := scanned.Diagnostics
	fresh := map[string]bool{}
	dropped := map[string]contract.Diagnostic{}
	drop := func(file string, record any, err error) {
		if _, exists := dropped[file]; !exists {
			dropped[file] = contract.Diagnostic{Type: "diagnostic", File: file, Severity: "error", Code: "extract_invalid_record", Message: fmt.Sprintf("%s: %s: %s", file, recordLabel(record), boundedText(err.Error()))}
			summary.Dropped++
		}
		delete(facts, file)
	}
	var jobs []parse.Job
	var pending []int
	// Rust files row RS-F4 may read (rust.PatternAttributes), and their
	// indexes in scanned.Files.
	var rustRecoveries []*rust.Recovery
	var rustRecovered []int
	report(Progress{Phase: PhaseCache, Total: len(scanned.Files), Counted: true})
	for i, f := range scanned.Files {
		if i > 0 {
			report(Progress{Phase: PhaseCache, Done: i, Total: len(scanned.Files), Counted: true})
		}
		summary.Files++
		summary.Languages[f.Record.Language]++
		facts[f.Record.Path] = typescript.Facts{Exports: map[string]string{}}
		if !supported(f.Record.Language) {
			continue
		}
		if f.Record.Size > options.MaxFileSize {
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: f.Record.Path, Severity: "warning", Code: "file_too_large", Message: fmt.Sprintf("file exceeds %d byte parsing limit", options.MaxFileSize)})
			if isTypeScript(f.Record.Language) {
				facts[f.Record.Path] = typescript.NotParsed()
			}
			continue
		}
		var data []byte
		var e error
		if options.CacheFallback != "" {
			data, e = s.FactCache(ctx, options.CacheFallback, f.Record.Path, f.Record.ContentHash, version)
		}
		if e == nil && len(data) == 0 {
			data, e = s.FactCache(ctx, options.Repository, f.Record.Path, f.Record.ContentHash, version)
		}
		if e != nil {
			return summary, fmt.Errorf("reading fact cache for %s: %w", f.Record.Path, e)
		}
		var cached typescript.Facts
		var cachedTemplate template.Facts
		reused := false
		if f.Record.Language == "html" {
			if len(data) > 0 && json.Unmarshal(data, &cachedTemplate) == nil && cachedTemplate.Valid() {
				templates[f.Record.Path] = cachedTemplate
				reused = true
			}
		} else if isTypeScript(f.Record.Language) {
			var payload typescript.CachedFacts
			if len(data) > 0 && json.Unmarshal(data, &payload) == nil && payload.Exports != nil {
				if payload.TemplateSyntax.Valid() && payload.Declarations.Matches(payload.TemplateSyntax) && payload.Injections.Valid() && payload.Router.Valid() && (payload.Models == nil || payload.Models.Valid()) {
					facts[f.Record.Path] = payload.Facts
					templateSyntax[f.Record.Path] = *payload.TemplateSyntax
					declarations[f.Record.Path] = *payload.Declarations
					injections[f.Record.Path] = *payload.Injections
					routers[f.Record.Path] = *payload.Router
					if payload.Models != nil {
						modelPayloads[f.Record.Path] = payload.Models
					}
					reused = true
				} else if record, e := validateLocal(f, payload.Facts); e != nil {
					// Preserve per-file rejection of malformed graph records even
					// when additive syntax fields require a cache miss.
					drop(f.Record.Path, record, e)
					continue
				}
			}
		} else if len(data) > 0 && json.Unmarshal(data, &cached) == nil && cached.Exports != nil && cached.Tauri.Valid() && cached.Python.ModelsValid() && cached.Python.ShapeValid() && cached.Rust.QueriesValid() && cached.Rust.ShapeValid() && cached.Go.ModelsValid() && (f.Record.Language != "go" || cached.Go != nil && cached.Go.LoneCR == f.LoneCR) && (f.Record.Language != "rust" || cached.Rust != nil && cached.Rust.LoneCR == f.LoneCR) && (!bestEffort(f.Record.Language) || cached.BestEffort.Valid()) {
			facts[f.Record.Path] = cached
			reused = true
		}
		if reused {
			summary.Reused++
			profileCacheHit(f.Record.Path)
			continue
		}
		pending = append(pending, i)
		language := f.Record.Language
		if language == "html" {
			language = "angular"
		}
		jobs = append(jobs, parse.Job{Language: language, Source: f.Source})
	}
	report(Progress{Phase: PhaseCache, Done: len(scanned.Files), Total: len(scanned.Files), Counted: true})
	profileBoundary("parse+extract")
	parseStarted := time.Now()
	report(Progress{Phase: PhaseParsing, Total: len(jobs), Counted: true})
	if len(jobs) > 0 {
		e := pipeline(ctx, jobs, options.Parser, func(i int, result parse.Result) {
			f := scanned.Files[pending[i]]
			summary.Parsed++
			defer report(Progress{Phase: PhaseParsing, Done: summary.Parsed, Total: len(jobs), Counted: true})
			if result.Retried {
				summary.Retried++
				if result.Err == nil {
					summary.Recovered++
				}
			}
			if result.Err != nil {
				message := fmt.Sprintf("parsing %s: %v", f.Record.Path, result.Err)
				summary.ParseFailed++
				var timeout *parse.TimeoutError
				if errors.As(result.Err, &timeout) {
					summary.ParseTimedOut++
					if result.Retried {
						message += fmt.Sprintf(" (also when parsed again alone with a %s deadline)", timeout.Deadline)
					}
				}
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: f.Record.Path, Severity: "error", Code: "parse_failed", Message: message})
				if isTypeScript(f.Record.Language) {
					facts[f.Record.Path] = typescript.NotParsed()
				}
				return
			}
			if f.Record.Language == "html" {
				captured, e := captureHTML(f.Source, result.Root)
				if e != nil {
					diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: f.Record.Path, Severity: "error", Code: "extract_failed", Message: e.Error()})
					return
				}
				templates[f.Record.Path] = captured
				fresh[f.Record.Path] = true
				return
			}
			extracted, e := safeExtract(extract, f.Record.Path, f.Source, result.Root, f.Record.Language)
			if e != nil {
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: f.Record.Path, Severity: "error", Code: "extract_failed", Message: e.Error()})
				if isTypeScript(f.Record.Language) {
					facts[f.Record.Path] = typescript.NotParsed()
				}
				return
			}
			if f.Record.Language == "go" && f.LoneCR {
				// Row GS-F3: the text Graphvein parsed is not the text Go reads.
				extracted = golang.RefuseLoneCR(extracted)
			}
			if f.Record.Language == "rust" && (f.LoneCR || result.SyntaxError) {
				// Rows RS-F2, RS-F3: a file tree-sitter recovered, or whose
				// text Rust reads differently, claims nothing beyond its file.
				extracted = rust.Refuse(extracted, f.LoneCR)
				if !f.LoneCR {
					// Row RS-F4 may still read it, after this pass.
					if rec := rust.PatternAttributes(result.Root, f.Source); rec != nil {
						rustRecoveries = append(rustRecoveries, rec)
						rustRecovered = append(rustRecovered, pending[i])
					}
				}
			}
			if result.SyntaxError {
				extracted.Diagnostics = append(extracted.Diagnostics, contract.Diagnostic{Type: "diagnostic", File: f.Record.Path, Severity: "warning", Code: "syntax_error", Message: "tree-sitter recovered from invalid syntax"})
				if isTypeScript(f.Record.Language) {
					// Row TS-F2 (O-S3-1 (a)): nothing beyond the file node.
					extracted = typescript.RefuseSyntaxError(extracted)
				}
				if f.Record.Language == "python" {
					// Row PS-F2 of docs/lang/python-structure.md.
					extracted = python.RefuseSyntaxError(extracted, f.Record.Path)
				}
				if bestEffort(f.Record.Language) {
					// Row BE-2: the parser also reports MISSING tokens the
					// detached tree cannot show.
					extracted = besteffort.Refuse(extracted, refusedScope(f.Record.Language, f.Source))
				}
			}
			facts[f.Record.Path] = extracted
			if isTypeScript(f.Record.Language) {
				captured, e := captureTypeScript(result.Root, extracted.Nodes)
				if e != nil {
					// Metadata capture must not invalidate successfully extracted host facts.
					diagnostics = append(diagnostics, syntaxCaptureDiagnostic(f.Record.Path, e))
					return
				}
				templateSyntax[f.Record.Path] = captured
				declarationSyntax, e := captureDeclarationSyntax(result.Root, extracted, captured)
				if e != nil {
					diagnostics = append(diagnostics, syntaxCaptureDiagnostic(f.Record.Path, e))
					return
				}
				declarations[f.Record.Path] = declarationSyntax
				injectionSyntax, e := captureInjectionSyntax(result.Root, extracted)
				if e != nil {
					diagnostics = append(diagnostics, syntaxCaptureDiagnostic(f.Record.Path, e))
					return
				}
				injections[f.Record.Path] = injectionSyntax
				routerSyntax, e := captureRouterSyntax(result.Root, extracted, declarationSyntax)
				if e != nil {
					diagnostics = append(diagnostics, syntaxCaptureDiagnostic(f.Record.Path, e))
					return
				}
				routers[f.Record.Path] = routerSyntax
				modelSyntax, e := captureModelSyntax(f.Record.Path, result.Root, extracted)
				if e != nil {
					// One file's model capture failure keeps its other facts
					// and every other file; the file is not cached, so the
					// next run tries again.
					diagnostics = append(diagnostics, modelCaptureDiagnostic(f.Record.Path, e))
					return
				}
				if modelSyntax != nil {
					modelPayloads[f.Record.Path] = modelSyntax
				}
			}
			fresh[f.Record.Path] = true
		})
		if e != nil {
			return summary, e
		}
	}
	// Row RS-F4: Rust files whose only syntax errors are attributes on
	// struct pattern fields are parsed again with those attributes blanked,
	// after every first tree is released; a file the checks pass replaces
	// its RS-F2 refusal with the facts of the clean tree.
	if e := rust.Recover(ctx, rustRecoveries, func(ctx context.Context, jobs []parse.Job, consume func(int, parse.Result)) error {
		return pipeline(ctx, jobs, options.Parser, consume)
	}, func(i int, root *parse.Node) {
		f := scanned.Files[rustRecovered[i]]
		extracted, e := safeExtract(extract, f.Record.Path, f.Source, root, f.Record.Language)
		if e != nil {
			// The RS-F2 facts stay; the failure is the file's own.
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: f.Record.Path, Severity: "error", Code: "extract_failed", Message: e.Error()})
			return
		}
		facts[f.Record.Path] = extracted
	}); e != nil {
		return summary, e
	}
	// All host roots have been released and workers joined before any guest
	// parse. Only byte ranges and ordinary facts cross this phase boundary.
	if e := captureInline(ctx, scanned.Files, templateSyntax, fresh, options.Parser, pipeline); e != nil {
		return summary, e
	}
	profileBoundary("local-validation")
	summary.Parse = time.Since(parseStarted)
	report(Progress{Phase: PhaseLinking})
	for _, f := range scanned.Files {
		finish := profileReplayValidation(f.Record.Path)
		record, e := validateLocal(f, facts[f.Record.Path])
		if finish != nil {
			finish()
		}
		if e != nil {
			drop(f.Record.Path, record, e)
		}
	}
	// A file of a precise language whose structure claims nothing gets the
	// default label before its file node is built (ADR 0068, T020 S1L).
	for i := range scanned.Files {
		f := &scanned.Files[i]
		_, bad := dropped[f.Record.Path]
		if refusedStructure(*f, facts[f.Record.Path], bad) {
			f.Record.Precision = string(precision.BestEffort)
		}
	}
	profileBoundary("link")
	var external []contract.Node
	var edges []contract.Edge
	var linkedDiagnostics []contract.Diagnostic
	var angularAttributes map[string]map[string]any
	var tableAccessAttributes map[string]map[string]any
	fileNodes := map[string]contract.Node{}
	scannedPaths := map[string]bool{}
	for _, f := range scanned.Files {
		scannedPaths[f.Record.Path] = true
	}
	for {

		tsFiles := map[string]typescript.Facts{}
		goFiles := map[string]typescript.Facts{}
		pyFiles := map[string]typescript.Facts{}
		rsFiles := map[string]typescript.Facts{}
		javaFiles := map[string]typescript.Facts{}
		csFiles := map[string]typescript.Facts{}
		dartFiles := map[string]typescript.Facts{}
		sources := map[string][]byte{}
		for _, f := range scanned.Files {
			if _, bad := dropped[f.Record.Path]; bad {
				continue
			}
			sources[f.Record.Path] = f.Source
			// TS relative imports may point at visible, file-level assets such as JSON.
			tsFiles[f.Record.Path] = typescript.Facts{Exports: map[string]string{}}
			switch f.Record.Language {
			case "go":
				goFiles[f.Record.Path] = facts[f.Record.Path]
			case "python":
				pyFiles[f.Record.Path] = facts[f.Record.Path]
			case "rust":
				rsFiles[f.Record.Path] = facts[f.Record.Path]
			case "java":
				javaFiles[f.Record.Path] = facts[f.Record.Path]
			case "c_sharp":
				csFiles[f.Record.Path] = facts[f.Record.Path]
			case "dart":
				dartFiles[f.Record.Path] = facts[f.Record.Path]
			case "typescript", "tsx", "javascript":
				tsFiles[f.Record.Path] = facts[f.Record.Path]
			}
		}
		// A dropped file is on disk but unread, as the scanner's skipped
		// entries: a require that Node would resolve to one of them before
		// a scanned file gives no value (row CJ-U1).
		unreadPaths := append([]string(nil), scanned.Skipped...)
		for file := range dropped {
			unreadPaths = append(unreadPaths, file)
		}
		moduleResolver := typescript.NewModuleResolver(tsFiles, sources).WithUnread(unreadPaths)
		external, edges, linkedDiagnostics = typescript.LinkWithResolver(tsFiles, moduleResolver)
		routeNodes, routeEdges, routeDiagnostics := routes.LinkWithResolver(tsFiles, moduleResolver, cfg.Tests)
		external = append(external, routeNodes...)
		edges = append(edges, routeEdges...)
		linkedDiagnostics = append(linkedDiagnostics, routeDiagnostics...)
		// Next.js route handlers and Pages Router API files (ADR 0077) join
		// before any HTTP consumer reads the node list.
		nextNodes, nextEdges, nextDiagnostics := routes.LinkNext(tsFiles, moduleResolver, sources, cfg.Tests)
		// Express and Pages Router API request and response shape (T029
		// SH-E) attaches to the route nodes before anything reads them.
		linkedDiagnostics = append(linkedDiagnostics, routes.LinkShapes(routeNodes, tsFiles, moduleResolver, summary.ShapeDepth)...)
		linkedDiagnostics = append(linkedDiagnostics, routes.LinkShapes(nextNodes, tsFiles, moduleResolver, summary.ShapeDepth)...)
		external = append(external, nextNodes...)
		edges = append(edges, nextEdges...)
		linkedDiagnostics = append(linkedDiagnostics, nextDiagnostics...)
		goExternal, goEdges, goDiagnostics := golang.Link(goFiles, golang.ModulePaths(sources))
		// Go routes (ADR 0076) read the cached router payload and go.mod.
		var goRouteForms map[string]map[string]int
		goRoutes, goRouteEdges, goRouteDiagnostics := linkGuarded("Go routes", func() ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
			nodes, edges, diagnostics, forms := routes.LinkGoCounted(goFiles, sources, summary.ShapeDepth, cfg.Tests)
			goRouteForms = forms
			return nodes, edges, diagnostics
		})
		goExternal = append(goExternal, goRoutes...)
		goEdges = append(goEdges, goRouteEdges...)
		goDiagnostics = append(goDiagnostics, goRouteDiagnostics...)
		pythonResolver := python.NewModuleResolver(pyFiles, sources).WithSkipped(unreadPaths).WithDirectories(scanned.Dirs)
		pyExternal, pyEdges, pyDiagnostics := python.LinkWithResolver(pyFiles, pythonResolver)
		pyRoutes, pyRouteEdges, pyRouteDiagnostics := routes.LinkFastAPI(pyFiles, pythonResolver, summary.ShapeDepth, cfg.Tests)
		pyExternal = append(pyExternal, pyRoutes...)
		pyEdges = append(pyEdges, pyRouteEdges...)
		pyDiagnostics = append(pyDiagnostics, pyRouteDiagnostics...)
		rustResolver := rust.NewModuleResolver(rsFiles, sources)
		rsExternal, rsEdges, rsDiagnostics := rust.LinkWithResolver(rsFiles, rustResolver)
		rsDiagnostics = append(rsDiagnostics, labelUnrooted(scanned.Files, rsFiles, rustResolver)...)
		rsRoutes, rsRouteEdges, rsRouteDiagnostics := routes.LinkAxumShapes(rsFiles, rustResolver, sources, summary.ShapeDepth, cfg.Tests)
		rsExternal = append(rsExternal, rsRoutes...)
		rsEdges = append(rsEdges, rsRouteEdges...)
		rsDiagnostics = append(rsDiagnostics, rsRouteDiagnostics...)
		external = append(append(external, goExternal...), pyExternal...)
		external = append(external, rsExternal...)
		// Java (T021, best effort) links imports only; nothing else reads it.
		javaExternal, javaEdges, javaDiagnostics := java.Link(javaFiles)
		external = append(external, javaExternal...)
		// C# (T021, best effort) links usings only (never file edges).
		csExternal, csEdges, csDiagnostics := csharp.Link(csFiles)
		external = append(external, csExternal...)
		// Dart (T021, best effort) links directives only; pubspec.yaml
		// names are read anew on every link, like go.mod module lines.
		dartExternal, dartEdges, dartDiagnostics := dart.Link(dartFiles, dart.PackageNames(sources))
		external = append(external, dartExternal...)
		// Package manifests (T026, ADR 0079) are read from the retained
		// sources and the cached Python and Go facts; no linker reads them.
		// They join the node list after every other linker has read it.
		packageNodes, packageDiagnostics := packages.Link(packages.Input{Sources: sources, Python: pyFiles, Go: goFiles})
		linkedDiagnostics = append(linkedDiagnostics, packageDiagnostics...)
		// Deploy configuration (T026, ADR 0079) is read per file
		// from the retained sources; like package nodes, its nodes join the
		// list after every other linker has read it.
		deployNodes, deployDiagnostics := deploy.Link(sources)
		linkedDiagnostics = append(linkedDiagnostics, deployDiagnostics...)
		openapiStarted := time.Now()
		specNodes, specEdges, specDiagnostics := openapi.Link(sources, cfg.OpenAPI, cfg.Tests, openapi.LinkOptions{Matcher: clients.NewRouteMatcher(tsFiles, moduleResolver, external), BasePaths: cfg.OpenAPIBasePaths,
			// Mapped shape items from linked committed operations (T029
			// SH-O) join the route shapes every framework stored above.
			Routes: external, ShapeDepth: summary.ShapeDepth})
		summary.OpenAPI += time.Since(openapiStarted)
		external = append(external, specNodes...)
		edges = append(edges, specEdges...)
		linkedDiagnostics = append(linkedDiagnostics, specDiagnostics...)
		clientNodes, clientEdges, clientDiagnostics := clients.LinkWithRoot(tsFiles, moduleResolver, external, edges, clients.Options{BasePaths: cfg.Clients.BasePaths, LinkGeneratedOutput: cfg.Clients.LinkGeneratedOutput}, sources, root, cfg.Tests)
		linkedDiagnostics = append(linkedDiagnostics, clientDiagnostics...)
		// What a call provably sends (T029 W4, rows SD-1..SD-5 of
		// docs/contract/claims/shape-differences.md); the server compares
		// it with the linked route's shape.
		linkedDiagnostics = append(linkedDiagnostics, routes.LinkClientSends(clientNodes, tsFiles, moduleResolver)...)
		external = append(external, clientNodes...)
		edges = append(edges, clientEdges...)
		attrs, angularNodes, angularEdges, angularDiagnostics := angular.Link(tsFiles, moduleResolver, templateSyntax, declarations, templates, sources, edges, root, cfg.Tests, injections, routers)
		angularAttributes = attrs
		edges = append(edges, angularEdges...)
		linkedDiagnostics = append(linkedDiagnostics, angularDiagnostics...)
		orvalNodes, orvalEdges, orvalDiagnostics := clients.LinkOrval(tsFiles, moduleResolver, sources, specNodes, external, root, cfg.Tests)
		external = append(external, orvalNodes...)
		// ui_route nodes join after every HTTP consumer has read the node list.
		external = append(external, angularNodes...)
		// JSX renders, passed functions and react-router routes (T026)
		// read the cached bridge syntax through the module resolver.
		bridgeNodes, bridgeEdges := typescript.LinkBridges(tsFiles, moduleResolver)
		external = append(external, bridgeNodes...)
		edges = append(edges, bridgeEdges...)
		edges = append(edges, orvalEdges...)
		linkedDiagnostics = append(linkedDiagnostics, orvalDiagnostics...)
		// Tauri IPC nodes are a separate transport; no HTTP consumer reads them.
		tauriNodes, tauriEdges, tauriDiagnostics := tauri.Link(tauri.Input{Rust: rsFiles, Resolver: rustResolver, TypeScript: tsFiles, Modules: moduleResolver, Sources: sources, Tests: cfg.Tests})
		external = append(external, tauriNodes...)
		edges = append(edges, tauriEdges...)
		linkedDiagnostics = append(linkedDiagnostics, tauriDiagnostics...)
		// Server actions (T024b, ADR 0077) are a third transport; no HTTP
		// consumer reads them.
		actionNodes, actionEdges, actionDiagnostics := actions.Link(actions.Input{TypeScript: tsFiles, Modules: moduleResolver, Sources: sources, Tests: cfg.Tests})
		// Server action arguments and results (T029 SH-N27..SH-N29) use the
		// route shape model on the server_action node.
		actionDiagnostics = append(actionDiagnostics, routes.LinkShapes(actionNodes, tsFiles, moduleResolver, summary.ShapeDepth)...)
		external = append(external, actionNodes...)
		edges = append(edges, actionEdges...)
		linkedDiagnostics = append(linkedDiagnostics, actionDiagnostics...)
		// Schema records come from migration sources; no other linker reads them.
		schemaStarted := time.Now()
		schemaNodes, schemaEdges, schemaDiagnostics := schema.Link(schema.Input{Sources: sources, Root: root, Tests: cfg.Tests})
		summary.Schema += time.Since(schemaStarted)
		external = append(external, schemaNodes...)
		edges = append(edges, schemaEdges...)
		linkedDiagnostics = append(linkedDiagnostics, schemaDiagnostics...)
		// Model tables (ADR 0054) compare with, never merge into, migration sets.
		modelNodes, modelEdges, modelDiagnostics := sqlmodel.Link(sqlmodel.Input{Python: pyFiles, Resolver: pythonResolver, Sources: sources, Tests: cfg.Tests, Migrations: schemaNodes, Edges: schemaEdges})
		external = append(external, modelNodes...)
		edges = append(edges, modelEdges...)
		linkedDiagnostics = append(linkedDiagnostics, modelDiagnostics...)
		// Prisma models (ADR 0057) read the retained .prisma, package.json,
		// lockfile and config sources; like SQLModel they never merge into
		// migration sets.
		prismaNodes, prismaEdges, prismaDiagnostics := prisma.Link(prisma.Input{Sources: sources, Migrations: schemaNodes, Edges: schemaEdges})
		external = append(external, prismaNodes...)
		edges = append(edges, prismaEdges...)
		linkedDiagnostics = append(linkedDiagnostics, prismaDiagnostics...)
		// Drizzle models (ADR 0059) read the cached TypeScript model
		// payloads through the module resolver, never re-parsing a file.
		drizzleNodes, drizzleEdges, drizzleDiagnostics := drizzle.Link(drizzle.Input{TypeScript: tsFiles, Models: modelPayloads, Resolver: moduleResolver, Sources: sources, Migrations: schemaNodes, Edges: schemaEdges})
		external = append(external, drizzleNodes...)
		edges = append(edges, drizzleEdges...)
		linkedDiagnostics = append(linkedDiagnostics, drizzleDiagnostics...)
		// Sequelize models (ADR 0061) read the same cached payloads and the
		// retained package.json, lockfile and config sources.
		sequelizeNodes, sequelizeEdges, sequelizeDiagnostics := sequelize.Link(sequelize.Input{TypeScript: tsFiles, Models: modelPayloads, Resolver: moduleResolver, Sources: sources, Tests: cfg.Tests, Migrations: schemaNodes, Edges: schemaEdges})
		external = append(external, sequelizeNodes...)
		edges = append(edges, sequelizeEdges...)
		linkedDiagnostics = append(linkedDiagnostics, sequelizeDiagnostics...)
		// Diesel models (ADR 0062) read the retained diesel.toml, Cargo and
		// schema.rs sources at link time; like the others they never merge
		// into migration sets.
		dieselNodes, dieselEdges, dieselDiagnostics := safeDiesel(diesel.Input{Sources: sources, Migrations: schemaNodes, Edges: schemaEdges})
		external = append(external, dieselNodes...)
		edges = append(edges, dieselEdges...)
		linkedDiagnostics = append(linkedDiagnostics, dieselDiagnostics...)
		// GORM models (ADR 0063) read the cached Go model payloads and the
		// retained go.mod sources, never re-parsing a file.
		gormNodes, gormEdges, gormDiagnostics := safeGorm(gorm.Input{Files: goFiles, Sources: sources, Migrations: schemaNodes, Edges: schemaEdges})
		external = append(external, gormNodes...)
		edges = append(edges, gormEdges...)
		linkedDiagnostics = append(linkedDiagnostics, gormDiagnostics...)
		// TypeORM entities (ADR 0064) read the same cached payloads, the
		// resolver's tsconfig chain and the retained package.json and
		// lockfile sources.
		// A panic outside its own isolated steps becomes a diagnostic
		// (Revision 4 C1).
		typeormNodes, typeormEdges, typeormDiagnostics := linkGuarded("TypeORM", func() ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
			return typeorm.Link(typeorm.Input{TypeScript: tsFiles, Models: modelPayloads, Resolver: moduleResolver, Sources: sources, Tests: cfg.Tests, Migrations: schemaNodes, Edges: schemaEdges})
		})
		external = append(external, typeormNodes...)
		edges = append(edges, typeormEdges...)
		linkedDiagnostics = append(linkedDiagnostics, typeormDiagnostics...)
		// Supabase generated types (T024c3, ADR 0077) read the retained .ts
		// sources; a package with a Supabase migration set keeps the set as
		// its schema source and the types are only compared with it.
		typesNodes, typesEdges, typesDiagnostics := linkGuarded("Supabase generated types", func() ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
			return supabasetypes.Link(supabasetypes.Input{Sources: sources, Migrations: schemaNodes, Edges: schemaEdges})
		})
		external = append(external, typesNodes...)
		edges = append(edges, typesEdges...)
		linkedDiagnostics = append(linkedDiagnostics, typesDiagnostics...)
		// Code-to-table links (T018h, ADR 0060) read the cached query
		// payloads and the schema records. Edges are plain v1 edges; the
		// per-edge data and the unknown sites are owner-node attributes.
		withheld := typescript.RejectedOwners(tsFiles)
		tableAccess := safeTableAccess(tableaccess.Input{Owners: withheld, Rust: rsFiles, Python: pyFiles, Resolver: pythonResolver, Sources: sources, Schema: schemaNodes, SchemaEdges: schemaEdges, Models: modelNodes, TypeScript: tsFiles, Prisma: prismaNodes, Drizzle: drizzleNodes, DrizzleModels: modelPayloads, Modules: moduleResolver, SupabaseTypes: typesNodes})
		edges = append(edges, tableAccess.Edges...)
		tableAccessAttributes = tableAccess.Attributes
		linkedDiagnostics = append(linkedDiagnostics, tableAccess.Diagnostics...)
		edges = reanchorWithheld(edges, withheld)
		external = append(external, packageNodes...)
		external = append(external, deployNodes...)
		// Model-type edges (T029 W2, docs/contract/claims/route-model-types.md):
		// a route's model uses whose declaration node is emitted become
		// uses_model edges; the others are dropped from the route.
		edges = append(edges, routeshape.LinkModelUses(external, declarationNodes(scanned.Files, facts, dropped))...)
		sort.Slice(external, func(i, j int) bool { return external[i].Identity < external[j].Identity })
		edges = append(append(edges, goEdges...), pyEdges...)
		edges = append(edges, rsEdges...)
		edges = append(edges, javaEdges...)
		edges = append(edges, csEdges...)
		edges = append(edges, dartEdges...)
		sort.Slice(edges, func(i, j int) bool {
			a, b := edges[i], edges[j]
			if a.Kind != b.Kind {
				return a.Kind < b.Kind
			}
			if a.Source != b.Source {
				return a.Source < b.Source
			}
			return a.Target < b.Target
		})
		linkedDiagnostics = append(append(linkedDiagnostics, goDiagnostics...), pyDiagnostics...)
		linkedDiagnostics = append(linkedDiagnostics, rsDiagnostics...)
		linkedDiagnostics = append(linkedDiagnostics, javaDiagnostics...)
		linkedDiagnostics = append(linkedDiagnostics, csDiagnostics...)
		linkedDiagnostics = append(linkedDiagnostics, dartDiagnostics...)
		before := len(dropped)
		identities := map[string]bool{}
		for _, f := range scanned.Files {
			if _, bad := dropped[f.Record.Path]; bad {
				continue
			}

			fileNode := typescript.FileNode(f.Record, f.Source)
			ext, unknown := 0, 0
			for _, d := range linkedDiagnostics {
				if d.File == f.Record.Path && d.Code == "unresolved_calls" {
					var total int
					fmt.Sscanf(d.Message, "%d calls could not be resolved without a type checker (external=%d, unknown=%d)", &total, &ext, &unknown)
				}
			}
			sites := []langfacts.UnknownSite{}
			for _, inventory := range []map[string]typescript.Facts{tsFiles, goFiles, pyFiles, rsFiles} {
				sites = append(sites, inventory[f.Record.Path].UnknownSites...)
			}
			sort.SliceStable(sites, func(i, j int) bool {
				a, b := sites[i].Span, sites[j].Span
				if a.StartLine != b.StartLine {
					return a.StartLine < b.StartLine
				}
				return a.StartColumn < b.StartColumn
			})
			fileNode.Attributes["unresolved_unknown_call_sites"] = sites
			fileNode.Attributes["unresolved_calls_external"] = ext
			fileNode.Attributes["unresolved_calls_unknown"] = unknown
			// The counts of the file's go_routes_unread diagnostic, from the
			// linker result itself (couldn't-read row CR-38).
			if forms := goRouteForms[f.Record.Path]; len(forms) > 0 {
				fileNode.Attributes["go_route_forms_outside_contract"] = forms
			}
			fileNodes[f.Record.Path] = fileNode
			identities[fileNode.Identity] = true
			for _, n := range emittedNodes(f, facts[f.Record.Path]) {
				identities[n.Identity] = true
			}
			if e := validateRecord(fileNode); e != nil {
				drop(f.Record.Path, fileNode, e)
			}
		}
		// Linker-created nodes belong to each source file referencing them.
		for _, n := range external {
			identities[n.Identity] = true
			if e := validateRecord(n); e != nil {
				for _, edge := range edges {
					if edge.Target == n.Identity || edge.Source == n.Identity {
						for _, evidence := range edge.Evidence {
							drop(evidence.File, n, e)
						}
					}
				}
				if len(dropped) == before {
					return summary, fmt.Errorf("linking synthetic node %s: %w", n.Identity, e)
				}
			}
		}
		for _, edge := range edges {
			e := validateRecord(edge)
			if e == nil && (!identities[edge.Source] || !identities[edge.Target]) {
				e = fmt.Errorf("linked edge endpoints must reference emitted nodes")
			}
			if e != nil {
				for _, evidence := range edge.Evidence {
					drop(evidence.File, edge, e)
				}
				if len(edge.Evidence) == 0 {
					_, file, _, _ := contract.ParseIdentity(edge.Source)
					drop(file, edge, e)
				}
			}
		}
		for _, d := range linkedDiagnostics {
			// A linker diagnostic the contract rejects drops the file it names,
			// but only a scanned file: a path outside the inventory (".", "",
			// absolute) has nothing to drop, and checkedDiagnostic replaces
			// the diagnostic itself at emission so it never aborts the run.
			if e := validateRecord(d); e != nil && scannedPaths[d.File] {
				drop(d.File, d, e)
			}
		}
		if len(dropped) == before {
			break
		}
	}
	profileBoundary("cache-save")
	// Only validated, successfully linked files are eligible for replay.
	for _, f := range scanned.Files {
		if !fresh[f.Record.Path] {
			continue
		}
		if _, bad := dropped[f.Record.Path]; bad {
			continue
		}
		var cached any = facts[f.Record.Path]
		if f.Record.Language == "html" {
			cached = templates[f.Record.Path]
		} else if isTypeScript(f.Record.Language) {
			syntax := templateSyntax[f.Record.Path]
			d := declarations[f.Record.Path]
			injection := injections[f.Record.Path]
			router := routers[f.Record.Path]
			cached = typescript.CachedFacts{Facts: facts[f.Record.Path], TemplateSyntax: &syntax, Declarations: &d, Injections: &injection, Router: &router, Models: modelPayloads[f.Record.Path]}
		}
		data, e := json.Marshal(cached)
		if e != nil {
			return summary, fmt.Errorf("encoding fact cache for %s: %w", f.Record.Path, e)
		}
		if e := s.SaveFactCache(ctx, options.Repository, f.Record.Path, f.Record.ContentHash, version, data); e != nil {
			return summary, fmt.Errorf("saving fact cache for %s: %w", f.Record.Path, e)
		}
	}

	diagnostics = append(diagnostics, linkedDiagnostics...)
	profileBoundary("wire-emit")
	writer := contract.NewWriter(output)
	if options.GeneratedAt == "" {
		options.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := writer.Write(contract.Header{Type: "header", ContractVersion: contract.Version, Indexer: contract.Indexer{Name: "graphvein", Version: options.ExtractorVersion}, Repository: options.Repository, GeneratedAt: options.GeneratedAt}); err != nil {
		return summary, err
	}
	for _, f := range scanned.Files {
		d, bad := dropped[f.Record.Path]
		if refusedStructure(f, facts[f.Record.Path], bad) {
			// Dropped while linking: only the record is written.
			f.Record.Precision = string(precision.BestEffort)
		}
		if err := writer.Write(f.Record); err != nil {
			return summary, err
		}
		if bad {
			diagnostics = append(diagnostics, d)
			continue
		}
		fileNode := fileNodes[f.Record.Path]
		nodes := append([]contract.Node{fileNode}, emittedNodes(f, facts[f.Record.Path])...)
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
		for _, n := range nodes {
			angularAttrs, angularOK := angularAttributes[n.Identity]
			accessAttrs, accessOK := tableAccessAttributes[n.Identity]
			if angularOK || accessOK {
				copy := map[string]any{}
				for key, value := range n.Attributes {
					copy[key] = value
				}
				if angularOK {
					copy["angular"] = angularAttrs
				}
				for key, value := range accessAttrs {
					copy[key] = value
				}
				n.Attributes = copy
			}
			if err := writer.Write(n); err != nil {
				return summary, err
			}
			summary.Nodes++
		}
		diagnostics = append(diagnostics, facts[f.Record.Path].Diagnostics...)
	}
	for _, n := range external {
		if err := writer.Write(n); err != nil {
			return summary, err
		}
		summary.Nodes++
	}
	for _, e := range edges {
		if err := writer.Write(e); err != nil {
			return summary, err
		}
		summary.Edges++
	}
	for i := range diagnostics {
		diagnostics[i] = checkedDiagnostic(diagnostics[i])
	}
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].File != diagnostics[j].File {
			return diagnostics[i].File < diagnostics[j].File
		}
		return diagnostics[i].Code < diagnostics[j].Code
	})
	for _, d := range diagnostics {
		if err := writer.Write(d); err != nil {
			return summary, err
		}
		summary.Diagnostics++
	}
	err = writer.Close()
	summary.Total = time.Since(started)
	return summary, err
}

// gormLink is gorm.Link; tests replace it to check the recover guard in
// safeGorm.
var gormLink = gorm.Link

// safeGorm runs the GORM model pass so that a failure outside its
// per-module and per-table guards drops only the GORM records, as one
// diagnostic, never the index run (T018g5 Revision 2, C1).
func safeGorm(in gorm.Input) (nodes []contract.Node, edges []contract.Edge, diags []contract.Diagnostic) {
	defer func() {
		if v := recover(); v != nil {
			nodes, edges = nil, nil
			diags = []contract.Diagnostic{{Type: "diagnostic", Severity: "warning", Code: "schema_model_internal_error",
				Message: fmt.Sprintf("GORM models could not be linked: %v", v)}}
		}
	}()
	return gormLink(in)
}

// tableAccessLink is tableaccess.Link; tests replace it to check the
// recover guard in safeTableAccess.
var tableAccessLink = tableaccess.Link

// safeTableAccess runs the code-to-table link pass so that a failure outside
// its per-file guards (a group or aggregation step) drops only its edges and
// attributes, as one diagnostic, never the index run (R5-B3).
func safeTableAccess(in tableaccess.Input) (out tableaccess.Output) {
	defer func() {
		if v := recover(); v != nil {
			out = tableaccess.Output{Diagnostics: []contract.Diagnostic{{Type: "diagnostic", Severity: "warning", Code: tableaccess.InternalErrorCode,
				Message: fmt.Sprintf("table access could not be linked: %v", v)}}}
		}
	}()
	return tableAccessLink(in)
}

// dieselLink is the Diesel model pass; a test swaps it to check the
// recover guard in safeDiesel.
var dieselLink = diesel.Link

// safeDiesel runs the Diesel model pass so that a failure outside its own
// per-step guards drops only its records, as one diagnostic, never the
// index run (ADR 0062 Revision 2, C).
func safeDiesel(in diesel.Input) (nodes []contract.Node, edges []contract.Edge, diags []contract.Diagnostic) {
	defer func() {
		if v := recover(); v != nil {
			nodes, edges = nil, nil
			diags = []contract.Diagnostic{{Type: "diagnostic", Severity: "warning", Code: "schema_model_internal_error",
				Message: fmt.Sprintf("Diesel models could not be linked: %v", v)}}
		}
	}()
	return dieselLink(in)
}

func safeExtract(extract func(string, []byte, *parse.Node, string) (typescript.Facts, error), file string, source []byte, root *parse.Node, language string) (facts typescript.Facts, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("extracting %s: %v", file, v)
		}
	}()
	return extract(file, source, root, language)
}

// refusedScope is the BE-8 scope of a best-effort file refused for the
// parser's syntax flag, read from its text by the language's scanner.
func refusedScope(language string, source []byte) besteffort.Scope {
	switch language {
	case "java":
		return java.RefusedScope(source)
	case "c_sharp":
		return csharp.RefusedScope(source)
	}
	return besteffort.Scope{}
}

func extractFile(file string, source []byte, root *parse.Node, language string) (facts typescript.Facts, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("extracting %s: %v", file, v)
		}
	}()
	switch language {
	case "go":
		facts = golang.Extract(file, source, root)
	case "python":
		facts = python.Extract(file, source, root)
	case "rust":
		facts = rust.Extract(file, source, root)
	case "java":
		facts = java.Extract(file, source, root)
	case "c_sharp":
		facts = csharp.Extract(file, source, root)
	case "dart":
		facts = dart.Extract(file, source, root)
	default:
		facts = typescript.Extract(file, source, root)
	}
	return facts, nil
}

// Freshness includes added, modified and deleted files, using the same ignore rules.
func Freshness(ctx context.Context, root, repo string, s store.Store) ([]string, error) {
	changed, _, err := FreshnessWithHashes(ctx, root, repo, s)
	return changed, err
}

// FreshnessWithHashes also returns the stored hash of every indexed file, which
// IDE presentation compares with current source. Indexed hashes come from file
// records, including external snapshots without file nodes, and are read from
// the files table's hash column rather than by decoding the whole graph.
func FreshnessWithHashes(ctx context.Context, root, repo string, s store.Store) ([]string, map[string]string, error) {
	scanned, err := scan.Walk(ctx, root)
	if err != nil {
		return nil, nil, err
	}
	// Only a walk failure makes the scan incomplete; an entry the walk
	// deliberately skips (scan.DeliberateSkip: a symlink, an image, a FIFO,
	// a non-UTF-8 file) is not indexed either, so it cannot be stale.
	for _, d := range scanned.Diagnostics {
		if !scan.DeliberateSkip(d.Code) {
			return nil, nil, fmt.Errorf("freshness scan incomplete: %s", d.Message)
		}
	}
	old, err := s.FileHashes(ctx, repo)
	if err != nil {
		return nil, nil, err
	}
	changed := []string{}
	seen := map[string]bool{}
	for _, f := range scanned.Files {
		if old[f.Record.Path] != f.Record.ContentHash {
			changed = append(changed, f.Record.Path)
		}
		seen[f.Record.Path] = true
	}
	for p := range old {
		if !seen[p] {
			changed = append(changed, p)
		}
	}
	sort.Strings(changed)
	return changed, old, nil
}

// linkGuarded runs one model front end's Link and turns a panic that
// escapes the front end's own isolated steps into a
// schema_model_internal_error diagnostic, so that front end's failure
// never stops the index or the other linkers (AGENTS.md: a failure while
// processing one part must not stop the others).
func linkGuarded(tool string, link func() ([]contract.Node, []contract.Edge, []contract.Diagnostic)) (nodes []contract.Node, edges []contract.Edge, diags []contract.Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			nodes, edges = nil, nil
			diags = []contract.Diagnostic{{Type: "diagnostic", File: ".", Severity: "warning", Code: "schema_model_internal_error", Message: fmt.Sprintf("the %s model front end could not be linked: %v", tool, r)}}
		}
	}()
	return link()
}

// declarationNodes maps the identity of every emitted source node of a
// kind a model declaration can have to its node (row MT-3).
func declarationNodes(files []scan.File, payloads map[string]typescript.Facts, dropped map[string]contract.Diagnostic) map[string]*contract.Node {
	out := map[string]*contract.Node{}
	for _, f := range files {
		if _, bad := dropped[f.Record.Path]; bad {
			continue
		}
		nodes := emittedNodes(f, payloads[f.Record.Path])
		for i := range nodes {
			if routeshape.DeclarationKind(nodes[i].Kind) {
				out[nodes[i].Identity] = &nodes[i]
			}
		}
	}
	return out
}

// emittedNodes are the declaration nodes a file's facts claim: none for a
// TypeScript or JavaScript file tree-sitter rejects (row TS-F2), whose
// facts the family extractors still read.
func emittedNodes(f scan.File, payload typescript.Facts) []contract.Node {
	if isTypeScript(f.Record.Language) && typescript.Rejected(payload) {
		return nil
	}
	return payload.Nodes
}

// reanchorWithheld applies row TS-F2 to the edges the family linkers
// built over a rejected file's facts: withheld maps each declaration the
// indexer does not emit to its file node (typescript.RejectedOwners). An
// edge from such a declaration (an owner relation: a client call it
// contains, a table it reads) is the file's; an edge to one (a handler, an
// injected class) cannot be restated for the file and is not emitted.
// Edges that become one (kind, source, target) merge their evidence, the
// strongest provenance and confidence kept.
func reanchorWithheld(edges []contract.Edge, withheld map[string]string) []contract.Edge {
	if len(withheld) == 0 {
		return edges
	}
	out := make([]contract.Edge, 0, len(edges))
	merged := map[[3]string]int{}
	for _, e := range edges {
		if _, ok := withheld[e.Target]; ok {
			continue
		}
		to, ok := withheld[e.Source]
		if !ok {
			if _, seen := merged[[3]string{e.Kind, e.Source, e.Target}]; !seen {
				merged[[3]string{e.Kind, e.Source, e.Target}] = len(out)
			}
			out = append(out, e)
			continue
		}
		e.Source = to
		key := [3]string{e.Kind, e.Source, e.Target}
		i, seen := merged[key]
		if !seen {
			merged[key] = len(out)
			out = append(out, e)
			continue
		}
		m := &out[i]
		m.Evidence = mergeEvidence(m.Evidence, e.Evidence)
		if e.Provenance == "fact" {
			m.Provenance = "fact"
		}
		if e.Confidence > m.Confidence {
			m.Confidence = e.Confidence
		}
	}
	return out
}

// mergeEvidence joins two evidence lists in source order without repeats.
func mergeEvidence(a, b []contract.Evidence) []contract.Evidence {
	all := append(append([]contract.Evidence(nil), a...), b...)
	sort.SliceStable(all, func(i, j int) bool {
		x, y := all[i], all[j]
		if x.File != y.File {
			return x.File < y.File
		}
		if x.Span.StartLine != y.Span.StartLine {
			return x.Span.StartLine < y.Span.StartLine
		}
		return x.Span.StartColumn < y.Span.StartColumn
	})
	out := all[:0]
	for i, e := range all {
		if i == 0 || e != all[i-1] {
			out = append(out, e)
		}
	}
	return out
}

// refusedStructure reports whether a file the scanner labelled precise
// (its language has a PL-L row of the precision rule table) claims no
// structure in this run, so its stored label must fall to best_effort: a
// file label promises the language's claim contract for the file, and a
// file whose structure was refused has nothing that contract covers. The
// server serves such a label with reason structure_refused.
//
// For Go (row PL-L1): a file dropped by validation, a file without a Go
// payload (not parsed: too large or a parse failure; not extracted; the
// payload capture failed), a file go/parser rejects (GS-F2) or a file with
// a lone CR (GS-F3). For Python (row PL-L2): a file dropped by
// validation, a file without a Python payload (not parsed or not
// extracted) or a file the reference Python rejects (PS-F2). For
// TypeScript, TSX and JavaScript (row PL-L3): a file dropped by
// validation, a file tree-sitter rejects (TS-F2) or one not parsed (over
// the size limit, a parse failure, an extraction failure; TS-F3). For Rust
// (row PL-L4): a file dropped by validation, a file without a Rust payload
// (not parsed or not extracted), a file in which tree-sitter reports a
// syntax error (RS-F2) or with a lone CR (RS-F3); labelUnrooted adds Rust
// files reached from no crate root.
func refusedStructure(f scan.File, payload typescript.Facts, dropped bool) bool {
	if f.Record.Precision != string(precision.Precise) {
		return false
	}
	if dropped {
		return true
	}
	switch f.Record.Language {
	case "go":
		return payload.Go == nil || payload.Go.Rejected || payload.Go.LoneCR
	case "python":
		return payload.Python == nil || payload.Python.Rejected
	case "rust":
		return payload.Rust == nil || payload.Rust.Rejected || payload.Rust.LoneCR
	}
	if isTypeScript(f.Record.Language) {
		// Row PL-L3: a file tree-sitter rejects (TS-F2) or one not parsed
		// or extracted (TS-F3).
		return typescript.Rejected(payload) || typescript.Unparsed(payload)
	}
	return false
}

// labelUnrooted lowers to best_effort the label of every Rust file of a
// precise language (row PL-L4) whose structure is claimed but which no
// crate root of a read Cargo.toml reaches, and returns one info diagnostic
// structure_unrooted per such file, which the server reads to serve the
// label with that reason (ADR 0071, T020 S4L). Such a file's declarations
// are claimed, but its imports are not resolved as a crate member and no
// oracle configuration compiles it, so its file label promises nothing the
// evidence covers. It runs on every pass of the link loop, before the file
// nodes are built, and depends only on that pass's inventory, so a later
// pass (more dropped files) recomputes the same diagnostics.
func labelUnrooted(files []scan.File, rsFiles map[string]typescript.Facts, r *rust.ModuleResolver) []contract.Diagnostic {
	if precision.FileLabel("rust") != string(precision.Precise) {
		return nil
	}
	var out []contract.Diagnostic
	for i := range files {
		f := &files[i]
		payload, ok := rsFiles[f.Record.Path]
		if f.Record.Language != "rust" || !ok || payload.Rust == nil || payload.Rust.Rejected || payload.Rust.LoneCR || !r.Unrooted(f.Record.Path) {
			continue
		}
		f.Record.Precision = string(precision.BestEffort)
		out = append(out, contract.Diagnostic{Type: "diagnostic", File: f.Record.Path, Severity: "info", Code: "structure_unrooted", Message: "no crate root of a read Cargo.toml reaches this file: its declarations are read, its imports are not resolved as a crate member, and its file label is best_effort (precision row PL-L4)"})
	}
	return out
}
