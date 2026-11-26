package gorm

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
	"github.com/graphvein/graphvein/internal/golden"
)

const fixtureDir = "../../../../testdata/fixtures/gorm-models"

// fixtureSources reads the fixture as repository-relative sources; the
// README is documentation, not input.
func fixtureSources(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{}
	err := filepath.WalkDir(fixtureDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(fixtureDir, p)
		rel = filepath.ToSlash(rel)
		if rel == "README.md" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sources[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

// linkFixture runs the claim pass over the fixture with the tag hook set.
func linkFixture(t *testing.T) ([]contract.Node, []contract.Edge, []contract.Diagnostic, *modeltest.Tags) {
	t.Helper()
	in := input(t, fixtureSources(t))
	tags := modeltest.NewTags()
	in.Tag = tags.Tag
	nodes, edges, diags := Link(in)
	return nodes, edges, diags, tags
}

// byIdentity indexes nodes.
func byIdentity(nodes []contract.Node) map[string]contract.Node {
	out := map[string]contract.Node{}
	for _, n := range nodes {
		out[n.Identity] = n
	}
	return out
}

// attr renders one attribute as JSON ("-" when absent or null).
func attr(n contract.Node, k string) string {
	v, ok := n.Attributes[k]
	if !ok || v == nil {
		return "-"
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

// TestGormFixture checks the claim pass over the fixture's first part:
// the records equal the golden, every claim is well formed
// (modeltest.CheckClaims, CheckDialectFacts), and the facts the rows
// decide hold for the forms named below.
func TestGormFixture(t *testing.T) {
	nodes, edges, diags, _ := linkFixture(t)
	golden.Check(t, "testdata/link.golden.json", map[string]any{"nodes": nodes, "edges": edges, "diagnostics": diags})
	modeltest.CheckClaims(t, nodes, models.ClaimFacts)
	for _, d := range diags {
		if d.Code == "schema_model_internal_error" {
			t.Errorf("internal error: %s", d.Message)
		}
	}
	n := byIdentity(nodes)
	tables := map[string]string{
		// GO-K4 names, TableName literal, pointer, constant,
		// concatenation and promotion (Revision 1, O7).
		"table:pg/model/names.go#Person":        "people",
		"table:pg/model/names.go#Human":         "humen",
		"table:pg/model/names.go#Leaf":          "leafs",
		"table:pg/model/names.go#HTTPLog":       "http_logs",
		"table:pg/model/names.go#UserOIDCID":    "user_o_id_c_ids",
		"table:pg/model/names.go#Item2B":        "item2_bs",
		"table:pg/model/names.go#Renamed":       "custom_renamed",
		"table:pg/model/names.go#RenamedPtr":    "custom_ptr",
		"table:pg/model/names.go#RenamedConst":  "custom_const",
		"table:pg/model/names.go#RenamedConcat": "custom_concat",
		"table:pg/model/names.go#Promoted":      "promoted_name",
		"table:named/db.go#Person":              "t_person",
		"table:named/db.go#HTTPLog":             "t_http_log",
		"table:nolower/db.go#NoLower":           "NoLowers",
		"table:nocfg/db.go#Item":                "items",
		// Build selection (golang.Package): a TableName in an ignored file
		// or a cgo file no configuration builds is not the model's.
		"table:buildsel/model/account.go#Account": "accounts",
		"table:buildsel/model/account.go#Ledger":  "ledgers",
	}
	for id, name := range tables {
		if got := attr(n[id], "name"); got != `"`+name+`"` {
			t.Errorf("%s: name %s, want %s", id, got, name)
		}
	}
	// Tables not claimed: TableName through the namer or a variable, a
	// name postgres truncates, a non-literal naming strategy, and the
	// provable failures (GO-P1) with the other models of their call.
	for _, id := range []string{"table:pg/model/names.go#RenamedNamer", "table:pg/model/names.go#RenamedVar", "table:pg/model/names.go#AVeryLongModelNameUsedToCheckIdentifierTruncationOnPostgresX", "table:dynamic/db.go#Thing", "table:lite/db.go#TwoAI", "table:lite/db.go#Sibling", "table:lite/db.go#Tagged", "table:lite/db.go#BadDefault", "table:lite/db.go#Draft", "table:buildsel/gen.go#Ghost", "table:buildsel/mixed/a.go#Thing"} {
		if _, ok := n[id]; ok {
			t.Errorf("%s is claimed", id)
		}
	}
	type fact struct{ id, key, want string }
	for _, f := range []fact{
		// GO-C1: sqlite keeps only the autoincrement field of a composite
		// key; postgres keeps a composite key.
		{"table:lite/db.go#CompositeAI", "primary_key", `["a"]`},
		{"table:pg/model/types.go#CompositePK", "primary_key", `["a","b"]`},
		{"table:pg/model/types.go#ExplicitPKNotID", "primary_key", `["key"]`},
		{"table:pg/model/types.go#NoIDTable", "primary_key", `[]`},
		{"column:pg/model/types.go#ExplicitPKNotID.key", "autoincrement", `true`},
		{"column:pg/model/types.go#PKNoAI.id", "autoincrement", `false`},
		// GO-T2 on postgres and sqlite.
		{"column:pg/model/types.go#TypeProbe.n_by", "type", `"smallint"`},
		{"column:pg/model/types.go#TypeProbe.dec_p", "type", `"numeric(8,0)"`},
		{"column:pg/model/types.go#TypeProbe.type_gen_int", "type", `"smallint"`},
		{"column:pg/model/types.go#TypeProbe.type_time_tag", "type", `"timestamp without time zone"`},
		{"column:pg/model/types.go#TypeProbe.sized", "length", `64`},
		{"column:pg/model/types.go#TypeProbe.t_prec", "precision", `6`},
		{"column:pg/model/types.go#TypeProbe.ser", "type", `"text"`},
		{"column:pg/model/types.go#PGOnly.odd", "type", "-"},
		{"column:lite/db.go#Note.stamp", "type", `"timestamp"`},
		{"column:lite/db.go#Note.title", "type", `"text"`},
		// GO-C2, GO-C3.
		{"column:pg/model/types.go#TypeProbe.not_null_false", "nullable", `true`},
		{"column:pg/model/types.go#TypeProbe.def_str_q", "server_default_text", `"'hello'::text"`},
		{"column:pg/model/types.go#TypeProbe.def_float", "server_default_text", `"1.5"`},
		{"column:pg/model/types.go#TypeProbe.def_null", "server_default", `false`},
		{"column:pg/model/types.go#TypeProbe.def_current", "server_default", `true`},
		{"column:pg/model/types.go#TypeProbe.def_current", "server_default_text", "-"},
		{"column:pg/model/types.go#TypeProbe.def_none", "default", `"absent"`},
		{"column:pg/model/types.go#TypeProbe.id", "server_default", `true`},
		{"column:lite/db.go#Note.seen", "server_default_text", `"null"`},
		{"column:lite/db.go#Note.body", "server_default_text", `"\"none\""`},
		{"column:lite/db.go#Note.id", "nullable", `true`},
		// GO-F1, GO-F2: embedding, prefixes, unknown columns.
		{"column:pg/model/types.go#Embeds.audit_by", "type", `"character varying(10)"`},
		{"column:pg/model/types.go#Embeds.ptr_at", "name", `"ptr_at"`},
		{"column:pg/model/types.go#TypeProbe.UpperCol", "name", `"UpperCol"`},
		{"column:pg/model/types.go#CustomProbe.mon", "state", `"unknown"`},
		{"column:pg/model/types.go#EmbedTagged.by", "state", `"unknown"`},
		{"column:pg/model/types.go#PGOnly.ident", "state", `"unknown"`},
		// GO-C4, GO-I1.
		{"column:pg/model/types.go#TypeProbe.uniq", "unique", `true`},
		{"column:pg/model/types.go#IndexProbe.c", "unique", `true`},
		{"column:pg/model/types.go#IndexProbe.a", "unique", `false`},
		{"column:pg/model/types.go#IndexProbe.a", "index", `true`},
		{"table:lite/db.go#Note", "unique_constraints", `[{"columns":["slug"],"name":null}]`},
		// GO-R1: the belongs-to relation copies Holder.ID's uint onto the
		// int32 Wallet.OwnerID (size kept: uint 32 is bigint on postgres).
		{"column:pg/model/names.go#Wallet.owner_id", "type", `"bigint"`},
		{"column:pg/model/names.go#Wallet.label", "type", `"text"`},
	} {
		if got := attr(n[f.id], f.key); got != f.want {
			t.Errorf("%s: %s %s, want %s", f.id, f.key, got, f.want)
		}
	}
	// Relations (GO-R1-R3, GO-K3 auto-add, GO-P1 narrowed to the call
	// order), as the investigation relations, relations-split, autoadd,
	// createtable and relations-ignore oracle runs give them.
	fk := func(id string) string {
		var out []string
		list, _ := n[id].Attributes["foreign_keys"].([]map[string]any)
		if n[id].Attributes["foreign_keys"] == nil {
			return "-"
		}
		for _, k := range list {
			out = append(out, fmt.Sprintf("%v.%v %v %v/%v", k["table"], k["column"], k["name"], k["on_delete"], k["on_update"]))
		}
		return strings.Join(out, ",")
	}
	for id, want := range map[string]string{
		"column:rel/models.go#Employee.company_id":        "companies.id fk_employees_company NO ACTION/NO ACTION",
		"column:rel/models.go#CreditCard.owner_id":        "owners.id fk_owners_card NO ACTION/NO ACTION",
		"column:rel/models.go#PComment.post_id":           "posts.id fk_posts_comments SET NULL/CASCADE",
		"column:rel/models.go#Self.manager_id":            "selves.id fk_selves_manager NO ACTION/NO ACTION",
		"column:rel/models.go#Referrer.refd_code":         "refds.code fk_referrers_refd NO ACTION/NO ACTION",
		"column:rel/models.go#NoFK.company_id":            "",
		"column:rel/models.go#Speaker.Langs.speaker_id":   "speakers.id fk_speaker_langs_speaker NO ACTION/NO ACTION",
		"column:rel/models.go#Speaker2.Langs.lang_code":   "langs.code fk_speaker_lang_twos_lang NO ACTION/NO ACTION",
		"column:rel/models.go#Toy.owner_id":               "-",
		"column:relsplit/db.go#CreditCard.owner_id":       "",
		"column:relsplit/db.go#PComment.post_id":          "",
		"column:relsplit/db.go#Pet.holder_id":             "holders.id fk_holders_pets CASCADE/NO ACTION",
		"column:relorder/db.go#CreditCard.owner_id":       "-",
		"column:relignore/db.go#Employee.company_id":      "",
		"column:ctlite/db.go#Employee.company_id":         "companies.id <nil> NO ACTION/NO ACTION",
		"column:ctlite/db.go#PComment.post_id":            "posts.id <nil> SET NULL/CASCADE",
		"column:runtime/database.go#Account.id":           "",
		"column:pg/model/names.go#Wallet.owner_id":        "holders.id fk_wallets_owner NO ACTION/NO ACTION",
		"column:rel/models.go#Article.Tags.article_id":    "articles.id fk_article_tags_article NO ACTION/NO ACTION",
		"column:rel/models.go#Branch.company_id":          "-",
		"column:rel/models.go#Speaker2.Langs.speaker_ref": "speaker2.id fk_speaker_lang_twos_speaker2 NO ACTION/NO ACTION",
		"column:rel/models.go#Speaker.Langs.lang_id":      "langs.id fk_speaker_langs_lang NO ACTION/NO ACTION",
		"column:rel/models.go#Article.Tags.tag_id":        "tags.id fk_article_tags_tag NO ACTION/NO ACTION",
	} {
		if got := fk(id); got != want {
			t.Errorf("%s: foreign keys %q, want %q", id, got, want)
		}
	}
	for id, want := range map[string]string{
		// Join tables (GO-R2): every column a key member; a join table
		// declared from both sides keeps no key order (O8).
		"table:rel/models.go#Speaker.Langs":  `["speaker_id","lang_id"]`,
		"table:rel/models.go#Speaker2.Langs": `["speaker_ref","lang_code"]`,
		"table:rel/models.go#Article.Tags":   "-",
		"table:rel/models.go#Company":        `["id"]`,
		"table:rel/models.go#Lang":           `["id"]`,
		"table:rel/models.go#Refd":           `["id"]`,
		"table:rel/models.go#Before":         `["id"]`,
		"table:relignore/db.go#Employee":     `["id"]`,
		"table:ctlite/db.go#Speaker":         `["id"]`,
	} {
		if got := attr(n[id], "primary_key"); got != want {
			t.Errorf("%s: primary_key %s, want %s", id, got, want)
		}
	}
	for _, id := range []string{"table:rel/models.go#Broken", "table:rel/models.go#After", "table:rel/models.go#Ticket", "table:rel/models.go#Queue", "table:rel/models.go#Pet", "table:relignore/db.go#Company", "table:relignore/db.go#Speaker.Langs", "table:ctlite/db.go#Company", "table:ctlite/db.go#Speaker.Langs", "table:rel/models.go#Tag.Articles"} {
		if _, ok := n[id]; ok {
			t.Errorf("%s is claimed", id)
		}
	}
	for _, d := range diags {
		if d.Code == "schema_model_not_proven" && strings.HasPrefix(d.File, "rel/") {
			t.Errorf("an auto-added model is marked not proven: %s", d.Message)
		}
	}
	// GO-K4: the generated index name hashed at postgres' 63, an explicit
	// name postgres would truncate left unknown.
	ix := attr(n["table:pg/model/names.go#LongIndexes"], "indexes")
	if !strings.Contains(ix, `"name":"idx_long_indexes_some_indexed_name_that_is_long_enough_ae8f2b70"`) || !strings.Contains(ix, `"columns":["other"],"descending":[false],"name":null`) {
		t.Errorf("long index names: %s", ix)
	}
	// A run-time dialect group (Revision 1, O1): per-dialect types with
	// their own verification, dialect-independent facts once.
	acc := n["column:runtime/database.go#Account.name"]
	entries := modeltest.DialectEntries(acc)
	if got := fmt.Sprintf("%v %v %v", entries["mysql"]["type"], entries["postgres"]["type"], entries["sqlite"]["type"]); got != "varchar(256) text text" {
		t.Errorf("Account.name per dialect: %v", entries)
	}
	for _, c := range modeltestClaims(t, acc) {
		want := models.VerificationOracle
		if c.Dialect == DialectMySQL {
			want = models.VerificationMapped
		}
		if c.Verification != want {
			t.Errorf("Account.name claim %+v, want %s", c, want)
		}
	}
	id := n["column:runtime/database.go#Account.id"]
	if attr(id, "nullable") != "-" || attr(id, "primary_key") != "true" || attr(n["column:runtime/database.go#Account.credits"], "nullable") != "false" {
		t.Errorf("Account.id: nullable %s, primary_key %s", attr(id, "nullable"), attr(id, "primary_key"))
	}
	if got := attr(n["table:runtime/database.go#Account"], "dialects"); got != `["mysql","postgres","sqlite"]` {
		t.Errorf("Account dialects %s", got)
	}
}

func modeltestClaims(t *testing.T, n contract.Node) []modeltest.Claim {
	t.Helper()
	raw, _ := json.Marshal(n.Attributes["claims"])
	var out []modeltest.Claim
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestGormClaimTags is the tag half of the contract test: every tag the
// fixture's claim pass emits is a contract row (a form tag a listed form
// of its row), every row with a branch is reached and every listed form of
// a multi-form row (GO-K2, GO-K3) is reached; every claim names a row of
// the contract that has a claim side, and each claimed fact comes from the
// rows that decide it.
func TestGormClaimTags(t *testing.T) {
	nodes, _, _, tags := linkFixture(t)
	rows := map[string]Row{}
	for _, r := range Contract {
		rows[r.ID] = r
	}
	for row := range tags.Rows {
		if _, ok := rows[row]; !ok {
			t.Errorf("tag %s is not a contract row", row)
		}
	}
	for f := range tags.Forms {
		row, form, _ := strings.Cut(f, "/")
		listed := false
		for _, x := range rows[row].Forms {
			listed = listed || x == form
		}
		if !listed {
			t.Errorf("form tag %s is not listed in its row", f)
		}
	}
	for _, r := range Contract {
		if r.Side == SideNone {
			continue
		}
		if !tags.Rows[r.ID] {
			t.Errorf("row %s is never reached by the fixture", r.ID)
		}
		for _, f := range r.Forms {
			if !tags.Forms[r.ID+"/"+f] {
				t.Errorf("row %s: form %q is never reached by the fixture", r.ID, f)
			}
		}
	}
	// The rows each claimed fact may come from (the contract's claim
	// cells).
	factRows := map[string]string{
		"name": "GO-K4 GO-F2", "type": "GO-T2", "length": "GO-T1", "precision": "GO-T1", "scale": "GO-T1",
		"nullable": "GO-C2", "primary_key": "GO-C1", "autoincrement": "GO-C1", "unique": "GO-C4 GO-I1",
		"index": "GO-I1", "default": "GO-C3", "server_default": "GO-C3", "foreign_keys": "GO-R3",
	}
	for id, list := range modeltest.CheckClaims(t, nodes, models.ClaimFacts) {
		for _, c := range list {
			if rows[c.Row].Side != SideClaim {
				t.Errorf("%s: claim %s by row %s, which has no claim side", id, c.Fact, c.Row)
			}
			if !strings.Contains(" "+factRows[c.Fact]+" ", " "+c.Row+" ") {
				t.Errorf("%s: claim %s by row %s, want one of %s", id, c.Fact, c.Row, factRows[c.Fact])
			}
		}
	}
}

// TestGormStepIsolation makes one table step fail: it becomes a
// schema_model_internal_error diagnostic and every other table is still
// emitted.
func TestGormStepIsolation(t *testing.T) {
	all, _, _, _ := linkFixture(t)
	faultHook = func(step, subject string) {
		if step == "table" && strings.HasSuffix(subject, "#Person") {
			panic("injected")
		}
	}
	defer func() { faultHook = nil }()
	nodes, _, diags, _ := linkFixture(t)
	failed := 0
	for _, d := range diags {
		if d.Code == "schema_model_internal_error" && strings.Contains(d.Message, "injected") {
			failed++
		}
	}
	tables := func(ns []contract.Node) int {
		c := 0
		for _, n := range ns {
			if n.Kind == "table" {
				c++
			}
		}
		return c
	}
	if failed != 3 || tables(nodes) != tables(all)-3 {
		t.Errorf("%d failed steps, %d of %d tables", failed, tables(nodes), tables(all))
	}
}
