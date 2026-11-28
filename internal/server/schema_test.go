package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
)

// schemaFixtureRoot copies T018a's sql-migrations fixture and adds the
// member Git cannot hold portably (its README): a migration that is not
// valid UTF-8. The fixture has no inert names.
func schemaFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/sql-migrations")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pg-unreadable/migrations/2_unreadable.sql"), []byte("CREATE TABLE \xff (id integer);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Every member of pg-all-unreadable is unreadable (T018b1 review).
	if err := os.WriteFile(filepath.Join(root, "pg-all-unreadable/migrations/1_only.sql"), []byte("CREATE TABLE \xff (id integer);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// ingestSchemaStates ingests the crafted snapshot as repository "states".
func ingestSchemaStates(t *testing.T, s store.Store) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "schema-states.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := ingest.Stream(context.Background(), f, s); err != nil {
		t.Fatal(err)
	}
	// Ingest records rejected records as diagnostics; the snapshot has
	// exactly its ten crafted ones.
	snapshot, err := s.Dump(context.Background(), "states")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Diagnostics) != 10 || len(snapshot.Nodes) != 36 || len(snapshot.Edges) != 28 {
		t.Fatalf("crafted snapshot: %d nodes, %d edges, diagnostics %v", len(snapshot.Nodes), len(snapshot.Edges), snapshot.Diagnostics)
	}
}

func schemaPath(endpoint, id string) string {
	return "/api/v0/schema/" + endpoint + "?id=" + url.QueryEscape(id)
}

func schemaCases() []apiCase {
	return []apiCase{
		{"schema-summary-fixture", "fixture", "/api/v0/schema/summary"},
		{"schema-summary-states", "states", "/api/v0/schema/summary"},
		{"schema-summary-not-indexed", "empty", "/api/v0/schema/summary"},
		{"schema-sets-fixture", "fixture", "/api/v0/schema/sets"},
		{"schema-table-complete", "fixture", schemaPath("table", "table:pg-app/migrations/2_relations.sql#tasks")},
		{"schema-table-partial", "fixture", schemaPath("table", "table:pg-drops/migrations/1_base.sql#accounts")},
		// notes stays partial (DROP CONSTRAINT IF EXISTS forgets its keys)
		// after T018a2 C3 made accounts complete; the web tests use it.
		{"schema-table-partial-notes", "fixture", schemaPath("table", "table:pg-drops/migrations/1_base.sql#notes")},
		{"schema-table-renamed-altered", "fixture", schemaPath("table", "table:pg-app/migrations/1_base.sql#members")},
		{"schema-table-serial", "fixture", schemaPath("table", "table:pg-serial/migrations/1_serials.sql#counters")},
		{"schema-migration-applied", "fixture", schemaPath("migration", "migration:pg-app/migrations/6_opaque.sql#migration")},
		{"schema-migration-down", "fixture", schemaPath("migration", "migration:pg-app/migrations/10_reversible.down.sql#migration")},
		{"schema-migration-sqlite-set", "fixture", schemaPath("migration", "migration:sqlite-app/migrations/1_init.sql#migration")},
		{"schema-states-sets", "states", "/api/v0/schema/sets"},
		{"schema-states-table", "states", schemaPath("table", "table:a/migrations/2_more.sql#t_partial_empty")},
		{"schema-table-self-reference", "states", schemaPath("table", "table:a/migrations/1_init.sql#t_self")},
		// T018f: the catalogue, model groups, model tables and the
		// model/migration overlap from the sqlmodel-link fixture.
		{"schema-assumptions", "fixture", "/api/v0/schema/assumptions"},
		{"schema-summary-models", "models", "/api/v0/schema/summary"},
		{"schema-sets-models", "models", "/api/v0/schema/sets"},
		{"schema-table-model", "models", schemaPath("table", "table:app/models.py#Team")},
		{"schema-table-model-partial", "models", schemaPath("table", "table:app/mutations.py#Keyed")},
		{"schema-table-model-overlap", "models", schemaPath("table", "table:migrations/0001_init.sql#team")},
		// T018f Revision 2 (A1, C4): Hero's rival_id -> nowhere.id is an
		// unresolved model foreign key; Team, its neighbour, is complete.
		{"schema-table-model-unresolved", "models", schemaPath("table", "table:app/models.py#Hero")},
		// T024c1: a Supabase set (platform assumption SB-V2) and a table
		// with row level security, policies and references to platform
		// tables.
		{"schema-summary-supabase", "supabase", "/api/v0/schema/summary"},
		{"schema-sets-supabase", "supabase", "/api/v0/schema/sets"},
		{"schema-table-supabase", "supabase", schemaPath("table", "table:app/supabase/migrations/20240101000000_profiles.sql#profiles")},
		// T029 W3 (rows MT-20..MT-29): routes that accept or return the
		// table's model, and a model table no route uses.
		{"schema-table-model-routes", "routes", schemaPath("table", "table:app/models.py#Hero")},
		{"schema-table-model-routes-none", "routes", schemaPath("table", "table:app/models.py#Audit")},
		// T070: refused_sources from the Prisma linker's real refusals (a
		// package whose schema setting is not a literal, .prisma files
		// outside every schema, a mongodb schema).
		{"schema-summary-prisma", "prisma", "/api/v0/schema/summary"},
	}
}

// schemaOptions indexes the fixture as "fixture", ingests the crafted
// snapshot as "states" and returns options for those and for "empty", a
// repository without a run.
func schemaOptions(t *testing.T, w, s store.Store) (map[string]Options, map[string]string) {
	t.Helper()
	root := schemaFixtureRoot(t)
	indexRoot(t, root, w)
	ingestSchemaStates(t, w)
	models := t.TempDir()
	if err := os.CopyFS(models, os.DirFS("../../testdata/fixtures/sqlmodel-link")); err != nil {
		t.Fatal(err)
	}
	indexRepository(t, models, w, "models")
	supabase := t.TempDir()
	if err := os.CopyFS(supabase, os.DirFS("../../testdata/fixtures/supabase-migrations")); err != nil {
		t.Fatal(err)
	}
	indexRepository(t, supabase, w, "supabase")
	routes := t.TempDir()
	if err := os.CopyFS(routes, os.DirFS(modelTablesFixture)); err != nil {
		t.Fatal(err)
	}
	indexRepository(t, routes, w, "routes")
	prisma := t.TempDir()
	if err := os.CopyFS(prisma, os.DirFS("../../testdata/fixtures/prisma-models")); err != nil {
		t.Fatal(err)
	}
	indexRepository(t, prisma, w, "prisma")
	options, roots := map[string]Options{}, map[string]string{}
	for _, repo := range []string{"fixture", "states", "empty", "models", "supabase", "routes", "prisma"} {
		dir := root
		switch repo {
		case "models":
			dir = models
		case "supabase":
			dir = supabase
		case "routes":
			dir = routes
		case "prisma":
			dir = prisma
		case "states", "empty":
			dir = t.TempDir()
		}
		opt := testOptions(dir, s)
		opt.Repository = repo
		options[repo], roots[repo] = opt, dir
	}
	return options, roots
}

// TestSchemaGoldens checks every schema golden on both backends and that
// both backends produce the same bodies.
func TestSchemaGoldens(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]string{}
	backends(t, func(t *testing.T, w, s store.Store) {
		options, roots := schemaOptions(t, w, s)
		got := map[string]string{}
		for _, tc := range schemaCases() {
			rec := get(t, options[tc.repo], http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", tc.path, rec.Code, rec.Body.String())
			}
			v := normalizeAPI(t, rec.Body.Bytes())
			normalizeLinks(v, roots[tc.repo])
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			got[tc.name] = golden.ReplaceRoot(string(data), filepath.ToSlash(roots[tc.repo]))
			golden.CheckRoot(t, filepath.Join("testdata", tc.name+".golden.json"), v, roots[tc.repo])
		}
		mu.Lock()
		bodies[filepath.Base(t.Name())] = got
		mu.Unlock()
	})
	if bodies["sqlite"] != nil && bodies["postgres"] != nil {
		for name, body := range bodies["sqlite"] {
			if bodies["postgres"][name] != body {
				t.Errorf("%s: SQLite and Postgres bodies differ", name)
			}
		}
	}
}

// setsBody is the part of a sets response the checks read.
type setsBody struct {
	Sets []struct {
		Set              *string        `json:"set"`
		Source           *string        `json:"source"`
		RestsOn          []string       `json:"rests_on"`
		Dialect          *string        `json:"dialect"`
		Objects          *string        `json:"objects"`
		TableList        *string        `json:"table_list"`
		OpaqueTotal      *int           `json:"opaque_total"`
		TestOnly         *bool          `json:"test_only"`
		DiagnosticCounts map[string]int `json:"diagnostic_counts"`
		Migrations       []struct {
			ID         string         `json:"id"`
			Attributes map[string]any `json:"attributes"`
			Creates    []string       `json:"creates"`
		} `json:"migrations"`
		Tables []struct {
			Node         contract.Node      `json:"node"`
			Completeness string             `json:"completeness"`
			CreatedBy    *string            `json:"created_by"`
			RestsOn      []string           `json:"rests_on"`
			Overlaps     []schemaOverlap    `json:"overlaps"`
			OpaqueSites  []schemaOpaqueSite `json:"opaque_sites"`
			Columns      []struct {
				Node         contract.Node `json:"node"`
				DefaultState string        `json:"default_state"`
			} `json:"columns"`
			Indexes []contract.Node `json:"indexes"`
		} `json:"tables"`
		ForeignKeys []struct {
			SourceColumn string `json:"source_column"`
			TargetColumn string `json:"target_column"`
		} `json:"foreign_keys"`
	} `json:"sets"`
	FailedSets          []schemaFailedSet          `json:"failed_sets"`
	SetsWithoutRecords  []schemaRecordlessSet      `json:"sets_without_records"`
	UnreadableManifests []schemaUnreadableManifest `json:"unreadable_manifests"`
	ScanGaps            []schemaScanGap            `json:"scan_gaps"`
}

// TestSchemaChecks asserts the precision rules on the fixture directly
// (S1, S2), so a golden rewrite cannot hide them: every record the API
// returns is attached by a stored edge, every stored record is returned
// once, and no derived field states a known value the records do not.
func TestSchemaChecks(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		options, _ := schemaOptions(t, w, s)
		opt := options["fixture"]
		ctx := context.Background()
		var body setsBody
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/sets", nil), &body)
		stored := func(kind string) map[string]contract.Node {
			nodes, err := s.Nodes(ctx, "fixture", store.Query{Kind: kind})
			if err != nil {
				t.Fatal(err)
			}
			out := map[string]contract.Node{}
			for _, n := range nodes {
				out[n.Identity] = n
			}
			return out
		}
		tables, columns, indexes, migrations := stored("table"), stored("column"), stored("db_index"), stored("migration")
		edgeSet := func(ids []string, kind string) map[[2]string]bool {
			edges, err := s.Edges(ctx, "fixture", ids, []string{kind}, store.Outgoing)
			if err != nil {
				t.Fatal(err)
			}
			out := map[[2]string]bool{}
			for _, e := range edges {
				out[[2]string{e.Source, e.Target}] = true
			}
			return out
		}
		keys := func(m map[string]contract.Node) []string {
			out := []string{}
			for k := range m {
				out = append(out, k)
			}
			return out
		}
		contains := edgeSet(append(keys(tables), keys(migrations)...), "contains")
		fks := edgeSet(keys(columns), "foreign_key")
		seenTables, seenColumns, seenIndexes, seenFKs := map[string]bool{}, map[string]bool{}, map[string]bool{}, 0
		modeledSets := 0
		for _, set := range body.Sets {
			if set.Set == nil {
				t.Errorf("fixture has a set: null group")
				continue
			}
			if set.Objects != nil && *set.Objects == objectsModeled {
				modeledSets++
				if set.Dialect == nil || *set.Dialect != "postgres" || set.DiagnosticCounts["schema_migration_order"] > 0 {
					t.Errorf("%s: modeled with dialect %v, counts %v", *set.Set, set.Dialect, set.DiagnosticCounts)
				}
				if set.OpaqueTotal == nil || *set.OpaqueTotal > 0 && *set.TableList == tableListComplete {
					t.Errorf("%s: table_list %s with opaque total %v", *set.Set, *set.TableList, set.OpaqueTotal)
				}
			} else if len(set.Tables) != 0 {
				t.Errorf("%s: not modeled with %d tables", *set.Set, len(set.Tables))
			}
			if set.TestOnly == nil || *set.TestOnly {
				t.Errorf("%s: test_only %v", *set.Set, set.TestOnly)
			}
			// A set with an unsupported statement (which may
			// create a table T018a does not model) is never complete.
			if set.TableList != nil && *set.TableList == tableListComplete {
				for _, m := range set.Migrations {
					if n, ok := m.Attributes["unsupported"].(float64); !ok && m.Attributes["applied"] != false || n > 0 {
						t.Errorf("%s: table_list complete with %s unsupported %v", *set.Set, m.ID, m.Attributes["unsupported"])
					}
				}
				if set.DiagnosticCounts["schema_unsupported_statement"] > 0 {
					t.Errorf("%s: table_list complete with unsupported statements", *set.Set)
				}
			}
			for _, m := range set.Migrations {
				for _, c := range m.Creates {
					if !contains[[2]string{m.ID, c}] {
						t.Errorf("%s creates %s without a stored edge", m.ID, c)
					}
				}
			}
			for _, tb := range set.Tables {
				id := tb.Node.Identity
				seenTables[id] = true
				if tb.CreatedBy == nil || !contains[[2]string{*tb.CreatedBy, id}] {
					t.Errorf("%s: created_by %v without a stored edge", id, tb.CreatedBy)
				}
				state, _ := tb.Node.Attributes["state"].(string)
				opaque, _ := intValue(tb.Node.Attributes["opaque_after"])
				if tb.Completeness == completenessComplete && (state != "complete" || opaque != 0) {
					t.Errorf("%s: complete with state %s, opaque_after %d", id, state, opaque)
				}
				for _, c := range tb.Columns {
					seenColumns[c.Node.Identity] = true
					if !contains[[2]string{id, c.Node.Identity}] {
						t.Errorf("%s: column %s without a stored edge", id, c.Node.Identity)
					}
					if _, present := c.Node.Attributes["default"]; c.DefaultState == defaultNone && (state != "complete" || !present) {
						t.Errorf("%s: default none on a %s table", c.Node.Identity, state)
					}
				}
				for _, ix := range tb.Indexes {
					seenIndexes[ix.Identity] = true
					if !contains[[2]string{id, ix.Identity}] {
						t.Errorf("%s: index %s without a stored edge", id, ix.Identity)
					}
				}
			}
			for _, fk := range set.ForeignKeys {
				seenFKs++
				if !fks[[2]string{fk.SourceColumn, fk.TargetColumn}] {
					t.Errorf("foreign key %s -> %s is not stored", fk.SourceColumn, fk.TargetColumn)
				}
			}
		}
		if len(seenTables) != len(tables) || len(seenColumns) != len(columns) || len(seenIndexes) != len(indexes) || seenFKs != len(fks) {
			t.Errorf("returned %d/%d tables, %d/%d columns, %d/%d indexes, %d/%d foreign keys", len(seenTables), len(tables), len(seenColumns), len(columns), len(seenIndexes), len(indexes), seenFKs, len(fks))
		}
		// T018a2 B1a: pg-unreadable is refused as a whole (P10: one modeled
		// set, 2 tables and 2 columns fewer); P3 states 3 columns and 1
		// index fewer (pg-opaque-exists, pg-rev2-columns). B1b: 3 partial
		// tables more in pg-select-into (P5, R1b). T018a2 Revision 1.9 Y52:
		// pg-rev2-types's DROP TYPE mood CASCADE drops its 2 columns.
		if len(body.Sets) != 19 || modeledSets != 15 || len(tables) != 52 || len(columns) != 98 || len(indexes) != 11 || len(fks) != 4 || len(body.FailedSets) != 0 {
			t.Errorf("fixture: %d sets (%d modeled), %d tables, %d columns, %d indexes, %d foreign keys, %d failed", len(body.Sets), modeledSets, len(tables), len(columns), len(indexes), len(fks), len(body.FailedSets))
		}
		// The set whose every member is unreadable and the
		// manifest the pass cannot read are listed, not dropped.
		if len(body.SetsWithoutRecords) != 1 || body.SetsWithoutRecords[0].Package != "pg-all-unreadable/Cargo.toml" || body.SetsWithoutRecords[0].DiagnosticCounts["schema_member_unreadable"] != 1 {
			t.Errorf("sets without records %+v", body.SetsWithoutRecords)
		}
		if len(body.UnreadableManifests) != 1 || body.UnreadableManifests[0].File != "bad-manifest/Cargo.toml" {
			t.Errorf("unreadable manifests %+v", body.UnreadableManifests)
		}
		// Every table and migration detail answers, and the table detail
		// lists the same columns as the sets response.
		for _, set := range body.Sets {
			for _, tb := range set.Tables {
				var detail struct {
					Completeness string `json:"completeness"`
					Columns      []struct {
						Node         contract.Node `json:"node"`
						DefaultState string        `json:"default_state"`
					} `json:"columns"`
				}
				decodeInto(t, get(t, opt, http.MethodGet, schemaPath("table", tb.Node.Identity), nil), &detail)
				if detail.Completeness != tb.Completeness || len(detail.Columns) != len(tb.Columns) {
					t.Errorf("%s: detail %s with %d columns, sets %s with %d", tb.Node.Identity, detail.Completeness, len(detail.Columns), tb.Completeness, len(tb.Columns))
				}
				for i, c := range detail.Columns {
					if c.Node.Identity != tb.Columns[i].Node.Identity || c.DefaultState != tb.Columns[i].DefaultState {
						t.Errorf("%s: column %d differs", tb.Node.Identity, i)
					}
				}
			}
			for _, m := range set.Migrations {
				if rec := get(t, opt, http.MethodGet, schemaPath("migration", m.ID), nil); rec.Code != http.StatusOK {
					t.Errorf("%s: status %d", m.ID, rec.Code)
				}
			}
		}
		// S2 for the summary: an index of this binary proves the pass ran,
		// so a repository with sets says sets and none is never stated for
		// another producer or for a repository without a run.
		var summary schemaSummaryResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/summary", nil), &summary)
		if summary.Found != foundSets || summary.Sets == nil || *summary.Sets != 19 || *summary.TestOnlySets != 0 || *summary.SetsWithoutRecords != 1 || *summary.UnreadableManifests != 1 {
			t.Errorf("fixture summary %+v", summary)
		}
		// Another producer without migration records: absence says nothing.
		ingestStatuses(t, w)
		external := testOptions(t.TempDir(), s)
		external.Repository = "statuses"
		options["statuses"] = external
		for repo, reason := range map[string]string{"states": "", "empty": "not_indexed", "statuses": "external_indexer"} {
			decodeInto(t, get(t, options[repo], http.MethodGet, "/api/v0/schema/summary", nil), &summary)
			if summary.Found == foundNone || reason != "" && (summary.UnknownReason == nil || *summary.UnknownReason != reason) {
				t.Errorf("%s summary %+v", repo, summary)
			}
		}
	})
}

func TestSchemaParameters(t *testing.T) {
	opt := handlerOptions(t)
	for _, tc := range []struct {
		path, code string
		status     int
	}{
		{"/api/v0/schema/table", "bad_request", 400},
		{"/api/v0/schema/table?id=", "bad_request", 400},
		{"/api/v0/schema/table?id=" + url.QueryEscape("table:a.sql#t t"), "bad_request", 400},
		{"/api/v0/schema/table?id=" + url.QueryEscape("column:a.sql#t.c"), "bad_request", 400},
		{"/api/v0/schema/table?id=" + url.QueryEscape("table:a.sql#t") + "&id=" + url.QueryEscape("table:a.sql#u"), "bad_request", 400},
		{"/api/v0/schema/table?id=" + url.QueryEscape("table:a.sql#t"), "not_found", 404},
		{"/api/v0/schema/table?id=" + url.QueryEscape("table:a.sql#t") + "&unknown=1", "not_found", 404},
		{"/api/v0/schema/migration", "bad_request", 400},
		{"/api/v0/schema/migration?id=nope", "bad_request", 400},
		{"/api/v0/schema/migration?id=" + url.QueryEscape("table:a.sql#t"), "bad_request", 400},
		{"/api/v0/schema/migration?id=a&id=b", "bad_request", 400},
		{"/api/v0/schema/migration?id=" + url.QueryEscape("migration:a.sql#migration"), "not_found", 404},
		{"/api/v0/schema/sets?unknown=1", "", 200},
		{"/api/v0/schema/summary?unknown=1", "", 200},
	} {
		rec := get(t, opt, http.MethodGet, tc.path, nil)
		if rec.Code != tc.status {
			t.Errorf("%s: status %d %s, want %d", tc.path, rec.Code, rec.Body.String(), tc.status)
			continue
		}
		if tc.code != "" && errorCode(t, rec) != tc.code {
			t.Errorf("%s: code %s", tc.path, rec.Body.String())
		}
	}
	// The not-found messages name the kind.
	rec := get(t, opt, http.MethodGet, "/api/v0/schema/migration?id="+url.QueryEscape("migration:a.sql#migration"), nil)
	if !strings.Contains(rec.Body.String(), "no migration with identity migration:a.sql#migration") {
		t.Errorf("message %s", rec.Body.String())
	}
}

// schemaRequestPaths are the schema requests the consistency and failure
// tests send against the fixture, covering every read of each endpoint.
func schemaRequestPaths() []string {
	return []string{
		"/api/v0/schema/summary",
		"/api/v0/schema/sets",
		schemaPath("table", "table:pg-app/migrations/2_relations.sql#tasks"),
		schemaPath("table", "table:pg-app/migrations/1_base.sql#members"),
		schemaPath("migration", "migration:pg-app/migrations/3_alter.sql#migration"),
	}
}

// TestSchemaConsistency: one retry, then 503, for every schema endpoint.
func TestSchemaConsistency(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := schemaFixtureRoot(t)
		indexRoot(t, root, w)
		for _, path := range schemaRequestPaths() {
			always := &changingStore{Store: s, change: func(n int) bool { return true }}
			rec := get(t, testOptions(root, always), http.MethodGet, path, nil)
			if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "index_changed" || always.calls != 4 {
				t.Errorf("%s always changing: status %d, %d runs reads: %s", path, rec.Code, always.calls, rec.Body.String())
			}
			once := &changingStore{Store: s, change: func(n int) bool { return n == 2 }}
			if rec := get(t, testOptions(root, once), http.MethodGet, path, nil); rec.Code != http.StatusOK || once.calls != 4 {
				t.Errorf("%s one change: status %d, %d runs reads", path, rec.Code, once.calls)
			}
			stable := &changingStore{Store: s, change: func(n int) bool { return false }}
			if rec := get(t, testOptions(root, stable), http.MethodGet, path, nil); rec.Code != http.StatusOK || stable.calls != 2 {
				t.Errorf("%s stable: status %d, %d runs reads", path, rec.Code, stable.calls)
			}
		}
	})
}

// TestSchemaStoreFailures fails each store read of each schema endpoint in
// turn: every one answers 500 store_error with the envelope.
func TestSchemaStoreFailures(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := schemaFixtureRoot(t)
		indexRoot(t, root, w)
		for _, path := range schemaRequestPaths() {
			counter := &recordingStore{Store: s}
			if rec := get(t, testOptions(root, counter), http.MethodGet, path, nil); rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d", path, rec.Code)
			}
			if counter.reads < 5 {
				t.Fatalf("%s: only %d reads", path, counter.reads)
			}
			for n := 1; n <= counter.reads; n++ {
				rec := get(t, testOptions(root, &recordingStore{Store: s, fail: n}), http.MethodGet, path, nil)
				var env errorEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || rec.Code != http.StatusInternalServerError || env.Error.Code != "store_error" || !strings.HasSuffix(env.Error.Message, errInjected.Error()) {
					t.Errorf("%s read %d: status %d %s", path, n, rec.Code, rec.Body.String())
				}
			}
		}
		// A failing diagnostics read alone is reported too.
		for _, path := range []string{"/api/v0/schema/summary", "/api/v0/schema/sets", schemaPath("migration", "migration:pg-app/migrations/3_alter.sql#migration")} {
			if rec := get(t, testOptions(root, failingStore{Store: s, diagnostics: true}), http.MethodGet, path, nil); rec.Code != http.StatusInternalServerError {
				t.Errorf("%s with failing diagnostics: status %d", path, rec.Code)
			}
		}
	})
}

// TestSchemaSummaryWithoutRecords indexes small repositories:
// a set whose every member is unreadable, not UTF-8 or ignored, is a
// set, never "none"; a manifest the pass cannot read makes "none"
// unknown; and a repository without manifests is "none" on this binary.
func TestSchemaSummaryWithoutRecords(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		for _, tc := range []struct {
			name, pkg      string
			member, ignore string
			found, reason  string
			without        int
		}{
			{"no manifest", "", "", "", foundNone, "", 0},
			{"manifest unreadable", "bad-manifest", "", "", foundUnknown, "manifest_unreadable", 0},
			{"member not utf-8", "pg-all-unreadable", "CREATE TABLE \xff (id integer);\n", "", foundSets, "", 1},
			{"member ignored", "pg-all-unreadable", "CREATE TABLE t (id integer);\n", "migrations/1_only.sql\n", foundSets, "", 1},
		} {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.pkg != "" {
				if err := os.CopyFS(root, os.DirFS(filepath.Join("../../testdata/fixtures/sql-migrations", tc.pkg))); err != nil {
					t.Fatal(err)
				}
			}
			if tc.member != "" {
				if err := os.WriteFile(filepath.Join(root, "migrations", "1_only.sql"), []byte(tc.member), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.ignore != "" {
				if err := os.WriteFile(filepath.Join(root, ".graphveinignore"), []byte(tc.ignore), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			indexRoot(t, root, w)
			var summary schemaSummaryResponse
			decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/summary", nil), &summary)
			reason := ""
			if summary.UnknownReason != nil {
				reason = *summary.UnknownReason
			}
			if summary.Found != tc.found || reason != tc.reason || summary.SetsWithoutRecords == nil || *summary.SetsWithoutRecords != tc.without || *summary.Sets != 0 {
				t.Errorf("%s: summary %+v", tc.name, summary)
			}
			var body setsBody
			decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
			if len(body.Sets) != 0 || len(body.SetsWithoutRecords) != tc.without || (len(body.UnreadableManifests) == 1) != (tc.reason == "manifest_unreadable") {
				t.Errorf("%s: sets %+v", tc.name, body)
			}
		}
	})
}

// TestSchemaScanGaps checks the scan-gap rule end to end: none needs proof that
// the scanner missed no manifest. A Cargo.toml symlink (created here; no
// symlink is committed) or a symlinked directory found by indexing, and a
// stored scan_read, invalid_path or ignore_read diagnostic, make the
// summary unknown with scan_incomplete; the symlinked Cargo.toml is also an
// unreadable manifest with the scanner code as its source.
func TestSchemaScanGaps(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		check := func(name string, opt Options, gaps, manifests int, manifestCode string) {
			t.Helper()
			var summary schemaSummaryResponse
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/summary", nil), &summary)
			if summary.Found != foundUnknown || summary.UnknownReason == nil || *summary.UnknownReason != "scan_incomplete" || summary.ScanGaps == nil || *summary.ScanGaps != gaps || *summary.UnreadableManifests != manifests || *summary.Sets != 0 {
				t.Errorf("%s: summary %+v", name, summary)
			}
			var body setsBody
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/sets", nil), &body)
			if len(body.Sets) != 0 || len(body.ScanGaps) != gaps || len(body.UnreadableManifests) != manifests {
				t.Errorf("%s: sets %+v", name, body)
			}
			if manifests > 0 && body.UnreadableManifests[0].Code != manifestCode {
				t.Errorf("%s: unreadable manifest %+v", name, body.UnreadableManifests[0])
			}
		}
		for _, tc := range []struct {
			name, link, target string
			targetDir          bool
			manifests          int
		}{
			{"symlinked Cargo.toml", "Cargo.toml", "manifest.txt", false, 1},
			{"symlinked directory", "linked", "elsewhere", true, 0},
		} {
			root := t.TempDir()
			outside := t.TempDir()
			target := filepath.Join(outside, tc.target)
			var err error
			if tc.targetDir {
				err = os.MkdirAll(target, 0o755)
				if err == nil {
					err = os.WriteFile(filepath.Join(target, "Cargo.toml"), []byte("[package]\nname = \"x\"\n"), 0o644)
				}
			} else {
				err = os.WriteFile(target, []byte("[package]\nname = \"x\"\n"), 0o644)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(root, tc.link)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			indexRoot(t, root, w)
			check(tc.name, testOptions(root, s), 1, tc.manifests, "symlink")
		}
		// Diagnostics the scanner writes only where the platform refuses a
		// read (as root nothing is refused), ingested as a graphvein run.
		for i, d := range []string{
			`{"type":"diagnostic","file":"private","severity":"warning","code":"scan_read","message":"open private: permission denied"}`,
			`{"type":"diagnostic","file":"","severity":"warning","code":"invalid_path","message":"unsupported repository path \"a\\\\b\""}`,
			`{"type":"diagnostic","file":"sub/.gitignore","severity":"warning","code":"ignore_read","message":"permission denied"}`,
			`{"type":"diagnostic","file":"Cargo.toml","severity":"warning","code":"file_read","message":"permission denied"}`,
		} {
			repo := fmt.Sprintf("gaps%d", i)
			snapshot := `{"type":"header","contract_version":1,"indexer":{"name":"graphvein","version":"extractors-28"},"repository":"` + repo + `","generated_at":"2026-11-04T00:00:00Z"}` + "\n" +
				`{"type":"file","path":"README.md","language":"markdown","precision":"best_effort","size":2,"content_hash":"` + contract.ContentHash([]byte("x\n")) + `"}` + "\n" + d + "\n" +
				`{"type":"footer","counts":{"records":2,"file":1,"node":0,"edge":0,"diagnostic":1}}` + "\n"
			if err := ingest.Stream(context.Background(), strings.NewReader(snapshot), w); err != nil {
				t.Fatal(err)
			}
			opt := testOptions(t.TempDir(), s)
			opt.Repository = repo
			manifests := 0
			if strings.Contains(d, "file_read") {
				manifests = 1
			}
			check(repo, opt, 1, manifests, "file_read")
		}
	})
}

// schemaLinks returns every link of the nodes a schema body presents: the
// link field of each nested ide object (the top-level ide status has none).
func schemaLinks(t *testing.T, body []byte) []string {
	t.Helper()
	var all any
	if err := json.Unmarshal(body, &all); err != nil {
		t.Fatal(err)
	}
	links := []string{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for _, key := range []string{"ide", "link"} {
				// "link" objects are T018f's claim, reason and opaque
				// site links, built as the nodes' ide links.
				if ide, ok := v[key].(map[string]any); ok {
					if link, ok := ide["link"].(string); ok {
						links = append(links, link)
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(all)
	return links
}

// TestSchemaEditorParameter checks the editor parameter of ADR 0050 on the
// two schema endpoints that present nodes: the browser's choice builds
// every link and wins over the repository setting, a load error with a
// requested editor still builds links, and an unknown, empty or repeated
// value is 400. The summary and sets present no nodes and carry no ide.
func TestSchemaEditorParameter(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := schemaFixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		paths := []string{
			schemaPath("table", "table:pg-app/migrations/2_relations.sql#tasks"),
			schemaPath("migration", "migration:pg-app/migrations/6_opaque.sql#migration"),
		}
		decode := func(t *testing.T, path string) (ideStatus, []string) {
			t.Helper()
			rec := get(t, opt, http.MethodGet, path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", path, rec.Code, rec.Body.String())
			}
			var top struct {
				IDE ideStatus `json:"ide"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil {
				t.Fatal(err)
			}
			return top.IDE, schemaLinks(t, rec.Body.Bytes())
		}
		for _, tc := range []struct {
			name, config, editor string
			want                 ideStatus
			prefix               string
		}{
			{"builtin", "", "", ideStatus{Available: true, Label: strp("Open in VS Code"), Editor: strp("vscode"), Source: ideSourceBuiltin}, "vscode://file"},
			{"repository", "version: 1\nide: zed\n", "", ideStatus{Available: true, Label: strp("Open in Zed"), Editor: strp("zed"), Source: ideSourceRepository}, "zed://file"},
			{"request-zed", "", "zed", ideStatus{Available: true, Label: strp("Open in Zed"), Editor: strp("zed"), Source: ideSourceRequest}, "zed://file"},
			{"request-jetbrains", "", "jetbrains", ideStatus{Available: true, Label: strp("Open in IntelliJ IDEA"), Editor: strp("jetbrains"), Source: ideSourceRequest}, "idea://open?file="},
			{"request-beats-repository", "version: 1\nide: zed\n", "vscode", ideStatus{Available: true, Label: strp("Open in VS Code"), Editor: strp("vscode"), Source: ideSourceRequest}, "vscode://file"},
			{"request-beats-custom", "version: 1\nide: {link_template: 'x://{rel_path}:{line}:{column}', label: Open in X}\n", "zed", ideStatus{Available: true, Label: strp("Open in Zed"), Editor: strp("zed"), Source: ideSourceRequest}, "zed://file"},
		} {
			writeConfig(t, root, tc.config)
			for _, path := range paths {
				if tc.editor != "" {
					path += "&editor=" + tc.editor
				}
				ide, links := decode(t, path)
				if !reflect.DeepEqual(ide, tc.want) {
					t.Errorf("%s %s: ide %+v, want %+v", tc.name, path, ide, tc.want)
				}
				if body := get(t, opt, http.MethodGet, path, nil).Body.String(); strings.Count(body, `"link":{"link"`) == 0 {
					t.Errorf("%s %s: no T018f claim, reason or site links", tc.name, path)
				}
				if len(links) == 0 {
					t.Errorf("%s %s: no links", tc.name, path)
				}
				for _, link := range links {
					if !strings.HasPrefix(link, tc.prefix) {
						t.Errorf("%s %s: link %s, want prefix %s", tc.name, path, link, tc.prefix)
					}
				}
			}
		}

		// A configuration that fails to load: no links without a request,
		// links of the requested editor with the error kept (K3 A).
		writeConfig(t, root, "version: 1\nide: emacs\n")
		for _, path := range paths {
			if ide, links := decode(t, path); ide.Available || ide.Error == nil || ide.Editor != nil || ide.Source != ideSourceRepository || len(links) != 0 {
				t.Errorf("load error %s: ide %+v links %v", path, ide, links)
			}
			ide, links := decode(t, path+"&editor=zed")
			if !ide.Available || ide.Error == nil || ide.Source != ideSourceRequest || len(links) == 0 || !strings.HasPrefix(links[0], "zed://file") {
				t.Errorf("load error with editor %s: ide %+v links %v", path, ide, links)
			}
		}
		writeConfig(t, root, "")

		for _, path := range paths {
			for _, tc := range []struct{ extra, message string }{
				{"&editor=sublime", `editor must be one of vscode, vscode-insiders, cursor, vscodium, jetbrains, zed; not "sublime"`},
				{"&editor=", `editor must be one of vscode, vscode-insiders, cursor, vscodium, jetbrains, zed; not ""`},
				{"&editor=Zed", `editor must be one of vscode, vscode-insiders, cursor, vscodium, jetbrains, zed; not "Zed"`},
				{"&editor=zed&editor=zed", "parameter editor is given more than once"},
			} {
				rec := get(t, opt, http.MethodGet, path+tc.extra, nil)
				var env errorEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || rec.Code != http.StatusBadRequest || env.Error.Code != "bad_request" || env.Error.Message != tc.message {
					t.Errorf("%s%s: status %d %s", path, tc.extra, rec.Code, rec.Body.String())
				}
			}
			// Naming the preset that already applies changes only ide.source.
			without := get(t, opt, http.MethodGet, path, nil).Body.String()
			with := get(t, opt, http.MethodGet, path+"&editor=vscode", nil).Body.String()
			if strings.Replace(with, `"source":"request"`, `"source":"builtin"`, 1) != without {
				t.Errorf("%s: bodies differ beyond ide.source:\n%s\n%s", path, without, with)
			}
		}

		// The summary and sets present no nodes: no ide, no links, and
		// editor is an unknown parameter there, ignored like any other.
		for _, path := range []string{"/api/v0/schema/summary", "/api/v0/schema/sets", "/api/v0/schema/assumptions"} {
			plain := get(t, opt, http.MethodGet, path, nil)
			with := get(t, opt, http.MethodGet, path+"?editor=sublime", nil)
			var top map[string]json.RawMessage
			if err := json.Unmarshal(plain.Body.Bytes(), &top); err != nil {
				t.Fatal(err)
			}
			if _, ok := top["ide"]; ok || strings.Contains(plain.Body.String(), `"link"`) {
				t.Errorf("%s carries ide or links: %s", path, plain.Body.String())
			}
			if with.Code != http.StatusOK || with.Body.String() != plain.Body.String() {
				t.Errorf("%s?editor=sublime: status %d, body differs", path, with.Code)
			}
		}
	})
}

// normalizeLinks replaces the root in the T018f link objects (claims,
// reasons and opaque sites carry "link": {"link": ...} beside the nodes'
// "ide", which golden.CheckRoot already normalizes).
func normalizeLinks(v any, root string) {
	var encoded strings.Builder
	for _, c := range []byte(filepath.ToSlash(root)) {
		if c == '/' || c == ':' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", rune(c)) {
			encoded.WriteByte(c)
		} else {
			fmt.Fprintf(&encoded, "%%%02X", c)
		}
	}
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			normalizeLinks(item, root)
		}
	case map[string]any:
		if link, ok := x["link"].(map[string]any); ok {
			if s, ok := link["link"].(string); ok {
				link["link"] = golden.ReplaceRoot(s, encoded.String())
			}
		}
		for _, item := range x {
			normalizeLinks(item, root)
		}
	}
}
