package sequelize

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

// baselineTable is one per-table row of the investigation's oracle baseline
// (docs/contract/claims/data/sequelize-models-inventory.jsonl, scope "oracle"): the catalog after
// Sequelize's own sync.
type baselineTable struct {
	columns, notnull, defaults, pk, uniques, fks, indexes int
}

// readBaseline reads the oracle rows of one corpus and form.
func readBaseline(t *testing.T, corpus, form string) map[string]baselineTable {
	t.Helper()
	f, err := os.Open("../../../../docs/contract/claims/data/sequelize-models-inventory.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]baselineTable{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r struct{ Corpus, Scope, Form, ID, Name string }
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.Corpus != corpus || r.Scope != "oracle" || r.Form != form {
			continue
		}
		var b baselineTable
		if _, err := fmt.Sscanf(r.Name, "columns=%d notnull=%d defaults=%d pk=%d uniques=%d fks=%d indexes=%d", &b.columns, &b.notnull, &b.defaults, &b.pk, &b.uniques, &b.fks, &b.indexes); err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		out[strings.TrimPrefix(r.ID, "public.")] = b
	}
	return out
}

// corpusLinkWant is the record count the claim pass reaches per corpus.
type corpusLinkWant struct {
	corpus, form string
	// tables is the number of claimed tables; columns the number of
	// column records (read or unknown); unknown the unknown ones.
	tables, columns, unknown int
	// fks is the number of claimed foreign keys; fkUnknown the read
	// columns whose foreign-key list is unknown.
	fks, fkUnknown int
}

var corpusLink = []corpusLinkWant{
	// opencollective-api: the 52 init models (the 10 Temporal histories
	// are row D); 753 column records: the catalog's 750 plus the 2
	// `new DataTypes.VIRTUAL(...)` attributes (unknown
	// records) and the id TransactionSettlement.removeAttribute("id")
	// removes. 6 unknown: the 2 VIRTUAL, 1 ARRAY(new
	// ExpenseTypesEnum()), 1 raw string type, the removed id, and
	// Members.CollectiveId, which the belongsToMany calls over the through
	// model Member assign (row SQ-R2); the 7 CustomDataTypes helpers are
	// read since Revision 2 (row SQ-A1 form "helper member", open point
	// O4). 131 foreign keys; 23 read columns have an unknown list, all
	// under the cyclic rule: 14 in the four alter-failing
	// models and on SocialLinks' primary key, 9 in the models with a
	// column that is not read (AccountingCategories 1, Collectives 4: the
	// `{type: new DataTypes.VIRTUAL(...)}` objects, Members 3,
	// TransactionSettlements 1).
	{corpus: "opencollective-api", form: "pg17_sync_catalog", tables: 52, columns: 753, unknown: 6, fks: 131, fkUnknown: 23},
	// Gladys: the 32 factory models, every column read; the 36 foreign
	// keys of the catalog, every list known.
	{corpus: "gladys", form: "sqlite_sync_catalog", tables: 32, columns: 328, unknown: 0, fks: 36, fkUnknown: 0},
}

// TestSequelizeCorpusLink runs the claim pass on the pinned
// opencollective-api and Gladys checkouts (parse only, no indexing) and
// compares each claimed table with the investigation's oracle baseline: the table
// exists in the catalog; its read and unknown columns are no more than
// the catalog's columns, and every catalog column the pass does not
// list is one an association adds; nullability, default presence,
// foreign keys, unique constraints and indexes claimed are compared per
// table as counts (a table whose every column and list is known claims
// exactly the catalog's foreign keys). It logs the per-table
// comparison and the totals the task summary reports. Set
// GRAPHVEIN_SEQUELIZE_CORPUS to the .corpus directory to run it.
func TestSequelizeCorpusLink(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_SEQUELIZE_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_SEQUELIZE_CORPUS is not set")
	}
	for _, want := range corpusLink {
		t.Run(want.corpus, func(t *testing.T) {
			name := want.corpus
			baselineCorpus := name
			if name == "gladys" {
				baselineCorpus = "Gladys"
			}
			base := readBaseline(t, baselineCorpus, want.form)
			in := corpusInput(t, filepath.Join(dir, name))
			nodes, _, diags := Link(in)
			modeltest.CheckClaims(t, nodes, models.ClaimFacts)
			for _, l := range diagLines(diags) {
				if !strings.HasPrefix(l, codeMigrations) {
					t.Log(l)
				}
			}
			tables := map[string]contract.Node{}
			cols := map[string][]contract.Node{}
			byIdentity := map[string]string{}
			for _, n := range nodes {
				if n.Kind == "table" {
					tables[n.Name] = n
					byIdentity[n.Identity] = n.Name
				}
			}
			for _, n := range nodes {
				if n.Kind == "column" {
					tid := "table:" + n.File + "#" + strings.SplitN(n.QualifiedName, ".", 2)[0]
					cols[byIdentity[tid]] = append(cols[byIdentity[tid]], n)
				}
			}
			var names []string
			for n := range tables {
				names = append(names, n)
			}
			sort.Strings(names)
			total, unknown, catalogCols, partial := 0, 0, 0, 0
			totalFKs, totalFKUnknown, catalogFKs, totalUniques, totalIndexes, catalogUniques, catalogIndexes := 0, 0, 0, 0, 0, 0, 0
			for _, n := range names {
				b, ok := base[n]
				if !ok {
					t.Errorf("table %s is not in the oracle catalog", n)
					continue
				}
				catalogCols += b.columns
				read, unk, notnull, defaults, defUnknown, nullUnknown := 0, 0, 0, 0, 0, 0
				fks, fkUnk := 0, 0
				for _, c := range cols[n] {
					if c.Attributes["state"] == "unknown" {
						unk++
						continue
					}
					read++
					switch list := c.Attributes["foreign_keys"].(type) {
					case []map[string]any:
						fks += len(list)
						for _, fk := range list {
							if fk["status"] != "resolved" {
								t.Logf("%s.%s: foreign key %v %v", n, c.Name, fk["table"], fk["status"])
							}
						}
					default:
						fkUnk++
					}
					if c.Attributes["nullable"] == false {
						notnull++
					}
					if c.Attributes["server_default"] == true {
						defaults++
					}
					if c.Attributes["server_default"] == nil {
						defUnknown++
					}
					if c.Attributes["nullable"] == nil {
						nullUnknown++
					}
				}
				total += read + unk
				unknown += unk
				totalFKs += fks
				totalFKUnknown += fkUnk
				catalogFKs += b.fks
				// A claimed foreign key the catalog lacks, or an absence
				// claimed on every column of a table whose catalog has more
				// (a wrong empty list), is a precision stop.
				if fks > b.fks || unk == 0 && fkUnk == 0 && fks != b.fks {
					t.Errorf("%s: %d foreign keys claimed (%d lists unknown), the catalog has %d", n, fks, fkUnk, b.fks)
				}
				uniques, _ := tables[n].Attributes["unique_constraints"].([]map[string]any)
				indexes, _ := tables[n].Attributes["indexes"].([]map[string]any)
				if len(uniques) > b.uniques || len(indexes) > b.indexes {
					t.Errorf("%s: %d unique constraints and %d indexes claimed, the catalog has %d and %d", n, len(uniques), len(indexes), b.uniques, b.indexes)
				}
				totalUniques += len(uniques)
				totalIndexes += len(indexes)
				catalogUniques += b.uniques
				catalogIndexes += b.indexes
				set := tables[n].Attributes["column_set"]
				if set == "partial" {
					partial++
				}
				// An unknown column may not exist (an unread type can be
				// VIRTUAL), so only the read ones are bounded.
				if read > b.columns {
					t.Errorf("%s: %d read columns, the catalog has %d", n, read, b.columns)
				}
				if set == "complete" && read+unk != b.columns {
					t.Errorf("%s: complete column set of %d, the catalog has %d", n, read+unk, b.columns)
				}
				if notnull > b.notnull || defaults > b.defaults {
					t.Errorf("%s: %d not-null and %d default columns claimed, the catalog has %d and %d", n, notnull, defaults, b.notnull, b.defaults)
				}
				t.Logf("%-32s catalog columns %3d notnull %3d defaults %2d fks %2d | read %3d unknown %2d notnull %3d (unknown %d) defaults %2d (unknown %d) fks %2d (unknown %d) column_set %s", n, b.columns, b.notnull, b.defaults, b.fks, read, unk, notnull, nullUnknown, defaults, defUnknown, fks, fkUnk, set)
			}
			t.Logf("%s: %d tables (%d partial column sets), %d columns (%d unknown); the catalog has %d columns in these tables", name, len(names), partial, total, unknown, catalogCols)
			t.Logf("%s: %d foreign keys claimed (%d read columns with an unknown list), the catalog has %d in these tables; %d unique constraints (catalog %d), %d indexes (catalog %d)", name, totalFKs, totalFKUnknown, catalogFKs, totalUniques, catalogUniques, totalIndexes, catalogIndexes)
			if totalFKs != want.fks || totalFKUnknown != want.fkUnknown {
				t.Errorf("%d foreign keys, %d unknown lists; want %d, %d", totalFKs, totalFKUnknown, want.fks, want.fkUnknown)
			}
			if len(names) != want.tables || total != want.columns || unknown != want.unknown {
				t.Errorf("%d tables, %d columns, %d unknown; want %d, %d, %d", len(names), total, unknown, want.tables, want.columns, want.unknown)
			}
			if out := os.Getenv("GRAPHVEIN_SEQUELIZE_DUMP"); out != "" {
				data, _ := json.MarshalIndent(nodes, "", " ")
				if err := os.WriteFile(filepath.Join(out, name+".json"), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
