// Package routeshape is the shared model of a route's request and response
// shape: parameters, bodies, responses and security, each item labelled
// with how Graphvein knows it (its provenance), plus the language-neutral
// call-following helper the per-framework shape extractors use.
//
// The model exists so that every framework's shape extractor (FastAPI,
// Express, gin, Axum, Next.js, committed specs, ...) writes the same
// attribute with the same honesty rules, and the generator and the
// Explorer read one format. The rules are rows SM-1..SM-29 of
// docs/contract/claims/route-shape.md; Rows in contract.go lists them in
// the same order and a test keeps the two 1:1.
//
// The central distinction is between what the code proves and what
// Graphvein did not read: an empty list never means "none" unless its
// section says provably_none with evidence.
package routeshape

import "strings"

// AttributeKey is the route node attribute that holds a Shape (row SM-1).
// A route without it has every section not_read.
const AttributeKey = "shape"

// ModelVersion is the version of the attribute's own format (row SM-1).
// A reader refuses any other version instead of guessing at its meaning.
const ModelVersion = 1

// Shape is the request and response shape of one route.
//
// Path, Query, Header and Cookie are the parameter sections; Body is the
// request body; Responses lists status codes; Security lists the
// requirements of the route, whose schemes are defined in Schemes. Models
// are the named schemas that Schema.Ref points to. Follow records the call
// chains the extractor followed to find all of it.
type Shape struct {
	Version   int             `json:"version"`
	Path      ParamSection    `json:"path"`
	Query     ParamSection    `json:"query"`
	Header    ParamSection    `json:"header"`
	Cookie    ParamSection    `json:"cookie"`
	Body      BodySection     `json:"body"`
	Responses ResponseSection `json:"responses"`
	Security  SecuritySection `json:"security"`
	Schemes   []Scheme        `json:"security_schemes"`
	Models    []Model         `json:"models"`
	Follow    FollowInfo      `json:"follow"`
}

// New returns a shape with every section not_read and the given call
// following depth limit: the starting point of every extractor, so a
// section it never touches can never read as "none".
func New(depthLimit int) *Shape {
	s := &Shape{Version: ModelVersion, Follow: FollowInfo{DepthLimit: depthLimit, Chains: []ChainRecord{}}}
	for _, sec := range s.sections() {
		*sec = Section{State: NotRead}
	}
	s.normalize()
	return s
}

// State is what a section claims about its items as a whole (rows
// SM-2..SM-5).
type State string

const (
	// NotRead: Graphvein did not read this part of the route. No items,
	// no claim either way (SM-2).
	NotRead State = "not_read"
	// ProvablyNone: the code proves there is nothing here, for example a
	// FastAPI handler whose declared parameters include no query
	// parameter and whose dependencies were all read. Needs evidence of
	// kind fact or mapped (SM-3).
	ProvablyNone State = "provably_none"
	// Partial: some items were read; more may exist. The reason says what
	// may be missing (SM-4).
	Partial State = "partial"
	// Complete: every item is known, proven by a declaration the framework
	// enforces or by a committed spec (SM-5).
	Complete State = "complete"
)

// Section is the state shared by every section.
type Section struct {
	State State `json:"state"`
	// Reason says, for partial, what may be missing (an unresolved call,
	// a dynamic key, a depth cap); optional otherwise.
	Reason string `json:"reason,omitempty"`
	// Evidence is the declaration that proves provably_none or complete.
	Evidence *Provenance `json:"evidence,omitempty"`
}

// SpecOnlyReason begins the reason of a section the code did not read
// and whose items are all mapped from a committed spec (row
// SH-O3 of docs/contract/claims/route-shape-spec.md). The section is
// partial: the spec says what it documents, the code may read more.
const SpecOnlyReason = "not read in the code: every item is mapped from the committed spec"

// CodeState is the section's state as the code alone proves it: not_read
// for a section whose items all come from a committed spec. The generator
// describes the code, so it reads this state, never the mapped one.
func (s Section) CodeState() State {
	if s.State == Partial && strings.HasPrefix(s.Reason, SpecOnlyReason) {
		return NotRead
	}
	return s.State
}

// ParamSection lists the parameters of one location (path, query, header
// or cookie).
type ParamSection struct {
	Section
	Items []Param `json:"items"`
}

// BodySection lists the request body variants, one per content type.
type BodySection struct {
	Section
	Items []Content `json:"items"`
}

// ResponseSection lists the responses, one per status and content type.
type ResponseSection struct {
	Section
	Items []Response `json:"items"`
}

// SecuritySection lists the security requirements: alternatives, any one
// of which is enough (OpenAPI's security list).
type SecuritySection struct {
	Section
	Items []Requirement `json:"items"`
}

// Access says how a parameter is known (rows SM-12, SM-13, SM-16).
type Access string

const (
	// Declared: part of the route's declared interface (a typed handler
	// parameter, a validator, a spec).
	Declared Access = "declared"
	// ReadByHandler: the handler, or a function it calls, reads the name.
	ReadByHandler Access = "read_by_handler"
	// ReadByDependency: a dependency or middleware reads the name (a
	// FastAPI dependency reading request.headers). Never a declared
	// parameter of the route.
	ReadByDependency Access = "read_by_dependency"
)

// Presence is whether a parameter or field must be sent (row SM-14).
type Presence string

const (
	Required        Presence = "required"
	Optional        Presence = "optional"
	PresenceUnknown Presence = "unknown"
)

// Param is one named parameter of a location.
type Param struct {
	Name     string   `json:"name"`
	Access   Access   `json:"access"`
	Presence Presence `json:"presence"`
	// Schema is the parameter's type; nil means the type was not read.
	Schema     *Schema    `json:"schema"`
	Provenance Provenance `json:"provenance"`
}

// Content is one request body variant.
type Content struct {
	// ContentType is the media type; nil means it was not read.
	ContentType *string `json:"content_type"`
	// Schema is the body's shape; nil means it was not read.
	Schema     *Schema    `json:"schema"`
	Provenance Provenance `json:"provenance"`
}

// Response is one response of the route.
type Response struct {
	// Status is a status code ("200"), a range ("4XX") or "default".
	Status      string  `json:"status"`
	ContentType *string `json:"content_type"`
	// Schema nil means the response body was not read; a proven empty
	// body is the schema of type null.
	Schema     *Schema    `json:"schema"`
	Provenance Provenance `json:"provenance"`
}

// Requirement is one alternative of the route's security: every scheme it
// names applies together. An empty Schemes list is the anonymous
// alternative (OpenAPI's {}), allowed only when proven (row SM-19).
type Requirement struct {
	Schemes    []SchemeUse `json:"schemes"`
	Provenance Provenance  `json:"provenance"`
}

// SchemeUse names a scheme of Shape.Schemes and the scopes required.
type SchemeUse struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// SchemeType is the type of a security scheme.
type SchemeType string

const (
	SchemeHTTP          SchemeType = "http"
	SchemeAPIKey        SchemeType = "apiKey"
	SchemeOAuth2        SchemeType = "oauth2"
	SchemeOpenIDConnect SchemeType = "openIdConnect"
	SchemeUnknown       SchemeType = "unknown"
)

// Scheme is one security scheme. Every detail is nil when not read.
type Scheme struct {
	Name string     `json:"name"`
	Type SchemeType `json:"type"`
	// Scheme is the HTTP auth scheme (bearer, basic, digest), http only.
	Scheme       *string `json:"scheme"`
	BearerFormat *string `json:"bearer_format"`
	// In and ParamName locate an API key (header, query, cookie).
	In        *string `json:"in"`
	ParamName *string `json:"param_name"`
	// TokenURL is the OAuth2 password or client-credentials token URL.
	TokenURL *string `json:"token_url"`
	// OpenIDConnectURL is the discovery URL of an openIdConnect scheme.
	OpenIDConnectURL *string    `json:"openid_connect_url"`
	Provenance       Provenance `json:"provenance"`
}

// Model is a named schema that Schema.Ref points to (a Pydantic model, a
// Go struct, a TypeScript interface). Its provenance kind is the evidence
// level of the type itself (row SM-15).
type Model struct {
	Name       string     `json:"name"`
	Schema     Schema     `json:"schema"`
	Provenance Provenance `json:"provenance"`
}

// FollowInfo records how calls were followed for this route (rows SM-21
// to SM-29).
type FollowInfo struct {
	// DepthLimit is the configured depth the run used (graphvein.yaml
	// index.shape_depth or --shape-depth), so a depth_cap stop can say
	// which limit it hit and that it can be raised.
	DepthLimit int `json:"depth_limit"`
	// Chains are the chains followed, each with its depth and stop.
	Chains []ChainRecord `json:"chains"`
}

// ChainRecord is one followed chain as stored on the route.
type ChainRecord struct {
	// Section names the section the chain was followed for ("query",
	// "body", ...; see SectionNames).
	Section string `json:"section"`
	// Start is the function the chain started in (normally the handler).
	Start Step `json:"start"`
	// Steps are the calls followed, outermost first; len(Steps) == Depth.
	Steps []Step `json:"steps"`
	Depth int    `json:"depth"`
	Stop  Stop   `json:"stop"`
	// At is the call or site where the chain stopped; nil for ended and
	// resolver_error stops.
	At     *Step  `json:"at"`
	Detail string `json:"detail,omitempty"`
}

// Kind is how Graphvein knows an item (rows SM-6..SM-10).
type Kind string

const (
	// Fact: a typed declaration the framework enforces at runtime
	// (FastAPI annotations, a zod parse, Axum Json<T>).
	Fact Kind = "fact"
	// ReadInHandler: the name is read in the handler or a function it
	// calls; its type and whether it must be sent are not read.
	ReadInHandler Kind = "read_in_handler"
	// Intended: a static type nothing checks at runtime (a TypeScript
	// generic, `as T`, a callee's parameter type).
	Intended Kind = "intended"
	// Returned: the declared type of a returned value, not validated.
	Returned Kind = "returned"
	// Mapped: from a committed spec or a typed frontend client linked to
	// the route; its confidence is that of the inferred link.
	Mapped Kind = "mapped"
)

// Provenance says how an item is known and where (row SM-11).
type Provenance struct {
	Kind Kind `json:"kind"`
	// Rule is the claim row of the extractor's own table (SH-F4, ...).
	Rule string `json:"rule"`
	File string `json:"file"`
	Line int    `json:"line"`
	// Chain is the calls followed from the handler to the site, outermost
	// first; empty when the site is in the handler itself.
	Chain []Step `json:"chain"`
	// Confidence is 1 for everything read from code; a mapped item takes
	// the confidence of the inferred link it came through (0.9, 0.7).
	Confidence float64 `json:"confidence"`
}

// Step is one place in a call chain: a file and line, and the function
// called (or read) there.
type Step struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Function string `json:"function"`
}

// SchemaType is the type of a schema node (row SM-20).
type SchemaType string

const (
	TypeObject  SchemaType = "object"
	TypeArray   SchemaType = "array"
	TypeString  SchemaType = "string"
	TypeNumber  SchemaType = "number"
	TypeInteger SchemaType = "integer"
	TypeBoolean SchemaType = "boolean"
	TypeNull    SchemaType = "null"
	TypeEnum    SchemaType = "enum"
	TypeRef     SchemaType = "ref"
	TypeUnknown SchemaType = "unknown"
)

// Additional says whether an object accepts fields it does not list.
type Additional string

const (
	AdditionalAllowed   Additional = "allowed"
	AdditionalForbidden Additional = "forbidden"
	AdditionalUnknown   Additional = "unknown"
)

// MaxSchemaDepth bounds schema nesting (row SM-20); a deeper schema is
// cut to unknown with a reason by the extractor, and Validate refuses one
// that is not.
const MaxSchemaDepth = 32

// Schema is a JSON-Schema-like subset. Only the fields of its Type are
// set: Fields and Additional for object, Items for array, Enum for enum,
// Ref for ref, Reason for unknown.
type Schema struct {
	Type       SchemaType `json:"type"`
	Fields     []Field    `json:"fields,omitempty"`
	Additional Additional `json:"additional,omitempty"`
	Items      *Schema    `json:"items,omitempty"`
	// Enum lists literals: strings, numbers, booleans or null.
	Enum []any `json:"enum,omitempty"`
	// Ref names a Model of the same shape.
	Ref string `json:"ref,omitempty"`
	// Nullable is true when null is also accepted.
	Nullable bool `json:"nullable,omitempty"`
	// Reason says why a schema is unknown (never empty for unknown).
	Reason string `json:"reason,omitempty"`
	// Decls are the identities of the model declarations this schema node
	// was read from (a struct and the structs it embeds, a model class and
	// its repository bases), as the extractor resolved them. They are not
	// part of the stored shape: ModelUses turns them into the route's
	// model uses (docs/contract/claims/route-model-types.md, row MT-3).
	Decls []string `json:"-"`
}

// Field is one field of an object schema.
type Field struct {
	Name     string   `json:"name"`
	Presence Presence `json:"presence"`
	Schema   Schema   `json:"schema"`
}

// Section names, as used in ChainRecord.Section and violation paths.
const (
	SectionPath      = "path"
	SectionQuery     = "query"
	SectionHeader    = "header"
	SectionCookie    = "cookie"
	SectionBody      = "body"
	SectionResponses = "responses"
	SectionSecurity  = "security"
)

// SectionNames lists the section names in attribute order.
var SectionNames = []string{SectionPath, SectionQuery, SectionHeader, SectionCookie, SectionBody, SectionResponses, SectionSecurity}

// sections returns pointers to the seven Section values in SectionNames
// order.
func (s *Shape) sections() []*Section {
	return []*Section{&s.Path.Section, &s.Query.Section, &s.Header.Section, &s.Cookie.Section, &s.Body.Section, &s.Responses.Section, &s.Security.Section}
}

// normalize replaces nil lists with empty ones, so the attribute always
// has the same JSON form whether a section was touched or not.
func (s *Shape) normalize() {
	for _, p := range []*ParamSection{&s.Path, &s.Query, &s.Header, &s.Cookie} {
		if p.Items == nil {
			p.Items = []Param{}
		}
		for i := range p.Items {
			normalizeProvenance(&p.Items[i].Provenance)
		}
	}
	if s.Body.Items == nil {
		s.Body.Items = []Content{}
	}
	for i := range s.Body.Items {
		normalizeProvenance(&s.Body.Items[i].Provenance)
	}
	if s.Responses.Items == nil {
		s.Responses.Items = []Response{}
	}
	for i := range s.Responses.Items {
		normalizeProvenance(&s.Responses.Items[i].Provenance)
	}
	if s.Security.Items == nil {
		s.Security.Items = []Requirement{}
	}
	for i := range s.Security.Items {
		r := &s.Security.Items[i]
		if r.Schemes == nil {
			r.Schemes = []SchemeUse{}
		}
		for j := range r.Schemes {
			if r.Schemes[j].Scopes == nil {
				r.Schemes[j].Scopes = []string{}
			}
		}
		normalizeProvenance(&r.Provenance)
	}
	if s.Schemes == nil {
		s.Schemes = []Scheme{}
	}
	for i := range s.Schemes {
		normalizeProvenance(&s.Schemes[i].Provenance)
	}
	if s.Models == nil {
		s.Models = []Model{}
	}
	for i := range s.Models {
		normalizeProvenance(&s.Models[i].Provenance)
	}
	if s.Follow.Chains == nil {
		s.Follow.Chains = []ChainRecord{}
	}
	for i := range s.Follow.Chains {
		if s.Follow.Chains[i].Steps == nil {
			s.Follow.Chains[i].Steps = []Step{}
		}
	}
	for _, sec := range s.sections() {
		if sec.Evidence != nil {
			normalizeProvenance(sec.Evidence)
		}
	}
}

func normalizeProvenance(p *Provenance) {
	if p.Chain == nil {
		p.Chain = []Step{}
	}
}
