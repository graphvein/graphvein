package python

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// modelPayloadRows maps every claim-contract row id (docs/lang/python-models.md,
// "Claim contract") to the payload fields its capture branch fills. Rows with
// no fields have no capture side: T018c2 computes them from other rows'
// payload or from facts that are already cached.
var modelPayloadRows = map[string][]string{
	"C1":   {"Name", "Identity", "Line", "EndLine", "Bases", "Keywords", "MembersCaptured", "Members"},
	"C1b":  {"Name", "Identity", "Line", "EndLine", "Bases", "MembersCaptured", "Members"},
	"C1c":  {"Name", "Identity", "Line", "EndLine", "Bases", "Keywords", "MembersCaptured", "Members"},
	"C2":   {"Name", "Identity", "Line", "EndLine", "Bases", "MembersCaptured", "Members"},
	"C3":   {"Name", "Identity", "Line", "EndLine", "Bases", "Keywords", "MembersCaptured", "Members"},
	"C4":   {"Name", "Identity", "Line", "EndLine", "Bases", "Keywords", "MembersCaptured", "Members"},
	"C5":   {"Kind", "Name", "Line", "Value"},
	"C6":   {"Name", "Identity", "Line", "EndLine", "Bases", "Keywords", "MembersCaptured", "Members"},
	"N1":   {"Kind", "Name", "Line", "Value"},
	"N2":   {"Name", "Identity", "Line", "EndLine", "Bases", "Keywords", "MembersCaptured", "Members"},
	"N3":   nil,
	"M0":   nil,
	"M1":   {"Kind", "Name", "Line", "Decorators"},
	"M2":   {"Kind", "Name", "Line", "Annotation", "Value"},
	"M3":   {"Kind", "Name", "Line", "Annotation"},
	"M4":   {"Kind", "Name", "Line", "Annotation", "Value"},
	"M5":   {"Kind", "Name", "Line", "Value"},
	"M6":   {"Kind", "Name", "Line", "Annotation"},
	"M7":   {"Kind", "Name"},
	"M8":   {"Kind", "Name", "Line", "Value"},
	"M9":   {"Kind", "Name", "Line", "Members"},
	"M10":  {"Kind", "Name", "Line", "Value"},
	"M11":  {"Kind", "Name", "Line", "Value"},
	"M12":  {"Kind", "Line", "Detail"},
	"A1":   {"Kind", "Args", "Text"},
	"A2":   {"Kind", "Name", "Text"},
	"A3":   {"Kind", "Literal", "LitKind", "Text"},
	"A5":   nil,
	"A1m":  {"Kind", "Name", "Args", "Text"},
	"F1":   {"Name", "Value"},
	"F2":   nil,
	"F3":   {"Name", "Value"},
	"F3b":  {"Name", "Value"},
	"F4":   {"Name", "Value"},
	"F5":   {"Kind", "Name", "Text"},
	"F6":   {"Name", "Value"},
	"F7":   {"Name", "Value"},
	"F7r":  {"Kind", "Name", "Keywords"},
	"F8":   {"Name", "Value"},
	"F9":   {"Name", "Value"},
	"K1":   {"Kind", "Name", "Args", "Keywords"},
	"K1b":  {"Kind", "Name", "Text"},
	"K2":   {"Name", "Value"},
	"K3":   {"Kind", "Name", "Args", "Keywords"},
	"K4":   {"Name", "Value"},
	"K5":   {"Name", "Value"},
	"K6":   {"Kind", "Name", "Args"},
	"T1":   {"Kind", "Name", "Args", "Text"},
	"TA1":  {"Kind", "Name", "Args", "Keywords"},
	"TA2":  {"Kind", "Name", "Args", "Keywords"},
	"TA3":  {"Kind", "Name", "Args", "Keywords"},
	"TA-D": {"Kind", "Name"},
	"U1":   {"Name", "Identity", "Line", "EndLine", "Bases"},
	"U2":   {"Kind", "Name", "Line", "Annotation", "Value"},
	"U3":   {"Line"},
	"U4":   {"Line"},
	"X1":   {"Args", "Kind", "Line", "Receiver"},
	"U5":   {"Kind", "Line", "Receiver", "Args"},
	"I1":   nil,
	"I2":   nil,
	"D":    {"Kind", "Name", "Line", "Annotation", "Value"},
}

const modelImports = "from typing import Annotated, ClassVar, Optional\nimport sqlalchemy as sa\nfrom sqlalchemy import Column, ForeignKey, Index, Sequence, String, UniqueConstraint, CheckConstraint, PrimaryKeyConstraint, event\nfrom sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, registry\nfrom pydantic import BeforeValidator, ConfigDict, field_validator\nfrom sqlmodel import Field, Relationship, SQLModel\n"

// modelSnippets holds one source per row with a capture side; the first
// record the capture tags with the row must fill exactly its listed fields.
var modelSnippets = map[string]string{
	"C1":   "class Hero(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n",
	"C1b":  "class HeroBase(SQLModel):\n    name: str\n",
	"C1c":  "class Flagged(SQLModel, table=FLAG):\n    id: int = Field(primary_key=True)\n",
	"C2":   "class Base(SQLModel):\n    name: str\nclass Hero(Base, table=True):\n    id: int = Field(primary_key=True)\n",
	"C3":   "class Parent(SQLModel, table=True):\n    id: int = Field(primary_key=True)\nclass Child(Parent, table=True):\n    extra: str\n",
	"C4":   "class Registered(SQLModel, registry=other, table=True):\n    id: int = Field(primary_key=True)\n",
	"C5":   "class Hero(SQLModel, table=True):\n    model_config = ConfigDict(from_attributes=True)\n",
	"C6":   "class Mixed(Mixin, SQLModel, table=True):\n    id: int = Field(primary_key=True)\n",
	"N1":   "class Hero(SQLModel, table=True):\n    __tablename__ = \"heroes\"\n",
	"N2":   "class Hero(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n",
	"M1":   "class Hero(SQLModel, table=True):\n    @field_validator(\"name\")\n    @classmethod\n    def check(cls, v):\n        return v\n",
	"M2":   "class Hero(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n",
	"M3":   "class Hero(SQLModel, table=True):\n    name: str\n",
	"M4":   "class Hero(SQLModel, table=True):\n    rank: int = 3\n",
	"M5":   "class Hero(SQLModel, table=True):\n    team: Optional[\"Team\"] = Relationship(back_populates=\"heroes\")\n",
	"M6":   "class Hero(SQLModel, table=True):\n    kind: ClassVar[str] = \"x\"\n",
	"M7":   "class Hero(SQLModel, table=True):\n    _cache: dict = {}\n",
	"M8":   "class Hero(SQLModel, table=True):\n    model_config = ConfigDict(from_attributes=True)\n",
	"M9":   "class Hero(SQLModel, table=True):\n    class Config:\n        frozen = True\n",
	"M10":  "class Hero(SQLModel, table=True):\n    __mapper_args__ = {\"eager_defaults\": True}\n",
	"M11":  "class Hero(SQLModel, table=True):\n    __table_args__ = (UniqueConstraint(\"a\"),)\n",
	"M12":  "class Hero(SQLModel, table=True):\n    if FLAG:\n        pass\n",
	"A1":   "class Hero(SQLModel, table=True):\n    name: str | None = None\n",
	"A2":   "class Hero(SQLModel, table=True):\n    name: str\n",
	"A3":   "class Hero(SQLModel, table=True):\n    count: \"int\" = None\n",
	"A1m":  "class Hero(SQLModel, table=True):\n    name: Annotated[str, BeforeValidator(strip)]\n",
	"F1":   "class Hero(SQLModel, table=True):\n    name: str = Field(index=True)\n",
	"F3":   "class Hero(SQLModel, table=True):\n    name: str = Field(default=\"x\")\n",
	"F3b":  "class Hero(SQLModel, table=True):\n    tags: dict = Field(default_factory=dict)\n",
	"F4":   "class Hero(SQLModel, table=True):\n    team_id: int = Field(foreign_key=\"team.id\")\n",
	"F5":   "class Hero(SQLModel, table=True):\n    name: str = Field(max_length=5)\n",
	"F6":   "class Hero(SQLModel, table=True):\n    data: dict = Field(sa_type=sa.JSON)\n",
	"F7":   "class Hero(SQLModel, table=True):\n    data: str = Field(sa_column=Column(String))\n",
	"F7r":  "class Hero(SQLModel, table=True):\n    id: int = Field(sa_column=Column(sa.Integer), primary_key=True)\n",
	"F8":   "class Hero(SQLModel, table=True):\n    data: str = Field(sa_column_kwargs={\"comment\": \"c\"})\n",
	"F9":   "class Hero(SQLModel, table=True):\n    name: str = Field(description=\"long text\")\n",
	"K1":   "class Hero(SQLModel, table=True):\n    data: str = Field(sa_column=Column(\"n\", String(5), nullable=True))\n",
	"K1b":  "class Hero(SQLModel, table=True):\n    data: str = Field(sa_column=Column(Trimmed()))\n",
	"K2":   "class Hero(SQLModel, table=True):\n    data: str = Field(sa_column=Column(String, nullable=False))\n",
	"K3":   "class Hero(SQLModel, table=True):\n    team_id: int = Field(sa_column=Column(ForeignKey(\"team.id\", ondelete=\"CASCADE\")))\n",
	"K4":   "class Hero(SQLModel, table=True):\n    data: str = Field(sa_column=Column(String, server_default=\"x\"))\n",
	"K5":   "class Hero(SQLModel, table=True):\n    data: str = Field(sa_column=Column(String, comment=\"long text\"))\n",
	"K6":   "class Hero(SQLModel, table=True):\n    n: int = Field(sa_column=Column(sa.Integer, Sequence(\"s\")))\n",
	"T1":   "class Hero(SQLModel, table=True):\n    name: str = Field(sa_type=String(64))\n",
	"TA1":  "class Hero(SQLModel, table=True):\n    __table_args__ = (UniqueConstraint(\"a\", name=\"uq\"),)\n",
	"TA2":  "class Hero(SQLModel, table=True):\n    __table_args__ = (Index(\"ix\", \"a\", unique=True),)\n",
	"TA3":  "class Hero(SQLModel, table=True):\n    __table_args__ = (CheckConstraint(\"a > 0\", name=\"ck\"),)\n",
	"TA-D": "class Hero(SQLModel, table=True):\n    __table_args__ = (PrimaryKeyConstraint(\"a\"),)\n",
	"U1":   "class Base(DeclarativeBase):\n    pass\n",
	"U2":   "class Hero(SQLModel, table=True):\n    id: Mapped[int] = mapped_column(primary_key=True)\n",
	"U3":   "events = sa.Table(\"events\", metadata)\n",
	"U4":   "registry().map_imperatively(Plain, events)\n",
	"X1":   "Hero.__table__.append_constraint(CheckConstraint(\"a > 0\"))\n",
	"U5":   "event.listen(Hero.__table__, \"after_create\", ddl)\n",
	"D":    "class Hero(SQLModel, table=True):\n    rank: int = LIMIT\n",
}

// contractRows parses the claim-contract table rows (id -> row line) of a
// Markdown file's "## Claim contract" section.
func contractRows(t *testing.T, path string) (map[string]string, []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatalf("%s: no Claim contract section", path)
	}
	section := text[start+1:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	rows := map[string]string{}
	var order []string
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| id |") {
			continue
		}
		id := strings.TrimSpace(strings.SplitN(line[1:], "|", 2)[0])
		if _, dup := rows[id]; dup {
			t.Fatalf("%s: duplicate row %s", path, id)
		}
		rows[id] = line
		order = append(order, id)
	}
	return rows, order
}

// captureSide reads the payload cell: "-", "nothing", "no new field ..." and
// "... already cached ..." mean the row has no capture branch.
func captureSide(row string) bool {
	cells := strings.Split(strings.ReplaceAll(row, `\|`, "\x00"), "|")
	payload := strings.TrimSpace(cells[3])
	return payload != "-" && payload != "nothing" && !strings.HasPrefix(payload, "no new field") && !strings.Contains(payload, "already cached")
}

// filledFields lists the non-zero exported fields of a tagged record.
func filledFields(record any) []string {
	v := reflect.ValueOf(record)
	if v.Kind() != reflect.Struct {
		return []string{"Line"}
	}
	var out []string
	for i := 0; i < v.NumField(); i++ {
		if !v.Field(i).IsZero() {
			out = append(out, v.Type().Field(i).Name)
		}
	}
	return out
}

type taggedRecord struct {
	row    string
	record any
}

// captureTags extracts the sources and returns every tag the capture emitted.
func captureTags(t *testing.T, sources map[string]string) []taggedRecord {
	t.Helper()
	var tags []taggedRecord
	modelTag = func(row string, record any) { tags = append(tags, taggedRecord{row, record}) }
	defer func() { modelTag = nil }()
	files, _ := syntaxFiles(t, sources)
	for file, f := range files {
		if f.Python == nil || !f.Python.ModelsValid() {
			t.Fatalf("%s: invalid model payload", file)
		}
	}
	return tags
}

func TestModelPayloadContract(t *testing.T) {
	docs, order := contractRows(t, "../../../docs/lang/python-models.md")
	task, _ := contractRows(t, "../../../docs/contract/claims/python-models-syntax.md")
	if !reflect.DeepEqual(docs, task) {
		for id, row := range task {
			if docs[id] != row {
				t.Errorf("row %s differs between the task and docs/lang/python-models.md", id)
			}
		}
		for id := range docs {
			if _, ok := task[id]; !ok {
				t.Errorf("row %s is only in docs/lang/python-models.md", id)
			}
		}
	}
	if len(order) != 61 {
		t.Errorf("contract rows = %d, want 61", len(order))
	}
	want := map[string]bool{}
	for _, id := range order {
		fields, ok := modelPayloadRows[id]
		if !ok {
			t.Errorf("row %s has no modelPayloadRows entry", id)
			continue
		}
		if captureSide(docs[id]) != (len(fields) > 0) {
			t.Errorf("row %s: payload cell capture side %v, modelPayloadRows %v", id, captureSide(docs[id]), fields)
		}
		if len(fields) > 0 {
			want[id] = true
		}
	}
	for id := range modelPayloadRows {
		if _, ok := docs[id]; !ok {
			t.Errorf("modelPayloadRows has %s, which is not a contract row", id)
		}
	}
	// One snippet per row: the first record tagged with the row fills
	// exactly the row's fields.
	for id := range want {
		snippet, ok := modelSnippets[id]
		if !ok {
			t.Errorf("row %s has no snippet", id)
			continue
		}
		tags := captureTags(t, map[string]string{"snippet.py": modelImports + snippet})
		found := false
		for _, tag := range tags {
			if tag.row != id {
				continue
			}
			found = true
			got := filledFields(tag.record)
			sort.Strings(got)
			exp := append([]string(nil), modelPayloadRows[id]...)
			sort.Strings(exp)
			if !reflect.DeepEqual(got, exp) {
				t.Errorf("row %s: filled %v, want %v (%+v)", id, got, exp, tag.record)
			}
			break
		}
		if !found {
			t.Errorf("row %s: snippet emitted no %s tag", id, id)
		}
	}
	// The whole fixture emits exactly the rows with a capture side, so a
	// branch without a row and a row without a branch both fail.
	sources := map[string]string{}
	paths, err := filepath.Glob("../../../testdata/fixtures/sqlmodel-models/*.py")
	if err != nil || len(paths) == 0 {
		t.Fatal("no fixture modules", err)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		sources[filepath.Base(p)] = string(data)
	}
	emitted := map[string]bool{}
	for _, tag := range captureTags(t, sources) {
		emitted[tag.row] = true
	}
	for id := range want {
		if !emitted[id] {
			t.Errorf("fixture emits no %s tag", id)
		}
	}
	for id := range emitted {
		if !want[id] {
			t.Errorf("capture emitted tag %s, which is not a contract row with a capture side", id)
		}
	}
}
