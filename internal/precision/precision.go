// Package precision decides Graphvein's precision labels (ADR 0066).
//
// A label is `precise` only when a row of the rule table (rules.go) names
// the scope and the run was produced by the built-in indexer at the
// binary's extractor version. Everything else falls to the single default
// branch, `best_effort`. The package holds no state and reads no store: the
// scanner asks FileLabel for each file's language, and the server asks
// ScopeLabel for each fact family it presents. Extractors never decide a
// label themselves, so the table stays the one place a test keeps 1:1 with
// docs/contract/claims/precision-labels.md and docs/extract/precision.md.
package precision

// Label is a scope's precision label. Its values are the contract v1 file
// `precision` enum, so a file label can be written to the file record as is.
type Label string

const (
	// Precise means the scope's claim contract, goldens, corpus checks,
	// independent review and oracle (where cheap) all exist. It never means
	// complete: unread forms stay unknown.
	Precise Label = "precise"
	// BestEffort is the default branch: no row of the rule table applies.
	BestEffort Label = "best_effort"
)

// DefaultRow is the row ID of the default branch in the rule table, the
// task file and docs/extract/precision.md.
const DefaultRow = "D"

// Reasons a scope label falls to the default branch. The server serves them
// so a viewer can tell "no row earns precise" from "a row exists but this
// run cannot use it".
const (
	// ReasonDefault: no row of the rule table names the scope (or the
	// marker fails the row's own refusal, such as a non-postgres sqlx set).
	ReasonDefault = "default_branch"
	// ReasonExternalIndexer: the run was produced by an external indexer
	// (ADR 0010), so Graphvein's evidence says nothing about its facts.
	ReasonExternalIndexer = "external_indexer"
	// ReasonNoRun: no run is stored for the repository, so there are no
	// facts to label and no producer to name. A run header always names
	// its indexer (header.schema.json requires a non-empty indexer.name
	// and the store's BeginRun validates it), so an unnamed run cannot
	// occur; only a missing run needs its own reason (T020 review).
	ReasonNoRun = "no_run"
	// ReasonExtractorMismatch: the run's extractor version differs from the
	// binary's, so the rule table that earned the label may not be the one
	// that produced the facts.
	ReasonExtractorMismatch = "extractor_mismatch"
	// ReasonMarkerConflict: no row matches because the scope's records
	// disagree on the marker (two tools or two dialects in one group), so
	// no single row can be named. Each tool may have a contract of its own;
	// the label says only that none applies to the group as a whole.
	ReasonMarkerConflict = "marker_conflict"
)

// Reasons lists every reason ScopeLabel can return, in a fixed order. The
// server adds its own reasons to it and serves the whole list's words in
// the UI, so a new reason here needs words there (a test checks both).
func Reasons() []string {
	return []string{ReasonDefault, ReasonMarkerConflict, ReasonNoRun, ReasonExternalIndexer, ReasonExtractorMismatch}
}

// Family names a fact family the server presents. Only the families with a
// rule-table row have constants here; any other value (routes, client
// calls, Angular, Tauri, OpenAPI, ...) is a valid marker that falls to the
// default branch.
type Family string

const (
	// FamilyMigrationSchema: tables, columns, indexes and migrations read
	// from a SQL migration set (ADR 0047), with the set's tool and dialect.
	FamilyMigrationSchema Family = "schema_migrations"
	// FamilyModelSchema: tables and columns read from ORM models, with the
	// group's `tool` attribute (ADR 0053).
	FamilyModelSchema Family = "schema_models"
	// FamilyTableAccess: `reads_table` / `writes_table` edges and the
	// owner's `table_access` rows (ADR 0060).
	FamilyTableAccess Family = "table_access"
	// FamilyCrossRepositoryLinks: the links a project link pass stores
	// between members of a multi-repository project (ADR 0075, T022 B11).
	// They are never precise, whatever the members' labels: each rests on
	// an assumption the evidence does not prove (a base URL, or a
	// declaration in graphvein-project.yaml), which every view shows with
	// the link, and its confidence is the path match tier's. No row may
	// name this family (TestPrecisionRules), and ScopeLabel answers the
	// default branch for it before any row is consulted.
	FamilyCrossRepositoryLinks Family = "cross_repository_links"
	// FamilyRoutes is the route nodes and their exposes_route edges, the
	// marker's Tool being the route's framework (T023 GO4).
	FamilyRoutes Family = "routes"
)

// neverPrecise lists the families no row may label precise: a row naming
// one is a contract change, not a table row (docs/extract/precision.md
// "Cross-repository links").
var neverPrecise = map[Family]bool{FamilyCrossRepositoryLinks: true}

// NeverPrecise reports whether f is a family no row may label precise.
// The test of the rule table uses it to refuse such a row.
func NeverPrecise(f Family) bool { return neverPrecise[f] }

// Marker is what the server knows about a fact's scope from stored data.
// The zero Marker names no scope and always falls to the default branch.
type Marker struct {
	Family Family
	// Tool is the migration set's or model group's `tool` attribute
	// ("sqlx", "prisma", ...); empty when unknown or conflicting.
	Tool string
	// Dialect is the migration set's `dialect` attribute; empty when
	// unknown or conflicting. Model rows do not look at it (open point O13:
	// mapped dialects keep their per-fact "unverified" label).
	Dialect string
	// Conflict is true when the scope's records name more than one tool or
	// dialect, which is why Tool or Dialect is empty. It never makes a row
	// match; it only says why none did (ReasonMarkerConflict).
	Conflict bool
}

// Run describes the index run whose facts are being labelled.
type Run struct {
	// BuiltIn is true when Graphvein's own indexer produced the run, false
	// for an external indexer (ADR 0010).
	BuiltIn bool
	// NoRun is true when no run is stored: it is refused as no_run rather
	// than described as an external indexer's.
	NoRun bool
	// ExtractorVersion is the run's stored extractor version.
	ExtractorVersion string
	// BinaryVersion is the running binary's extractor version
	// (index.Version). The package cannot import index (index imports the
	// scanner, which imports this package), so the caller passes it.
	BinaryVersion string
}

// Refusal returns why no row may label this run's facts, or "" when the run
// is eligible.
func (r Run) Refusal() string {
	if r.NoRun {
		return ReasonNoRun
	}
	if !r.BuiltIn {
		return ReasonExternalIndexer
	}
	if r.ExtractorVersion == "" || r.ExtractorVersion != r.BinaryVersion {
		return ReasonExtractorMismatch
	}
	return ""
}

// Decision is a scope label with the row that decided it. Row is
// DefaultRow when the default branch applies, and Reason then says why.
type Decision struct {
	Label  Label
	Row    string
	Reason string
}

// FileLabel returns the contract v1 file `precision` for a file of the given
// scanner language. It is the language's structural scope only: a precise
// fact family read from the file never raises it and a best-effort one never
// lowers it. The scanner is the built-in indexer at the binary's version, so
// no run refusal applies here. The indexer may still store `best_effort` for
// one file of a precise language whose structure it refused (a file the
// language's own parser rejects, for one): the label is an upper bound for
// the file, never a promise that every file reaches it.
func FileLabel(language string) string {
	if FileRow(language) != DefaultRow {
		return string(Precise)
	}
	return string(BestEffort)
}

// FileRow returns the ID of the file row (PL-L*) that names the language,
// or DefaultRow when none does. The server serves it with a file label, so
// the UI can show the row's evidence without joining anything itself.
func FileRow(language string) string {
	for _, r := range rules {
		if r.Kind != KindFile {
			continue
		}
		for _, l := range r.Languages {
			if l == language {
				return r.ID
			}
		}
	}
	return DefaultRow
}

// ScopeLabel returns the label of a fact family's scope for facts of the
// given run. The run refusals come first, so no caller can label an external
// or mismatched run's facts precise by forgetting a check.
func ScopeLabel(run Run, m Marker) Decision {
	if reason := run.Refusal(); reason != "" {
		return Decision{Label: BestEffort, Row: DefaultRow, Reason: reason}
	}
	if neverPrecise[m.Family] {
		return Decision{Label: BestEffort, Row: DefaultRow, Reason: ReasonDefault}
	}
	for _, r := range rules {
		if r.Kind == KindFamily && r.Match.matches(m) {
			return Decision{Label: Precise, Row: r.ID}
		}
	}
	if m.Conflict {
		return Decision{Label: BestEffort, Row: DefaultRow, Reason: ReasonMarkerConflict}
	}
	return Decision{Label: BestEffort, Row: DefaultRow, Reason: ReasonDefault}
}

// matches reports whether a marker falls in the row's scope. An empty
// Dialects list means the row does not look at the dialect.
func (s Match) matches(m Marker) bool {
	if m.Family == "" || m.Family != s.Family {
		return false
	}
	if len(s.Tools) > 0 && !contains(s.Tools, m.Tool) {
		return false
	}
	if len(s.Dialects) > 0 && !contains(s.Dialects, m.Dialect) {
		return false
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
