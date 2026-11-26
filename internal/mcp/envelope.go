package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// EnvelopeVersion is the version of the response envelope. A field is
// only added, never renamed or removed, without bumping it.
const EnvelopeVersion = 1

// Envelope is every tool's answer (structuredContent and, by default, the
// same JSON as text; D5). Each list is present, never omitted: an empty
// list means the reused code reported nothing of that kind, which is not a
// claim that nothing exists (row MC-D and the per-row conditions of the
// claim contract, docs/contract/claims/mcp-server.md).
type Envelope struct {
	Tool     string `json:"tool"`
	Envelope int    `json:"envelope"`
	// Repository is the one repository served; empty in project mode,
	// where Project names the project and every fact names its member.
	Repository string `json:"repository,omitempty"`
	Project    string `json:"project,omitempty"`
	// Error is set on a refusal (isError results, MC-8). It, the bounds
	// and the page come before the results, so a client that cuts a long
	// text still sees them (T030 review).
	Error *ToolError `json:"error,omitempty"`
	// Freshness is the index freshness, checked for this call (MC-1).
	Freshness Freshness `json:"freshness"`
	// Request echoes every parameter after defaults (MC-2).
	Request map[string]any `json:"request"`
	// Bounds lists every cap that bound (MC-3): results stop at the cap,
	// they do not say nothing lies beyond.
	Bounds []Bound `json:"bounds"`
	// Page is this tool's own paging of a complete list; null for a tool
	// that answers one object.
	Page *Page `json:"page"`
	// Summary is a tool's one summary object (endpoints: the transport
	// counts and labels); null for the other tools.
	Summary any `json:"summary"`
	// Results are the facts, in the reused code's order, paged (MC-4).
	Results []any `json:"results"`
	// Unknowns are what the reused code reported it could not tell. A
	// list an unknown carries is paged like results (listPage).
	Unknowns []Unknown `json:"unknowns"`
	// Assumptions are what the facts rest on.
	Assumptions []Assumption `json:"assumptions"`
	// Labels explains each label this response uses, and only those
	// (MC-5, D11).
	Labels map[string]string `json:"labels"`
	// Omitted names every field left out of the reused views' objects,
	// each with how to get it (MC-6, MC-29): presentation fields (editor
	// links, raw attributes) and, in compact detail, the parts a detail
	// parameter or another tool returns. Nothing is left out silently.
	Omitted []Omission `json:"omitted"`

	// unknownsOffset and unknownsLimit page the lists unknowns carry
	// (listPage); a limit of 0 lists them whole.
	unknownsOffset, unknownsLimit int
	labelSet                      map[string]string
	omitSet                       map[string]string
	// assumeSet holds the assumption IDs facts rest on, by row, until
	// the catalogue texts are filled in (Server.fillAssumptions).
	assumeSet map[string]string
	// changed is the whole changed-file list of the freshness check, and
	// checked is true when the check succeeded, so a tool can hand it to
	// the query package instead of a second scan (query.Options.Fresh).
	changed []string
	checked bool
	// status is the status view the freshness check read (one
	// repository), so the status tool pages its changed files without a
	// second read.
	status *statusView
	// changedNodes are the files of served nodes whose file_state is not
	// unchanged (Server.recheck).
	changedNodes map[string]string
}

// Omission is one field left out of an answer and how to get it.
type Omission struct {
	Field string `json:"field"`
	How   string `json:"how"`
}

// Freshness is the index state as /api/v0/status computes it for this
// call (row MC-1), or per member in project mode.
type Freshness struct {
	// State is fresh, stale, not_indexed or unknown; any other value the
	// view serves is reported as unknown (MC-D).
	State        string   `json:"state"`
	CheckedAt    string   `json:"checked_at,omitempty"`
	ChangedCount *int     `json:"changed_count"`
	ChangedFiles []string `json:"changed_files"`
	// ChangedMore counts the changed files not listed here.
	ChangedMore int `json:"changed_more"`
	// Reused is true when this call reused the check of an earlier call
	// of the same batch (FreshnessReuse).
	Reused bool       `json:"reused,omitempty"`
	Text   string     `json:"text,omitempty"`
	Error  string     `json:"error,omitempty"`
	Index  *IndexInfo `json:"index"`
	// Members and LinkPass are the project mode's per-member freshness
	// and last link pass; State is then the weakest member state.
	Members  []MemberFreshness `json:"members,omitempty"`
	LinkPass *LinkPass         `json:"link_pass,omitempty"`
}

// IndexInfo is the last run.
type IndexInfo struct {
	FinishedAt      string `json:"finished_at"`
	Indexer         string `json:"indexer"`
	IndexerVersion  string `json:"indexer_version"`
	ContractVersion int    `json:"contract_version"`
	// Commit is null when the run recorded none.
	Commit *string `json:"commit"`
	// ExtractorCurrent is this binary's extractor version; Matches is
	// null when the run came from an external indexer (not comparable).
	ExtractorCurrent string `json:"extractor_current"`
	ExtractorMatches *bool  `json:"extractor_matches"`
}

// MemberFreshness is one project member's freshness.
type MemberFreshness struct {
	Repository   string     `json:"repository"`
	Checkout     bool       `json:"checkout"`
	State        string     `json:"state"`
	ChangedCount *int       `json:"changed_count"`
	ChangedFiles []string   `json:"changed_files"`
	ChangedMore  int        `json:"changed_more"`
	Text         string     `json:"text,omitempty"`
	Error        string     `json:"error,omitempty"`
	Index        *IndexInfo `json:"index"`
	// Settings is the member's settings comparison; only the status tool
	// reports it (row MC-33), so it is not part of every envelope.
	Settings *settingsView `json:"-"`
}

// LinkPass is the project's last link pass.
type LinkPass struct {
	State        string   `json:"state"`
	Pass         int64    `json:"pass"`
	Links        int      `json:"links"`
	StaleLinks   int      `json:"stale_links"`
	StaleReasons []string `json:"stale_reasons"`
}

// Unknown is one thing the reused code could not tell, with the row that
// says so.
type Unknown struct {
	Kind string `json:"kind"`
	Row  string `json:"row"`
	Text string `json:"text"`
	// Data carries the reused code's own fields for this unknown
	// (verbatim), when it has any.
	Data any `json:"data,omitempty"`
}

// Assumption is one assumption a fact rests on.
type Assumption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Row  string `json:"row"`
}

// Bound is one cap that bound.
type Bound struct {
	Cap   string `json:"cap"`
	Limit int    `json:"limit"`
	Step  *int   `json:"step,omitempty"`
	// Beyond counts what the cap kept out; null when the reused code
	// names the cap without a count (change impact's caps list).
	Beyond *int   `json:"beyond"`
	Text   string `json:"text"`
	Row    string `json:"row"`
}

// Page is a tool's paging of a complete list (MC-4).
type Page struct {
	Returned   int    `json:"returned"`
	Total      int    `json:"total"`
	More       int    `json:"more"`
	NextCursor string `json:"next_cursor,omitempty"`
	Text       string `json:"text,omitempty"`
}

// ToolError is a refusal: not_indexed, not_found, ambiguous (with
// candidates), index_changed, bad_request, project_only or store_error.
type ToolError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Candidates []any  `json:"candidates,omitempty"`
}

func newEnvelope(tool string) *Envelope {
	return &Envelope{Tool: tool, Envelope: EnvelopeVersion, Request: map[string]any{}, Results: []any{},
		Unknowns: []Unknown{}, Assumptions: []Assumption{}, Bounds: []Bound{}, Labels: map[string]string{},
		Omitted: []Omission{}, labelSet: map[string]string{}, omitSet: map[string]string{}, assumeSet: map[string]string{}, changedNodes: map[string]string{}}
}

// unknown adds an unknown of row.
func (e *Envelope) unknown(row, kind, text string, data any) {
	tagRow(row)
	// A view object passed as data often carries the same sentence as
	// text; it is said once.
	if m, ok := data.(map[string]any); ok && str(m["text"]) == text {
		d := make(map[string]any, len(m))
		for k, v := range m {
			if k != "text" {
				d[k] = v
			}
		}
		data = d
	}
	e.Unknowns = append(e.Unknowns, Unknown{Kind: kind, Row: row, Text: text, Data: data})
}

// notModelled is row MC-D: a field, label or form the claim contract does
// not name is passed on as an unknown with its value, never dropped and
// never restated as a fact.
func (e *Envelope) notModelled(field string, value any) {
	e.unknown("MC-D", "not_modelled_by_mcp", "field "+field+" is not modelled by the MCP server; its value is passed on as served, with no claim", value)
}

// omit records a presentation field dropped from a view object (MC-6),
// with the standard way to get what it carried (omitHowDefault).
func (e *Envelope) omit(field string) {
	how, ok := omitHowDefault[field]
	if !ok {
		how = "presentation only: not served by the MCP server"
	}
	e.omitHow(field, how)
}

// omitHow records a field left out and how to get it (MC-6, MC-29).
func (e *Envelope) omitHow(field, how string) {
	tagRow("MC-6")
	e.omitSet[field] = how
}

// omitHowDefault says, per presentation field the tools drop, where its
// content is (MC-6). A field not listed here gets "presentation only".
var omitHowDefault = map[string]string{
	"ide":                "editor links: open the file at file and line; the source tool shows the lines",
	"shape_links":        "editor links of shape items: the source tool shows the lines",
	"node.ide":           "editor link: open the file at file and line; the source tool shows the lines",
	"node.attributes":    "the node's raw stored attributes: find, callers, impact and source do not serve them; endpoint (detail full), table (detail full), chain and unresolved serve the facts they claim from them",
	"node.content_hash":  "the stored content hash: file_state says whether the file still has it",
	"node.type":          "the record type (always node)",
	"node.level":         "the presentation level of the node kind",
	"via.evidence":       "evidence spans past the first location: the source tool shows the lines",
	"project.evidence":   "evidence spans of project links: the source tool of the member shows the lines",
	"project.ide_status": "which editor built the project link's editor link (editor links are left out): open the file at file and line",
	"link":               "editor links: open the file at file and line",
	"claims.link":        "editor links of claims: open the file at the claim's span",
	"map":                "the UI's folding hint (members drawn per step); every member is listed",
	"users.chain":        "the chain past the users (their own callers, ends and not-followed counts): call chain with the table and direction up",
	"items.ide":          "editor links of change items: open the file at the item's span",
	"paths.evidence":     "evidence spans of impact steps past the first: the source tool shows the lines",
	"calls.ide":          "editor links of calls: open the file at file and line",
	"precision.evidence": "the precision rules' oracle evidence: graphvein serve's Precision view or GET /api/v0/precision",
}

// fileState labels a served node's file state and remembers a file that
// changed or is missing since indexing, so the answer's freshness is
// checked against it (Server.recheck, R1). An empty state (a synthetic
// node) says nothing.
func (e *Envelope) fileState(file, state string) {
	if state == "" {
		return
	}
	e.label("file_state", state)
	if state != "unchanged" {
		e.changedNodes[file] = state
	}
}

// mapKeys maps a view object by its known keys (MC-6, MC-D; T030 review):
// a key in keep is copied, a key in omitted is left out and named in
// omitted with how to get it (as what.key; an empty how marks a key the
// caller maps itself), and any other key is passed on as an MC-D unknown.
// No key is dropped silently, now or after the view gains one.
func (e *Envelope) mapKeys(what string, m map[string]any, keep map[string]bool, omitted map[string]string) map[string]any {
	out := map[string]any{}
	for _, k := range sortedKeys(m) {
		switch how, omit := omitted[k]; {
		case keep[k]:
			out[k] = m[k]
		case omit && how == "":
		case omit:
			e.omitHow(what+"."+k, how)
		default:
			e.notModelled(what+"."+k, m[k])
		}
	}
	return out
}

func keySet(keys ...string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

// assume records an assumption a fact rests on, by its catalogue ID; the
// text is filled from the schema assumptions view when the call ends.
func (e *Envelope) assume(id, row string) {
	if id == "" {
		return
	}
	tagRow(row)
	if _, ok := e.assumeSet[id]; !ok {
		e.assumeSet[id] = row
	}
}

// finish sorts the label and omitted sets into their fields and drops
// repeated unknowns (the same stale link seen on several paths).
func (e *Envelope) finish() {
	seen := map[string]bool{}
	unknowns := e.Unknowns[:0]
	for _, u := range e.Unknowns {
		key, _ := json.Marshal(u)
		if !seen[string(key)] {
			seen[string(key)] = true
			unknowns = append(unknowns, u)
		}
	}
	e.Unknowns = unknowns
	for k, v := range e.labelSet {
		e.Labels[k] = v
	}
	e.Omitted = e.Omitted[:0]
	for _, f := range sortedStrings(e.omitSet) {
		e.Omitted = append(e.Omitted, Omission{Field: f, How: e.omitSet[f]})
	}
}

// Paging (D6): every list a tool pages has these limits.
const (
	DefaultLimit = 50
	MaxLimit     = 500
	// DefaultUnknownsLimit is how many items of each list an unknown
	// carries are shown by default (possibly missing call sites, sites
	// that may also use a table, external stubs); the total is always
	// exact and unknowns_cursor pages the rest (T030 review).
	DefaultUnknownsLimit = 20
	// DefaultTableUsers is the table tool's default page of users (R14).
	DefaultTableUsers = 20
)

// unknownsArgs are the paging arguments of the lists unknowns carry, on
// the tools whose unknowns carry long lists.
type unknownsArgs struct {
	UnknownsLimit  int    `json:"unknowns_limit,omitempty" jsonschema:"items of each list an unknown carries (call sites, sites, stubs; default 20, at most 500)"`
	UnknownsCursor string `json:"unknowns_cursor,omitempty" jsonschema:"the next_cursor of an unknown's list"`
}

// unknownsPaging checks and records the unknowns' list paging.
func (e *Envelope) unknownsPaging(a unknownsArgs) error {
	limit := a.UnknownsLimit
	if limit == 0 {
		limit = DefaultUnknownsLimit
	}
	offset, n, err := pageParams(limit, a.UnknownsCursor)
	if err != nil {
		return err
	}
	e.unknownsOffset, e.unknownsLimit = offset, n
	e.Request["unknowns_limit"], e.Request["unknowns_cursor"] = n, a.UnknownsCursor
	return nil
}

// listPage pages one list an unknown carries: the items from the
// unknowns cursor, at most the unknowns limit, with the exact total and,
// when more exist, the cursor of the rest (R14). Without paging arguments
// the list is whole.
func (e *Envelope) listPage(list []any) map[string]any {
	if list == nil {
		list = []any{}
	}
	total := len(list)
	if e.unknownsLimit == 0 {
		return map[string]any{"items": list, "total": total}
	}
	offset := min(e.unknownsOffset, total)
	end := min(offset+e.unknownsLimit, total)
	out := map[string]any{"items": append([]any{}, list[offset:end]...), "total": total}
	if end < total {
		out["more"] = total - end
		out["next_cursor"] = strconv.Itoa(end)
		e.omitHow("unknowns' list items past unknowns_limit", "each cut list says more and next_cursor: call again with unknowns_cursor")
	}
	return out
}

// pageParams checks limit and cursor: limit 0 is the default, the cursor
// is the opaque offset a previous page returned.
func pageParams(limit int, cursor string) (offset, n int, err error) {
	n = limit
	if n == 0 {
		n = DefaultLimit
	}
	if n < 0 || n > MaxLimit {
		return 0, 0, badRequest("limit must be 1 to %d, not %d", MaxLimit, limit)
	}
	if cursor != "" {
		offset, err = strconv.Atoi(cursor)
		if err != nil || offset < 0 {
			return 0, 0, badRequest("cursor %q is not one this server returned", cursor)
		}
	}
	return offset, n, nil
}

// page cuts items to one page and records it (MC-4): a cut list always
// says how many more exist and how to get them.
func page[T any](e *Envelope, items []T, offset, n int) []T {
	tagRow("MC-4")
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + n
	if end > total {
		end = total
	}
	out := append([]T{}, items[offset:end]...)
	p := &Page{Returned: len(out), Total: total, More: total - end}
	if p.More > 0 {
		p.NextCursor = strconv.Itoa(end)
		p.Text = fmt.Sprintf("%d more not shown; call again with cursor %q", p.More, p.NextCursor)
	}
	e.Page = p
	return out
}

// toolError is a refusal with its code.
type toolError struct {
	code       string
	message    string
	candidates []any
}

func (t *toolError) Error() string { return t.code + ": " + t.message }

func badRequest(format string, args ...any) error {
	return &toolError{code: "bad_request", message: fmt.Sprintf(format, args...)}
}

// decode reads a view's JSON with numbers kept exact.
func decode(raw json.RawMessage) (map[string]any, error) {
	var out map[string]any
	dec := json.NewDecoder(bytesReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding view answer: %w", err)
	}
	return out, nil
}

func sortedStrings(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Detail levels of the tools with a detail parameter (MC-29): compact by
// default, full on request. What compact leaves out is named in omitted.
const (
	detailCompact = "compact"
	detailFull    = "full"
)

func detailParam(d string) (string, error) {
	switch d {
	case "", detailCompact:
		tagRow("MC-29")
		return detailCompact, nil
	case detailFull:
		return detailFull, nil
	}
	return "", badRequest("detail must be compact or full, not %q", d)
}
