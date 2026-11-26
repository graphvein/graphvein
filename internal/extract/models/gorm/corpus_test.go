package gorm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/lockfile"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
	"github.com/graphvein/graphvein/internal/scan"
)

const inventoryPath = "../../../../docs/contract/claims/data/gorm-models-inventory.jsonl"

// corpusDirs maps the inventory's corpus names to checkout names
// (scripts/cloud-corpus.sh).
var corpusDirs = map[string]string{"gotify": "gotify", "realworld": "realworld-gin"}

// inventoryRow is one row of docs/contract/claims/data/gorm-models-inventory.jsonl.
type inventoryRow struct {
	Corpus string `json:"corpus"`
	Scope  string `json:"scope"`
	Row    string `json:"row"`
	Form   string `json:"form"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	ID     string `json:"id"`
}

func rowKey(row, form, file string, line int, id string) string {
	return fmt.Sprintf("%s|%s|%s:%d|%s", row, form, file, line, id)
}

// discoveryRows are the inventory's discovery and read rows of the rows
// discovery decides (GO-K1, GO-K2, GO-K3, GO-M1), per corpus.
func discoveryRows(t *testing.T) map[string]map[string]bool {
	t.Helper()
	f, err := os.Open(inventoryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r inventoryRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Scope != "discovery" && r.Scope != "read" {
			continue
		}
		switch r.Row {
		case "GO-K1", "GO-K2", "GO-K3", "GO-M1":
		default:
			continue
		}
		if out[r.Corpus] == nil {
			out[r.Corpus] = map[string]bool{}
		}
		out[r.Corpus][rowKey(r.Row, r.Form, r.File, r.Line, r.ID)] = true
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// receiverForms are the inventory's receiver forms of the receiver proofs.
var receiverForms = map[string]string{"var": "ident:var_typed(*gorm.DB)", "param": "ident:param_typed(*gorm.DB)"}

// derivedRows re-derives the inventory's discovery rows from Discover's
// result alone.
func derivedRows(t *testing.T, m *Module, sources map[string][]byte) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	add := func(row, form, file string, line int, id string) { out[rowKey(row, form, file, line, id)] = true }
	mod := lockfile.ParseGoMod(sources[m.GoMod])
	add("GO-K1", "module", m.GoMod, mod.Line, m.Path)
	for _, r := range m.Requires {
		form := "require"
		if r.Indirect {
			form = "require_indirect"
		}
		add("GO-K1", form, m.GoMod, r.Line, r.Path+" "+r.Version)
	}
	pkgPath := func(dir string) string {
		if dir == m.Dir {
			return m.Path
		}
		return m.Path + "/" + strings.TrimPrefix(dir, m.Dir+"/")
	}
	for _, o := range m.Opens {
		cfg := map[string]string{"once-defined local": "ident", "config literal": "&literal"}[o.Config.Form]
		add("GO-K2", "gorm.Open(dialector="+o.Driver+"."+o.Constructor+", config="+cfg+")", o.File, o.Line, "")
		add("GO-K2", "gorm.Config literal{"+strings.Join(o.Config.Keys, ",")+"}", o.File, o.Config.Line, "")
	}
	for _, s := range m.Sites {
		receiver := receiverForms[s.Receiver]
		if s.Receiver == "module function" {
			receiver = "ident:define_from_call(" + s.ReceiverCall + ",result0)"
		}
		call := strings.TrimPrefix(s.Call, "Migrator().")
		add("GO-K3", "migrate_site:"+call+":receiver="+receiver, s.File, s.Line, fmt.Sprintf("%d args", len(s.Args)))
		for _, a := range s.Args {
			if a.Model == nil {
				t.Errorf("%s:%d: argument not read: %s", s.File, s.Line, a.Reason)
				continue
			}
			scope := "qualified"
			if a.Model.Dir == path.Dir(s.File) {
				scope = "local"
			}
			form := map[string]string{"new(T)": "new(" + scope + ")", "&T{...}": "&" + scope + "{}", "T{...}": scope + "{}"}[a.Form]
			add("GO-K3", "migrate_arg:"+form, s.File, s.Line, pkgPath(a.Model.Dir)+"."+a.Model.Struct.Name)
		}
	}
	for _, e := range m.Expansions {
		add("GO-K3", "package_function_call:"+e.Call, e.File, e.Line, pkgPath(path.Dir(strings.SplitN(e.Callee, "#", 2)[0])))
	}
	for _, mo := range m.Models {
		add("GO-M1", "struct:migrate_arg", mo.File, mo.Struct.Line, mo.Struct.Name)
	}
	return out
}

// TestGormDiscoveryCorpus runs Discover on gotify and realworld at their
// pins and checks that its modules, pins, gorm.Open sites, configuration
// literals, migrate sites (receiver proof and arguments), the expanded
// package-function call and the listed models equal the inventory's
// discovery and read rows, both ways; that both modules are read with the
// dialects, configuration and order of the task file's Discovery section;
// and that discovery reports no marker (both corpora use only listed
// forms). It only parses; set GRAPHVEIN_GORM_CENSUS_CORPUS to the directory
// holding the checkouts (scripts/cloud-corpus.sh gotify realworld-gin).
func TestGormDiscoveryCorpus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GORM_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_GORM_CENSUS_CORPUS is not set")
	}
	inventory := discoveryRows(t)
	for _, corpus := range []string{"gotify", "realworld"} {
		t.Run(corpus, func(t *testing.T) {
			root := filepath.Join(dir, corpusDirs[corpus])
			scanned, err := scan.Walk(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			sources := map[string][]byte{}
			for _, f := range scanned.Files {
				sources[f.Record.Path] = f.Source
			}
			in := build(t, sources)
			mods, diags := Discover(in)
			m := one(t, mods)
			if m.Refused != "" || m.VersionStatus != VersionVerified || !m.OrderProven || len(m.Incomplete) != 0 || len(m.InflectionChanged) != 0 {
				t.Errorf("module: refused %q, version %s, order %v, incomplete %v", m.Refused, m.VersionStatus, m.OrderProven, m.Incomplete)
			}
			for _, d := range diags {
				t.Errorf("diagnostic %s %s: %s", d.Code, d.File, d.Message)
			}
			c := m.Config
			switch corpus {
			case "gotify":
				if strings.Join(m.Dialects, ",") != "mysql,postgres,sqlite" || m.Dialect != "" || !c.DisableForeignKeys.Known || !c.DisableForeignKeys.Value {
					t.Errorf("gotify: dialects %v, config %+v", m.Dialects, c)
				}
			case "realworld":
				if m.Dialect != "sqlite" || !c.DisableForeignKeys.Known || c.DisableForeignKeys.Value {
					t.Errorf("realworld: dialect %q, config %+v", m.Dialect, c)
				}
				// Migrate expands users.AutoMigrate() before its own 5 calls.
				order := "users/models.go:48 users/models.go:49 hello.go:17 hello.go:18 hello.go:19 hello.go:20 hello.go:21"
				if len(m.Sequences) != 1 || strings.Join(sequence(m.Sequences[0]), " ") != order {
					t.Errorf("realworld: sequences %d, want one: %s", len(m.Sequences), order)
				}
			}
			if !c.Naming.Known || c.Naming.Naming != (Naming{}) || !c.IgnoreRelationships.Known || c.IgnoreRelationships.Value {
				t.Errorf("%s: config %+v", corpus, c)
			}
			for _, s := range m.Sites {
				if strings.HasSuffix(s.File, "_test.go") {
					t.Errorf("test-scope site read: %s:%d", s.File, s.Line)
				}
			}
			got := derivedRows(t, m, sources)
			want := inventory[corpus]
			var missing, extra []string
			for k := range want {
				if !got[k] {
					missing = append(missing, k)
				}
			}
			for k := range got {
				if !want[k] {
					extra = append(extra, k)
				}
			}
			sort.Strings(missing)
			sort.Strings(extra)
			for _, k := range missing {
				t.Errorf("inventory row not discovered: %s", k)
			}
			for _, k := range extra {
				t.Errorf("discovered row not in the inventory: %s", k)
			}
			t.Logf("%s: %d discovery rows equal, %d Open sites, %d migrate sites, %d models", corpus, len(want), len(m.Opens), len(m.Sites), len(m.Models))
		})
	}
}

// TestGormCorpusLink checks the claim pass on both corpora (set
// GRAPHVEIN_GORM_CENSUS_CORPUS) against the investigation coverage targets: every
// table claimed with every column read and typed on every dialect of its
// group (gotify: mysql, postgres, sqlite; realworld: sqlite), the index
// count, and the foreign keys: gotify's proven absent (the configuration
// disables them), realworld's 10 as the investigation oracle's
// sqlite catalog lists them, with article_tags' key order
// unknown (open point O8).
func TestGormCorpusLink(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GORM_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_GORM_CENSUS_CORPUS is not set")
	}
	type target struct{ tables, columns, indexes, fks int }
	for corpus, want := range map[string]target{"gotify": {5, 42, 10, 0}, "realworld": {8, 46, 9, 10}} {
		t.Run(corpus, func(t *testing.T) {
			scanned, err := scan.Walk(context.Background(), filepath.Join(dir, corpusDirs[corpus]))
			if err != nil {
				t.Fatal(err)
			}
			sources := map[string][]byte{}
			for _, f := range scanned.Files {
				sources[f.Record.Path] = f.Source
			}
			nodes, _, diags := Link(build(t, sources))
			for _, d := range diags {
				t.Errorf("diagnostic %s %s: %s", d.Code, d.File, d.Message)
			}
			modeltest.CheckClaims(t, nodes, models.ClaimFacts)
			var got target
			var fks []string
			for _, n := range nodes {
				switch n.Kind {
				case "table":
					got.tables++
					list, ok := n.Attributes["indexes"].([]map[string]any)
					if !ok {
						t.Errorf("%s: indexes %v", n.Identity, n.Attributes["indexes"])
					}
					got.indexes += len(list)
					if n.Attributes["constraint_set"] != "complete" || n.Attributes["column_set"] != "complete" {
						t.Errorf("%s: %v", n.Identity, n.Attributes["reasons"])
					}
				case "column":
					got.columns++
					typed := n.Attributes["type"] != nil
					if e := modeltest.DialectEntries(n); e != nil {
						typed = len(e) == 3
						for _, d := range e {
							typed = typed && d["type"] != nil
						}
					}
					if n.Attributes["state"] != "read" || !typed || n.Attributes["nullable"] == nil && n.Attributes["primary_key"] != true {
						t.Errorf("%s: %v", n.Identity, n.Attributes)
					}
					list, ok := n.Attributes["foreign_keys"].([]map[string]any)
					if !ok {
						t.Errorf("%s: foreign_keys %v", n.Identity, n.Attributes["foreign_keys"])
					}
					for _, fk := range list {
						got.fks++
						fks = append(fks, fmt.Sprintf("%s -> %s.%s %v/%v %v", n.QualifiedName, fk["table"], fk["column"], fk["on_delete"], fk["on_update"], fk["status"]))
					}
				}
			}
			if got != want {
				t.Errorf("%+v, want %+v", got, want)
			}
			if corpus != "realworld" {
				return
			}
			sort.Strings(fks)
			wantFKs := []string{
				"ArticleModel.Tags.article_model_id -> article_models.id NO ACTION/NO ACTION resolved",
				"ArticleModel.Tags.tag_model_id -> tag_models.id NO ACTION/NO ACTION resolved",
				"ArticleModel.author_id -> article_user_models.id NO ACTION/NO ACTION resolved",
				"ArticleUserModel.user_model_id -> user_models.id NO ACTION/NO ACTION resolved",
				"CommentModel.article_id -> article_models.id NO ACTION/NO ACTION resolved",
				"CommentModel.author_id -> article_user_models.id NO ACTION/NO ACTION resolved",
				"FavoriteModel.favorite_by_id -> article_user_models.id NO ACTION/NO ACTION resolved",
				"FavoriteModel.favorite_id -> article_models.id NO ACTION/NO ACTION resolved",
				"FollowModel.followed_by_id -> user_models.id NO ACTION/NO ACTION resolved",
				"FollowModel.following_id -> user_models.id NO ACTION/NO ACTION resolved",
			}
			if !reflect.DeepEqual(fks, wantFKs) {
				t.Errorf("foreign keys:\n%s", strings.Join(fks, "\n"))
			}
			for _, n := range nodes {
				if n.Identity == "table:articles/models.go#ArticleModel.Tags" && (n.Attributes["name"] != "article_tags" || n.Attributes["primary_key"] != nil) {
					t.Errorf("article_tags: %v", n.Attributes)
				}
			}
		})
	}
}
