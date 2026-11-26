package typeorm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// invRow is one row of docs/contract/claims/data/typeorm-models-inventory.jsonl.
type invRow struct {
	Kind, Corpus, Row, File, Form, Status string
	Line                                  int
}

func inventory(t *testing.T) map[string][]invRow {
	t.Helper()
	f, err := os.Open("../../../../docs/contract/claims/data/typeorm-models-inventory.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][]invRow{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r invRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Kind == "discovery" {
			out[r.Corpus] = append(out[r.Corpus], r)
		}
	}
	return out
}

// corpusGroup is what discovery must find for one group of a corpus.
type corpusGroup struct {
	// Site is "<file>:<line>"; Form the group form; Read whether its
	// options are read; Entities the reached entity count; Naming,
	// Dialects and AutoLoad the scope results; Incomplete the number of
	// incompleteness reasons.
	Site, Form string
	Read       bool
	Entities   int
	Naming     string
	Dialects   []string
	AutoLoad   bool
	Incomplete int
	// Why is a substring of OptionsWhy for a group that is not read.
	Why string
}

// corpusWant is the discovery result of one corpus (the inventory's
// discovery rows, docs/contract/claims/data/typeorm-models-inventory.jsonl, and the T018g6
// coverage targets).
type corpusWant struct {
	Dir, Line string
	Pins      map[string]any
	Versions  map[string]string
	Drivers   []string
	Entities  int
	Features  int
	Groups    []corpusGroup
	// Config is the tsconfig nearest to the entity files and Flags the
	// compiler options their chain proves.
	Config string
	Flags  map[string]bool
}

var corpusDiscover = map[string]corpusWant{
	"seerr": {
		Dir: ".", Line: Line03,
		Pins:     map[string]any{"typeorm": "0.3.31", "typescript": "5.4.5"},
		Versions: map[string]string{"typeorm": "0.3.31", "typescript": "5.4.5"},
		Drivers:  []string{"pg", "sqlite3"}, Entities: 14,
		Groups: []corpusGroup{
			{Site: "server/datasource.ts:184", Form: "datasource", Read: true, Entities: 14, Naming: "default", Dialects: []string{"postgres", "sqlite"}},
		},
		Config: "server/tsconfig.json",
		Flags:  map[string]bool{"experimentalDecorators": true, "emitDecoratorMetadata": true, "strict": true},
	},
	"nest-admin": {
		Dir: ".", Line: Line03,
		Pins:     map[string]any{"typeorm": "0.3.22", "typescript": "5.8.3"},
		Versions: map[string]string{"typeorm": "0.3.22", "typescript": "5.8.3", "@nestjs/typeorm": "11.0.0"},
		Drivers:  []string{"mysql2"}, Entities: 15, Features: 12,
		Groups: []corpusGroup{
			// The CLI data source: its only entities element is a dist
			// glob (open point O10).
			{Site: "src/config/database.config.ts:37", Form: "datasource", Read: true, Entities: 0, Naming: "default", Dialects: []string{"mysql"}, Incomplete: 1},
			// The NestJS root: its useFactory holds a try statement and
			// its dataSourceFactory is an async block (T018g6 Revision 3
			// addendum 2, OA); its options come through TO-K5, its 15
			// entities through autoLoadEntities, and the dist glob
			// leaves its list incomplete (O10).
			{Site: "src/shared/database/database.module.ts:20", Form: "forRootAsync", Read: true, Entities: 15, Naming: "default", Dialects: []string{"mysql"}, AutoLoad: true, Incomplete: 1},
		},
		Config: "tsconfig.json",
		Flags:  map[string]bool{"experimentalDecorators": true, "emitDecoratorMetadata": true, "strictNullChecks": false},
	},
	"maintainerr": {
		Dir: "apps/server", Line: Line1,
		Pins:     map[string]any{"typeorm": "1.1.2", "typescript": "6.0.3"},
		Versions: map[string]string{"typeorm": "1.1.2", "typescript": "6.0.3", "@nestjs/typeorm": "12.0.3"},
		Drivers:  []string{"better-sqlite3"}, Entities: 17, Features: 10,
		Groups: []corpusGroup{
			{Site: "apps/server/src/datasource-config.ts:3", Form: "datasource", Read: true, Entities: 17, Naming: "default", Dialects: []string{"sqlite"}},
			{Site: "apps/server/src/app/app.module.ts:49", Form: "forRoot", Read: true, Entities: 17, Naming: "default", Dialects: []string{"sqlite"}, AutoLoad: true},
		},
		Config: "apps/server/tsconfig.json",
		Flags:  map[string]bool{"experimentalDecorators": true, "emitDecoratorMetadata": true, "strictNullChecks": false},
	},
	"aqualink-app": {
		Dir: "packages/api", Line: Line03,
		Pins:     map[string]any{"typeorm": "0.3.31", "typescript": "5.9.3"},
		Versions: map[string]string{"typeorm": "0.3.31", "typescript": "5.9.3", "@nestjs/typeorm": "11.0.1", "typeorm-naming-strategies": "4.1.0"},
		Drivers:  []string{"pg"}, Entities: 28, Features: 19,
		Groups: []corpusGroup{
			{Site: "packages/api/ormconfig.ts:50", Form: "datasource", Read: true, Entities: 27, Naming: "snake", Dialects: []string{"postgres"}},
			{Site: "packages/api/src/app.module.ts:40", Form: "forRoot", Why: "not an object literal"},
		},
		Config: "packages/api/tsconfig.json",
		Flags:  map[string]bool{"experimentalDecorators": true, "emitDecoratorMetadata": true, "strict": true, "strictNullChecks": true},
	},
}

// TestTypeORMCorpusDiscover runs discovery on the four pinned corpora
// (parse only, no indexing) and compares it with the inventory's
// discovery rows both ways: every row is found where it says (packages,
// versions and drivers, the tsconfig chain and its flags, data sources,
// option objects, the selector, the entities array, registerAs, the
// NestJS roots, forFeature calls, the naming strategy, the migrations
// option), and every data source, option object, selector, entities array
// and forFeature call discovery reports has a row at its line. The groups
// and their reached entities follow the coverage targets (T018g6 investigation,
// TA2 and TA3 sections). Set GRAPHVEIN_TYPEORM_CORPUS to a directory
// holding the four checkouts (scripts/cloud-corpus.sh seerr nest-admin
// maintainerr aqualink-app) to run it.
func TestTypeORMCorpusDiscover(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_TYPEORM_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_TYPEORM_CORPUS is not set")
	}
	rows := inventory(t)
	for _, corpus := range []string{"seerr", "nest-admin", "maintainerr", "aqualink-app"} {
		t.Run(corpus, func(t *testing.T) {
			want := corpusDiscover[corpus]
			in := corpusInput(t, filepath.Join(dir, corpus))
			d, pkgs := runDiscovery(in)
			for _, l := range describe(pkgs) {
				t.Log(l)
			}
			for _, l := range diagLines(d.diags) {
				t.Log(l)
			}
			if len(pkgs) != 1 {
				t.Fatalf("%d packages, want 1", len(pkgs))
			}
			p := pkgs[0]
			checkPackage(t, p, want)
			sites := map[string]bool{}
			for _, r := range rows[corpus] {
				checkRow(t, d, p, r)
				if r.Line > 0 {
					sites[fmt.Sprintf("%s:%d", r.File, r.Line)] = true
				}
			}
			// The other way: what discovery reports has a row.
			report := func(site, what string) {
				if !sites[site] {
					t.Errorf("%s %s has no inventory row", what, site)
				}
			}
			for _, g := range p.Groups {
				report(fmt.Sprintf("%s:%d", g.File, g.Span.StartLine), "group")
				for _, o := range g.Options {
					report(fmt.Sprintf("%s:%d", o.File, o.Span.StartLine), "options object")
				}
				if g.Selector != "" {
					report(g.Selector, "selector")
				}
				if g.EntitiesAt != "" && g.Options[0].Value("entities").Kind == "identifier" {
					report(g.EntitiesAt, "entities array")
				}
			}
			if corpus != "aqualink-app" {
				// aqualink's inventory counts its 19 calls in one row.
				for _, f := range p.Features {
					report(fmt.Sprintf("%s:%d", f.File, f.Span.StartLine), "forFeature call")
				}
			}
		})
	}
}

func checkPackage(t *testing.T, p *Package, want corpusWant) {
	t.Helper()
	if p.Dir != want.Dir || p.Line != want.Line || p.Refused != "" {
		t.Errorf("package %s line %s refused %q", p.Dir, p.Line, p.Refused)
	}
	if !reflect.DeepEqual(p.Pins, want.Pins) || !reflect.DeepEqual(p.Drivers, want.Drivers) {
		t.Errorf("pins %v drivers %v", p.Pins, p.Drivers)
	}
	for k, v := range want.Versions {
		if p.Versions[k] != v {
			t.Errorf("version of %s = %q, want %q", k, p.Versions[k], v)
		}
	}
	if len(p.Entities) != want.Entities || len(p.Features) != want.Features {
		t.Errorf("%d entity classes, %d forFeature calls; want %d, %d", len(p.Entities), len(p.Features), want.Entities, want.Features)
	}
	for _, f := range p.Features {
		if !f.NameKnown || f.Name != "default" || len(f.Unread) > 0 || len(f.Entities) == 0 {
			t.Errorf("forFeature %s:%d: %+v", f.File, f.Span.StartLine, f)
		}
	}
	for _, e := range p.Entities {
		if e.Flags.Config != want.Config || len(e.Flags.Unresolved) > 0 {
			t.Errorf("%s: tsconfig %s (unresolved %v), want %s", e.File, e.Flags.Config, e.Flags.Unresolved, want.Config)
		}
		for k, v := range want.Flags {
			if got := e.Flags.Values[k]; got == nil || *got != v {
				t.Errorf("%s: %s = %v, want %v", e.File, k, got, v)
			}
		}
	}
	if len(p.Groups) != len(want.Groups) {
		t.Fatalf("%d groups, want %d", len(p.Groups), len(want.Groups))
	}
	for i, wg := range want.Groups {
		g := p.Groups[i]
		site := fmt.Sprintf("%s:%d", g.File, g.Span.StartLine)
		if site != wg.Site || g.Form != wg.Form || g.Root != g.File || g.Name != "default" || !g.NameKnown {
			t.Errorf("group %d: %s %s root %s name %s(%v), want %s %s", i, site, g.Form, g.Root, g.Name, g.NameKnown, wg.Site, wg.Form)
		}
		if !wg.Read {
			if g.OptionsWhy == "" || !strings.Contains(g.OptionsWhy, wg.Why) {
				t.Errorf("group %s: options why %q, want it to name %q", site, g.OptionsWhy, wg.Why)
			}
			continue
		}
		if !g.Claimable() || len(g.Entities) != wg.Entities || g.Naming != wg.Naming || !reflect.DeepEqual(g.Dialects, wg.Dialects) || g.AutoLoad != wg.AutoLoad || len(g.Incomplete) != wg.Incomplete {
			t.Errorf("group %s: why %q refused %q entities %d naming %s dialects %v autoload %v incomplete %q", site, g.OptionsWhy, g.Refused, len(g.Entities), g.Naming, g.Dialects, g.AutoLoad, g.Incomplete)
		}
		if !g.HasMigrations || len(g.Migrations) == 0 {
			t.Errorf("group %s: no migrations", site)
		}
	}
}

var (
	lockRE    = regexp.MustCompile(`lock ([0-9][0-9.]*)`)
	versionRE = regexp.MustCompile(`^(?:dependency )?(\S+) [~^]?([0-9][0-9.]*)`)
)

// checkRow checks one inventory discovery row against discovery.
func checkRow(t *testing.T, d *discovery, p *Package, r invRow) {
	t.Helper()
	site := fmt.Sprintf("%s:%d", r.File, r.Line)
	fail := func(format string, a ...any) {
		t.Errorf("%s %s (%s): %s", r.Row, site, r.Form, fmt.Sprintf(format, a...))
	}
	base := path.Base(r.File)
	switch {
	case base == "package.json":
		if r.File != p.Manifest {
			fail("not the package manifest %s", p.Manifest)
		}
		if strings.HasPrefix(r.Form, "driver ") {
			name := strings.Fields(r.Form)[1]
			if !contains(p.Drivers, name) {
				fail("driver %s not recorded (%v)", name, p.Drivers)
			}
			return
		}
		m := versionRE.FindStringSubmatch(r.Form)
		if m == nil {
			fail("unparsed form")
			return
		}
		v := m[2]
		if l := lockRE.FindStringSubmatch(r.Form); l != nil {
			v = l[1]
		}
		if m[1] == "reflect-metadata" {
			var man manifest
			_ = json.Unmarshal(d.in.Sources[r.File], &man)
			if _, ok := man.spec(m[1]); !ok {
				fail("not declared")
			}
			return
		}
		if p.Versions[m[1]] != v {
			fail("locked %s = %q, want %q", m[1], p.Versions[m[1]], v)
		}
	case strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".yaml"):
		if pin := strings.TrimPrefix(r.Form, "pin typeorm@"); pin != p.Versions["typeorm"] || p.Lockfile != r.File {
			fail("lockfile %s version %s", p.Lockfile, p.Versions["typeorm"])
		}
	case strings.HasPrefix(base, "tsconfig"):
		// The chain's flags are checked on every entity file
		// (checkPackage); the row names the nearest config or one it
		// extends from a directory above.
		config := p.Entities[0].Flags.Config
		if r.File != config && !strings.HasPrefix(config, strings.TrimSuffix(r.File, base)) {
			fail("the entity files read %s", config)
		}
	case base == "nest-cli.json":
		// The swagger CLI plugin: no TypeORM metadata (TO-V3 note).
	case r.Row == "TO-P2":
		// Project helpers: the claim pass.
	case r.Row == "TO-U1":
		for _, g := range p.Groups {
			for _, m := range g.Migrations {
				if m.File == r.File && m.Line == r.Line {
					return
				}
			}
		}
		fail("no migrations element at this line")
	case r.Row == "TO-K4" && r.Line == 0:
		n := 0
		for _, f := range p.Features {
			if strings.HasPrefix(f.File, r.File+"/") {
				n++
			}
		}
		if !strings.HasPrefix(r.Form, fmt.Sprint(n)+" ") || hasDiag(d.diags, codeUnread, p.Manifest, "in test files") {
			fail("%d forFeature calls", n)
		}
	case r.Row == "TO-K4":
		for _, f := range p.Features {
			if f.File == r.File && f.Span.StartLine == r.Line {
				return
			}
		}
		fail("no forFeature call")
	case r.Row == "TO-K5":
		for _, regs := range d.registrations(p.Dir) {
			for _, reg := range regs {
				if reg.file == r.File && reg.line == r.Line && reg.why == "" && reg.opts != nil {
					return
				}
			}
		}
		fail("no readable registerAs")
	case r.Row == "TO-K6":
		for _, g := range p.Groups {
			if o, ok := optionsAt(g, r.File); ok && o.Value("namingStrategy") != nil && o.Value("namingStrategy").Span.StartLine == r.Line && g.Naming == "snake" {
				return
			}
		}
		fail("no SnakeNamingStrategy group")
	default:
		groupRow(p, r, fail)
	}
}

// groupRow checks a TO-K1 or TO-K3 row: a group, an option object, the
// selector or the entities array at the row's line. A group row's status
// says whether its options are read.
func groupRow(p *Package, r invRow, fail func(string, ...any)) {
	site := fmt.Sprintf("%s:%d", r.File, r.Line)
	for _, g := range p.Groups {
		switch {
		case g.File == r.File && g.Span.StartLine == r.Line:
			read := r.Status == "read" || r.Status == "proposed"
			if read != (g.OptionsWhy == "") {
				fail("options why %q, status %s", g.OptionsWhy, r.Status)
			}
			return
		case g.Selector == site || g.EntitiesAt == site:
			return
		}
		for _, o := range g.Options {
			if o.File == r.File && o.Span.StartLine == r.Line {
				return
			}
		}
	}
	fail("nothing discovered at this line")
}

// optionsAt returns the group's first options object written in file.
func optionsAt(g *Group, file string) (*Options, bool) {
	for _, o := range g.Options {
		if o.File == file {
			return o, true
		}
	}
	return nil, false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// describe renders discovery's result for the test log.
func describe(pkgs []*Package) []string {
	var out []string
	for _, p := range pkgs {
		out = append(out, fmt.Sprintf("package %s spec %s line %s versions %v pins %v lock %s refused %q drivers %v entities %d features %d", p.Dir, p.Spec, p.Line, p.Versions, p.Pins, p.Lockfile, p.Refused, p.Drivers, len(p.Entities), len(p.Features)))
		for _, g := range p.Groups {
			var names []string
			for _, e := range g.Entities {
				names = append(names, e.Name)
			}
			var from []string
			for _, o := range g.Options {
				from = append(from, fmt.Sprintf("%s:%d/%s", o.File, o.Span.StartLine, o.From))
			}
			var migs []string
			for _, m := range g.Migrations {
				migs = append(migs, fmt.Sprintf("%s:%d %q dir %s files %d %s", m.File, m.Line, m.Pattern, m.Dir, m.Files, m.Why))
			}
			out = append(out, fmt.Sprintf("  group %s form %s name %s(%v) options %v why %q selector %s entities at %s types %v dialects %v %v failures %v refused %q naming %s prefix %q schema %q autoload %v entities %d %v incomplete %q migrations %q sync %v run %v",
				g.Root, g.Form, g.Name, g.NameKnown, from, g.OptionsWhy, g.Selector, g.EntitiesAt, g.Types, g.Dialects, g.Verification, g.Failures, g.Refused, g.Naming, g.EntityPrefix, g.Schema, g.AutoLoad, len(g.Entities), strings.Join(names, ","), g.Incomplete, migs, g.Synchronize, g.MigrationsRun))
		}
	}
	return out
}
