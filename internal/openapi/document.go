// Package openapi writes an OpenAPI 3.1 document from the routes Graphvein
// stored for one repository (T029, ADR 0081). It runs no code and reads no
// source file: every field comes from stored route nodes, their auth and
// shape attributes, imported specs and the repository's explorer settings,
// under the claim contract docs/contract/claims/openapi-generation.md
// (rows OG-1..OG-93; Rows in contract.go lists them and a test keeps the
// two 1:1).
//
// The document states what the code registers, never what a running
// server answers. A field Graphvein did not read is absent and named in the
// operation's x-graphvein.not_read; absence is never written as an empty
// value OpenAPI would read as "none". Forms OpenAPI cannot express,
// routes without a usable path and conflicts are listed in the root
// x-graphvein, never guessed into paths.
//
// One function, Generate, feeds the CLI (graphvein openapi), the server
// (GET /api/v0/openapi) and the UI download, so all three give the same
// bytes for one store state.
package openapi

import "github.com/graphvein/graphvein/internal/extract/routeshape"

// Version is the OpenAPI version written (row OG-1): 3.1 makes responses
// optional, so the document can say nothing rather than invent one.
const Version = "3.1.1"

// Document is the generated OpenAPI document. Field order is the order of
// the JSON output.
type Document struct {
	OpenAPI    string               `json:"openapi"`
	Info       Info                 `json:"info"`
	Servers    []Server             `json:"servers,omitempty"`
	Paths      map[string]*PathItem `json:"paths"`
	Components *Components          `json:"components,omitempty"`
	X          RootExt              `json:"x-graphvein"`
	// Mounts are the unresolved mount hops of the routes without a path
	// read from code (rows OG-94, OG-95), for the explorer's editor; they
	// are not part of the written document.
	Mounts []Mount `json:"-"`
}

// Mount is one unresolved mount hop and the production routes stored
// below it (row OG-94).
type Mount struct {
	Key          string `json:"key"`
	Hop          string `json:"hop"`
	File         string `json:"file"`
	Line         int    `json:"line"`
	Reason       string `json:"reason"`
	Registration string `json:"registration"`
	// ShadowingNotChecked reports that the linker could not run its
	// sibling and shadow checks above the hop (OG-96).
	ShadowingNotChecked bool `json:"shadowing_not_checked"`
	// Routes counts the production routes below the hop without a path
	// read from code; Examples are some of their paths below the hop
	// ("GET /api/x"), sorted.
	Routes   int            `json:"routes"`
	Examples []MountExample `json:"examples"`
	// Ambiguous reports a hop whose text has a byte-identical twin in the
	// same function: no value can be tied to it (OG-94).
	Ambiguous bool `json:"ambiguous"`
	// Covers says what a value for the hop states (OG-95): "hop" (only
	// the part the hop adds; Above are the known prefixes above it, ""
	// for the root), "nothing" (the hop adds no unread part: only "/"
	// is accepted) or "root" (the whole prefix from the root: the
	// routers above the hop are not proven either).
	Covers string   `json:"value_covers"`
	Above  []string `json:"above"`
	// Framework is the framework of the routes below the hop (the
	// editor's preview joins paths as the generator does).
	Framework string `json:"framework"`
}

// MountExample is one route below a hop: its method, the known prefix
// above the hop and its path below the hop.
type MountExample struct {
	Method string `json:"method"`
	Above  string `json:"above"`
	Path   string `json:"path"`
}

// Info is the document's info object (rows OG-2..OG-4).
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// Server is one entry of servers (row OG-5): only a configured try-it
// environment, never a base URL inferred from code.
type Server struct {
	URL         string    `json:"url"`
	Description string    `json:"description"`
	X           ServerExt `json:"x-graphvein"`
}

// ServerExt names the configured environment a server comes from.
type ServerExt struct {
	Environment string `json:"environment"`
	Label       string `json:"label"`
	// File is graphvein.yaml or graphvein-project.yaml.
	File string `json:"file"`
	Line int    `json:"line"`
}

// Components holds the security schemes and schemas operations refer to
// (rows OG-7, OG-85, OG-87).
type Components struct {
	Schemas         map[string]map[string]any `json:"schemas,omitempty"`
	SecuritySchemes map[string]map[string]any `json:"securitySchemes,omitempty"`
}

// PathItem holds the operations of one path key in OpenAPI's method order
// (row OG-51).
type PathItem struct {
	Get     *Operation  `json:"get,omitempty"`
	Put     *Operation  `json:"put,omitempty"`
	Post    *Operation  `json:"post,omitempty"`
	Delete  *Operation  `json:"delete,omitempty"`
	Options *Operation  `json:"options,omitempty"`
	Head    *Operation  `json:"head,omitempty"`
	Patch   *Operation  `json:"patch,omitempty"`
	Trace   *Operation  `json:"trace,omitempty"`
	X       PathItemExt `json:"x-graphvein"`
}

// slot returns the field of method (upper case), or nil for a method
// outside OpenAPI's eight.
func (p *PathItem) slot(method string) **Operation {
	switch method {
	case "GET":
		return &p.Get
	case "PUT":
		return &p.Put
	case "POST":
		return &p.Post
	case "DELETE":
		return &p.Delete
	case "OPTIONS":
		return &p.Options
	case "HEAD":
		return &p.Head
	case "PATCH":
		return &p.Patch
	case "TRACE":
		return &p.Trace
	}
	return nil
}

// PathItemExt is a path item's x-graphvein (rows OG-16, OG-17).
type PathItemExt struct {
	Entries   []EntryRef `json:"entries"`
	AnyMethod []RouteRef `json:"any_method,omitempty"`
}

// EntryRef is one stored path entry that gives this key.
type EntryRef struct {
	Route       string       `json:"route"`
	Method      string       `json:"method"`
	Resolution  string       `json:"resolution"`
	Conditional bool         `json:"conditional"`
	Assumptions []Assumption `json:"assumptions"`
}

// RouteRef names a route and where it is registered.
type RouteRef struct {
	Route     string `json:"route"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Framework string `json:"framework"`
}

// Operation is one operation (rows OG-20..OG-32, OG-70..OG-89).
type Operation struct {
	Parameters  []Parameter            `json:"parameters,omitempty"`
	RequestBody *RequestBody           `json:"requestBody,omitempty"`
	Responses   map[string]*Response   `json:"responses,omitempty"`
	Security    []map[string][]string  `json:"security,omitempty"`
	X           OperationExt           `json:"x-graphvein"`
	shape       *routeshape.Shape      // the route's valid stored shape, or nil
	route       *routeInfo             // the route the operation comes from
	security    []map[string][]string  // a requirement waiting for the scheme check
	schemes     map[string]schemeEntry // schemes the requirement names
}

// Parameter is one path, query, header or cookie parameter.
type Parameter struct {
	Name     string         `json:"name"`
	In       string         `json:"in"`
	Required *bool          `json:"required,omitempty"`
	Schema   map[string]any `json:"schema"`
	X        map[string]any `json:"x-graphvein"`
}

// RequestBody is the request body written from fact items (row OG-78).
type RequestBody struct {
	Content map[string]*MediaType `json:"content"`
	X       map[string]any        `json:"x-graphvein"`
}

// MediaType is one content type of a body or response.
type MediaType struct {
	Schema map[string]any `json:"schema,omitempty"`
	X      map[string]any `json:"x-graphvein"`
}

// Response is one response written from fact items (rows OG-80, OG-81).
type Response struct {
	Description string                `json:"description"`
	Content     map[string]*MediaType `json:"content,omitempty"`
	X           map[string]any        `json:"x-graphvein"`
}

// OperationExt is an operation's x-graphvein (rows OG-32, OG-40..OG-44,
// OG-70..OG-89, OG-92).
type OperationExt struct {
	Route             string  `json:"route"`
	File              string  `json:"file"`
	Line              int     `json:"line"`
	Framework         string  `json:"framework"`
	Handler           *string `json:"handler"`
	HandlerExpression *string `json:"handler_expression"`
	// Rule is the extraction rule set that produced the route (OG-40).
	Rule string `json:"rule"`
	// Confidence is 1: a route is a stored fact (ARCHITECTURE: facts are 1).
	Confidence  float64      `json:"confidence"`
	Template    string       `json:"template"`
	Resolution  string       `json:"resolution"`
	Conditional bool         `json:"conditional"`
	Assumptions []Assumption `json:"assumptions"`
	// EquivalentRegistrations lists the Express registrations collapsed
	// into this operation (OG-20).
	EquivalentRegistrations []string       `json:"equivalent_registrations,omitempty"`
	DocumentedBy            []DocumentedBy `json:"documented_by"`
	Auth                    map[string]any `json:"auth"`
	NotRead                 []string       `json:"not_read"`
	Shape                   *ShapeExt      `json:"shape,omitempty"`
	ShapeError              *string        `json:"shape_error,omitempty"`
	// MayBeShadowed lists the routes that may answer this operation's
	// requests first (OG-19).
	MayBeShadowed []Shadow `json:"may_be_shadowed,omitempty"`
	// Shadowing is "not_checked" for an operation placed by a mount value
	// below a hop whose sibling and shadow checks need the prefix (OG-96).
	Shadowing string `json:"shadowing,omitempty"`
}

// Shadow is one route whose path matches every request of an operation in
// a first-match framework (OG-19).
type Shadow struct {
	Route  string `json:"route"`
	Method string `json:"method"`
	Key    string `json:"key"`
	// Order is "not_checked": which of the two is registered first on the
	// same router or app is not stored.
	Order string `json:"order"`
	// SetByYou is the explorer.path_values key whose value placed the
	// shadowing route: a candidate refused against this code-read
	// operation (OG-92) still answers its requests first when it is
	// registered earlier (OG-95).
	SetByYou string `json:"set_by_you,omitempty"`
}

// Assumption is one stated assumption an operation's path rests on: an
// extraction assumption id (GR-V5, NX-V1, ...) or a value set by the user.
type Assumption struct {
	ID string `json:"id"`
	// From says where it is stated: "route", "path_entry" or
	// "graphvein.yaml explorer.path_values".
	From string `json:"from"`
	// Key and Value are set for a value set by the user (OG-92).
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
	Text  string `json:"text,omitempty"`
}

// DocumentedBy is one imported spec operation linked to the route by an
// inferred documents_route edge (OG-44). The committed spec is never
// merged into the document (OG-60).
type DocumentedBy struct {
	Operation  string  `json:"operation"`
	File       string  `json:"file"`
	Confidence float64 `json:"confidence"`
}

// RootExt is the root x-graphvein (row OG-9).
type RootExt struct {
	Generator       string            `json:"generator"`
	ContractVersion int               `json:"contract_version"`
	Index           *IndexInfo        `json:"index"`
	Repository      string            `json:"repository"`
	Group           string            `json:"group"`
	Scope           string            `json:"scope"`
	Counts          Counts            `json:"counts"`
	Unresolved      []Unresolved      `json:"unresolved"`
	Unrepresentable []Unrepresentable `json:"unrepresentable"`
	Conflicts       []Conflict        `json:"conflicts"`
	ExcludedTest    ExcludedTest      `json:"excluded_test"`
	ImportedSpecs   []ImportedSpec    `json:"imported_specs"`
	UserValues      []UserValue       `json:"user_values"`
	ShapeErrors     int               `json:"shape_errors"`
}

// IndexInfo names the index run the document was generated from.
type IndexInfo struct {
	Run        string `json:"run"`
	FinishedAt string `json:"finished_at"`
	Extractor  string `json:"extractor"`
	Commit     string `json:"commit,omitempty"`
}

// Counts are the document's totals (row OG-49).
type Counts struct {
	Routes          int `json:"routes"`
	Production      int `json:"production_routes"`
	Operations      int `json:"operations"`
	PathKeys        int `json:"path_keys"`
	AnyMethod       int `json:"any_method"`
	Unresolved      int `json:"unresolved"`
	Unrepresentable int `json:"unrepresentable"`
	Conflicts       int `json:"conflicts"`
	SetByYou        int `json:"set_by_you"`
	Conditional     int `json:"conditional"`
	SettingsDefault int `json:"settings_default"`
	WithShape       int `json:"with_shape"`
	MayBeShadowed   int `json:"may_be_shadowed"`
}

// Unresolved is a production route with no emittable path (row OG-45),
// with the part Graphvein could not read named.
type Unresolved struct {
	Route  string `json:"route"`
	Method string `json:"method"`
	// Pattern is the route's own pattern as stored.
	Pattern string `json:"pattern"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	// Reason is no_path or unresolved_base.
	Reason string `json:"reason"`
	// TemplatesAfterBase are the templates after an unknown base path
	// (reason unresolved_base).
	TemplatesAfterBase []string `json:"templates_after_base,omitempty"`
	Unknown            Unknown  `json:"unknown"`
	// Keys are the explorer.path_values keys that would supply the
	// unknown part, most specific first.
	Keys []string `json:"keys"`
}

// Unknown names the part of a route's path Graphvein could not read.
type Unknown struct {
	// Part is "base_path" (a Next.js basePath not read as a literal) or
	// "mount_or_prefix" (the chain from an app to the registration).
	Part string `json:"part"`
	Text string `json:"text"`
	// Diagnostic and Expression name the stored diagnostic of the
	// registration, when one names it.
	Diagnostic string `json:"diagnostic,omitempty"`
	Expression string `json:"expression,omitempty"`
	// Project is the Next.js project directory of an unknown basePath.
	Project string `json:"project,omitempty"`
}

// Unrepresentable is a route path OpenAPI 3.1 cannot express (row OG-46).
type Unrepresentable struct {
	Route  string `json:"route"`
	Method string `json:"method"`
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// Conflict is a set of keys or routes no single operation can stand for
// (row OG-47): none of them is picked.
type Conflict struct {
	Reason string   `json:"reason"`
	Keys   []string `json:"keys"`
	Method string   `json:"method,omitempty"`
	Routes []string `json:"routes"`
}

// ExcludedTest counts test-scope routes and path entries left out (OG-48).
type ExcludedTest struct {
	Routes      int `json:"routes"`
	PathEntries int `json:"path_entries"`
}

// ImportedSpec is one committed spec of the repository (row OG-60), listed
// beside the document and never merged into it.
type ImportedSpec struct {
	Spec        string         `json:"spec"`
	File        string         `json:"file"`
	Title       *string        `json:"title"`
	SpecVersion *string        `json:"spec_version"`
	Scope       string         `json:"scope"`
	Operations  int            `json:"operations"`
	Statuses    map[string]int `json:"statuses"`
}

// UserValue is one explorer.path_values entry and its use (row OG-93).
type UserValue struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Status string `json:"status"`
	// Operations counts the operations whose path uses the value.
	Operations int `json:"operations"`
	// Why says why a value is unused.
	Why string `json:"why,omitempty"`
	// Registration is the route content hash recorded with a route key
	// (OG-90).
	Registration string `json:"registration,omitempty"`
	// Warning says that a route key has no registration hash, so the
	// value may move to another route (the UI shows it).
	Warning string `json:"warning,omitempty"`
}

// Reasons and warnings of user values (rows OG-90, OG-93).
const (
	WhyNoRoute             = "no route without a usable path has this key"
	WhyNoOperation         = "its path conflicts with another value set by you (see x-graphvein.conflicts)"
	WhyRegistrationChanged = "registration changed: the route with this identity is no longer the registration the value was set for (a registration added or removed in the file can move an identity); set it again"
	WhyRegistrationTwin    = "ambiguous registration: another registration in the same file has the same text, so a recorded hash cannot tell which one the value was set for"
	WhyMountChanged        = "registration changed: the hop with this key is no longer the call the value was set for (its text changed, the routers or prefixes above it changed, or a hop with the same text was added or removed above it in the function); set it again"
	WhyMountTwin           = "ambiguous hop: another hop in the same function has the same text, so a recorded hash cannot tell which one the value was set for"
	WhyMountRouteValue     = "every route below the hop has its own value set by you (route keys come first)"
	WhyMountNothing        = "this hop adds no prefix Graphvein could not read: the paths below it are the served paths, so the only value is / (the hop passes them on)"
	WarnNoMountHash        = "no registration hash is recorded: if a hop with the same text is added or removed above it in the function, the value moves to another hop; set it again from the explorer to record the hash"
	WarnNoRegistration     = "no registration hash is recorded: if a registration with the same method and pattern is added or removed above it in the file, the value moves to another route; set it again from the explorer to record the hash"
)

// User value statuses.
const (
	UserValueUsed   = "used"
	UserValueUnused = "unused"
)
