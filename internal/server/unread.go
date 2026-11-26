package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	routepaths "github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/openapi"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/testscope"
)

// This file serves GET /api/v0/unread, the "What Graphvein couldn't read"
// page (T040, ADR 0089) under the claim contract
// docs/contract/claims/couldnt-read.md (rows CR-1..CR-43 and D). Every part
// comes from the stored index; a part another endpoint serves is computed
// by that endpoint's code (the explorer's lists from generateDocument, the
// call status from statusOf, the compact shape from routeShapeSummary, the
// scanner and not-read codes shared with the schema views).

// unreadPath is the couldn't-read endpoint.
const unreadPath = "/api/v0/unread"

// List bounds (row CR-4).
const (
	unreadDefaultLimit = 200
	unreadMaxLimit     = 2000
)

// Reasons a part is not available.
const (
	reasonNotIndexed      = "not_indexed"
	reasonExternalIndexer = "external_indexer"
	reasonInvalidConfig   = "invalid_config"
)

// Config sources of the test rules (rows CR-18, CR-30).
const (
	configRepository = "repository"
	configDefaults   = "defaults"
	configError      = "error"
)

// builtinIndexer is the indexer name of Graphvein's own runs.
const builtinIndexer = "graphvein"

// Diagnostic codes the page reads besides fileNotReadCodes and the
// scanner's codes.
const (
	codeSyntaxError    = "syntax_error"
	codeDynamicPattern = "route_dynamic_pattern"
	codeDynamicMethods = "route_dynamic_methods"
	codeGoRoutesUnread = "go_routes_unread"
	// Rows CR-40..CR-43 (ADR 0089 Revision 2).
	codeRecordDropped   = "extract_invalid_record"
	codeGoRoutesFailed  = "go_routes_failed"
	codeMountUnresolved = "route_mount_unresolved"
	codeTSStructure     = "ts_structure_unread"
	statusDynamicCall   = "dynamic"
	statusWrapperCall   = "wrapper"
	fileAttrExternal    = "unresolved_calls_external"
	fileAttrUnknown     = "unresolved_calls_unknown"
)

// unreadRow is one row of the claim contract; unreadRows and the document
// are kept 1:1 by TestUnreadContractTable, and each row names the test
// that checks it.
type unreadRow struct {
	ID    string
	Topic string
	Test  string
}

// unreadRows is the claim contract in table order.
var unreadRows = []unreadRow{
	{"CR-1", "scope", "TestUnreadScope"},
	{"CR-2", "built-in run", "TestUnreadExternalRun"},
	{"CR-3", "not indexed", "TestUnreadNotIndexed"},
	{"CR-4", "lists", "TestUnreadLimit"},
	{"CR-5", "items", "TestUnreadSynthetic"},
	{"CR-6", "routes without a path", "TestUnreadFixtureGolden"},
	{"CR-7", "unknown mount hops", "TestUnreadFixtureGolden"},
	{"CR-8", "document not built", "TestUnreadInvalidConfig"},
	{"CR-9", "patterns built at run time", "TestUnreadSynthetic"},
	{"CR-10", "router forms not read", "TestUnreadSynthetic"},
	{"CR-11", "dynamic client calls", "TestUnreadSynthetic"},
	{"CR-12", "wrapper client calls", "TestUnreadSynthetic"},
	{"CR-13", "call items", "TestUnreadSynthetic"},
	{"CR-14", "routes without a shape", "TestUnreadSynthetic"},
	{"CR-15", "sections not read", "TestUnreadSynthetic"},
	{"CR-16", "shape items", "TestUnreadSynthetic"},
	{"CR-17", "depth caps hit", "TestUnreadSynthetic"},
	{"CR-18", "file test split", "TestUnreadScope"},
	{"CR-19", "files not read", "TestUnreadSynthetic"},
	{"CR-20", "read with syntax errors", "TestUnreadSynthetic"},
	{"CR-21", "scanner gaps", "TestUnreadSynthetic"},
	{"CR-22", "files without facts", "TestUnreadSynthetic"},
	{"CR-23", "files with a reader", "TestUnreadSynthetic"},
	{"CR-24", "counted languages", "TestUnreadSynthetic"},
	{"CR-25", "unresolved call totals", "TestUnreadSynthetic"},
	{"CR-26", "counted files", "TestUnreadSynthetic"},
	{"CR-27", "call items", "TestUnreadSynthetic"},
	{"CR-28", "member", "TestUnreadProject"},
	{"CR-29", "project scope", "TestUnreadProject"},
	{"CR-30", "no checkout", "TestUnreadProject"},
	{"CR-31", "sidebar total", "TestUnreadWording"},
	{"CR-32", "wording", "TestUnreadWording"},
	{"CR-33", "everything else", "TestUnreadSynthetic"},
	{"CR-34", "scanner skips", "TestUnreadScannerSkips"},
	{"CR-35", "deliberate exclusions", "TestUnreadScannerSkips"},
	{"CR-36", "file categories", "TestUnreadCategories"},
	{"CR-37", "category counts", "TestUnreadCategories"},
	{"CR-38", "router form counts", "TestUnreadSynthetic"},
	{"CR-39", "in-process reader", "TestUnreadReader"},
	{"CR-40", "records dropped", "TestUnreadGapDiagnostics"},
	{"CR-41", "route recognition failed", "TestUnreadGapDiagnostics"},
	{"CR-42", "mounts not resolved", "TestUnreadGapDiagnostics"},
	{"CR-43", "structure not read", "TestUnreadGapDiagnostics"},
	{"CR-44", "route paths withheld", "TestUnreadGapDiagnostics"},
	{"D", "default", "TestUnreadSynthetic"},
}

// Stored file gaps (rows CR-19..CR-21), shared with the search answer's
// searched counts (row JS-13).

// scannerGapCodes are the scanner's not-indexed codes other than
// file_read, which the page lists as a file not read (scanNotRead holds
// every scanner code).
func scannerGapCodes() []string {
	var out []string
	for code := range scanNotRead {
		if !fileNotReadCodes[code] {
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

// fileGaps are the stored file-level diagnostics of the page, in stored
// order.
type fileGaps struct {
	notRead []contract.Diagnostic
	syntax  []contract.Diagnostic
	scanner []contract.Diagnostic
	// dropped and structure are rows CR-40 and CR-43.
	dropped   []contract.Diagnostic
	structure []contract.Diagnostic
}

func readFileGaps(rd *reader, hashes map[string]string) (fileGaps, error) {
	codes := append(sortedKeys(fileNotReadCodes), codeSyntaxError)
	codes = append(codes, scannerGapCodes()...)
	codes = append(codes, codeRecordDropped, codeTSStructure)
	diagnostics, err := rd.s.DiagnosticsWithCodes(rd.ctx, rd.repo, codes, nil)
	if err != nil {
		return fileGaps{}, fmt.Errorf("reading file diagnostics: %w", err)
	}
	var out fileGaps
	for _, d := range diagnostics {
		switch {
		case fileNotReadCodes[d.Code]:
			out.notRead = append(out.notRead, d)
		case d.Code == codeSyntaxError:
			if _, stored := hashes[d.File]; stored {
				out.syntax = append(out.syntax, d)
			}
		case d.Code == codeRecordDropped:
			out.dropped = append(out.dropped, d)
		case d.Code == codeTSStructure:
			out.structure = append(out.structure, d)
		default:
			out.scanner = append(out.scanner, d)
		}
	}
	return out, nil
}

// notReadFiles are the distinct files of the not-read diagnostics.
func (g fileGaps) notReadFiles() []string {
	seen := map[string]bool{}
	for _, d := range g.notRead {
		seen[d.File] = true
	}
	return sortedKeys(seen)
}

// Diagnostic message spans (row CR-9). The route linkers write
// "<expression> at L:C-L:C" (docs/extract/routes.md: contract v1
// diagnostics have no span field).
var messageSpan = regexp.MustCompile(`(?s)^(.*) at (\d+):(\d+)-(\d+):(\d+)$`)

// parseMessageSpan reads the expression and span of a route diagnostic
// message; ok is false for any other form.
func parseMessageSpan(message string) (expression string, span contract.Span, ok bool) {
	m := messageSpan.FindStringSubmatch(message)
	if m == nil {
		return "", contract.Span{}, false
	}
	var n [4]int
	for i := range n {
		v, err := strconv.Atoi(m[i+2])
		if err != nil || v < 0 {
			return "", contract.Span{}, false
		}
		n[i] = v
	}
	if n[0] < 1 || n[2] < n[0] {
		return "", contract.Span{}, false
	}
	return m[1], contract.Span{StartLine: n[0], StartColumn: n[1], EndLine: n[2], EndColumn: n[3]}, true
}

// Response bodies.

// unreadPart says whether a part could be computed (rows CR-2, CR-3,
// CR-8).
type unreadPart struct {
	Available bool    `json:"available"`
	Reason    *string `json:"reason"`
	Error     *string `json:"error,omitempty"`
}

func availablePart() unreadPart { return unreadPart{Available: true} }

func missingPart(reason string) unreadPart { return unreadPart{Reason: &reason} }

// unreadList is a bounded list (row CR-4).
type unreadList[T any] struct {
	Total     int  `json:"total"`
	Truncated bool `json:"truncated"`
	Items     []T  `json:"items"`
}

func listOf[T any](items []T, limit int) unreadList[T] {
	out := unreadList[T]{Total: len(items), Items: items}
	if out.Items == nil {
		out.Items = []T{}
	}
	if len(out.Items) > limit {
		out.Items, out.Truncated = out.Items[:limit], true
	}
	return out
}

// unreadResponse is one repository's page.
type unreadResponse struct {
	APIVersion int       `json:"api_version"`
	Repository string    `json:"repository"`
	Scope      string    `json:"scope"`
	Limit      int       `json:"limit"`
	IDE        ideStatus `json:"ide"`
	Index      *lastRun  `json:"index"`
	BuiltinRun bool      `json:"builtin_run"`
	// Config is where the test rules come from: repository, defaults (no
	// checkout here) or error (graphvein.yaml does not load).
	Config      string  `json:"config"`
	ConfigError *string `json:"config_error"`
	// Counts are every count of the page by name, null where the part is
	// not available; no count adds units (row CR-31).
	Counts      map[string]*int   `json:"counts"`
	RoutePaths  unreadRoutePaths  `json:"route_paths"`
	ClientCalls unreadClientCalls `json:"client_calls"`
	Shapes      unreadShapes      `json:"shapes"`
	DepthCaps   unreadDepthCaps   `json:"depth_caps"`
	Files       unreadFiles       `json:"files"`
	CodeCalls   unreadCodeCalls   `json:"code_calls"`
}

// unreadRoutePaths is rows CR-6..CR-10.
type unreadRoutePaths struct {
	// Document is the generated document's state (rows CR-6..CR-8): its
	// lists are production routes, whatever the scope.
	Document    unreadPart                  `json:"document"`
	Unplaced    unreadList[unplacedRoute]   `json:"unplaced"`
	Mounts      unreadList[unreadMount]     `json:"mounts"`
	Dynamic     unreadList[dynamicPattern]  `json:"dynamic"`
	RouterForms unreadList[routerFormsFile] `json:"router_forms"`
	// Failed is the stored go_routes_failed diagnostics (row CR-41): Go
	// route recognition or linking failed, so those routes are not known.
	Failed unreadList[dynamicPattern] `json:"failed"`
	// MountsUnresolved is the stored route_mount_unresolved diagnostics
	// of mounts (row CR-42): a mount whose router is not known claims no
	// path.
	MountsUnresolved unreadList[dynamicPattern] `json:"mounts_unresolved"`
	// RoutesWithheld is the stored route_mount_unresolved diagnostics of
	// route registrations (row CR-44): the route is found, but its path is
	// withheld because another pattern or route may answer it.
	RoutesWithheld unreadList[dynamicPattern] `json:"routes_withheld"`
	// Forms is the number of forms over the router form files whose file
	// node holds the structured counts (row CR-38); FormFilesNotCounted is
	// the number of files without them. Both null when not indexed.
	Forms               *int `json:"forms"`
	FormFilesNotCounted *int `json:"form_files_not_counted"`
}

type unplacedRoute struct {
	openapi.Unresolved
	// PrefixUnknown is the route's unknown-prefix path as the endpoint
	// list shows it ("…/api/x", row CR-6), or null when the stored route
	// has none: the page never shows the stored pattern as its path.
	PrefixUnknown *routePrefixView `json:"prefix_unknown"`
	IDE           *query.IDELink   `json:"ide"`
}

type unreadMount struct {
	openapi.Mount
	IDE *query.IDELink `json:"ide"`
}

type dynamicPattern struct {
	Code       string         `json:"code"`
	File       string         `json:"file"`
	Line       *int           `json:"line"`
	Expression *string        `json:"expression"`
	Message    string         `json:"message"`
	IDE        *query.IDELink `json:"ide"`
}

type routerFormsFile struct {
	File    string `json:"file"`
	Message string `json:"message"`
	// Forms and ByForm are the file node's structured counts (row CR-38),
	// null when the node holds none.
	Forms  *int           `json:"forms"`
	ByForm map[string]int `json:"by_form"`
	IDE    *query.IDELink `json:"ide"`
}

// unreadClientCalls is rows CR-11..CR-13.
type unreadClientCalls struct {
	Dynamic unreadList[unreadCall] `json:"dynamic"`
	Wrapper unreadList[unreadCall] `json:"wrapper"`
}

type unreadCall struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Status string         `json:"status"`
	Owner  *ownerRef      `json:"owner"`
	File   string         `json:"file"`
	Line   *int           `json:"line"`
	IDE    *query.IDELink `json:"ide"`
}

// unreadShapes is rows CR-14..CR-16.
type unreadShapes struct {
	Routes       int                     `json:"routes"`
	WithoutShape unreadList[unreadRoute] `json:"without_shape"`
	ShapeInvalid unreadList[unreadRoute] `json:"shape_invalid"`
	// Sections counts, per section, the routes whose stored shape has it
	// not read.
	Sections        map[string]int          `json:"sections"`
	SectionsNotRead unreadList[unreadRoute] `json:"sections_not_read"`
}

type unreadRoute struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Method   *string        `json:"method"`
	File     string         `json:"file"`
	Line     *int           `json:"line"`
	Sections []string       `json:"sections,omitempty"`
	DepthCap *int           `json:"depth_cap,omitempty"`
	Limit    *int           `json:"depth_limit,omitempty"`
	IDE      *query.IDELink `json:"ide"`
}

// unreadDepthCaps is row CR-17.
type unreadDepthCaps struct {
	Routes unreadList[unreadRoute] `json:"routes"`
}

// unreadFiles is rows CR-18..CR-23.
type unreadFiles struct {
	// ScopeApplied is false when the test rules are unknown (row CR-18).
	ScopeApplied bool                      `json:"scope_applied"`
	NotRead      unreadNotRead             `json:"not_read"`
	SyntaxErrors unreadList[unreadFile]    `json:"syntax_errors"`
	ScannerGaps  unreadList[unreadScanGap] `json:"scanner_gaps"`
	WithoutFacts unreadWithoutFacts        `json:"without_facts"`
	// RecordsDropped is the files with a stored extract_invalid_record
	// diagnostic (row CR-40); StructureOutside the files with a stored
	// ts_structure_unread diagnostic (row CR-43), declarations or imports outside the structural claim contract.
	RecordsDropped   unreadList[unreadFile] `json:"records_dropped"`
	StructureOutside unreadList[unreadFile] `json:"structure_outside_contract"`
}

// unreadNotRead is the files not read (row CR-19) with their category
// counts (row CR-37).
type unreadNotRead struct {
	unreadList[unreadFile]
	ByCategory []categoryCount `json:"by_category"`
}

type unreadFile struct {
	File string `json:"file"`
	// Category is the file name's category (row CR-36).
	Category string `json:"category"`
	// Stored is false for a file_read path: the scanner found it and could
	// not read it, so the store has no record of it.
	Stored  bool           `json:"stored"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Test    *bool          `json:"test"`
	IDE     *query.IDELink `json:"ide"`
}

type unreadScanGap struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type unreadWithoutFacts struct {
	unreadPart
	ByExtension []extensionCount              `json:"by_extension"`
	ByCategory  []categoryCount               `json:"by_category"`
	Files       unreadList[unreadWithoutFact] `json:"files"`
}

type extensionCount struct {
	Extension string `json:"extension"`
	Count     int    `json:"count"`
}

type unreadWithoutFact struct {
	File      string         `json:"file"`
	Extension string         `json:"extension"`
	Category  string         `json:"category"`
	Language  string         `json:"language"`
	Test      *bool          `json:"test"`
	IDE       *query.IDELink `json:"ide"`
}

// unreadCodeCalls is rows CR-24..CR-27.
type unreadCodeCalls struct {
	unreadPart
	Languages       []string                   `json:"languages"`
	External        *int                       `json:"external"`
	Unknown         *int                       `json:"unknown"`
	FilesCounted    *int                       `json:"files_counted"`
	FilesNotCounted *int                       `json:"files_not_counted"`
	Files           unreadList[unresolvedFile] `json:"files"`
}

type unresolvedFile struct {
	File     string         `json:"file"`
	External int            `json:"external"`
	Unknown  int            `json:"unknown"`
	Test     *bool          `json:"test"`
	IDE      *query.IDELink `json:"ide"`
}

// unreadProjectResponse is the project-scope page (row CR-29).
type unreadProjectResponse struct {
	APIVersion int             `json:"api_version"`
	Project    string          `json:"project"`
	Scope      string          `json:"scope"`
	Limit      int             `json:"limit"`
	Totals     map[string]*int `json:"totals"`
	// NotAvailable names, per count, the members it is not available
	// for; their project total is null.
	NotAvailable map[string][]string `json:"not_available"`
	Members      []unreadResponse    `json:"members"`
}

// Parameters.

type unreadRequest struct {
	scope  string
	limit  int
	editor *editorChoice
}

func parseUnread(q url.Values) (unreadRequest, error) {
	var out unreadRequest
	var err error
	if out.scope, err = scopeParam(q); err != nil {
		return out, err
	}
	out.limit = unreadDefaultLimit
	limit, present, err := param(q, "limit")
	if err != nil {
		return out, err
	}
	if present {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 1 || n > unreadMaxLimit {
			return out, badRequest("limit must be an integer from 1 to %d, not %q", unreadMaxLimit, limit)
		}
		out.limit = n
	}
	out.editor, err = editorParam(q)
	return out, err
}

// Computing one repository's page.

// unreadRepository runs one repository's page inside the API's
// consistency check.
func unreadRepository(ctx context.Context, opt Options, req unreadRequest) (unreadResponse, error) {
	h := &apiHandler{opt: opt}
	body, err := h.consistent(ctx, func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		return unreadRead(ctx, h, rd, runs, req)
	})
	if err != nil {
		return unreadResponse{}, err
	}
	return body.(unreadResponse), nil
}

// emptyUnread is a page with every list empty and every part missing for
// reason.
func emptyUnread(repository string, req unreadRequest, reason string) unreadResponse {
	return unreadResponse{
		Repository: repository, Scope: req.scope, Limit: req.limit, Config: configRepository,
		Counts: map[string]*int{},
		RoutePaths: unreadRoutePaths{Document: missingPart(reason), Unplaced: listOf[unplacedRoute](nil, req.limit), Mounts: listOf[unreadMount](nil, req.limit),
			Dynamic: listOf[dynamicPattern](nil, req.limit), RouterForms: listOf[routerFormsFile](nil, req.limit),
			Failed: listOf[dynamicPattern](nil, req.limit), MountsUnresolved: listOf[dynamicPattern](nil, req.limit), RoutesWithheld: listOf[dynamicPattern](nil, req.limit)},
		ClientCalls: unreadClientCalls{Dynamic: listOf[unreadCall](nil, req.limit), Wrapper: listOf[unreadCall](nil, req.limit)},
		Shapes: unreadShapes{WithoutShape: listOf[unreadRoute](nil, req.limit), ShapeInvalid: listOf[unreadRoute](nil, req.limit),
			Sections: map[string]int{}, SectionsNotRead: listOf[unreadRoute](nil, req.limit)},
		DepthCaps: unreadDepthCaps{Routes: listOf[unreadRoute](nil, req.limit)},
		Files: unreadFiles{NotRead: unreadNotRead{unreadList: listOf[unreadFile](nil, req.limit), ByCategory: []categoryCount{}}, SyntaxErrors: listOf[unreadFile](nil, req.limit), ScannerGaps: listOf[unreadScanGap](nil, req.limit),
			WithoutFacts:   unreadWithoutFacts{unreadPart: missingPart(reason), ByExtension: []extensionCount{}, ByCategory: []categoryCount{}, Files: listOf[unreadWithoutFact](nil, req.limit)},
			RecordsDropped: listOf[unreadFile](nil, req.limit), StructureOutside: listOf[unreadFile](nil, req.limit)},
		CodeCalls: unreadCodeCalls{unreadPart: missingPart(reason), Languages: countedLanguages(), Files: listOf[unresolvedFile](nil, req.limit)},
	}
}

// countedLanguages lists the languages whose unresolved calls are
// counted (row CR-24).
func countedLanguages() []string {
	var out []string
	for _, l := range []string{"go", "javascript", "python", "rust", "tsx", "typescript"} {
		if index.CountsUnresolvedCalls(l) {
			out = append(out, l)
		}
	}
	return out
}

func unreadRead(ctx context.Context, h *apiHandler, rd *reader, runs []store.Run, req unreadRequest) (unreadResponse, error) {
	out := emptyUnread(h.opt.Repository, req, reasonNotIndexed)
	hashes, err := rd.fileHashes()
	if err != nil {
		return out, err
	}
	p := h.newPresenter(hashes, req.editor)
	out.IDE = p.ide
	out.Index = indexOf(runs)
	if out.Index == nil {
		// Row CR-3: nothing is counted as zero.
		out.Counts = countsOf(out)
		return out, nil
	}
	out.BuiltinRun = out.Index.Indexer.Name == builtinIndexer
	// Test rules (rows CR-18, CR-30).
	var rules *testscope.Rules
	switch {
	case h.opt.noCheckout:
		out.Config = configDefaults
		d := config.Defaults().Tests
		rules = &d
	default:
		cfg, err := h.opt.loadConfig()
		if err != nil {
			msg := err.Error()
			out.Config, out.ConfigError = configError, &msg
		} else {
			rules = &cfg.Tests
		}
	}
	testOf := func(file string) *bool {
		if rules == nil {
			return nil
		}
		t := testscope.Classify(file, *rules) == testscope.Test
		return &t
	}
	fileInScope := func(file string) bool {
		// A run-level diagnostic (no file) belongs to every scope.
		if file == "" {
			return true
		}
		t := testOf(file)
		if t == nil {
			return true
		}
		switch req.scope {
		case scopeProduction:
			return !*t
		case scopeTest:
			return *t
		}
		return true
	}
	out.Files.ScopeApplied = rules != nil
	// Links: a stored node's own link, or a link built from a file and a
	// span read from a diagnostic (rows CR-5, CR-9).
	at := func(file string, span contract.Span) *query.IDELink {
		if _, stored := hashes[file]; !stored || span.StartLine < 1 {
			return nil
		}
		s := span
		return p.node(contract.Node{File: file, Span: &s}).IDE
	}
	ideOf := func(n contract.Node) *query.IDELink { return p.node(n).IDE }

	// Route paths (rows CR-6..CR-10).
	out.RoutePaths.Document = availablePart()
	doc, err := generateDocument(ctx, h.opt)
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		out.RoutePaths.Document = missingPart(reasonInvalidConfig)
		out.RoutePaths.Document.Error = &ae.message
	case err != nil:
		return out, err
	default:
		ids := make([]string, 0, len(doc.X.Unresolved))
		for _, u := range doc.X.Unresolved {
			ids = append(ids, u.Route)
		}
		routeNodes, err := rd.byIdentity(ids)
		if err != nil {
			return out, err
		}
		var unplaced []unplacedRoute
		for _, u := range doc.X.Unresolved {
			item := unplacedRoute{Unresolved: u, IDE: at(u.File, contract.Span{StartLine: u.Line, EndLine: u.Line})}
			// The document holds production routes (CR-6), so the
			// unknown-prefix path is the production one.
			if n, ok := routeNodes[u.Route]; ok {
				item.PrefixUnknown = routePrefix(n, scopeProduction)
			}
			unplaced = append(unplaced, item)
		}
		out.RoutePaths.Unplaced = listOf(unplaced, req.limit)
		var mounts []unreadMount
		for _, m := range doc.Mounts {
			mounts = append(mounts, unreadMount{Mount: m, IDE: at(m.File, contract.Span{StartLine: m.Line, EndLine: m.Line})})
		}
		out.RoutePaths.Mounts = listOf(mounts, req.limit)
	}
	routeDiagnostics, err := rd.s.DiagnosticsWithCodes(rd.ctx, rd.repo, []string{codeDynamicMethods, codeDynamicPattern, codeGoRoutesUnread, codeGoRoutesFailed, codeMountUnresolved}, nil)
	if err != nil {
		return out, fmt.Errorf("reading route diagnostics: %w", err)
	}
	// The sites of the CR-44 registrations, for the route labels below.
	withheldSites := routepaths.WithheldSites(routeDiagnostics)
	var dynamic, failed, unresolvedMounts, withheld []dynamicPattern
	var forms []routerFormsFile
	for _, d := range routeDiagnostics {
		if d.Code == codeGoRoutesUnread {
			if fileInScope(d.File) {
				forms = append(forms, routerFormsFile{File: d.File, Message: d.Message, IDE: at(d.File, contract.Span{StartLine: 1, EndLine: 1})})
			}
			continue
		}
		if !fileInScope(d.File) {
			continue
		}
		item := dynamicPattern{Code: d.Code, File: d.File, Message: d.Message}
		// Row CR-41: a go_routes_failed message has no span form; it is
		// kept verbatim (CR-9's parser is for the linkers' span form).
		if d.Code == codeGoRoutesFailed {
			failed = append(failed, item)
			continue
		}
		if expression, span, ok := parseMessageSpan(d.Message); ok {
			line := span.StartLine
			item.Line, item.Expression, item.IDE = &line, &expression, at(d.File, span)
		}
		if d.Code == codeMountUnresolved {
			// Rows CR-42 and CR-44: one code, two writers, told apart by
			// the registration's message form.
			if routepaths.IsWithheldRegistration(d) {
				withheld = append(withheld, item)
			} else {
				unresolvedMounts = append(unresolvedMounts, item)
			}
			continue
		}
		dynamic = append(dynamic, item)
	}
	for _, list := range [][]dynamicPattern{dynamic, failed, unresolvedMounts, withheld} {
		sort.SliceStable(list, func(i, j int) bool {
			return lessFileLine(list[i].File, list[i].Line, list[j].File, list[j].Line)
		})
	}
	out.RoutePaths.Failed, out.RoutePaths.MountsUnresolved = listOf(failed, req.limit), listOf(unresolvedMounts, req.limit)
	out.RoutePaths.RoutesWithheld = listOf(withheld, req.limit)
	sort.SliceStable(forms, func(i, j int) bool { return forms[i].File < forms[j].File })
	// Row CR-38: the structured counts of the files' nodes, never the
	// message.
	formTotal, formFilesNotCounted, err := routerFormCounts(rd, forms)
	if err != nil {
		return out, err
	}
	out.RoutePaths.Forms, out.RoutePaths.FormFilesNotCounted = &formTotal, &formFilesNotCounted
	out.RoutePaths.Dynamic, out.RoutePaths.RouterForms = listOf(dynamic, req.limit), listOf(forms, req.limit)

	// Client calls (rows CR-11..CR-13).
	calls, err := rd.nodes(httpTransport.CallKind)
	if err != nil {
		return out, err
	}
	var gapCalls []contract.Node
	for _, c := range calls {
		if s := statusOf(c); inScope(c, req.scope) && (s == statusDynamicCall || s == statusWrapperCall) {
			gapCalls = append(gapCalls, c)
		}
	}
	sort.Slice(gapCalls, func(i, j int) bool { return lessBySite(gapCalls[i], gapCalls[j]) })
	owners, err := rd.owners(identities(gapCalls))
	if err != nil {
		return out, err
	}
	var dynamicCalls, wrapperCalls []unreadCall
	for _, c := range gapCalls {
		item := unreadCall{ID: c.Identity, Name: c.Name, Status: statusOf(c), Owner: owners[c.Identity], File: c.File, Line: lineOf(c), IDE: ideOf(c)}
		if item.Status == statusDynamicCall {
			dynamicCalls = append(dynamicCalls, item)
		} else {
			wrapperCalls = append(wrapperCalls, item)
		}
	}
	out.ClientCalls.Dynamic, out.ClientCalls.Wrapper = listOf(dynamicCalls, req.limit), listOf(wrapperCalls, req.limit)

	// Shapes and depth caps (rows CR-14..CR-17).
	routes, err := rd.nodes(httpTransport.EndpointKind)
	if err != nil {
		return out, err
	}
	var scoped []contract.Node
	for _, n := range routes {
		if inScope(n, req.scope) {
			scoped = append(scoped, n)
		}
	}
	sort.Slice(scoped, func(i, j int) bool { return lessBySite(scoped[i], scoped[j]) })
	out.Shapes.Routes = len(scoped)
	for _, name := range routeshape.SectionNames {
		out.Shapes.Sections[name] = 0
	}
	var without, invalid, sections, capped []unreadRoute
	for _, n := range scoped {
		row := unreadRoute{ID: n.Identity, Label: routeLabel(n, withheldSites), Method: stringAttr(n, "method"), File: n.File, Line: lineOf(n), IDE: ideOf(n)}
		shape, shapeErr := routeShape(n)
		switch {
		case shape == nil && shapeErr == nil:
			without = append(without, row)
			continue
		case shape == nil:
			invalid = append(invalid, row)
			continue
		}
		sum := routeshape.Summarize(shape)
		for _, name := range routeshape.SectionNames {
			if sum.Sections[name].State == routeshape.NotRead {
				out.Shapes.Sections[name]++
				row.Sections = append(row.Sections, name)
			}
		}
		if len(row.Sections) > 0 {
			sections = append(sections, row)
		}
		if sum.DepthCap > 0 {
			c, l := sum.DepthCap, sum.DepthLimit
			capRow := row
			capRow.Sections, capRow.DepthCap, capRow.Limit = nil, &c, &l
			capped = append(capped, capRow)
		}
	}
	out.Shapes.WithoutShape, out.Shapes.ShapeInvalid = listOf(without, req.limit), listOf(invalid, req.limit)
	out.Shapes.SectionsNotRead = listOf(sections, req.limit)
	out.DepthCaps.Routes = listOf(capped, req.limit)

	// Files (rows CR-18..CR-23).
	gaps, err := readFileGaps(rd, hashes)
	if err != nil {
		return out, err
	}
	records, err := rd.s.Files(rd.ctx, rd.repo)
	if err != nil {
		return out, fmt.Errorf("reading file records: %w", err)
	}
	fileNodeIDs := map[string]string{}
	for _, f := range records {
		if id, err := contract.Identity("file", f.Path, f.Path); err == nil {
			fileNodeIDs[f.Path] = id
		}
	}
	// File links come from the stored file nodes of the files shown.
	var linked []string
	fileItem := func(d contract.Diagnostic) unreadFile {
		_, stored := hashes[d.File]
		if stored {
			linked = append(linked, fileNodeIDs[d.File])
		}
		return unreadFile{File: d.File, Category: fileCategory(d.File), Stored: stored, Code: d.Code, Message: d.Message, Test: testOf(d.File)}
	}
	var notRead, syntax, dropped, structure []unreadFile
	for _, d := range gaps.dropped {
		if fileInScope(d.File) {
			dropped = append(dropped, fileItem(d))
		}
	}
	for _, d := range gaps.structure {
		if fileInScope(d.File) {
			structure = append(structure, fileItem(d))
		}
	}
	for _, list := range [][]unreadFile{dropped, structure} {
		sort.SliceStable(list, func(i, j int) bool { return list[i].File < list[j].File })
	}
	for _, d := range gaps.notRead {
		if fileInScope(d.File) {
			notRead = append(notRead, fileItem(d))
		}
	}
	for _, d := range gaps.syntax {
		if fileInScope(d.File) {
			syntax = append(syntax, fileItem(d))
		}
	}
	sort.SliceStable(notRead, func(i, j int) bool { return notRead[i].File < notRead[j].File })
	sort.SliceStable(syntax, func(i, j int) bool { return syntax[i].File < syntax[j].File })
	var scanner []unreadScanGap
	for _, d := range gaps.scanner {
		scanner = append(scanner, unreadScanGap{Path: d.File, Code: d.Code, Message: d.Message})
	}
	out.Files.ScannerGaps = listOf(scanner, req.limit)

	// Files without facts and unresolved calls need a built-in run (row
	// CR-2).
	gapFile := map[string]bool{}
	for _, d := range gaps.notRead {
		gapFile[d.File] = true
	}
	for _, d := range gaps.syntax {
		gapFile[d.File] = true
	}
	var withoutFacts []unreadWithoutFact
	var unresolved []unresolvedFile
	if !out.BuiltinRun {
		out.Files.WithoutFacts.unreadPart = missingPart(reasonExternalIndexer)
		out.CodeCalls.unreadPart = missingPart(reasonExternalIndexer)
	} else {
		facts, err := rd.s.FactFiles(rd.ctx, rd.repo)
		if err != nil {
			return out, fmt.Errorf("reading fact files: %w", err)
		}
		out.Files.WithoutFacts.unreadPart = availablePart()
		byExt := map[string]int{}
		var counted []string
		notCounted := 0
		for _, f := range records {
			if !fileInScope(f.Path) {
				continue
			}
			if !index.Reads(f.Language) && !facts[f.Path] {
				ext := strings.ToLower(path.Ext(f.Path))
				byExt[ext]++
				withoutFacts = append(withoutFacts, unreadWithoutFact{File: f.Path, Extension: ext, Category: fileCategory(f.Path), Language: f.Language, Test: testOf(f.Path)})
			}
			if index.CountsUnresolvedCalls(f.Language) {
				if gapFile[f.Path] {
					notCounted++
					continue
				}
				counted = append(counted, f.Path)
			}
		}
		for ext, n := range byExt {
			out.Files.WithoutFacts.ByExtension = append(out.Files.WithoutFacts.ByExtension, extensionCount{Extension: ext, Count: n})
		}
		sort.Slice(out.Files.WithoutFacts.ByExtension, func(i, j int) bool {
			a, b := out.Files.WithoutFacts.ByExtension[i], out.Files.WithoutFacts.ByExtension[j]
			if a.Count != b.Count {
				return a.Count > b.Count
			}
			return a.Extension < b.Extension
		})
		var withoutFactPaths []string
		for _, f := range withoutFacts {
			withoutFactPaths = append(withoutFactPaths, f.File)
		}
		out.Files.WithoutFacts.ByCategory = countCategories(withoutFactPaths)
		sort.SliceStable(withoutFacts, func(i, j int) bool {
			if withoutFacts[i].Extension != withoutFacts[j].Extension {
				return withoutFacts[i].Extension < withoutFacts[j].Extension
			}
			return withoutFacts[i].File < withoutFacts[j].File
		})
		// Unresolved in-code calls (rows CR-24..CR-27): the stored file
		// node attributes, never a diagnostic message.
		var ids []string
		for _, f := range counted {
			ids = append(ids, fileNodeIDs[f])
		}
		nodes, err := rd.byIdentity(ids)
		if err != nil {
			return out, err
		}
		ext, unk, files := 0, 0, 0
		for _, f := range counted {
			n, ok := nodes[fileNodeIDs[f]]
			e, okE := countAttr(n.Attributes[fileAttrExternal])
			u, okU := countAttr(n.Attributes[fileAttrUnknown])
			if !ok || !okE || !okU {
				notCounted++
				continue
			}
			files++
			ext, unk = ext+e, unk+u
			if e+u > 0 {
				unresolved = append(unresolved, unresolvedFile{File: f, External: e, Unknown: u, Test: testOf(f), IDE: ideOf(n)})
			}
		}
		sort.SliceStable(unresolved, func(i, j int) bool {
			a, b := unresolved[i], unresolved[j]
			if a.External+a.Unknown != b.External+b.Unknown {
				return a.External+a.Unknown > b.External+b.Unknown
			}
			return a.File < b.File
		})
		out.CodeCalls.unreadPart = availablePart()
		out.CodeCalls.External, out.CodeCalls.Unknown = &ext, &unk
		out.CodeCalls.FilesCounted, out.CodeCalls.FilesNotCounted = &files, &notCounted
	}
	out.Files.WithoutFacts.Files = listOf(withoutFacts, req.limit)
	out.CodeCalls.Files = listOf(unresolved, req.limit)
	var notReadPaths []string
	for _, f := range notRead {
		notReadPaths = append(notReadPaths, f.File)
	}
	out.Files.NotRead = unreadNotRead{unreadList: listOf(notRead, req.limit), ByCategory: countCategories(notReadPaths)}
	out.Files.SyntaxErrors = listOf(syntax, req.limit)
	out.Files.RecordsDropped, out.Files.StructureOutside = listOf(dropped, req.limit), listOf(structure, req.limit)
	// Links of the file items shown.
	for _, it := range out.Files.WithoutFacts.Files.Items {
		linked = append(linked, fileNodeIDs[it.File])
	}
	fileNodes, err := rd.byIdentity(linked)
	if err != nil {
		return out, err
	}
	link := func(file string) *query.IDELink {
		if n, ok := fileNodes[fileNodeIDs[file]]; ok {
			return ideOf(n)
		}
		return nil
	}
	for i := range out.Files.NotRead.Items {
		out.Files.NotRead.Items[i].IDE = link(out.Files.NotRead.Items[i].File)
	}
	for _, items := range [][]unreadFile{out.Files.SyntaxErrors.Items, out.Files.RecordsDropped.Items, out.Files.StructureOutside.Items} {
		for i := range items {
			items[i].IDE = link(items[i].File)
		}
	}
	for i := range out.Files.WithoutFacts.Files.Items {
		out.Files.WithoutFacts.Files.Items[i].IDE = link(out.Files.WithoutFacts.Files.Items[i].File)
	}
	out.Counts = countsOf(out)
	return out, nil
}

// fileAttrRouterForms is the file node attribute holding a file's Go
// router forms outside the route claim contract, {form: n} (row CR-38).
const fileAttrRouterForms = "go_route_forms_outside_contract"

// routerFormCounts fills each router form item's structured counts from
// its file node and returns the total over the items with counts and the
// number of items without (row CR-38).
func routerFormCounts(rd *reader, forms []routerFormsFile) (int, int, error) {
	ids := make([]string, len(forms))
	var lookup []string
	for i, f := range forms {
		if id, err := contract.Identity("file", f.File, f.File); err == nil {
			ids[i] = id
			lookup = append(lookup, id)
		}
	}
	nodes, err := rd.byIdentity(lookup)
	if err != nil {
		return 0, 0, err
	}
	total, notCounted := 0, 0
	for i := range forms {
		byForm, sum, ok := formCounts(nodes[ids[i]].Attributes[fileAttrRouterForms])
		if !ok {
			notCounted++
			continue
		}
		forms[i].Forms, forms[i].ByForm = &sum, byForm
		total += sum
	}
	return total, notCounted, nil
}

// formCounts reads a stored {form: n} attribute: a non-empty object of
// positive integers (row CR-38).
func formCounts(v any) (map[string]int, int, bool) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, 0, false
	}
	out := map[string]int{}
	sum := 0
	for form, x := range m {
		n, ok := countAttr(x)
		if !ok || n < 1 || form == "" {
			return nil, 0, false
		}
		out[form] = n
		sum += n
	}
	return out, sum, true
}

// countAttr reads a stored count: a non-negative integer (row CR-26).
func countAttr(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n >= 0 && n == float64(int(n)) {
			return int(n), true
		}
	case int:
		return n, n >= 0
	case interface{ Int64() (int64, error) }:
		i, err := n.Int64()
		return int(i), err == nil && i >= 0
	}
	return 0, false
}

// routeLabel is a route item's text: its shown path, its unknown-prefix
// path, else its pattern or its name marked by pathlessLabel.
func routeLabel(n contract.Node, withheld map[routepaths.RouteSite]bool) string {
	if shown := displayPath(objects(n.Attributes["paths"]), scopeProduction); shown != nil {
		return *shown
	}
	if prefix := routePrefix(n, scopeAll); prefix != nil {
		return prefix.Path
	}
	if pattern := stringAttr(n, "pattern"); pattern != nil {
		return pathlessLabel(*pattern, n, withheld)
	}
	return pathlessLabel(n.Name, n, withheld)
}

// pathlessLabel is the label of a route the index holds no path for
// (neither a full path nor an unknown-prefix path): the pattern as
// written (or the node name), marked so it is never read as the path
// (rows JS-4, CR-16; ADR 0089 Revisions 4 and 5). The mark says what the
// stored facts know, in the endpoint list's words: "shadowed" (an earlier
// mount takes every composed path), "path withheld" (row CR-44: withheld
// holds its site), else "path not known". "Not mounted" is never said: no
// stored fact proves a route unreachable from every app.
func pathlessLabel(pattern string, n contract.Node, withheld map[routepaths.RouteSite]bool) string {
	switch {
	case len(objects(n.Attributes[routepaths.ShadowedAttribute])) > 0:
		return pattern + " (shadowed)"
	case routepaths.PathWithheld(n, withheld):
		return pattern + " (path withheld)"
	}
	return pattern + " (path not known)"
}

func lessFileLine(fa string, la *int, fb string, lb *int) bool {
	if fa != fb {
		return fa < fb
	}
	a, b := 0, 0
	if la != nil {
		a = *la
	}
	if lb != nil {
		b = *lb
	}
	return a < b
}

// Count names of the page (rows CR-29, CR-31): each counts one unit.
const (
	countUnplacedRoutes     = "unplaced_routes"
	countMounts             = "mounts"
	countDynamicPatterns    = "dynamic_patterns"
	countRouterFormFiles    = "router_form_files"
	countRouterForms        = "router_forms"
	countFormFilesNotCount  = "router_form_files_not_counted"
	countDynamicCalls       = "dynamic_calls"
	countWrapperCalls       = "wrapper_calls"
	countRoutesWithoutShape = "routes_without_shape"
	countShapeInvalid       = "routes_shape_invalid"
	countSectionsNotRead    = "routes_with_sections_not_read"
	countDepthCaps          = "routes_depth_cap"
	countFilesNotRead       = "files_not_read"
	countSyntaxErrors       = "files_syntax_errors"
	countScannerGaps        = "scanner_gaps"
	countWithoutFacts       = "files_without_facts"
	countExternalCalls      = "unresolved_calls_external"
	countUnknownCalls       = "unresolved_calls_unknown"
	countFilesCounted       = "files_counted"
	countFilesNotCounted    = "files_not_counted"
	countRouteFailures      = "route_failures"
	countMountsUnresolved   = "mounts_unresolved"
	countRoutesWithheld     = "routes_withheld"
	countRecordsDropped     = "files_records_dropped"
	countStructureOutside   = "files_structure_outside_contract"
)

// countsOf names every count of a page; null where the part is not
// available.
func countsOf(r unreadResponse) map[string]*int {
	n := func(v int) *int { return &v }
	out := map[string]*int{}
	indexed := r.Index != nil
	set := func(name string, ok bool, v int) {
		if ok {
			out[name] = n(v)
		} else {
			out[name] = nil
		}
	}
	deref := func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	}
	doc := r.RoutePaths.Document.Available
	set(countUnplacedRoutes, doc, r.RoutePaths.Unplaced.Total)
	set(countMounts, doc, r.RoutePaths.Mounts.Total)
	set(countDynamicPatterns, indexed, r.RoutePaths.Dynamic.Total)
	set(countRouterFormFiles, indexed, r.RoutePaths.RouterForms.Total)
	set(countRouteFailures, indexed, r.RoutePaths.Failed.Total)
	set(countMountsUnresolved, indexed, r.RoutePaths.MountsUnresolved.Total)
	set(countRoutesWithheld, indexed, r.RoutePaths.RoutesWithheld.Total)
	set(countRouterForms, indexed && r.RoutePaths.Forms != nil, deref(r.RoutePaths.Forms))
	set(countFormFilesNotCount, indexed && r.RoutePaths.FormFilesNotCounted != nil, deref(r.RoutePaths.FormFilesNotCounted))
	set(countDynamicCalls, indexed, r.ClientCalls.Dynamic.Total)
	set(countWrapperCalls, indexed, r.ClientCalls.Wrapper.Total)
	set(countRoutesWithoutShape, indexed, r.Shapes.WithoutShape.Total)
	set(countShapeInvalid, indexed, r.Shapes.ShapeInvalid.Total)
	set(countSectionsNotRead, indexed, r.Shapes.SectionsNotRead.Total)
	set(countDepthCaps, indexed, r.DepthCaps.Routes.Total)
	set(countFilesNotRead, indexed, r.Files.NotRead.Total)
	set(countSyntaxErrors, indexed, r.Files.SyntaxErrors.Total)
	set(countScannerGaps, indexed, r.Files.ScannerGaps.Total)
	set(countRecordsDropped, indexed, r.Files.RecordsDropped.Total)
	set(countStructureOutside, indexed, r.Files.StructureOutside.Total)
	set(countWithoutFacts, r.Files.WithoutFacts.Available, r.Files.WithoutFacts.Files.Total)
	calls := r.CodeCalls.Available
	set(countExternalCalls, calls, deref(r.CodeCalls.External))
	set(countUnknownCalls, calls, deref(r.CodeCalls.Unknown))
	set(countFilesCounted, calls, deref(r.CodeCalls.FilesCounted))
	set(countFilesNotCounted, calls, deref(r.CodeCalls.FilesNotCounted))
	return out
}

// projectTotals sums the members' counts; a count missing for a member is
// null at project scope with the member named (row CR-29).
func projectTotals(members []unreadResponse) (map[string]*int, map[string][]string) {
	totals := map[string]*int{}
	missing := map[string][]string{}
	for _, m := range members {
		for name, v := range m.Counts {
			if v == nil {
				missing[name] = append(missing[name], m.Repository)
				continue
			}
			if _, seen := totals[name]; !seen {
				zero := 0
				totals[name] = &zero
			}
			*totals[name] += *v
		}
	}
	for name := range missing {
		totals[name] = nil
	}
	return totals, missing
}

// Handlers.

// unreadHandler answers GET /api/v0/unread for one repository or member.
type unreadHandler struct{ opt Options }

func (h unreadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := parseUnread(r.URL.Query())
	var body any
	if err == nil {
		body, err = unreadRepository(r.Context(), h.opt, req)
	}
	answer(w, r, h.opt, body, err)
}

// unreadProjectHandler answers GET /api/v0/unread on a project server:
// with repository= the member's page (row CR-28), else every member's and
// the project totals (row CR-29).
type unreadProjectHandler struct {
	ps       *projectState
	dispatch http.Handler
}

func (h unreadProjectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if _, present := q["repository"]; present {
		h.dispatch.ServeHTTP(w, r)
		return
	}
	req, err := parseUnread(q)
	var body any
	if err == nil {
		out := unreadProjectResponse{Project: h.ps.project.Name, Scope: req.scope, Limit: req.limit, Members: []unreadResponse{}}
		for _, name := range h.ps.names {
			var m unreadResponse
			if m, err = unreadRepository(r.Context(), h.ps.members[name], req); err != nil {
				break
			}
			out.Members = append(out.Members, m)
		}
		if err == nil {
			out.Totals, out.NotAvailable = projectTotals(out.Members)
			body = out
		}
	}
	answer(w, r, h.ps.opt, body, err)
}
