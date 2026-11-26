package sequelize

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// This file covers the forms of task Revision 3 (the independent review's
// findings) beyond the fixture's one case per finding: the variants of
// the sequelize-cli loop and options (R3-C4), hooks on Sequelize's class
// (R3-B1), keys that are not unique on sqlite (R3-B2), ENUM defaults on
// sqlite (R3-B3) and association calls whose order to a load-time throw
// is not proven (R3-C1).

func fkJSON(n *contract.Node) string {
	if n == nil {
		return "<no column>"
	}
	raw, _ := json.Marshal(n.Attributes["foreign_keys"])
	return string(raw)
}

// TestAssociateLoopForms runs A.associate through each recognised loop
// form; an unguarded loop and a models object changed before the loop are
// not read.
func TestAssociateLoopForms(t *testing.T) {
	head := "const A = sequelize.define('A', { x: DataTypes.STRING });\nconst B = sequelize.define('B', { y: DataTypes.STRING });\nA.associate = (m) => { A.hasMany(m.B); };\nconst db = { A, B };\n"
	want := `[{"column":"id","columns":["id"],"local_columns":["AId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"As"}]`
	for name, loop := range map[string]string{
		"keys forEach &&":     "Object.keys(db).forEach((k) => db[k].associate && db[k].associate(db));",
		"values forEach if":   "Object.values(db).forEach((m) => { if (typeof m.associate === 'function') m.associate(db); });",
		"for of keys if":      "for (const k of Object.keys(db)) { if (db[k].associate) { db[k].associate(db); } }",
		"for of values &&":    "for (const m of Object.values(db)) m.associate && m.associate(db);",
		"filter (unchanged)":  "Object.values(db).filter((m) => typeof m.associate === 'function').forEach((m) => m.associate(db));",
		"entry after the obj": "db.C = sequelize;\nObject.keys(db).forEach((k) => { if (db[k].associate) db[k].associate(db); });",
	} {
		t.Run(name, func(t *testing.T) {
			nodes, _ := linkOne(t, "postgres", "", head+loop)
			if got := fkJSON(column(nodes, "#B.AId")); got != want {
				t.Errorf("B.AId: %s", got)
			}
		})
	}
	for name, loop := range map[string]string{
		"unguarded":           "Object.keys(db).forEach((k) => db[k].associate(db));",
		"computed assignment": "const k = 'Z';\ndb[k] = A;\nObject.keys(db).forEach((n) => db[n].associate && db[n].associate(db));",
	} {
		t.Run(name, func(t *testing.T) {
			nodes, _ := linkOne(t, "postgres", "", head+loop)
			if c := column(nodes, "#B.AId"); c != nil && c.Attributes["state"] == "read" {
				t.Error("B.AId claimed from a loop that is not read")
			}
			if b := tableNamed(nodes, "Bs"); b == nil || b.Attributes["column_set"] != "partial" {
				t.Errorf("Bs: %+v", b)
			}
		})
	}
}

// TestStaticAssociate reads a class's own static associate and refuses one
// inherited from a base class (its `this` is the subclass).
func TestStaticAssociate(t *testing.T) {
	own := "const { Model } = require('sequelize');\nclass A extends Model { static associate(m) { A.hasMany(m.B); } }\nA.init({ x: DataTypes.STRING }, { sequelize });\nclass B extends Model {}\nB.init({ y: DataTypes.STRING }, { sequelize });\nconst db = { A, B };\nfor (const m of Object.values(db)) { if (m.associate) m.associate(db); }"
	nodes, _ := linkOne(t, "postgres", "", own)
	if got := fkJSON(column(nodes, "#B.AId")); !strings.Contains(got, `"table":"As"`) {
		t.Errorf("own static associate: B.AId %s", got)
	}
	inherited := "const { Model } = require('sequelize');\nclass Base extends Model { static associate(m) {} }\nclass A extends Base {}\nA.init({ x: DataTypes.STRING }, { sequelize });\nconst db = { A };\nfor (const m of Object.values(db)) { if (m.associate) m.associate(db); }"
	nodes, _ = linkOne(t, "postgres", "", inherited)
	if a := tableNamed(nodes, "As"); a == nil || a.Attributes["column_set"] != "partial" {
		t.Errorf("inherited static associate: %+v", a)
	}
}

// TestRequiredConfigOptions reads `require(config)[env]` when the entries
// agree and refuses it when they disagree on an option key.
func TestRequiredConfigOptions(t *testing.T) {
	src := func(config string) map[string]string {
		return map[string]string{
			"package.json":       `{"dependencies": {"sequelize": "6.37.8", "pg": "8.0.0"}}`,
			"package-lock.json":  lockJSON("6.37.8"),
			"config/config.json": config,
			"models/index.js":    "const Sequelize = require('sequelize');\nconst env = process.env.NODE_ENV || 'development';\nconst config = require(__dirname + '/../config/config.json')[env];\nconst sequelize = new Sequelize(config.database, config.username, config.password, config);\nsequelize.define('A', { x: Sequelize.DataTypes.STRING });\n",
		}
	}
	nodes, _, _ := Link(input(t, src(`{"development": {"dialect": "sqlite", "database": "a"}, "production": {"dialect": "sqlite", "database": "b"}}`)))
	if a := tableNamed(nodes, "As"); a == nil || a.Attributes["dialect"] != "sqlite" {
		t.Errorf("agreeing entries: %+v", a)
	}
	nodes, _, diags := Link(input(t, src(`{"development": {"dialect": "sqlite"}, "production": {"dialect": "postgres"}}`)))
	if tableNamed(nodes, "As") != nil {
		t.Error("claimed with entries that disagree on the dialect")
	}
	if !hasDiag(diags, codeNotProven, "models/index.js", "disagree on dialect") {
		t.Errorf("no options diagnostic: %v", diagLines(diags))
	}
}

// TestClassHooks refuses every instance of the package when a hook is
// registered on Sequelize's class.
func TestClassHooks(t *testing.T) {
	nodes, diags := linkOne(t, "postgres", "", "Sequelize.addHook('beforeDefine', () => {});\nsequelize.define('A', { x: DataTypes.STRING });")
	if len(nodes) != 0 {
		t.Errorf("%d nodes claimed", len(nodes))
	}
	if !hasDiag(diags, codeNotProven, "models.js", "Sequelize.addHook(...) registers a hook") {
		t.Errorf("no hook diagnostic: %v", diagLines(diags))
	}
}

// TestKeyNotUniqueSqlite: sqlite accepts a foreign key to a column that is
// not unique, so the table is claimed and the list is unknown; a key that
// is not proven unique (an unread index) leaves the list unknown on
// postgres too.
func TestKeyNotUniqueSqlite(t *testing.T) {
	src := "const T = sequelize.define('T', { code: DataTypes.STRING });\nconst R = sequelize.define('R', { name: DataTypes.STRING });\nR.belongsTo(T, { as: 'byCode', targetKey: 'code' });"
	nodes, diags := linkOne(t, "sqlite", "", src)
	if hasDiag(diags, codeImportFailure, "models.js") {
		t.Errorf("failure on sqlite: %v", diagLines(diags))
	}
	c := column(nodes, "#R.byCodeId")
	if c == nil || c.Attributes["state"] != "read" || c.Attributes["foreign_keys"] != nil {
		t.Errorf("sqlite R.byCodeId: %+v", c)
	}
	unread := "const n = [];\nconst T = sequelize.define('T', { code: DataTypes.STRING }, { indexes: n });\nconst R = sequelize.define('R', { name: DataTypes.STRING });\nR.belongsTo(T, { as: 'byCode', targetKey: 'code' });"
	nodes, diags = linkOne(t, "postgres", "", unread)
	if hasDiag(diags, codeImportFailure, "models.js") {
		t.Errorf("failure without proof: %v", diagLines(diags))
	}
	if c := column(nodes, "#R.byCodeId"); c == nil || c.Attributes["foreign_keys"] != nil {
		t.Errorf("postgres R.byCodeId: %+v", c)
	}
}

// TestEnumDefaultSqlite: sqlite writes an ENUM as TEXT and takes a default
// outside its labels; postgres rejects it (SQ-P1).
func TestEnumDefaultSqlite(t *testing.T) {
	nodes, diags := linkOne(t, "sqlite", "", "sequelize.define('A', { e: { type: DataTypes.ENUM('a'), defaultValue: 'b' } });")
	if hasDiag(diags, codeImportFailure, "models.js") {
		t.Errorf("failure on sqlite: %v", diagLines(diags))
	}
	if c := column(nodes, "#A.e"); c == nil || c.Attributes["server_default_text"] != "'b'" {
		t.Errorf("A.e: %+v", c)
	}
}

// TestUnprovenAssociation: an association call in another module than a
// load-time throw may not run: its holder's column set and
// foreign keys are unknown, never claimed.
func TestUnprovenAssociation(t *testing.T) {
	in := input(t, map[string]string{
		"package.json":      `{"dependencies": {"sequelize": "6.37.8", "pg": "8.0.0"}}`,
		"package-lock.json": lockJSON("6.37.8"),
		"db.js":             "const { Sequelize, DataTypes } = require('sequelize');\nconst sequelize = new Sequelize({ dialect: 'postgres' });\nconst A = sequelize.define('A', { x: DataTypes.STRING });\nconst B = sequelize.define('B', { id: DataTypes.INTEGER });\nmodule.exports = { sequelize, A };\n",
		"assoc.js":          "const { DataTypes } = require('sequelize');\nconst { sequelize, A } = require('./db');\nconst C = sequelize.define('C', { y: DataTypes.STRING });\nC.belongsTo(A);\n",
	})
	nodes, _, diags := Link(in)
	if !hasDiag(diags, codeImportFailure, "db.js", "an id attribute without primaryKey") {
		t.Errorf("no load failure: %v", diagLines(diags))
	}
	if tableNamed(nodes, "Cs") != nil {
		t.Error("C, defined in a module whose order to the throw is not proven, is claimed")
	}
	if !hasDiag(diags, codeNotClaimed, "assoc.js", "not proven to run before it") {
		t.Errorf("no not-claimed diagnostic: %v", diagLines(diags))
	}
	if a := tableNamed(nodes, "As"); a == nil {
		t.Error("A, defined before the throw, is not claimed")
	}
}
