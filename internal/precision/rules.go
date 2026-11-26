package precision

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Kind says what a row labels.
type Kind string

const (
	// KindFile rows label a language's structural scope (files,
	// declarations, imports): the contract v1 file `precision`. Steps S1-S4
	// of T020 add them (PL-L1..PL-L4), each after its evidence exists.
	KindFile Kind = "file"
	// KindFamily rows label a fact family's scope; the server computes the
	// label, nothing stores it.
	KindFamily Kind = "family"
)

// Match is the marker condition of a family row.
type Match struct {
	Family   Family
	Tools    []string
	Dialects []string
}

// TestRef names a test function in a package directory (slash path from
// the module root). TestPrecisionRules parses the package's test files to
// prove the function exists.
type TestRef struct {
	Package string
	Name    string
}

// Citation is a verbatim quote from an ADR's "Review notes" section.
// TestPrecisionRules checks the quote is there (whitespace and Markdown bold
// markers are normalised), so a row cannot cite evidence the notes lack.
type Citation struct {
	ADR   string
	Quote string
}

// Rule is one row of the label rule table: a scope that earns `precise` and
// the evidence of the v1 verification bar it rests on (P1-P5 of the task
// file). Evidence names are data the API serves; they never change a label
// on their own.
type Rule struct {
	ID    string
	Scope string
	Kind  Kind
	// Languages are the scanner language names a file row labels.
	Languages []string
	// Match is the marker condition of a family row.
	Match Match
	// Marker describes in prose what is labelled.
	Marker string
	// Contract (P1) names the claim-contract tests.
	Contract []TestRef
	// Goldens (P2) names fixture goldens under testdata/fixtures or
	// testdata/repos (without ".golden.json").
	Goldens []string
	// Corpus (P3) cites the corpus results in ADR review notes.
	Corpus []Citation
	// Review (P4) cites the independent review in ADR review notes.
	Review []Citation
	// Oracle (P5) names the oracle tests; OracleNone states why no oracle
	// is cheap when Oracle is empty.
	Oracle     []TestRef
	OracleNone string
	// Refusal describes, beyond the global run refusals, when the scope's
	// facts fall to the default branch.
	Refusal string
	// Task names the task that added a family row after the T020 record
	// was written (its own task file plans it); "" for T020's rows.
	Task string
}

const (
	pkgSchema      = "internal/extract/schema"
	pkgSQLModel    = "internal/extract/models/sqlmodel"
	pkgPrisma      = "internal/extract/models/prisma"
	pkgDrizzle     = "internal/extract/models/drizzle"
	pkgSequelize   = "internal/extract/models/sequelize"
	pkgDiesel      = "internal/extract/models/diesel"
	pkgGorm        = "internal/extract/models/gorm"
	pkgTypeORM     = "internal/extract/models/typeorm"
	pkgTableAccess = "internal/extract/tableaccess"
	pkgGo          = "internal/lang/golang"
	pkgRoutes      = "internal/extract/routes"
	pkgPython      = "internal/lang/python"
	pkgTypeScript  = "internal/lang/typescript"
	pkgRust        = "internal/lang/rust"

	refusalModel = "none beyond the global ones"
)

// modelRule builds a row for one ORM front end; the seven model rows share
// marker shape and refusal.
func modelRule(id, scope, tool, pkg, contract string, goldens []string, corpus, review Citation, oracle ...string) Rule {
	r := Rule{
		ID: id, Scope: scope, Kind: KindFamily,
		Match:    Match{Family: FamilyModelSchema, Tools: []string{tool}},
		Marker:   fmt.Sprintf("model `table` / `column` nodes and their edges in groups with `tool` `%s`", tool),
		Contract: []TestRef{{pkg, contract}},
		Goldens:  goldens,
		Corpus:   []Citation{corpus},
		Review:   []Citation{review},
		Refusal:  refusalModel,
	}
	for _, o := range oracle {
		r.Oracle = append(r.Oracle, TestRef{pkg, o})
	}
	return r
}

// rules is the label rule table. A row is added only when every piece of
// its evidence exists; TestPrecisionRules checks that and pins the table's
// digest to index.Version (any change needs an extractor version bump).
var rules = []Rule{
	{
		ID: "PL-F1", Scope: "SQL migration schema, sqlx sets, postgres dialect", Kind: KindFamily,
		Match:    Match{Family: FamilyMigrationSchema, Tools: []string{"sqlx"}, Dialects: []string{"postgres"}},
		Marker:   "`migration`, `table`, `column`, `db_index` nodes of a migration set with tool `sqlx` and dialect `postgres`; their `contains`, `alters`, `foreign_key` edges",
		Contract: []TestRef{{pkgSchema, "TestContractRows"}, {pkgSchema, "TestContractDocument"}},
		Goldens:  []string{"sql-migrations"},
		Corpus:   []Citation{{"0051", "Corpora: ten corpora identical to main except 14 axum schema records"}},
		Review: []Citation{
			{"0047", "Nine reviews by fresh reviewers found confident wrong facts in rarer forms; Revisions 2-9 closed them as classes"},
			{"0051", "Revision 1.4 (after the independent review): class rules R1-R5"},
		},
		Oracle:  []TestRef{{pkgSchema, "TestOracleFixtures"}, {pkgSchema, "TestOracleRealHistories"}},
		Refusal: "a set with another or no dialect (`schema_unsupported_dialect`), or another tool",
	},
	modelRule("PL-F2", "SQLModel / SQLAlchemy models", "sqlmodel", pkgSQLModel, "TestModelClaimContract",
		[]string{"sqlmodel-models", "sqlmodel-link"},
		Citation{"0054", "Corpus oracle (Python 3.14.6, corpus locks as constraints): fastapi 2 tables, 12 columns"},
		Citation{"0054", "Independent review: no class-A finding."},
		"TestModelOracle"),
	modelRule("PL-F3", "Prisma models", "prisma", pkgPrisma, "TestPrismaContract",
		[]string{"prisma-models"},
		Citation{"0057", "corpora umami 7.10.0 (2,513 facts: 26/26 tables complete"},
		Citation{"0057", "Independent review: one class A (sqlserver `@unique` is a UNIQUE constraint; was claimed as an `oracle` unique index) and B1-B3, C1-C5, all fixed as class rules"},
		"TestPrismaOracle"),
	modelRule("PL-F4", "Drizzle models", "drizzle", pkgDrizzle, "TestDrizzleContract",
		[]string{"drizzle-models"},
		Citation{"0059", "kaneo 5,349 facts (51 tables, 442 columns, 66 foreign keys, 83 indexes), dokploy 9,499 facts"},
		Citation{"0059", "Independent review: no wrong stored fact on probes"},
		"TestDrizzleOracle", "TestDrizzleOracleForms"),
	modelRule("PL-F5", "Sequelize models", "sequelize", pkgSequelize, "TestSequelizeContract",
		[]string{"sequelize-models"},
		Citation{"0061", "opencollective-api 6,952, Gladys 3,077, 0 disagreements"},
		Citation{"0061", "3 (independent review: mssql FK actions, instance hooks, non-unique target keys"},
		"TestSequelizeOracle"),
	modelRule("PL-F6", "Diesel models", "diesel", pkgDiesel, "TestDieselContract",
		[]string{"diesel-models"},
		Citation{"0062", "Corpus snapshot: 16 corpora identical to main; crates.io gains only Diesel records"},
		Citation{"0062", "Independent review (DR): class A findings all fixed"},
		"TestDieselOracle"),
	modelRule("PL-F7", "GORM models", "gorm", pkgGorm, "TestGormContract",
		[]string{"gorm-models"},
		Citation{"0063", "Corpus snapshot: 16 of 18 corpora identical to main, gotify gains 5 complete tables, realworld-gin 8"},
		Citation{"0063", "Independent review (GR): class A findings all fixed"},
		"TestGormOracle"),
	modelRule("PL-F8", "TypeORM models", "typeorm", pkgTypeORM, "TestTypeORMContract",
		[]string{"typeorm-models"},
		Citation{"0064", "Corpus snapshot: 18 shared corpora identical to main; seerr 14, nest-admin 17"},
		Citation{"0064", "Independent review (TR): class A findings all fixed"},
		"TestTypeORMOracle"),
	{
		ID: "PL-F9", Scope: "Code-to-table links", Kind: KindFamily,
		// Links into tables of these tools only (the target table's group
		// tool): Supabase tables (T024c2) earn no label until the SB rows
		// carry their own evidence.
		Match:    Match{Family: FamilyTableAccess, Tools: []string{"sqlx", "sqlmodel", "prisma", "drizzle"}},
		Marker:   "`reads_table` / `writes_table` edges and the owner's `table_access` rows",
		Contract: []TestRef{{pkgTableAccess, "TestTableAccessContract"}},
		Goldens:  []string{"table-access-sqlmodel", "table-access-prisma", "table-access-drizzle", "table-access-sqlx"},
		Corpus:   []Citation{{"0060", "corpora axum 24, axum-ddd 29, langflow 68, kaneo 19, umami 1"}},
		Review:   []Citation{{"0060", "5 (review: build-then-execute role, FROM-position function keywords"}},
		Oracle:   []TestRef{{pkgTableAccess, "TestSQLTablesOracle"}, {pkgTableAccess, "TestSQLAlchemyOracle"}},
		Refusal:  "owner sites in `table_access_unknown` stay unknown (not labelled)",
	},
	{
		ID: "PL-F10", Scope: "Go HTTP routes", Kind: KindFamily, Task: "T023",
		Match:    Match{Family: FamilyRoutes, Tools: []string{"net/http", "chi", "gin"}},
		Marker:   "`route` nodes with `framework` `net/http`, `chi` or `gin`: route existence, `method`, `pattern`, `host` and the path entries' `template`, `trailing_slash` and `resolution`, with the GR-V assumptions shown, and the `go_routes_unread` counts (`docs/extract/routes.md`, Go section); not `handler`, `middleware`, the `exposes_route` edges, `conditional` or `scope` (no oracle compares them: served `unverified`, edges `not_compared`), nor the `calls_route` links of client calls (`client_calls`, row D)",
		Contract: []TestRef{{pkgRoutes, "TestGoRoutesContract"}, {pkgRoutes, "TestGoRoutesContractTags"}, {pkgGo, "TestGoRoutePayloadContract"}},
		Goldens:  []string{"go-routes"},
		Corpus:   []Citation{{"0076", "Corpus: realworld-gin 27 of 27 production routes with a path, gotify 55 of 56, miniflux 74 of 174, owncast 272 of 273, dozzle 0 of 85"}},
		Review:   []Citation{{"0076", "Independent review (GR-R): class A findings all fixed"}},
		Oracle:   []TestRef{{pkgRoutes, "TestGoRoutesMuxOracle"}, {pkgRoutes, "TestGoMuxPatternOracle"}},
		Refusal:  "routes of another framework (Express, FastAPI, Axum) stay best effort; a transport whose routes name several frameworks is `marker_conflict`",
	},
	{
		ID: "PL-L1", Scope: "Go structure", Kind: KindFile,
		Languages: []string{"go"},
		Marker:    "files with language `go` (test files included): file records and nodes, top-level declarations and their `contains` edges, `imports` edges (`docs/lang/go-structure.md`); not `calls` edges (`code_calls`, row D)",
		Contract:  []TestRef{{pkgGo, "TestGoStructureContract"}},
		Goldens:   []string{"go-structure", "go"},
		Corpus:    []Citation{{"0068", "0 disagreements on the fixture (63 Go files with the written cases: 3 lone-CR, 2 rejected, 42 import specs of which 5 malformed and 9 refused), gotify (142 files, 734 import specs), realworld-gin (21 files, 106 import specs) and the self-index (820 files, 4700 import specs)"}},
		Review: []Citation{
			{"0068", "The PL-L1 flip is blocked. Four class A findings"},
			{"0068", "Every class A and B finding is fixed as a class rule"},
			{"0068", "No class A finding is open."},
		},
		Oracle:  []TestRef{{pkgGo, "TestGoStructureOracle"}, {pkgGo, "TestGoStructureGoList"}},
		Refusal: "a Go file whose structure claims nothing (go/parser rejects it, GS-F2; a lone CR, GS-F3; not parsed, not extracted or dropped by validation) is stored `best_effort`, served with reason `structure_refused`",
	},
	{
		ID: "PL-L2", Scope: "Python structure", Kind: KindFile,
		Languages: []string{"python"},
		Marker:    "files with language `python` (test files included): file records and nodes, top-level declarations and their `contains` edges, `imports` edges to repository files and to `python-stdlib` / `pypi` external nodes (`docs/lang/python-structure.md`); not `calls` edges (`code_calls`, row D)",
		Contract:  []TestRef{{pkgPython, "TestPythonStructureContract"}},
		Goldens:   []string{"python-structure", "python-implicit-roots", "python"},
		Corpus:    []Citation{{"0069", "TestPythonStructureOracle (CPython 3.14.10): 0 gate records on the fixture, fastapi, langflow and the self-index"}},
		Review: []Citation{
			{"0069", "The PL-L2 flip was blocked by one class A finding (A1)"},
			{"0069", "Every class A and B finding is fixed as a class rule"},
			{"0069", "No class A finding is open."},
		},
		Oracle:  []TestRef{{pkgPython, "TestPythonStructureOracle"}},
		Refusal: "a Python file whose structure claims nothing (the reference Python rejects it, PS-F2; not parsed, not extracted or dropped by validation) is stored `best_effort`, served with reason `structure_refused`",
	},
	{
		ID: "PL-L3", Scope: "TypeScript, TSX, JavaScript structure", Kind: KindFile,
		Languages: []string{"typescript", "tsx", "javascript"},
		Marker:    "files with language `typescript`, `tsx` or `javascript` (declaration and test files included): file records and nodes, declarations and their `contains` edges, `imports` edges to repository files and to `npm` external nodes (row TS-I5, `docs/lang/typescript-structure.md`); not `calls` edges (`code_calls`, row D)",
		Contract:  []TestRef{{pkgTypeScript, "TestTypeScriptStructureContract"}},
		Goldens:   []string{"typescript-structure", "module-resolution", "typescript"},
		Corpus:    []Citation{{"0070", "0 claimed disagreements on the fixture, the 17 corpora of the S3 review and the self-index"}},
		Review: []Citation{
			{"0070", "The PL-L3 flip is blocked by one class A finding (A1)"},
			{"0070", "Each is fixed as a class rule"},
			{"0070", "No class A finding is open."},
		},
		// The oracle is opt-in: it skips without GRAPHVEIN_TS_ORACLE_DIR (a
		// scratch install of typescript@6.0.3), so it does not run in CI.
		Oracle:  []TestRef{{pkgTypeScript, "TestTypeScriptStructureOracle"}},
		Refusal: "a TypeScript, TSX or JavaScript file whose structure claims nothing (tree-sitter rejects it, TS-F2; not parsed or not extracted, TS-F3; or dropped by validation) is stored `best_effort`, served with reason `structure_refused`",
	},
	{
		ID: "PL-L4", Scope: "Rust structure", Kind: KindFile,
		Languages: []string{"rust"},
		Marker:    "files with language `rust` (tests, examples, benches and build scripts included): file records and nodes, module-level declarations, impl associated `const` / `type` items (RS-D9) and items in the bodies of claimed functions (RS-D10, `local: true`) with their `contains` edges, `mod` and `use` `imports` edges to repository files and to `cargo` external nodes for declared dependencies and sysroot crates (`docs/lang/rust-structure.md`); not `calls` edges (`code_calls`, row D)",
		Contract:  []TestRef{{pkgRust, "TestRustStructureContract"}},
		Goldens:   []string{"rust-structure", "rust"},
		Corpus: []Citation{
			{"0071", "TestRustStructureOracle reports 0 disagreements on claimed facts under 12 configurations"},
			{"0071", "48 Graphvein evidence entries no oracle configuration reaches (kftray 2, hoppscotch 22, localsend 13, self 11)"},
			{"0071", "is opt-in and never runs in CI"},
		},
		Review: []Citation{
			{"0071", "The Rust structure flip (PL-L4) is not ready. One class A finding"},
			{"0071", "Every class A and B finding is fixed as a class rule"},
			{"0071", "No class A finding is open."},
		},
		Oracle:  []TestRef{{pkgRust, "TestRustStructureOracle"}},
		Refusal: "a Rust file whose structure claims nothing (tree-sitter reports a syntax error, RS-F2; a lone CR, RS-F3; not parsed, not extracted or dropped by validation) is stored `best_effort`, served with reason `structure_refused`; a Rust file reached from no crate root of a read `Cargo.toml` (its imports are not resolved as a crate member, and no oracle configuration compiles it) is stored `best_effort`, served with reason `structure_unrooted`",
	},
}

// Rules returns a copy of the rule table, for the API and tests.
func Rules() []Rule {
	out := make([]Rule, len(rules))
	for i, r := range rules {
		out[i] = r
		out[i].Languages = append([]string(nil), r.Languages...)
		out[i].Match.Tools = append([]string(nil), r.Match.Tools...)
		out[i].Match.Dialects = append([]string(nil), r.Match.Dialects...)
		out[i].Contract = append([]TestRef(nil), r.Contract...)
		out[i].Goldens = append([]string(nil), r.Goldens...)
		out[i].Corpus = append([]Citation(nil), r.Corpus...)
		out[i].Review = append([]Citation(nil), r.Review...)
		out[i].Oracle = append([]TestRef(nil), r.Oracle...)
	}
	return out
}

// Digest is a SHA-256 over the whole rule table. TestPrecisionRules pins it
// next to index.Version, so a row change without an extractor version bump
// fails: a label flip must show in change impact as `extractor_or_relink`,
// never as a bare relink (ADR 0066, open point O14).
func Digest() string {
	raw, err := json.Marshal(rules)
	if err != nil {
		// The table is static data of plain types; Marshal cannot fail.
		panic(fmt.Sprintf("encoding the precision rule table: %v", err))
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}
