package sequelize

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

// linkOne links one package whose models.js holds src after an instance
// of dialect (driver names the package's driver dependency; "" none).
func linkOne(t *testing.T, dialect, driver, src string) ([]contract.Node, []contract.Diagnostic) {
	t.Helper()
	deps := `"sequelize": "6.37.8"`
	if driver != "" {
		deps += `, "` + driver + `": "1.0.0"`
	}
	opts := "{ logging: false }"
	if dialect != "" {
		opts = "{ dialect: '" + dialect + "', logging: false }"
	}
	in := input(t, map[string]string{
		"package.json":      `{"dependencies": {` + deps + `}}`,
		"package-lock.json": lockJSON("6.37.8"),
		"models.js":         "const { Sequelize, DataTypes } = require('sequelize');\nconst sequelize = new Sequelize(" + opts + ");\n" + src,
	})
	nodes, _, diags := Link(in)
	modeltest.CheckClaims(t, nodes, models.ClaimFacts)
	return nodes, diags
}

func column(nodes []contract.Node, suffix string) *contract.Node {
	for i := range nodes {
		if nodes[i].Kind == "column" && strings.HasSuffix(nodes[i].Identity, suffix) {
			return &nodes[i]
		}
	}
	return nil
}

func tableNamed(nodes []contract.Node, name string) *contract.Node {
	for i := range nodes {
		if nodes[i].Kind == "table" && nodes[i].Name == name {
			return &nodes[i]
		}
	}
	return nil
}

// TestModelFailures covers the provable failures this step reads (row
// SQ-P1): Sequelize throws while defining the model, or the database
// rejects its table; the model is not claimed and the diagnostic names
// the row.
func TestModelFailures(t *testing.T) {
	cases := []struct {
		name, dialect, driver, src, want string
	}{
		{"id without primaryKey", "postgres", "", "sequelize.define('A', { id: DataTypes.INTEGER });", "an id attribute without primaryKey"},
		{"ENUM without values", "postgres", "", "sequelize.define('A', { e: DataTypes.ENUM });", "an ENUM without values"},
		{"ENUM number label on postgres", "postgres", "", "sequelize.define('A', { e: DataTypes.ENUM('a', 2) });", "not a string literal"},
		{"fn default on sqlite", "sqlite", "", "sequelize.define('A', { d: { type: DataTypes.DATE, defaultValue: sequelize.fn('now') } });", "sqlite rejects"},
		{"two models one table", "postgres", "", "sequelize.define('A', { x: DataTypes.STRING }, { tableName: 't' });\nsequelize.define('B', { y: DataTypes.STRING }, { tableName: 't' });", "have the table name t"},
		{"index on a missing column", "postgres", "", "sequelize.define('A', { x: DataTypes.STRING }, { indexes: [{ fields: ['y'] }] });", "not a column of the table"},
		{"two columns one name", "postgres", "", "sequelize.define('A', { x: DataTypes.STRING, y: { type: DataTypes.STRING, field: 'x' } });", "both have the column name x"},
		{"two autoIncrement attributes", "postgres", "", "sequelize.define('A', { a: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true }, b: { type: DataTypes.INTEGER, autoIncrement: true } });", "more than one autoIncrement"},
		{"empty timestamp name", "postgres", "", "sequelize.define('A', { x: DataTypes.STRING }, { createdAt: '' });", "empty string"},
		{"no type", "postgres", "", "sequelize.define('A', { x: { allowNull: false } });", "Unrecognized datatype"},
		{"default outside the ENUM", "postgres", "", "sequelize.define('A', { e: { type: DataTypes.ENUM('a'), defaultValue: 'b' } });", "not one of the ENUM's labels"},
		{"VIRTUAL with unique", "postgres", "", "sequelize.define('A', { v: { type: DataTypes.VIRTUAL, unique: true } });", "VIRTUAL attribute with unique"},
		{"SERIAL with a default", "postgres", "", "sequelize.define('A', { id: { type: DataTypes.INTEGER, primaryKey: true, autoIncrement: true, defaultValue: 1 } });", "two defaults"},
		{"AUTOINCREMENT on a text key", "sqlite", "", "sequelize.define('A', { id: { type: DataTypes.UUID, primaryKey: true, autoIncrement: true } });", "INTEGER PRIMARY KEY"},
		{"two indexes one name", "postgres", "", "sequelize.define('A', { x: DataTypes.STRING }, { indexes: [{ name: 'i', fields: ['x'] }, { name: 'i', fields: ['id'] }] });", "two indexes named i"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nodes, diags := linkOne(t, c.dialect, c.driver, c.src)
			if len(nodes) != 0 {
				t.Errorf("%d nodes claimed", len(nodes))
			}
			if !hasDiag(diags, codeImportFailure, "models.js", "SQ-P1", c.want) {
				t.Errorf("no SQ-P1 failure with %q: %v", c.want, diagLines(diags))
			}
		})
	}
}

// TestModelNotClaimed covers the forms whose table name is unknown (row
// SQ-K2) or that no row reads (row D): a not-claimed diagnostic with the
// group's list marker, no table.
func TestModelNotClaimed(t *testing.T) {
	cases := []struct {
		name, dialect, src, row, want string
	}{
		{"non-literal tableName", "postgres", "const n = 'x';\nsequelize.define('A', { x: DataTypes.STRING }, { tableName: n });", "SQ-K2", "tableName option"},
		{"non-literal underscored", "postgres", "const u = true;\nsequelize.define('A', { x: DataTypes.STRING }, { underscored: u });", "SQ-K2", "underscored option"},
		{"options spread", "postgres", "const o = {};\nsequelize.define('A', { x: DataTypes.STRING }, { ...o });", "D", "not a plain property"},
		{"unread option", "postgres", "sequelize.define('A', { x: DataTypes.STRING }, { engine: 'InnoDB' });", "D", "option engine"},
		{"non-literal attributes", "postgres", "const attrs = {};\nsequelize.define('A', attrs);", "D", "attributes argument"},
		{"schema on sqlite", "sqlite", "sequelize.define('A', { x: DataTypes.STRING }, { schema: 's' });", "SQ-K2", "schema option on sqlite"},
		{"class chain", "postgres", "class Base {}\nclass A extends Base {}\nA.init({ x: DataTypes.STRING }, { sequelize });", "D", "does not end in Sequelize's Model"},
		{"init twice", "postgres", "const { Model } = require('sequelize');\nclass A extends Model {}\nA.init({ x: DataTypes.STRING }, { sequelize });\nA.init({ y: DataTypes.STRING }, { sequelize });", "D", "called 2 times"},
		{"non-ASCII model name", "postgres", "sequelize.define('Straße', { x: DataTypes.STRING });", "SQ-K2", "not ASCII"},
		{"long table name", "postgres", "sequelize.define('A', { x: DataTypes.STRING }, { tableName: '" + strings.Repeat("t", 64) + "' });", "SQ-K2", "longer than 63 bytes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nodes, diags := linkOne(t, c.dialect, "", c.src)
			for _, n := range nodes {
				if n.Kind == "table" {
					t.Errorf("table %s claimed", n.Identity)
				}
			}
			if !hasDiag(diags, codeNotClaimed, "models.js", c.row+": ", c.want, "may be incomplete") {
				t.Errorf("no %s not-claimed diagnostic with %q: %v", c.row, c.want, diagLines(diags))
			}
		})
	}
}

// TestColumnRules covers column rules outside the fixture: unknown
// dialect (only dialect-independent facts), texts Sequelize's DDL
// rewriting matches, sqlite's boolean-default rewrite, a unique object
// without a name, the postgres enum regex with a REFERENCES clause, and an
// unread `index` key.
func TestColumnRules(t *testing.T) {
	t.Run("unknown dialect", func(t *testing.T) {
		nodes, _ := linkOne(t, "", "", "sequelize.define('A', { x: { type: DataTypes.STRING, primaryKey: true } });")
		c := column(nodes, "#A.x")
		if c == nil || c.Attributes["state"] != "read" || c.Attributes["primary_key"] != true || c.Attributes["type"] != nil || c.Attributes["nullable"] != nil {
			t.Fatalf("%+v", c)
		}
		tb := tableNamed(nodes, "As")
		if tb == nil || tb.Attributes["verification"] != models.VerificationMapped || tb.Attributes["constraint_set"] != "partial" {
			t.Errorf("%+v", tb)
		}
	})
	unknown := func(t *testing.T, dialect, src, suffix, why string) {
		t.Helper()
		nodes, _ := linkOne(t, dialect, "", src)
		c := column(nodes, suffix)
		if c == nil || c.Attributes["state"] != "unknown" {
			t.Fatalf("%s: %+v", suffix, c)
		}
		reasons, _ := c.Attributes["reasons"].([]models.Reason)
		if len(reasons) == 0 || !strings.Contains(reasons[len(reasons)-1].Reason, why) {
			t.Errorf("%s: reasons %+v, want %q", suffix, reasons, why)
		}
	}
	t.Run("marker in a default", func(t *testing.T) {
		unknown(t, "postgres", "sequelize.define('A', { x: { type: DataTypes.STRING, defaultValue: 'NOT NULL' } });", "#A.x", "rewriting matches")
	})
	t.Run("sqlite boolean default rewrite", func(t *testing.T) {
		unknown(t, "sqlite", "sequelize.define('A', { x: { type: DataTypes.STRING, defaultValue: 'true story' } });", "#A.x", "replaceBooleanDefaults")
	})
	t.Run("enum with references on postgres", func(t *testing.T) {
		unknown(t, "postgres", "sequelize.define('A', { e: { type: DataTypes.ENUM('a'), references: { model: 'Bs', key: 'id' } } });", "#A.e", "last \")\"")
	})
	t.Run("smallserial", func(t *testing.T) {
		unknown(t, "postgres", "sequelize.define('A', { id: { type: DataTypes.SMALLINT, primaryKey: true, autoIncrement: true } });", "#A.id", "SMALLSERIAL")
	})
	t.Run("unique object without a name", func(t *testing.T) {
		nodes, _ := linkOne(t, "postgres", "", "sequelize.define('A', { x: { type: DataTypes.STRING, unique: { msg: 'm' } } });")
		c := column(nodes, "#A.x")
		if c == nil || c.Attributes["state"] != "read" || c.Attributes["unique"] != nil {
			t.Fatalf("%+v", c)
		}
		if tb := tableNamed(nodes, "As"); tb == nil || tb.Attributes["constraint_set"] != "partial" {
			t.Errorf("%+v", tb)
		}
	})
	t.Run("index key outside JSONB", func(t *testing.T) {
		nodes, _ := linkOne(t, "postgres", "", "sequelize.define('A', { x: { type: DataTypes.STRING, index: true } });")
		if c := column(nodes, "#A.x"); c == nil || c.Attributes["index"] != nil || c.Attributes["unique"] != false {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("removeAttribute with a variable", func(t *testing.T) {
		nodes, _ := linkOne(t, "postgres", "", "const A = sequelize.define('A', { x: DataTypes.STRING });\nconst k = 'x';\nA.removeAttribute(k);")
		for _, n := range nodes {
			if n.Kind == "column" && n.Attributes["state"] != "unknown" {
				t.Errorf("%s: %v", n.Identity, n.Attributes["state"])
			}
		}
	})
}

// TestLinkStepIsolation checks that a failing model step becomes a
// diagnostic and the other models are still claimed.
func TestLinkStepIsolation(t *testing.T) {
	faultHook = func(step, subject string) {
		if step == "model" && strings.Contains(subject, "models.js:3") {
			panic("boom")
		}
	}
	defer func() { faultHook = nil }()
	nodes, diags := linkOne(t, "postgres", "", "sequelize.define('A', { x: DataTypes.STRING });\nsequelize.define('B', { y: DataTypes.STRING });")
	if tableNamed(nodes, "As") != nil || tableNamed(nodes, "Bs") == nil {
		t.Errorf("tables %v", nodes)
	}
	if !hasDiag(diags, "schema_model_internal_error", "models.js", "boom") {
		t.Errorf("no internal error: %v", diagLines(diags))
	}
}

// TestForeignKeyStepAllOrNothing injects a panic into the group's
// foreign-key step after its first foreign key is
// written: the step is all-or-nothing, so no list it wrote survives and
// every read column's list is unknown with the step's reason.
func TestForeignKeyStepAllOrNothing(t *testing.T) {
	n := 0
	claimHook = func(col *models.Column, fact, row, form string) {
		if fact == "foreign_keys" {
			n++
			if n == 2 {
				panic("boom")
			}
		}
	}
	defer func() { claimHook = nil }()
	nodes, diags := linkOne(t, "postgres", "", "const A = sequelize.define('A', { x: DataTypes.STRING });\nconst B = sequelize.define('B', { y: DataTypes.STRING });\nB.belongsTo(A);\nB.belongsTo(A, { as: 'other' });")
	if n != 2 {
		t.Fatalf("%d foreign keys claimed before the fault", n)
	}
	if !hasDiag(diags, "schema_model_internal_error", "models.js", "foreign keys step", "boom") {
		t.Errorf("no internal error: %v", diagLines(diags))
	}
	cols := 0
	for _, c := range nodes {
		if c.Kind != "column" || c.Attributes["state"] != "read" {
			continue
		}
		cols++
		if c.Attributes["foreign_keys"] != nil {
			t.Errorf("%s: foreign keys %v survived the failed step", c.Identity, c.Attributes["foreign_keys"])
		}
		reasons, _ := c.Attributes["reasons"].([]models.Reason)
		found := false
		for _, r := range reasons {
			found = found || r.Fact != nil && *r.Fact == "foreign_keys" && strings.Contains(r.Reason, "foreign-key step of the group failed")
		}
		if !found {
			t.Errorf("%s: no step reason in %+v", c.Identity, reasons)
		}
	}
	if cols < 8 {
		t.Errorf("%d read columns", cols)
	}
}
