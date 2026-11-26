package sequelize

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// corpusWant is what discovery must find in a pinned corpus: the investigation's
// discovery rows (docs/contract/claims/data/sequelize-models-inventory.jsonl, scope "discovery" and
// "not_read").
type corpusWant struct {
	Dir, Manifest, Spec, Lockfile string
	Pins                          map[string]any
	Drivers                       []string
	// Instance is "<file>:<line> <name>"; Dialect "<dialect>/<from>";
	// From and Source where the options were read; Spread the number of
	// configuration spreads read under SQ-V5; Define the number of entries
	// of the global define (-1: none).
	Instance, Dialect, From, Source string
	Spread, Define                  int
	// Models counts the reached models per "<form>/<context>";
	// Associations the reached association calls per context.
	Models       map[string]int
	Associations map[string]int
	// Migrations is the migrations folder and its script-file count.
	Migrations     string
	MigrationFiles int
	SyncSites      []string
	// Wrappers is the number of project wrapper calls marked (open point
	// O8); Incomplete the number of incompleteness reasons of the group.
	Wrappers, Incomplete int
	// TestSites says whether the test-file marker must be present.
	TestSites bool
}

var corpusDiscover = map[string]corpusWant{
	"opencollective-api": {
		Dir: ".", Manifest: "package.json", Spec: "6.37.8", Lockfile: "package-lock.json",
		Pins:     map[string]any{"sequelize": "6.37.8", "inflection": "1.13.4"},
		Drivers:  []string{"pg"},
		Instance: "server/lib/sequelize.ts:60 sequelize", Dialect: "postgres/driver",
		From: "literal", Source: "server/lib/sequelize.ts:60", Spread: 1, Define: -1,
		Models:       map[string]int{"init/module": 52},
		Associations: map[string]int{"module": 197},
		Migrations:   "migrations", MigrationFiles: 715,
		SyncSites: []string{"scripts/db_setup.ts:47"},
		Wrappers:  10, Incomplete: 11, TestSites: true,
	},
	"gladys": {
		Dir: "server", Manifest: "server/package.json", Spec: "^6.29.0", Lockfile: "server/package-lock.json",
		Pins:     map[string]any{"sequelize": "6.37.8", "inflection": "1.13.4"},
		Drivers:  []string{"sqlite3"},
		Instance: "server/models/index.js:15 sequelize", Dialect: "sqlite/literal",
		From: "environment", Source: "server/config/config.js:1", Spread: 0, Define: 4,
		Models:       map[string]int{"define/factory": 32},
		Associations: map[string]int{"associate": 53},
		Migrations:   "server/migrations", MigrationFiles: 81,
	},
}

// TestSequelizeCorpusDiscover runs discovery on the pinned
// opencollective-api and Gladys checkouts (parse only, no indexing) and
// compares it with the investigation's discovery and not-read rows: the package and
// its sequelize range, the lockfile pins of sequelize and inflection, the
// single driver, the instance, the dialect (opencollective-api by the
// driver rule, Gladys literal through open point O2's environment
// selector), the global define (opencollective-api's configuration spread
// read under SQ-V5, open point O1; Gladys's four-key define), the models
// that reach the instance (52 init statements importing the instance; 32
// factories called with it), the association calls (197 at module level
// of models/index.ts in statement order; 53 in associate functions run by
// the recognised loop), every source and target resolved, the migrations
// folder (SQ-U1), the sync site (SQ-V2), the Temporal wrapper calls (open
// point O8: ten history tables, the group incomplete) and the test-file
// marker. Set GRAPHVEIN_SEQUELIZE_CORPUS to the .corpus directory to run
// it.
func TestSequelizeCorpusDiscover(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_SEQUELIZE_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_SEQUELIZE_CORPUS is not set")
	}
	for _, corpus := range []string{"opencollective-api", "gladys"} {
		t.Run(corpus, func(t *testing.T) {
			want := corpusDiscover[corpus]
			in := corpusInput(t, filepath.Join(dir, corpus))
			pkgs, diags := Discover(in)
			for _, l := range diagLines(diags) {
				t.Log(l)
			}
			if len(pkgs) != 1 {
				t.Fatalf("%d packages, want 1", len(pkgs))
			}
			p := pkgs[0]
			if p.Dir != want.Dir || p.Manifest != want.Manifest || p.Spec != want.Spec || p.Lockfile != want.Lockfile || p.Refused != "" {
				t.Errorf("package %s %s %s %s refused %q", p.Dir, p.Manifest, p.Spec, p.Lockfile, p.Refused)
			}
			if !reflect.DeepEqual(p.Pins, want.Pins) || !reflect.DeepEqual(p.Drivers, want.Drivers) {
				t.Errorf("pins %v drivers %v", p.Pins, p.Drivers)
			}
			if !reflect.DeepEqual(p.Migrations, []string{want.Migrations}) || p.MigrationFiles[want.Migrations] != want.MigrationFiles {
				t.Errorf("migrations %v %v", p.Migrations, p.MigrationFiles)
			}
			if !hasDiag(diags, codeMigrations, want.Manifest, migrationsMarker+want.Migrations) {
				t.Error("no migrations marker")
			}
			if len(p.Instances) != 1 {
				t.Fatalf("%d instances, want 1", len(p.Instances))
			}
			g := p.Instances[0]
			if got := g.File + ":" + strconv.Itoa(g.Span.StartLine) + " " + g.Name; got != want.Instance || g.Root != g.File {
				t.Errorf("instance %s root %s", got, g.Root)
			}
			if got := g.Dialect + "/" + g.DialectFrom; got != want.Dialect || g.DialectReason != "" {
				t.Errorf("dialect %s (%s)", got, g.DialectReason)
			}
			o := g.Options
			define := -1
			if o.Define != nil {
				define = len(o.Define.Entries)
			}
			if !o.Known || o.From != want.From || o.Source != want.Source || len(o.Spread) != want.Spread || define != want.Define || !o.SchemaKnown || o.Schema != "" {
				t.Errorf("options %+v", o)
			}
			models := map[string]int{}
			for _, m := range g.Models {
				models[m.Form+"/"+m.Context]++
				if m.Form == "init" && (m.Class == nil || m.Class.Name != m.ClassName) {
					t.Errorf("%s:%d: init of %s without its class", m.File, m.Span.StartLine, m.ClassName)
				}
				if m.Factory != nil && (m.Factory.CallFile != g.File || m.Factory.Object != "models" || m.Factory.Key == "" || m.Binding == "") {
					t.Errorf("%s: factory %+v binding %q", m.File, m.Factory, m.Binding)
				}
			}
			if !reflect.DeepEqual(models, want.Models) {
				t.Errorf("models %v, want %v", models, want.Models)
			}
			assocs := map[string]int{}
			for _, a := range g.Associations {
				assocs[a.Context]++
				if a.Source == nil || a.Target == nil {
					t.Errorf("%s:%d: %s with source %v target %v", a.File, a.Span.StartLine, a.Method, a.Source != nil, a.Target != nil)
				}
			}
			if !reflect.DeepEqual(assocs, want.Associations) || !g.OrderProven {
				t.Errorf("associations %v (order proven %v), want %v", assocs, g.OrderProven, want.Associations)
			}
			if !reflect.DeepEqual(g.SyncSites, want.SyncSites) {
				t.Errorf("sync sites %v, want %v", g.SyncSites, want.SyncSites)
			}
			wrappers := 0
			for _, x := range g.Incomplete {
				if strings.Contains(x, "project wrapper Temporal") {
					wrappers++
				}
			}
			if wrappers != want.Wrappers || len(g.Incomplete) != want.Incomplete {
				t.Errorf("%d wrapper calls, %d incompleteness reasons: %v", wrappers, len(g.Incomplete), g.Incomplete)
			}
			if got := hasDiag(diags, codeUnread, want.Manifest, "in test files are not read"); got != want.TestSites {
				t.Errorf("test-file marker %v, want %v", got, want.TestSites)
			}
			for _, d := range diags {
				if d.Code == codeNotProven {
					t.Errorf("unexpected %s %s: %s", d.Code, d.File, d.Message)
				}
			}
		})
	}
}
