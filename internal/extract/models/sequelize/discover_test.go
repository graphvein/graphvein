package sequelize

import (
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

const lockV3 = `{"lockfileVersion": 3, "packages": {
  "": {"dependencies": {"sequelize": "^6.37.0"}},
  "node_modules/sequelize": {"version": "6.37.8"},
  "node_modules/sequelize/node_modules/inflection": {"version": "1.13.4"},
  "node_modules/inflection": {"version": "3.0.0"}
}}`

// esm is an opencollective-api shaped package: a TypeScript instance module
// with literal options, class models with one init each that import the
// instance (one through a ModelWithPublicId-style base class), and an
// index module with module-level association calls.
var esm = map[string]string{
	"package.json":      `{"dependencies": {"sequelize": "^6.37.0", "pg": "8.0.0"}}`,
	"package-lock.json": lockV3,
	"src/lib/sequelize.ts": `import { Sequelize, DataTypes } from 'sequelize';
const sequelize = new Sequelize('db', 'user', 'pw', { dialect: 'postgres', define: { underscored: true }, schema: 'app' });
export { DataTypes };
export default sequelize;
`,
	"src/models/Base.ts": `import { Model } from 'sequelize';
export class Base extends Model {}
`,
	"src/models/User.ts": `import { Model } from 'sequelize';
import sequelize, { DataTypes } from '../lib/sequelize';
class User extends Model {}
User.init({ name: DataTypes.STRING }, { sequelize, tableName: 'users' });
export default User;
`,
	"src/models/Post.ts": `import sequelize from '../lib/sequelize';
import { Base } from './Base';
export class Post extends Base {}
Post.init({ title: { type: 'x' } }, { sequelize: sequelize });
`,
	"src/models/index.ts": `import type { ThroughOptions } from 'sequelize';
import User from './User';
import { Post } from './Post';
User.hasMany(Post, { foreignKey: 'UserId' });
Post.belongsTo(User);
export { User, Post };
`,
	"migrations/20200101-init.js": `module.exports = { up() {} };`,
}

// cjs is a Gladys shaped package: CommonJS factories required by an index
// module that builds the instance from an environment-indexed options
// object (open point O2) and runs the recognised associate loop.
var cjs = map[string]string{
	"server/package.json":      `{"dependencies": {"sequelize": "^6.29.0", "sqlite3": "^5.1.2"}}`,
	"server/package-lock.json": lockV3,
	"server/config/config.js": `module.exports = {
  development: { dialect: 'sqlite', storage: 'a.db', define: { underscored: true, freezeTableName: true } },
  production: { dialect: 'sqlite', storage: process.env.X, define: { underscored: true, freezeTableName: true } },
};
`,
	"server/utils/getConfig.js": `const config = require('../config/config');
module.exports = () => {
  const env = process.env.NODE_ENV || 'development';
  return config[env];
};
`,
	"server/models/area.js": `module.exports = (sequelize, DataTypes) => {
  const area = sequelize.define('t_area', { name: DataTypes.STRING });
  area.associate = (models) => {
    area.belongsTo(models.House, { foreignKey: 'house_id' });
    area.hasMany(models.House);
  };
  return area;
};
`,
	"server/models/house.js": `module.exports = (sequelize, DataTypes) => {
  const house = sequelize.define('t_house', { name: DataTypes.STRING });
  return house;
};
`,
	"server/models/index.js": `const Sequelize = require('sequelize');
const getConfig = require('../utils/getConfig');
const AreaModel = require('./area');
const HouseModel = require('./house');
const config = getConfig();
const sequelize = new Sequelize('mainDB', null, null, config);
const models = {
  Area: AreaModel(sequelize, Sequelize),
  House: HouseModel(sequelize, Sequelize),
};
Object.values(models)
  .filter((model) => typeof model.associate === 'function')
  .forEach((model) => model.associate(models));
module.exports = { sequelize, models };
`,
	"server/migrations/20190205-create-user.js": `module.exports = { up() {} };`,
}

func merge(base map[string]string, over map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if v == "" {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

func discover(t *testing.T, sources map[string]string, tags *modeltest.Tags) ([]*Package, []string) {
	t.Helper()
	in := input(t, sources)
	if tags != nil {
		in.Tag = tags.Tag
	}
	pkgs, diags := Discover(in)
	return pkgs, diagLines(diags)
}

func only(t *testing.T, pkgs []*Package) *Instance {
	t.Helper()
	if len(pkgs) != 1 || len(pkgs[0].Instances) != 1 {
		t.Fatalf("want one package with one instance, got %d packages", len(pkgs))
	}
	return pkgs[0].Instances[0]
}

func hasLine(lines []string, parts ...string) bool {
	for _, l := range lines {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(l, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// TestDiscoverESM covers the ESM path: pins (inflection resolved from the
// installed sequelize directory first), the single driver, the instance
// with literal dialect, define and schema, init models that import the
// instance (default import, shorthand and property option), module-level
// associations with resolved source and target, and the migrations marker.
func TestDiscoverESM(t *testing.T) {
	tags := modeltest.NewTags()
	pkgs, diags := discover(t, esm, tags)
	g := only(t, pkgs)
	p := pkgs[0]
	if !reflect.DeepEqual(p.Pins, map[string]any{"sequelize": "6.37.8", "inflection": "1.13.4"}) || !reflect.DeepEqual(p.Drivers, []string{"pg"}) || p.Refused != "" {
		t.Errorf("package %+v", p)
	}
	if g.File != "src/lib/sequelize.ts" || g.Name != "sequelize" || g.Root != g.File || g.SetKey() != "models/sequelize/src/lib/sequelize.ts" {
		t.Errorf("instance %s %s %s", g.File, g.Name, g.Root)
	}
	if g.Dialect != "postgres" || g.DialectFrom != "literal" || !g.Options.Known || g.Options.Define == nil || g.Options.Schema != "app" || g.Options.From != "literal" {
		t.Errorf("dialect %s/%s options %+v", g.Dialect, g.DialectFrom, g.Options)
	}
	var names []string
	for _, m := range g.Models {
		names = append(names, m.Form+":"+m.ClassName)
		if m.Class == nil {
			t.Errorf("%s: no class", m.ClassName)
		}
	}
	if !reflect.DeepEqual(names, []string{"init:Post", "init:User"}) {
		t.Errorf("models %v", names)
	}
	if len(g.Associations) != 2 || !g.OrderProven {
		t.Fatalf("associations %d order %v", len(g.Associations), g.OrderProven)
	}
	a, b := g.Associations[0], g.Associations[1]
	if a.Method != "hasMany" || a.Source.ClassName != "User" || a.Target.ClassName != "Post" || b.Method != "belongsTo" || b.Source.ClassName != "Post" || b.Target.ClassName != "User" {
		t.Errorf("associations %+v %+v", a, b)
	}
	if !hasLine(diags, "schema_model_migrations package.json", migrationsMarker+"migrations") || len(diags) != 1 {
		t.Errorf("diagnostics %v", diags)
	}
	if len(g.Incomplete) != 0 {
		t.Errorf("incomplete %v", g.Incomplete)
	}
	for _, f := range []string{"package dependency", "lockfile pins", "`new Sequelize(...)`", "literal `dialect`", "`define`", "`schema`", "imported instance binding", "module level"} {
		if !tags.Forms["SQ-K1/"+f] {
			t.Errorf("form %s not tagged", f)
		}
	}
	if !tags.Rows["SQ-U1"] {
		t.Error("SQ-U1 not tagged")
	}
}

// TestDiscoverCommonJS covers the Gladys path: the relative requires, open
// point O2's environment selector (entries agreeing on dialect and define),
// factories called with the instance as values of the models object, and
// the associate functions the recognised loop runs in key order.
func TestDiscoverCommonJS(t *testing.T) {
	tags := modeltest.NewTags()
	pkgs, diags := discover(t, cjs, tags)
	g := only(t, pkgs)
	if g.File != "server/models/index.js" || g.Dialect != "sqlite" || g.DialectFrom != "literal" || g.Options.From != "environment" || g.Options.Source != "server/config/config.js:1" || g.Options.Define == nil {
		t.Errorf("instance %s dialect %s/%s options %+v", g.File, g.Dialect, g.DialectFrom, g.Options)
	}
	var keys []string
	for _, m := range g.Models {
		keys = append(keys, m.Factory.Key+"="+m.Binding+"@"+m.File)
		if m.Context != "factory" || m.Factory.Object != "models" || !reflect.DeepEqual(m.Factory.Params, []string{"sequelize", "DataTypes"}) {
			t.Errorf("model %+v", m)
		}
	}
	if !reflect.DeepEqual(keys, []string{"Area=area@server/models/area.js", "House=house@server/models/house.js"}) {
		t.Errorf("models %v", keys)
	}
	if len(g.Associations) != 2 || g.Associations[0].Context != "associate" || g.Associations[0].Source.Binding != "area" || g.Associations[0].Target.Binding != "house" || g.Associations[1].Method != "hasMany" {
		t.Errorf("associations %+v", g.Associations)
	}
	if len(g.Incomplete) != 0 || len(diags) != 1 {
		t.Errorf("incomplete %v diagnostics %v", g.Incomplete, diags)
	}
	for _, f := range []string{"factory's first parameter", "`associate(models)` loop", "literal `dialect`", "`define`"} {
		if !tags.Forms["SQ-K1/"+f] {
			t.Errorf("form %s not tagged", f)
		}
	}
}

// TestDiscoverOptions covers the options and dialect rules: open point
// O1's configuration spread (read when no node-config file sets define
// under the path; JSON with comments; any other file naming a key), open
// point O2's disagreeing entries, the URL form, the driver rule (none,
// several, dialectModule), unsupported dialects and non-literal values.
func TestDiscoverOptions(t *testing.T) {
	instance := func(opts string) map[string]string {
		// node-config is a declared dependency, as in opencollective-api
		// (row TS-I5: an import is the package only when declared).
		return merge(esm, map[string]string{"package.json": `{"dependencies": {"sequelize": "^6.37.0", "pg": "8.0.0", "config": "3.3.0"}}`, "src/lib/sequelize.ts": `import config from 'config';
import { Sequelize } from 'sequelize';
const sequelize = new Sequelize(` + opts + `);
export default sequelize;
`})
	}
	cases := []struct {
		name    string
		sources map[string]string
		known   bool
		dialect string
		from    string
		reason  string
	}{
		{"spread read under SQ-V5", merge(instance(`'db', 'u', 'p', { dialect: 'postgres', ...config.database.options })`), map[string]string{
			"config/default.json":    "{\n  // comment\n  \"database\": {\"options\": {\"pool\": {}}}, \"x\": \"a // b\"\n}",
			"config/production.json": `{"database": {"url": "x"}}`,
		}), true, "postgres", "literal", ""},
		{"spread with define", merge(instance(`'db', 'u', 'p', { dialect: 'postgres', ...config.database.options })`), map[string]string{
			"config/default.json": `{"database": {"options": {"define": {"underscored": true}}}}`,
		}), false, "postgres", "driver", "may set define"},
		{"spread overriding the dialect", merge(instance(`'db', 'u', 'p', { dialect: 'postgres', ...config.database.options })`), map[string]string{
			"config/default.json": `{"database": {"options": {"dialect": "mysql"}}}`,
		}), true, "postgres", "driver", ""},
		{"spread with a non-JSON config naming define", merge(instance(`'db', 'u', 'p', { ...config.database.options })`), map[string]string{
			"config/default.js": `module.exports = { database: { options: { define: {} } } };`,
		}), false, "postgres", "driver", "may set define"},
		{"spread of another object", instance(`'db', 'u', 'p', { ...other.options })`), false, "postgres", "driver", "not the node-config object"},
		{"non-literal define", instance(`'db', 'u', 'p', { dialect: 'postgres', define: defaults })`), false, "postgres", "driver", "define option is not an object literal"},
		{"three arguments: no options", instance(`'db', 'u', 'p'`), true, "postgres", "driver", ""},
		{"URL form overwrites the dialect", instance(`'sqlite::memory:', { dialect: 'postgres' })`), true, "postgres", "driver", ""},
		{"no driver", merge(instance(`'db', 'u', 'p'`), map[string]string{"package.json": `{"dependencies": {"sequelize": "6.37.8"}}`}), true, "", "", "no Sequelize driver"},
		{"several drivers", merge(instance(`'db', 'u', 'p'`), map[string]string{"package.json": `{"dependencies": {"sequelize": "6.37.8", "pg": "8", "sqlite3": "5"}}`}), true, "", "", "several Sequelize drivers (pg, sqlite3)"},
		{"dialectModule", instance(`'db', 'u', 'p', { dialectModule: pg })`), true, "", "", "dialectModule"},
		{"unsupported dialect", instance(`'db', 'u', 'p', { dialect: 'snowflake' })`), true, "snowflake", "literal", "row D"},
		{"postgresql alias", instance(`'db', 'u', 'p', { dialect: 'postgresql' })`), true, "postgres", "literal", ""},
		{"options with a computed key", instance(`'db', 'u', 'p', { [k]: v })`), false, "postgres", "driver", "does not read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pkgs, diags := discover(t, c.sources, nil)
			g := only(t, pkgs)
			why := g.Options.Reason + g.DialectReason
			if g.Options.Known != c.known || g.Dialect != c.dialect || g.DialectFrom != c.from || (c.reason == "") != (why == "") || !strings.Contains(why, c.reason) {
				t.Errorf("known %v dialect %q/%q reason %q; diagnostics %v", g.Options.Known, g.Dialect, g.DialectFrom, why, diags)
			}
		})
	}
	t.Run("O2 entries disagree", func(t *testing.T) {
		pkgs, diags := discover(t, merge(cjs, map[string]string{"server/config/config.js": `module.exports = {
  development: { dialect: 'sqlite', define: { underscored: true } },
  production: { dialect: 'sqlite', define: { underscored: false } },
};
`}), nil)
		g := only(t, pkgs)
		if g.Options.Known || !strings.Contains(g.Options.Reason, "disagree on define") || !hasLine(diags, "schema_model_not_proven server/models/index.js") {
			t.Errorf("options %+v diagnostics %v", g.Options, diags)
		}
		// The driver rule still gives the dialect.
		if g.Dialect != "sqlite" || g.DialectFrom != "driver" {
			t.Errorf("dialect %s/%s", g.Dialect, g.DialectFrom)
		}
	})
	t.Run("O2 selector with a side effect", func(t *testing.T) {
		pkgs, _ := discover(t, merge(cjs, map[string]string{"server/utils/getConfig.js": `const config = require('../config/config');
module.exports = () => {
  config.development.define = {};
  return config[process.env.NODE_ENV];
};
`}), nil)
		if g := only(t, pkgs); g.Options.Known {
			t.Errorf("options %+v", g.Options)
		}
	})
}

// TestDiscoverPackages covers the package rule: another major is refused,
// a range without a lockfile keeps claims without pins, a pnpm lockfile
// proves sequelize but not the transitive inflection, sequelize-typescript
// and .sequelizerc are markers, and a package without an instance says so.
func TestDiscoverPackages(t *testing.T) {
	pkgs, diags := discover(t, merge(esm, map[string]string{"package-lock.json": `{"lockfileVersion": 3, "packages": {"node_modules/sequelize": {"version": "7.0.0-alpha.9"}}}`}), nil)
	if pkgs[0].Refused == "" || len(pkgs[0].Instances) != 0 || !hasLine(diags, "schema_model_not_proven package.json", "sequelize 7.0.0-alpha.9") {
		t.Errorf("major 7: %+v %v", pkgs[0], diags)
	}
	pkgs, _ = discover(t, merge(esm, map[string]string{"package-lock.json": ""}), nil)
	if p := pkgs[0]; p.Refused != "" || p.Pins != nil || len(p.Instances) != 1 {
		t.Errorf("no lockfile: %+v", p)
	}
	pkgs, _ = discover(t, merge(esm, map[string]string{"package-lock.json": "", "pnpm-lock.yaml": "lockfileVersion: '9.0'\nimporters:\n  .:\n    dependencies:\n      sequelize:\n        specifier: ^6.37.0\n        version: 6.37.8\n"}), nil)
	if p := pkgs[0]; p.Versions["sequelize"] != "6.37.8" || p.Pins != nil {
		t.Errorf("pnpm: %+v", p)
	}
	_, diags = discover(t, merge(esm, map[string]string{
		"package.json": `{"dependencies": {"sequelize": "^6.37.0", "pg": "8.0.0", "sequelize-typescript": "2.1.6"}}`,
		".sequelizerc": `module.exports = { 'migrations-path': 'db/migrations' };`,
	}), nil)
	if !hasLine(diags, "schema_model_unread package.json", "sequelize-typescript") || !hasLine(diags, "schema_model_unread .sequelizerc", "migrations-path") {
		t.Errorf("markers %v", diags)
	}
	pkgs, diags = discover(t, map[string]string{"package.json": `{"devDependencies": {"sequelize": "6.37.8"}}`, "a.js": `const x = 1;`}, nil)
	if len(pkgs) != 1 || !hasLine(diags, "schema_model_not_proven package.json", "no module-level `new Sequelize(...)`") {
		t.Errorf("no instance: %v", diags)
	}
}

// TestDiscoverUnreached covers the markers for definitions and calls the
// reach rule does not read, each of which makes the group incomplete, and
// the refusal of a model that reaches two instances.
func TestDiscoverUnreached(t *testing.T) {
	sources := merge(esm, map[string]string{
		"src/models/Orphan.ts": `import { Model } from 'sequelize';
import { other } from './other';
class Orphan extends Model {}
Orphan.init({}, { sequelize: other });
class NoInstance extends Model {}
NoInstance.init({}, { tableName: 'x' });
`,
		"src/models/other.ts": `import { Sequelize } from 'sequelize';
export let other = new Sequelize('sqlite::memory:');
`,
		"src/models/loader.js": `const fs = require('fs');
const Sequelize = require('sequelize');
fs.readdirSync(__dirname).forEach((file) => { require(file); });
`,
		"src/models/factory.js": `module.exports = (sequelize) => { const x = sequelize.define('never', {}); return x; };
`,
		"src/lib/temporal.ts": `import { Sequelize } from 'sequelize';
const Temporal = (model, sequelize) => { sequelize.define(model.name + 'History', {}); return model; };
export default Temporal;
`,
		"src/models/Audit.ts": `import { Model } from 'sequelize';
import sequelize from '../lib/sequelize';
import Temporal from '../lib/temporal';
class Audit extends Model {}
Audit.init({}, { sequelize });
Temporal(Audit, sequelize);
`,
		"src/jobs/sync.ts": `import User from '../models/User';
import { Post } from '../models/Post';
export function later() { User.hasOne(Post); }
`,
		"test/models.test.ts": `import User from '../src/models/User';
import { Post } from '../src/models/Post';
it('x', () => { User.belongsToMany(Post, { through: 'x' }); });
`,
	})
	pkgs, diags := discover(t, sources, nil)
	g := only(t, pkgs)
	for _, want := range [][]string{
		{"schema_model_unread src/models/other.ts", "declared with let"},
		{"schema_model_unread src/models/Orphan.ts", ":4: the init's sequelize option does not resolve"},
		{"schema_model_unread src/models/Orphan.ts", ":6: init without a readable sequelize option"},
		{"schema_model_unread src/models/loader.js", "directory loader of src/models/"},
		{"schema_model_unread src/models/factory.js", "a factory define that no module-level call runs"},
		{"schema_model_unread src/lib/temporal.ts", ":2: sequelize.define(...) inside a function"},
		{"schema_model_unread src/models/Audit.ts", ":6: Temporal(Audit, ...) calls the project wrapper Temporal (src/lib/temporal.ts:2)"},
		{"schema_model_unread src/jobs/sync.ts", ":3: User.hasOne(...) outside the reach rule"},
		{"schema_model_unread package.json", "1 model definition or association call site(s) in test files"},
	} {
		if !hasLine(diags, want...) {
			t.Errorf("no diagnostic %v", want)
		}
	}
	if len(g.Incomplete) != 7 {
		t.Errorf("incomplete %d: %v", len(g.Incomplete), g.Incomplete)
	}
	var names []string
	for _, m := range g.Models {
		names = append(names, m.ClassName)
	}
	if !reflect.DeepEqual(names, []string{"Audit", "Post", "User"}) {
		t.Errorf("models %v", names)
	}

	// Two instances in one file: one group each, roots name the binding;
	// a define whose receiver alternates is not claimed by either.
	two := map[string]string{
		"package.json": `{"dependencies": {"sequelize": "6.37.8", "pg": "8"}}`,
		"db.js": `const { Sequelize } = require('sequelize');
const a = new Sequelize('db', 'u', 'p', { dialect: 'postgres' });
const b = new Sequelize('db', 'u', 'p', { dialect: 'sqlite' });
const A = a.define('ta', {});
const B = b.define('tb', {});
module.exports = { a, b };
`,
		"f.js": `module.exports = (s) => { const m = s.define('shared', {}); return m; };
`,
		"index.js": `require('sequelize');
const { a, b } = require('./db');
const F = require('./f');
const models = { X: F(a), Y: F(b) };
`,
	}
	pkgs, diags = discover(t, two, nil)
	if len(pkgs[0].Instances) != 2 {
		t.Fatalf("instances %d", len(pkgs[0].Instances))
	}
	for i, root := range []string{"db.js#a", "db.js#b"} {
		g := pkgs[0].Instances[i]
		if g.Root != root || len(g.Models) != 1 || g.Models[0].Context != "module" {
			t.Errorf("%s: root %s models %d", root, g.Root, len(g.Models))
		}
	}
	if !hasLine(diags, "schema_model_unread f.js", "reaches 2 Sequelize instances (db.js#a, db.js#b)") {
		t.Errorf("diagnostics %v", diags)
	}
}

// TestDiscoverAssociateReach covers associate functions: without the
// recognised loop they are markers; a statement in them that is not an
// association call is a marker; association calls of two modules leave
// the cross-module order unproven.
func TestDiscoverAssociateReach(t *testing.T) {
	noLoop := merge(cjs, map[string]string{"server/models/index.js": strings.Replace(cjs["server/models/index.js"], "typeof model.associate === 'function'", "model.associate", 1)})
	pkgs, diags := discover(t, noLoop, nil)
	g := only(t, pkgs)
	if len(g.Associations) != 0 || !hasLine(diags, "schema_model_unread server/models/area.js", "no recognised associate(models) loop runs") || len(g.Incomplete) != 3 {
		t.Errorf("associations %d incomplete %v diagnostics %v", len(g.Associations), g.Incomplete, diags)
	}
	extra := merge(cjs, map[string]string{"server/models/area.js": strings.Replace(cjs["server/models/area.js"], "area.hasMany(models.House);", "if (x) { area.hasMany(models.House); }", 1)})
	pkgs, diags = discover(t, extra, nil)
	g = only(t, pkgs)
	if len(g.Associations) != 1 || !hasLine(diags, "schema_model_unread server/models/area.js", "not an association call") {
		t.Errorf("associations %d diagnostics %v", len(g.Associations), diags)
	}
	twoModules := merge(esm, map[string]string{"src/models/User.ts": esm["src/models/User.ts"] + "import { Post } from './Post';\nUser.hasOne(Post);\n"})
	pkgs, _ = discover(t, twoModules, nil)
	if g := only(t, pkgs); len(g.Associations) != 3 || g.OrderProven {
		t.Errorf("associations %d order proven %v", len(g.Associations), g.OrderProven)
	}
}

// TestResolveRequire pins Node's CommonJS order: the file as written, then
// .js, .json, .node, then the directory's index; a directory package and a
// TypeScript-only match are not followed.
func TestResolveRequire(t *testing.T) {
	d := &discovery{in: Input{Sources: map[string][]byte{
		"a/b.js": nil, "a/b/index.js": nil, "a/c/index.js": nil, "a/d/package.json": nil, "a/d/index.js": nil, "a/e.ts": nil, "a/f.json": nil,
	}}}
	for spec, want := range map[string]string{"./b": "a/b.js", "./c": "a/c/index.js", "./d": "", "./e": "", "./f": "a/f.json", "sequelize": "", "../../x": ""} {
		if got, _ := d.resolveRequire("a/index.js", spec); got != want {
			t.Errorf("%s: %q, want %q", spec, got, want)
		}
	}
}

// TestDiscoverTags checks discovery's half of the tags: over the ESM, the
// CommonJS and the O1 sources every SQ-K1 form and SQ-U1 are reached, and
// every tag discovery emits is a contract row with a branch and, for a
// form, one the row lists. The claim rows' tags come with SQB3 and SQB4.
func TestDiscoverTags(t *testing.T) {
	tags := modeltest.NewTags()
	discover(t, esm, tags)
	discover(t, cjs, tags)
	discover(t, merge(esm, map[string]string{
		"package.json": `{"dependencies": {"sequelize": "^6.37.0", "pg": "8.0.0", "config": "3.3.0"}}`,
		"src/lib/sequelize.ts": `import config from 'config';
import { Sequelize } from 'sequelize';
const sequelize = new Sequelize('db', 'u', 'p', { ...config.database.options });
export default sequelize;
`,
		"config/default.json": `{"database": {"options": {}}}`,
	}), tags)
	// The sequelize-cli forms of the fixture's cli and
	// cliclass packages.
	cli := map[string]string{}
	for p, src := range fixtureSources(t) {
		if strings.HasPrefix(p, "cli/") || strings.HasPrefix(p, "cliclass/") {
			cli[p] = src
		}
	}
	discover(t, cli, tags)
	rows := map[string]Row{}
	for _, r := range Contract {
		rows[r.ID] = r
	}
	for row := range tags.Rows {
		r, ok := rows[row]
		if !ok || r.Side == SideNone {
			t.Errorf("tag %s is not a contract row with a branch", row)
		}
	}
	for form := range tags.Forms {
		row, name, _ := strings.Cut(form, "/")
		listed := false
		for _, f := range rows[row].Forms {
			listed = listed || f == name
		}
		if !listed {
			t.Errorf("form tag %s is not listed in row %s", form, row)
		}
	}
	for _, f := range ScopeForms {
		if !tags.Forms["SQ-K1/"+f] {
			t.Errorf("form SQ-K1/%s is never tagged", f)
		}
	}
	if !tags.Rows["SQ-U1"] {
		t.Error("SQ-U1 is never tagged")
	}
}

// TestDiscoverPackageShadow covers the S3 review's B2 case: a package.json
// of the repository named sequelize that is not a declared workspace
// member may be what `sequelize` installs (a `file:` dependency), so the
// resolver refuses the import (unlinked_package) and neither the ESM
// import nor the CommonJS require is read as the package: no instance.
func TestDiscoverPackageShadow(t *testing.T) {
	for name, sources := range map[string]map[string]string{"esm": esm, "cjs": cjs} {
		t.Run(name, func(t *testing.T) {
			pkgs, _ := discover(t, merge(sources, map[string]string{"vendor/sequelize/package.json": `{"name": "sequelize", "main": "index.js"}`}), nil)
			for _, p := range pkgs {
				if len(p.Instances) != 0 {
					t.Errorf("%d instances read through a shadowed sequelize import", len(p.Instances))
				}
			}
		})
	}
}

// TestDiscoverUnreadConfig covers the S3L rule for unread options: a
// tsconfig.json extending a package outside the repository leaves the
// files' options unread (row TS-C1), so a bare import claims no import
// edge (config_unread); `sequelize` is still the package for discovery
// because package.json declares it (row TS-I5, rule (ii)), for the ESM
// import and the CommonJS require alike. A package.json of the repository
// named sequelize still refuses it.
func TestDiscoverUnreadConfig(t *testing.T) {
	unread := `{"extends": "@tsconfig/node20/tsconfig.json"}`
	for name, sources := range map[string]map[string]string{
		"esm": merge(esm, map[string]string{"tsconfig.json": unread}),
		"cjs": merge(cjs, map[string]string{"server/tsconfig.json": `{"extends": "@tsconfig/node20/tsconfig.json", "compilerOptions": {"allowJs": true}}`}),
	} {
		t.Run(name, func(t *testing.T) {
			in := input(t, sources)
			for file, module := range map[string]string{"src/models/User.ts": "sequelize", "server/models/index.js": "sequelize"} {
				if _, ok := sources[file]; !ok {
					continue
				}
				if res := in.Resolver.Resolve(file, module); res.External || res.Refusal != "config_unread" || !res.Package() {
					t.Fatalf("%s: resolution %+v, want config_unread read as the declared package", file, res)
				}
			}
			g := only(t, mustDiscover(t, sources))
			if len(g.Models) != 2 || len(g.Associations) != 2 {
				t.Errorf("models %d associations %d", len(g.Models), len(g.Associations))
			}
		})
		t.Run(name+"/shadow", func(t *testing.T) {
			pkgs := mustDiscover(t, merge(sources, map[string]string{"vendor/sequelize/package.json": `{"name": "sequelize", "main": "index.js"}`}))
			for _, p := range pkgs {
				if len(p.Instances) != 0 {
					t.Errorf("%d instances read through a shadowed sequelize import", len(p.Instances))
				}
			}
		})
	}
}

func mustDiscover(t *testing.T, sources map[string]string) []*Package {
	t.Helper()
	pkgs, _ := discover(t, sources, nil)
	return pkgs
}
