package drizzle

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
)

// corpusWant is what discovery must find in a pinned corpus: the investigation's
// discovery rows (docs/contract/claims/data/drizzle-models-inventory.jsonl, scope "discovery";
// the task file's "Coverage targets").
type corpusWant struct {
	// Packages are the Drizzle package directories with their config
	// summaries ("<root> <named> <scripts>"; none for a package without a
	// config).
	Packages map[string][]string
	Pins     map[string]any
	Dialect  string
	// Schema are the config's schema files; Modules the number of modules
	// reached; Tables the table builder declarations in them (every table
	// of both corpora is exported, so the investigation's table count).
	Schema  []string
	Modules int
	Tables  int
	// Outside is the outside-schema marker's file list; NoConfig the
	// packages with a no-config marker.
	Outside  string
	NoConfig []string
	// Out is the config's drizzle-kit migrations folder and SQL the .sql
	// files directly in it (the server's drizzle-kit unread tool, G2B5).
	Out string
	SQL int
}

var corpusDiscover = map[string]corpusWant{
	"kaneo": {
		Packages: map[string][]string{"apps/api": {"apps/api/drizzle.config.ts default db:generate,db:migrate,db:studio"}},
		Pins:     map[string]any{"drizzle-orm": "0.45.4", "drizzle-kit": "0.31.11"},
		Dialect:  "postgresql",
		Schema:   []string{"apps/api/src/database/schema.ts"},
		Modules:  1,
		Tables:   51,
		Outside:  "1 file(s) with 10 table builder call(s): apps/api/auth-schema.ts (10)",
		Out:      "apps/api/drizzle",
		SQL:      74,
	},
	"dokploy": {
		Packages: map[string][]string{
			"apps/dokploy":    {"apps/dokploy/server/db/drizzle.config.ts --config db:push,db:studio,migration:drop,migration:generate,migration:up,studio"},
			"apps/schedules":  nil,
			"packages/server": nil,
		},
		Pins:     map[string]any{"drizzle-orm": "0.45.2", "drizzle-kit": "0.31.9"},
		Dialect:  "postgresql",
		Schema:   []string{"apps/dokploy/server/db/schema/index.ts"},
		Modules:  52,
		Tables:   68,
		Outside:  "1 file(s) with 13 table builder call(s): packages/server/auth-schema2.ts (13)",
		NoConfig: []string{"packages/server/package.json"},
		Out:      "apps/dokploy/drizzle",
		SQL:      200,
	},
}

// TestDrizzleCorpusDiscover runs discovery on the pinned kaneo and dokploy
// checkouts (parse only, no indexing) and compares it with the investigation's
// discovery rows: packages and configs (default name in kaneo, --config in
// six dokploy scripts), pins, the schema file resolved against the package
// directory, the modules its `export *` barrel reaches through
// tsconfig paths, the tables they declare, the stale-file marker (A6), the
// no-config marker of dokploy's packages/server (A7), no marker for
// apps/schedules and none for the commented-out auth-schema.ts (A8). Set
// GRAPHVEIN_DRIZZLE_CORPUS to the .corpus directory to run it.
func TestDrizzleCorpusDiscover(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_DRIZZLE_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_DRIZZLE_CORPUS is not set")
	}
	for _, corpus := range []string{"kaneo", "dokploy"} {
		t.Run(corpus, func(t *testing.T) {
			want := corpusDiscover[corpus]
			in := corpusInput(t, filepath.Join(dir, corpus))
			pkgs, diags := Discover(in)
			for _, l := range diagLines(diags) {
				t.Log(l)
			}
			got := map[string][]string{}
			var config *Config
			for _, p := range pkgs {
				got[p.Dir] = nil
				for _, c := range p.Configs {
					got[p.Dir] = append(got[p.Dir], c.Root+" "+c.Named+" "+strings.Join(sortedCopy(c.Scripts), ","))
					config = c
				}
				if len(p.Configs) > 0 && !reflect.DeepEqual(p.Pins, want.Pins) {
					t.Errorf("%s pins %v, want %v", p.Dir, p.Pins, want.Pins)
				}
			}
			if !reflect.DeepEqual(got, want.Packages) {
				t.Fatalf("packages %v, want %v", got, want.Packages)
			}
			if !config.Proven() || config.Dialect != want.Dialect || config.Casing != "" || !config.CasingKnown || config.SchemaFilter != nil || !reflect.DeepEqual(config.Files, want.Schema) {
				t.Errorf("config %+v", config)
			}
			builders := builderFiles(in)
			tables := 0
			for _, m := range config.Modules {
				tables += builders[m.File]
			}
			if len(config.Modules) != want.Modules || tables != want.Tables || len(config.Unresolved) != 0 || len(config.NotRead) != 0 {
				t.Errorf("modules %d (want %d), tables %d (want %d), unresolved %v, not read %v", len(config.Modules), want.Modules, tables, want.Tables, config.Unresolved, config.NotRead)
			}
			if !hasDiag(diags, codeUnread, config.Path, want.Outside+" outside the configured schema") {
				t.Errorf("no outside-schema marker %q", want.Outside)
			}
			sqlFiles := 0
			for f := range in.Sources {
				if path.Dir(f) == config.Out && strings.HasSuffix(f, ".sql") {
					sqlFiles++
				}
			}
			_, journal := in.Sources[config.Out+"/meta/_journal.json"]
			if config.Out != want.Out || sqlFiles != want.SQL || !journal || !hasDiag(diags, codeMigrations, config.Path, migrationsMarker+want.Out) {
				t.Errorf("migrations folder %q with %d .sql files (journal %v), want %q with %d", config.Out, sqlFiles, journal, want.Out, want.SQL)
			}
			var noConfig []string
			for _, d := range diags {
				if d.Code == codeNotProven {
					noConfig = append(noConfig, d.File)
				}
				if d.Code == codeUnread && d.File != config.Path {
					t.Errorf("unexpected marker %s: %s", d.File, d.Message)
				}
				if strings.Contains(d.Message, "auth-schema.ts") && corpus == "dokploy" {
					t.Errorf("the commented-out auth-schema.ts is in a marker: %s", d.Message)
				}
			}
			if !reflect.DeepEqual(noConfig, want.NoConfig) {
				t.Errorf("not-proven markers on %v, want %v", noConfig, want.NoConfig)
			}
			t.Logf("%s: %d packages, %d modules, %d tables, %d diagnostics", corpus, len(pkgs), len(config.Modules), tables, len(diags))
		})
	}
}

// corpusInput scans and parses a checkout as the indexer does (every
// scanned file in the TypeScript map, facts and model payloads for script
// files) without indexing it.
func corpusInput(t *testing.T, root string) Input {
	t.Helper()
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("%v (run scripts/cloud-corpus.sh)", err)
	}
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	in := Input{TypeScript: map[string]typescript.Facts{}, Models: map[string]*typescript.ModelSyntax{}, Sources: map[string][]byte{}}
	var files []scan.File
	var jobs []parse.Job
	for _, f := range scanned.Files {
		in.Sources[f.Record.Path] = f.Source
		in.TypeScript[f.Record.Path] = typescript.Facts{Exports: map[string]string{}}
		switch f.Record.Language {
		case "typescript", "tsx", "javascript":
			files = append(files, f)
			jobs = append(jobs, parse.Job{Language: f.Record.Language, Source: f.Source})
		}
	}
	options := parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}
	err = parse.Consume(context.Background(), jobs, options, func(i int, result parse.Result) {
		p := files[i].Record.Path
		if result.Err != nil {
			t.Errorf("%s: %v", p, result.Err)
			return
		}
		f := typescript.Extract(p, files[i].Source, result.Root)
		in.TypeScript[p] = f
		if m := typescript.CaptureModels(p, result.Root, f); m != nil {
			in.Models[p] = m
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	in.Resolver = typescript.NewModuleResolver(in.TypeScript, in.Sources)
	return in
}

// corpusLinkWant is the investigation's oracle baseline per corpus (task file
// "Oracle baseline"): tables, columns, NOT NULL columns and columns with
// a database default.
var corpusLinkWant = map[string][4]int{
	"kaneo":   {51, 442, 306, 140},
	"dokploy": {68, 960, 481, 193},
}

// corpusKeysWant is the investigation's oracle baseline for the keys (task file
// "Oracle baseline"): primary keys, unique constraints, foreign keys,
// indexes (non-constraint), unique indexes, partial indexes; and the
// relations() entries read (kaneo 9 calls in the schema, dokploy 50).
var corpusKeysWant = map[string][7]int{
	"kaneo":   {51, 25, 66, 83, 4, 6, 9},
	"dokploy": {68, 22, 135, 9, 2, 0, 50},
}

// TestDrizzleCorpusLink runs the claim pass on the pinned kaneo and
// dokploy checkouts and compares it with the investigation's oracle baseline:
// tables, columns, NOT NULL columns and database defaults (every column
// that is not null or has a default there is claimed so, and no other
// column is; no nullability unknown), and the keys: primary keys, unique
// constraints, foreign keys (resolved, with names), indexes, unique and
// partial indexes (with predicate text), every table complete, and the
// relations() calls of the files holding claimed tables, every entry
// read with its target. Set GRAPHVEIN_DRIZZLE_CORPUS to the .corpus
// directory to run it.
func TestDrizzleCorpusLink(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_DRIZZLE_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_DRIZZLE_CORPUS is not set")
	}
	for _, corpus := range []string{"kaneo", "dokploy"} {
		t.Run(corpus, func(t *testing.T) {
			in := corpusInput(t, filepath.Join(dir, corpus))
			nodes, edges, diags := Link(in)
			modeltest.CheckClaims(t, nodes, models.ClaimFacts)
			var tables, columns, notNull, nullable, nullUnknown, defaults, noDefault, defaultUnknown, texts, unknownCols int
			var keys [7]int
			fkNames := map[string]bool{}
			for _, n := range nodes {
				a := n.Attributes
				switch n.Kind {
				case "table":
					tables++
					if a["state"] != "complete" {
						t.Errorf("table %s is %v: %v", n.Identity, a["state"], a["reasons"])
					}
					if pk, ok := a["primary_key"].([]string); ok && len(pk) > 0 {
						keys[0]++
					}
					u, _ := a["unique_constraints"].([]map[string]any)
					keys[1] += len(u)
					ix, _ := a["indexes"].([]map[string]any)
					for _, x := range ix {
						keys[3]++
						if x["unique"] == true {
							keys[4]++
						}
						if x["partial"] == true {
							keys[5]++
							if x["where"] == nil {
								t.Errorf("%s: index %v has no predicate text", n.Identity, x["name"])
							}
						}
					}
					for _, r := range a["reasons"].([]models.Reason) {
						if r.Fact != nil && *r.Fact == "orm_relations" {
							t.Errorf("%s: relations not read: %s", n.Identity, r.Reason)
						}
					}
					rels, _ := a["orm_relations"].([]map[string]any)
					for _, r := range rels {
						if r["target"] == nil {
							t.Errorf("%s: relation %v has no target", n.Identity, r["name"])
						}
					}
					if len(rels) > 0 {
						keys[6]++
					}
				case "column":
					columns++
					if a["state"] != "read" {
						unknownCols++
					}
					switch a["nullable"] {
					case false:
						notNull++
					case true:
						nullable++
					default:
						nullUnknown++
					}
					switch a["server_default"] {
					case true:
						defaults++
						if a["server_default_text"] != nil {
							texts++
						}
					case false:
						noDefault++
					default:
						defaultUnknown++
						t.Logf("default unknown: %s %v", n.Identity, a["reasons"])
					}
					if a["type"] == nil {
						t.Logf("type unknown: %s %v", n.Identity, a["reasons"])
					}
					fks, ok := a["foreign_keys"].([]map[string]any)
					if !ok {
						t.Errorf("%s: foreign keys unknown: %v", n.Identity, a["reasons"])
					}
					for _, fk := range fks {
						if fk["status"] != "resolved" || fk["name"] == nil {
							t.Errorf("%s: foreign key %v", n.Identity, fk)
						}
						fkNames[n.File+"\x00"+fk["name"].(string)] = true
					}
				}
			}
			for _, d := range diags {
				if d.Code == codeImportFailure || d.Code == codeNotClaimed || d.Code == "schema_model_internal_error" || d.Code == codeUnread && strings.Contains(d.Message, "(row D)") {
					t.Errorf("diagnostic %s %s: %s", d.Code, d.File, d.Message)
				}
			}
			want := corpusLinkWant[corpus]
			got := [4]int{tables, columns, notNull, defaults}
			if got != want || unknownCols != 0 || defaultUnknown != 0 || nullUnknown != 0 {
				t.Errorf("tables, columns, NOT NULL, defaults %v, want %v; %d unknown columns, %d unknown defaults, %d unknown nullability", got, want, unknownCols, defaultUnknown, nullUnknown)
			}
			keys[2] = len(fkNames)
			// Relations are counted per relations() call: one source table
			// may have several, so count the declarations instead.
			keys[6] = 0
			claimed := map[string]bool{}
			for _, n := range nodes {
				claimed[n.File] = true
			}
			for file, d := range in.Models {
				if !claimed[file] {
					continue
				}
				for _, decl := range d.Declarations {
					if base, _, ok := chain(decl.Value); ok && base.Callee != nil && base.Callee.Kind == typescript.ModelIdentifier && base.Callee.Name == "relations" {
						keys[6]++
					}
				}
			}
			fkEdges := 0
			for _, e := range edges {
				if e.Kind == "foreign_key" {
					fkEdges++
				}
			}
			if keys != corpusKeysWant[corpus] {
				t.Errorf("PKs, uniques, FKs, indexes, unique indexes, partial indexes, relations() %v, want %v", keys, corpusKeysWant[corpus])
			}
			t.Logf("%s: keys %v, %d foreign_key edges", corpus, keys, fkEdges)
			t.Logf("%s: %d tables, %d columns; nullable false %d, true %d, unknown %d; server default true %d (text %d), false %d, unknown %d",
				corpus, tables, columns, notNull, nullable, nullUnknown, defaults, texts, noDefault, defaultUnknown)
		})
	}
}
