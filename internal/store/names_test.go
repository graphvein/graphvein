package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/migrations"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

// nameColumnLimit mirrors the store's NULL rule bound (ADR 0039).
const nameColumnLimit = 1536

// nameBackfillBatch mirrors the rows one schema 3 backfill read holds.
const nameBackfillBatch = 1000

// syntheticNames builds a graph whose names exercise the NULL rule, LIKE
// metacharacters, case variants, non-ASCII text and more identities than one
// NodesByIdentity chunk holds.
func syntheticNames(t *testing.T, repository string) []byte {
	t.Helper()
	return namesGraph(t, repository, func(add func(name, qualified string)) {
		for i := range 300 {
			add(fmt.Sprintf("f%03d", i), fmt.Sprintf("Mod.f%03d", i))
		}
		add("nul\x00name", "Nul.n")
		add("long", "Long."+strings.Repeat("q", 800))
		// Exactly at the bound for five-byte repository IDs: 5+18+5+2*754 = 1536.
		add("edge5", "Edge."+strings.Repeat("e", 749))
		for _, name := range []string{"%", "_", "!", `\`, "a%b", "a_b", "A_B", "build_flow", "Build_Flow", "Ünï", "ünï", "x!%_"} {
			add(name, "Special."+name)
		}
	})
}

// namesGraph builds a one-file graph holding the functions nodes adds.
func namesGraph(t *testing.T, repository string, nodes func(add func(name, qualified string))) []byte {
	t.Helper()
	hash := contract.ContentHash(nil)
	lines := []any{
		contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "fixture", Version: "1.0"}, Repository: repository, GeneratedAt: "2026-11-03T00:00:00Z"},
		contract.File{Type: "file", Path: "src/n.ts", Language: "typescript", Precision: "precise", ContentHash: hash},
	}
	add := func(name, qualified string) {
		id, err := contract.Identity("function", "src/n.ts", qualified)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, contract.Node{Type: "node", Identity: id, Kind: "function", Level: 4, Name: name, QualifiedName: qualified, File: "src/n.ts", Span: &contract.Span{StartLine: 1, EndLine: 1, EndColumn: 1}, ContentHash: hash, Attributes: map[string]any{}})
	}
	nodes(add)
	count := len(lines) - 2
	lines = append(lines, contract.Footer{Type: "footer", Counts: contract.Counts{Records: len(lines) - 1, File: 1, Node: count}})
	var b bytes.Buffer
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(data, '\n'))
	}
	return b.Bytes()
}

// nullRule reports whether a node's name columns must be NULL.
func nullRule(repository string, n contract.Node) bool {
	return strings.ContainsRune(n.Name, 0) || strings.ContainsRune(n.QualifiedName, 0) || len(repository)+len(n.Identity)+len(n.Name)+len(n.QualifiedName) > nameColumnLimit
}

// checkNameColumns compares the stored columns with the decoded payloads.
func checkNameColumns(t *testing.T, db *sql.DB, s store.Store, repository string) (nulls int) {
	t.Helper()
	ctx := context.Background()
	dump, err := s.Dump(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]contract.Node{}
	for _, n := range dump.Nodes {
		byID[n.Identity] = n
	}
	rows, err := db.QueryContext(ctx, "SELECT identity,name,qualified_name FROM nodes WHERE repository=$1", repository)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		var name, qualified sql.NullString
		if err := rows.Scan(&id, &name, &qualified); err != nil {
			t.Fatal(err)
		}
		count++
		n := byID[id]
		if name.Valid != qualified.Valid {
			t.Fatalf("%s: name and qualified_name disagree on NULL", id)
		}
		if !name.Valid {
			nulls++
			if !nullRule(repository, n) {
				t.Fatalf("%s: NULL name columns outside the NULL rule", id)
			}
			continue
		}
		if nullRule(repository, n) || name.String != n.Name || qualified.String != n.QualifiedName {
			t.Fatalf("%s: columns (%q, %q) differ from payload (%q, %q)", id, name.String, qualified.String, n.Name, n.QualifiedName)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != len(dump.Nodes) || count == 0 {
		t.Fatalf("checked %d rows for %d nodes", count, len(dump.Nodes))
	}
	return nulls
}

// termSet follows T016a A3: names, qualified names, qualified-name suffixes,
// identities, ASCII case variants and SQL metacharacters.
func termSet(nodes []contract.Node, step int) []string {
	set := map[string]bool{"%": true, "_": true, `\`: true, "a%b": true, "!": true, "zzzz_nomatch": true, "Ünï": true, "\xff": true, "a\x00b": true, strings.Repeat("q", nameColumnLimit+1): true}
	for i, n := range nodes {
		set[n.Name] = true
		set[n.QualifiedName] = true
		if i%step != 0 {
			continue
		}
		for _, k := range []int{1, 3, 8} {
			if len(n.QualifiedName) >= k {
				set[n.QualifiedName[len(n.QualifiedName)-k:]] = true
			}
		}
		set[n.Identity] = true
		swapped := []byte(n.Name)
		for j, c := range swapped {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
				swapped[j] = c ^ 0x20
			}
		}
		set[string(swapped)] = true
	}
	delete(set, "")
	terms := []string{}
	for term := range set {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	return terms
}

// checkTerms compares Nodes with a term against the pre-schema-3 Go filter
// over a full dump, with and without other filters and limits.
func checkTerms(t *testing.T, s store.Store, repository string, step int) int {
	t.Helper()
	ctx := context.Background()
	dump, err := s.Dump(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	compared := 0
	for _, term := range termSet(dump.Nodes, step) {
		for _, filter := range []store.Query{{Term: term}, {Term: term, Limit: 1}, {Term: term, Kind: "function"}, {Term: term, File: "."}} {
			want := []contract.Node{}
			for _, n := range dump.Nodes {
				if filter.Kind != "" && n.Kind != filter.Kind || filter.File != "" && n.File != filter.File {
					continue
				}
				if n.Name != term && n.QualifiedName != term && !strings.HasSuffix(n.QualifiedName, term) {
					continue
				}
				want = append(want, n)
				if filter.Limit > 0 && len(want) >= filter.Limit {
					break
				}
			}
			got, err := s.Nodes(ctx, repository, filter)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Nodes(%+v) returned %d nodes, reference %d", filter, len(got), len(want))
			}
			compared++
		}
	}
	return compared
}

// checkFileHashes compares FileHashes with the file records in a dump.
func checkFileHashes(t *testing.T, s store.Store, repository string) {
	t.Helper()
	ctx := context.Background()
	dump, err := s.Dump(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, f := range dump.Files {
		want[f.Path] = f.ContentHash
	}
	got, err := s.FileHashes(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FileHashes = %v, dump = %v", got, want)
	}
}

// fixtureRepositories loads every contract fixture and every indexed CLI
// fixture, one repository each, and returns their repository IDs.
func fixtureRepositories(t *testing.T, s store.Store) []string {
	t.Helper()
	ctx := context.Background()
	var repositories []string
	for _, name := range []string{"valid", "diagnostic", "version-b", "invalid-identity", "malformed-body", "unknown-fields"} {
		repository := "contract-" + name
		data := strings.ReplaceAll(string(fixture(t, name)), `"repository":"fixture"`, `"repository":"`+repository+`"`)
		if err := ingest.Stream(ctx, strings.NewReader(data), s); err != nil {
			t.Fatal(err)
		}
		repositories = append(repositories, repository)
	}
	for _, language := range []string{"go", "python", "rust", "typescript"} {
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/repos/"+language)); err != nil {
			t.Fatal(err)
		}
		if language == "typescript" {
			for _, p := range [][2]string{{"_graphveinignore", ".graphveinignore"}, {"_claude", ".claude"}, {"nested/_gitignore", "nested/.gitignore"}, {"nested/_skip.ts", "nested/skip.ts"}} {
				if err := os.Rename(filepath.Join(root, p[0]), filepath.Join(root, p[1])); err != nil {
					t.Fatal(err)
				}
			}
		}
		var stream bytes.Buffer
		if _, err := index.Emit(ctx, root, s, &stream, index.Options{Repository: language, Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")}}); err != nil {
			t.Fatal(err)
		}
		if err := ingest.Stream(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		repositories = append(repositories, language)
	}
	if err := ingest.Stream(ctx, bytes.NewReader(syntheticNames(t, "names")), s); err != nil {
		t.Fatal(err)
	}
	return append(repositories, "names")
}

func TestNameColumnsAndTermLookup(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		compared := 0
		for _, repository := range fixtureRepositories(t, s) {
			nulls := checkNameColumns(t, db, s, repository)
			if repository == "names" && nulls != 2 {
				t.Fatalf("synthetic graph has %d NULL rows, want the NUL and over-bound nodes", nulls)
			}
			compared += checkTerms(t, s, repository, 1)
			checkFileHashes(t, s, repository)
		}
		t.Logf("compared %d term lookups", compared)
	})
}

func TestNodesByIdentity(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		if err := ingest.Stream(ctx, bytes.NewReader(syntheticNames(t, "names")), s); err != nil {
			t.Fatal(err)
		}
		dump, err := s.Dump(ctx, "names")
		if err != nil {
			t.Fatal(err)
		}
		if len(dump.Nodes) <= 2*128 {
			t.Fatalf("synthetic graph has %d nodes; need more than two chunks", len(dump.Nodes))
		}
		// Reverse order, duplicates and missing identities in every chunk.
		var ids []string
		var want []contract.Node
		for i := len(dump.Nodes) - 1; i >= 0; i-- {
			if i%3 == 0 {
				continue
			}
			ids = append(ids, dump.Nodes[i].Identity, dump.Nodes[i].Identity, dump.Nodes[i].Identity+"-missing")
		}
		for i, n := range dump.Nodes {
			if i%3 != 0 {
				want = append(want, n)
			}
		}
		got, err := s.NodesByIdentity(ctx, "names", ids)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("NodesByIdentity returned %d nodes, want %d", len(got), len(want))
		}
		if got, err := s.NodesByIdentity(ctx, "names", nil); err != nil || len(got) != 0 || got == nil {
			t.Fatalf("empty NodesByIdentity = %v, %v", got, err)
		}
		if got, err := s.NodesByIdentity(ctx, "other", ids); err != nil || len(got) != 0 {
			t.Fatalf("other repository NodesByIdentity = %v, %v", got, err)
		}
	})
}

// downgradeNames turns a schema 3 store back into schema 2.
func downgradeNames(t *testing.T, db *sql.DB) {
	t.Helper()
	dropChangeTables(t, db)
	for _, q := range []string{"DROP INDEX nodes_names", "ALTER TABLE nodes DROP COLUMN name", "ALTER TABLE nodes DROP COLUMN qualified_name", "UPDATE schema_version SET version=2"} {
		if _, err := db.ExecContext(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
}

// reopenNames opens db again, which migrates a schema 2 store to schema 3.
func reopenNames(t *testing.T, db *sql.DB) store.Store {
	t.Helper()
	ctx := context.Background()
	dialect := "sqlite"
	if strings.HasSuffix(t.Name(), "/postgres") {
		dialect = "postgres"
	}
	upgraded, err := sqlstore.Open(ctx, db, dialect)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.QueryRowContext(ctx, "SELECT version FROM schema_version").Scan(&version); err != nil || version != migrations.Version {
		t.Fatal("migration version", version, err)
	}
	return upgraded
}

// TestNameBackfillAcrossBatches places a repository boundary exactly at the
// first batch edge and a second edge inside one repository, so every keyset
// continuation (next repository, same repository) is taken.
func TestNameBackfillAcrossBatches(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		// Rows are read in (repository, identity) order: "batch-a" fills the
		// first batch exactly, "batch-b" spans the second and third.
		sizes := map[string]int{"batch-a": nameBackfillBatch, "batch-b": nameBackfillBatch + nameBackfillBatch/2}
		for _, repository := range []string{"batch-a", "batch-b"} {
			graph := namesGraph(t, repository, func(add func(name, qualified string)) {
				for i := range sizes[repository] {
					add(fmt.Sprintf("g%04d", i), fmt.Sprintf("Batch.g%04d", i))
				}
			})
			if err := ingest.Stream(ctx, bytes.NewReader(graph), s); err != nil {
				t.Fatal(err)
			}
		}
		var total int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM nodes").Scan(&total); err != nil || total != 2*nameBackfillBatch+nameBackfillBatch/2 {
			t.Fatal("node rows", total, err)
		}
		downgradeNames(t, db)
		upgraded := reopenNames(t, db)
		for _, repository := range []string{"batch-a", "batch-b"} {
			if nulls := checkNameColumns(t, db, upgraded, repository); nulls != 0 {
				t.Fatalf("%s: %d NULL rows after backfill, want 0", repository, nulls)
			}
		}
		checkTerms(t, upgraded, "batch-b", 499)
	})
}

// TestNameBackfillFromVersion2 rebuilds a schema 2 store holding a node with
// U+0000 in its name and one over the length bound, then migrates it.
func TestNameBackfillFromVersion2(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		for _, repository := range []string{"names", "other"} {
			if err := ingest.Stream(ctx, bytes.NewReader(syntheticNames(t, repository)), s); err != nil {
				t.Fatal(err)
			}
		}
		importFixture(t, s, "valid")
		downgradeNames(t, db)
		upgraded := reopenNames(t, db)
		for _, repository := range []string{"names", "other", "fixture"} {
			nulls := checkNameColumns(t, db, upgraded, repository)
			if repository != "fixture" && nulls != 2 {
				t.Fatalf("%s: %d NULL rows after backfill, want 2", repository, nulls)
			}
			checkTerms(t, upgraded, repository, 1)
		}
	})
}
