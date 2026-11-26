package server

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/graphvein/graphvein/internal/extract/models"
	// The SQLModel front end registers its catalogue in models (T018g0).
	_ "github.com/graphvein/graphvein/internal/extract/models/sqlmodel"
	"github.com/graphvein/graphvein/internal/extract/schema"
	"github.com/graphvein/graphvein/internal/extract/tableaccess"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/store"
)

// GET /api/v0/schema/assumptions (T018f, ADR 0055 option 6A): the
// assumption catalogue of this Graphvein build, built once per process
// from the Go tables of the two extractors. It presents no nodes, so it
// has no links and no editor parameter.

type schemaAssumptionsResponse struct {
	APIVersion int      `json:"api_version"`
	Repository string   `json:"repository"`
	Index      *lastRun `json:"index"`
	// Extractor is the serving binary's aggregate version: the catalogue
	// is the build's, which may differ from index.indexer.version.
	Extractor   string                                   `json:"extractor"`
	Assumptions []schemaAssumption                       `json:"assumptions"`
	Rows        map[string]map[string]schemaCatalogueRow `json:"rows"`
}

type schemaAssumption struct {
	ID         string  `json:"id"`
	Source     string  `json:"source"`
	Title      string  `json:"title"`
	Text       string  `json:"text"`
	FoldedInto *string `json:"folded_into"`
}

// schemaCatalogueRow is one contract row or precondition. Rows are keyed
// by source, then id, because the contracts reuse ids (M1, D, ...). Model
// rows are keyed per tool, under "models/<tool>" (Tool names it), because
// every model contract names its default row D: one shared "models" key
// would let the last registered tool's D replace the others' (T018g6
// Revision 3 addendum 3, OP2). Their assumptions stay in the "models"
// source, whose ids are distinct across tools.
//
// Branch is the form the row covers as plain text (plainText of the
// migration contract's form cell); null for model rows, whose code table
// names the Go function that holds the branch, which is no form a user
// reads (T018f Revision 2, C3).
type schemaCatalogueRow struct {
	Source      string   `json:"source"`
	Tool        *string  `json:"tool"`
	Branch      *string  `json:"branch"`
	Summary     string   `json:"summary"`
	Assumptions []string `json:"assumptions"`
}

var (
	catalogueOnce sync.Once
	catalogue     schemaAssumptionsResponse
	// rowAssumptions are the migration contract's assumption sets by row
	// and precondition id.
	rowAssumptions map[string][]string
)

func buildCatalogue() {
	catalogue = schemaAssumptionsResponse{Extractor: index.Version, Assumptions: []schemaAssumption{}, Rows: map[string]map[string]schemaCatalogueRow{sourceMigrations: {}}}
	rowAssumptions = map[string][]string{}
	folded := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	text := func(id, raw string) string {
		if t, ok := assumptionDisplay[id]; ok {
			return t
		}
		return plainText(raw)
	}
	for _, a := range schema.Assumptions() {
		catalogue.Assumptions = append(catalogue.Assumptions, schemaAssumption{ID: a.ID, Source: sourceMigrations, Title: migrationText(a.Title), Text: text(a.ID, a.Text), FoldedInto: folded(a.FoldedInto)})
	}
	// Model contracts, one per registered ORM front end (T018g0): their
	// assumption ids are distinct across tools (later tools use a prefix),
	// so they share the "models" source; their rows are keyed per tool
	// below.
	tools := models.Catalogues()
	for _, c := range tools {
		for _, a := range c.Assumptions {
			catalogue.Assumptions = append(catalogue.Assumptions, schemaAssumption{ID: a.ID, Source: sourceModels, Title: plainText(a.Title), Text: text(a.ID, a.Text), FoldedInto: folded(a.FoldedInto)})
		}
	}
	// The code-to-table contract (T018h, ADR 0060): its assumptions are
	// what an inferred reads_table or writes_table edge's `assumes` ids
	// name (the change impact panel shows them, T019b part 2). Its rows are
	// not schema facts, so the catalogue lists no rows for this source.
	for _, a := range tableaccess.AssumptionTexts {
		catalogue.Assumptions = append(catalogue.Assumptions, schemaAssumption{ID: a.ID, Source: sourceTableAccess, Title: plainText(a.Title), Text: plainText(a.Text)})
	}
	// The Supabase rows' assumption (T024c2): what `assumes: SB-V1` on a
	// Supabase table_access entry means.
	for _, a := range tableaccess.SupabaseAssumptionTexts {
		catalogue.Assumptions = append(catalogue.Assumptions, schemaAssumption{ID: a.ID, Source: sourceTableAccess, Title: plainText(a.Title), Text: plainText(a.Text)})
	}
	for _, r := range schema.Contract() {
		form := migrationText(r.Branch)
		catalogue.Rows[sourceMigrations][r.ID] = schemaCatalogueRow{Source: sourceMigrations, Branch: &form, Summary: migrationText(r.Summary), Assumptions: r.Assumptions}
		rowAssumptions[r.ID] = r.Assumptions
	}
	for _, c := range tools {
		key := modelRowsKey(c.Tool)
		catalogue.Rows[key] = map[string]schemaCatalogueRow{}
		for _, r := range c.Rows {
			tool := c.Tool
			catalogue.Rows[key][r.ID] = schemaCatalogueRow{Source: sourceModels, Tool: &tool, Summary: plainText(r.Summary), Assumptions: r.Assumptions}
		}
	}
}

// modelRowsKey is the rows key of one model tool's contract rows.
func modelRowsKey(tool string) string {
	return sourceModels + "/" + tool
}

// assumptionDisplay replaces the served text of an assumption whose
// document text is about the contract's revisions rather than the
// assumption itself (T018f Revision 2, C2). The documents keep their text;
// TestCatalogue checks the Go tables against them.
var assumptionDisplay = map[string]string{
	"E7": "Runtime resources the migrations need are available; now covered by E8 (History applies).",
	"V7": "The model modules import successfully, as they do when the application runs; an import failure the source proves is reported, not assumed away.",
}

// Process markers of the contract documents that plainText removes: a
// revision reference ("Revision 1.6 V4", "Rev 3.3", optionally "as
// replaced by" another) with an optional parenthetical note, and a review
// finding pair ("B2, g5-s80").
var (
	revisionRef     = `Rev(?:ision)? \d+(?:\.\d+)*(?: [A-Z][A-Za-z]?\d+[a-z]?)?(?: as replaced by Rev(?:ision)? \d+(?:\.\d+)*(?: [A-Z][A-Za-z]?\d+[a-z]?)?)?`
	markerOnlyParen = regexp.MustCompile(`\s*\(` + revisionRef + `\)`)
	markerLeadParen = regexp.MustCompile(`\(` + revisionRef + `[;,:]\s*`)
	markerTailParen = regexp.MustCompile(`:\s*` + revisionRef + `\)`)
	markerPrefix    = regexp.MustCompile(revisionRef + `(?: \([^)]*\))?:\s*`)
	markerInline    = regexp.MustCompile(`\s*` + revisionRef)
	reviewPair      = regexp.MustCompile(`\s*\([A-Z]\d+, [a-z]\d+-s\d+\)`)
	doubleSpace     = regexp.MustCompile(` {2,}`)
	// Finding ids of the migration contract's revisions (R, U, V, W, X, Y
	// series) used as a clause label ("; Y45: ...") or alone in
	// parentheses ("(W2)"). E ids are assumptions and stay; the model
	// contract's V ids are assumptions too, so this applies to migration
	// cells only.
	findingLabel = regexp.MustCompile(`([;(]) [RUVWXY]\d{1,2}[a-z]?: `)
	findingParen = regexp.MustCompile(`\s*\([RUVWXY]\d{1,2}[a-z]?\)`)
	// markdownEmphasis drops Markdown emphasis and code marks; "reg*" (the
	// Postgres OID alias types regclass, regtype, ...) is the one star the
	// documents use as text, and is spelled out.
	markdownEmphasis = strings.NewReplacer("**", "", "`", "", "reg*", "regclass-family")
)

// plainText is the user-facing text of a contract cell or assumption
// (T018f Revision 2, C2): the documents' Markdown emphasis and code marks
// and their process markers (revision and review references) removed, the
// words kept. The documents and the Go tables stay as written and 1:1;
// only what the API serves is plain. TestCataloguePlainText rejects any
// served string that still holds a mark.
func plainText(s string) string {
	return plain(s)
}

// migrationText is plainText for a migration contract cell, which also
// drops the revisions' finding labels.
func migrationText(s string) string {
	s = plain(s)
	s = findingLabel.ReplaceAllString(s, "$1 ")
	s = findingParen.ReplaceAllString(s, "")
	return strings.TrimSpace(doubleSpace.ReplaceAllString(s, " "))
}

func plain(s string) string {
	s = markdownEmphasis.Replace(s)
	s = reviewPair.ReplaceAllString(s, "")
	s = markerOnlyParen.ReplaceAllString(s, "")
	s = markerLeadParen.ReplaceAllString(s, "(")
	s = markerTailParen.ReplaceAllString(s, ")")
	s = markerPrefix.ReplaceAllString(s, "")
	s = markerInline.ReplaceAllString(s, "")
	s = doubleSpace.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, " ;", ";")
	s = strings.ReplaceAll(s, "; ;", ";")
	s = strings.ReplaceAll(s, "( ", "(")
	return strings.TrimSpace(s)
}

// migrationRowAssumptions returns the migration contract's assumption sets
// by row and precondition id (an id not listed rests on none).
func migrationRowAssumptions() map[string][]string {
	catalogueOnce.Do(buildCatalogue)
	return rowAssumptions
}

func computeSchemaAssumptions(h *apiHandler, _ url.Values) (readFunc, error) {
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		catalogueOnce.Do(buildCatalogue)
		out := catalogue
		out.Repository, out.Index = h.opt.Repository, indexOf(runs)
		return out, nil
	}, nil
}
