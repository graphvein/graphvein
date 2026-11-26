// Package supabasetypes reads Supabase's generated TypeScript types (the
// Supabase CLI's `supabase gen types typescript`, rendered by
// postgres-meta's typescript template) as a schema source labelled
// "generated types" (T024c3, rows SB-G1 and SB-G2 of
// docs/contract/claims/nextjs-supabase.md, copied into
// docs/extract/schema-supabase-types.md; ADR 0077). A file is read only
// when its declarations are exactly the generator's: anything else is no
// schema source. Its tables go through the model kit (ADR 0058) as group
// models/supabase_types/<file>; a package that has a Supabase migration
// set keeps the set as its schema source and the file is only compared
// with it (disagreements are diagnostics, never resolved).
package supabasetypes

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// Tool is the group tool of tables read from generated types.
const Tool = "supabase_types"

// Label is how the Schema view names the source of these tables.
const Label = "generated types"

// Input carries the repository's link-time sources (every scanned file)
// and the migration pass's records, for SB-G2.
type Input struct {
	Sources    map[string][]byte
	Migrations []contract.Node
	Edges      []contract.Edge
}

// Diagnostic codes.
const (
	codeUnread       = "supabase_types_unread"
	codeCompared     = "supabase_types_compared"
	codeDisagreement = "supabase_types_disagreement"
)

// claimTag and faultHook are test-only hooks (as in the other front ends).
var (
	claimTag  func(row string)
	faultHook func(step, subject string)
)

func tag(row string) {
	if claimTag != nil {
		claimTag(row)
	}
}

// rows names SB-G1 for the kit's node shapes. Foreign keys are not
// resolved (see buildTable) and the migration comparison of the kit has
// no SB row.
var rows = models.Rows{Emit: "SB-G1"}

// pins names the generator versions whose template defines the accepted
// form (read from the npm package @supabase/postgres-meta, MIT).
var pins = map[string]any{"postgres-meta": "0.70.0 to 0.97.0 (dist/server/templates/typescript.js)"}

// platformSchemas are the Supabase platform schemas (SB-V2) and the API
// schema graphql_public: the generated types list them, migrations never
// create them, so SB-G2 does not compare them.
var platformSchemas = map[string]bool{"auth": true, "storage": true, "extensions": true, "realtime": true, "vault": true, "graphql_public": true}

// Link reads every generated types file of the repository. A failure in
// one file never stops the others: it becomes a diagnostic.
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	steps := &models.Steps{Hook: faultHook}
	sets := supabaseSets(in)
	var nodes []contract.Node
	var edges []contract.Edge
	for _, file := range candidates(in.Sources) {
		steps.Guard(file, "generated types", "Supabase generated types "+file, func() {
			n, e := linkFile(file, in, sets, steps)
			nodes = append(nodes, n...)
			edges = append(edges, e...)
		})
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, edges, steps.Diagnostics
}

// candidates are the .ts files whose first declaration is the generator's
// `export type Json` and that declare Database: only these can be
// generated types, so only these get an unread diagnostic when their
// shape differs (a hand-written Database type elsewhere is D).
func candidates(sources map[string][]byte) []string {
	var out []string
	for f, src := range sources {
		if !strings.HasSuffix(f, ".ts") {
			continue
		}
		body := bytes.TrimLeft(bytes.TrimPrefix(src, []byte("\xef\xbb\xbf")), " \t\r\n")
		if bytes.HasPrefix(body, []byte("export type Json")) && (bytes.Contains(src, []byte("export type Database")) || bytes.Contains(src, []byte("export interface Database"))) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func linkFile(file string, in Input, sets map[string][]*migrationSet, steps *models.Steps) ([]contract.Node, []contract.Edge) {
	tf, forms, err := parseFile(in.Sources[file])
	if err != nil {
		tag("SB-G1/" + codeUnread)
		steps.Diag(file, "info", codeUnread, fmt.Sprintf("%s: not read as Supabase generated types (row SB-G1): %v", file, err))
		return nil, nil
	}
	pkg, hasPkg := npmPackage(file, in.Sources)
	if hasPkg && len(sets[pkg]) > 0 {
		// SB-G2: the package's migration set is the schema source.
		tag("SB-G2")
		compare(file, tf, sets[pkg], steps)
		return nil, nil
	}
	tag("SB-G1")
	for f := range forms {
		tag("SB-G1/" + f)
	}
	var tables []*models.Table
	for _, s := range tf.schemas {
		for i := range s.tables {
			tables = append(tables, buildTable(file, pkg, hasPkg, tf, s.name, &s.tables[i]))
		}
		if len(s.views) > 0 {
			tag("SB-G1/Views")
		}
	}
	if len(tables) == 0 {
		return nil, nil
	}
	g := models.Group{Tool: Tool, Root: file, Pins: pins, Assumptions: []string{"SB-V6"}, Dialect: "postgres", Verification: models.VerificationMapped}
	return models.Link(tables, models.LinkInput{Group: g, Rows: rows, Tag: tag, Steps: steps, Sources: in.Sources, Migrations: in.Migrations, Edges: in.Edges})
}

// buildTable is SB-G1's claim branch for one Tables entry: the table
// <schema>.<name>; its columns are the Insert properties (the generator
// writes every column there, and only columns), named and with
// nullability from the Row type; the TypeScript type is kept as written
// (tool_type, not a database fact). Keys, defaults and constraints are
// not stated by the generator: unknown. Relationships are listed as
// written but not resolved to foreign keys: the generator writes no
// referenced schema, and some versions leave out keys to other schemas.
func buildTable(file, pkg string, hasPkg bool, tf *typesFile, schema string, r *relation) *models.Table {
	schemaName := schema
	t := &models.Table{
		Identity: "table:" + file + "#" + schema + "." + r.name, File: file, Name: r.name,
		Model: schema + "." + r.name, ModelLine: r.line, Span: contract.Span{StartLine: r.line, EndLine: r.endLine},
		Schema: &schemaName, SchemaKnown: true,
		ColumnsComplete: true, ConstraintsComplete: false, PrimaryKeyOpen: true, ElementsUnknown: true,
		Verification: models.VerificationMapped,
	}
	t.Reasons = []models.Reason{{Row: "SB-G1", Fact: models.Fact("constraint_set"), File: file, Line: r.line,
		Reason: "generated types state no primary key, unique, index or check constraint"}}
	var computed []string
	for _, f := range r.row[len(r.insert):] {
		computed = append(computed, f.name)
	}
	rels := []map[string]any{}
	for _, rel := range r.rels {
		var one any
		if rel.oneToOne != nil {
			one = *rel.oneToOne
		}
		rels = append(rels, map[string]any{"foreign_key_name": rel.name, "columns": rel.columns, "referenced_relation": rel.refRel, "referenced_columns": rel.refColumns, "is_one_to_one": one, "line": rel.line})
	}
	attrs := map[string]any{"label": Label, "types_header": tf.header, "relationships": rels, "computed_fields": nonNilStrings(computed)}
	if hasPkg {
		attrs["package"] = pkg
	} else {
		attrs["package"] = nil
	}
	t.Attributes = attrs
	for i, ins := range r.insert {
		row := r.row[i]
		var nullable any
		if row.nullable != nil {
			nullable = *row.nullable
		}
		c := &models.Column{Name: ins.name, Attribute: ins.name, File: file, Line: row.line,
			Attributes:         map[string]any{"type": nil, "tool_type": row.typ, "nullable": nullable, "primary_key": nil, "unique": nil, "default": nil},
			Claims:             map[string]models.ClaimSource{"name": {Row: "SB-G1"}, "nullable": {Row: "SB-G1"}},
			ForeignKeysUnknown: true,
		}
		c.Reasons = []models.Reason{{Row: "SB-G1", Fact: models.Fact("foreign_keys"), File: file, Line: row.line, Reason: "generated types name no referenced schema; relationships are listed on the table as written"}}
		if row.nullable == nil {
			c.Reasons = append(c.Reasons, models.Reason{Row: "SB-G1", Fact: models.Fact("nullable"), File: file, Line: row.line, Reason: "type " + row.typ + " includes null; the generator writes no | null beside it"})
		}
		t.Columns = append(t.Columns, c)
	}
	return t
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// npmPackage returns the directory of the package.json nearest to file
// ("." for the root); ok is false when there is none.
func npmPackage(file string, sources map[string][]byte) (string, bool) {
	dir := path.Dir(file)
	for {
		if _, ok := sources[path.Join(dir, "package.json")]; ok {
			return dir, true
		}
		if dir == "." || dir == "/" || dir == "" {
			return "", false
		}
		dir = path.Dir(dir)
	}
}
