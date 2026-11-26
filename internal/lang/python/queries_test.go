package python

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// renderQuery prints a reduced chain compactly: other is ?, a keyword keeps
// its name, a chained link prints its Object first.
func renderQuery(e facts.PythonModelExpr) string {
	switch e.Kind {
	case "call":
		var parts []string
		for _, a := range e.Args {
			parts = append(parts, renderQuery(a))
		}
		for _, k := range e.Keywords {
			parts = append(parts, k.Name+"="+renderQuery(k.Value))
		}
		prefix := ""
		if e.Object != nil {
			prefix = renderQuery(*e.Object) + "."
		}
		return prefix + e.Name + "(" + strings.Join(parts, ", ") + ")"
	case "name":
		return e.Name
	case "literal":
		return fmt.Sprintf("%s:%q", e.LitKind, e.Literal)
	case "list", "tuple":
		var parts []string
		for _, a := range e.Args {
			parts = append(parts, renderQuery(a))
		}
		return e.Kind + "[" + strings.Join(parts, ", ") + "]"
	}
	return "?"
}

// querySites parses one file and returns its rendered query sites as
// "line owner expression", and the payload.
func querySites(t *testing.T, file, source string) ([]string, *facts.PythonModels) {
	t.Helper()
	files, _ := syntaxFiles(t, map[string]string{file: source})
	f := files[file]
	if !f.Python.ModelsValid() {
		t.Fatalf("invalid payload: %+v", f.Python.Models)
	}
	m := f.Python.Models
	if m == nil {
		return nil, nil
	}
	var out []string
	for _, q := range m.Queries {
		out = append(out, fmt.Sprintf("%d %s %s", q.Span.StartLine, q.Owner, renderQuery(q.Expression)))
	}
	return out, m
}

const querySource = `from sqlmodel import Session, select, col
import sqlalchemy as sa
from app.models import User, Team

Q = select(User)

def list_users(session: Session, team: Team | None, limit: int = 3, other: dict = None):
    stmt = select(User, col(Team.id)).join(Team, User.team_id == Team.id).where(User.id > 1)
    rows = session.exec(stmt).all()
    session.get(User, 1)
    d = {}
    d.get("x")
    other.get(limit)
    d.update(rows)
    sa.update(User).values(name="x")
    session.add(User(name="a"))
    session.add_all([User(), team])
    session.refresh(team)
    session.exec(sa.text("select * from " "users"))
    sa.text(f"select {limit}")
    router.delete("/items")
    session.add(  # a comment, never captured
        team)
    # session.add(User()) in a comment is no call

    def inner():
        session.delete(team)
    return rows

from sqlalchemy.dialects.postgresql import insert as pg_insert

def upsert(conn):
    conn.execute(pg_insert(Team).values(id=1).on_conflict_do_nothing())

class Repo:
    q = select(Team)
    stmt = sa.insert(Team).from_select(["id"], select(User.id))
`

func TestQuerySites(t *testing.T) {
	got, m := querySites(t, "app/main.py", querySource)
	want := []string{
		`5 constant:app/main.py#Q select(User)`,
		`8 function:app/main.py#list_users select(User, col(Team.id)).join(Team, ?).where(?)`,
		`10 function:app/main.py#list_users session.get(User, int:"1")`,
		`13 function:app/main.py#list_users other.get(limit)`,
		`15 function:app/main.py#list_users sa.update(User).values(name=?)`,
		`16 function:app/main.py#list_users session.add(User(name=?))`,
		`17 function:app/main.py#list_users session.add_all(list[User(), team])`,
		`18 function:app/main.py#list_users session.refresh(team)`,
		`19 function:app/main.py#list_users sa.text(str:"select * from users")`,
		`20 function:app/main.py#list_users sa.text(?)`,
		`22 function:app/main.py#list_users session.add(team)`,
		`27 function:app/main.py#list_users.inner session.delete(team)`,
		`33 function:app/main.py#upsert pg_insert(Team).values(id=?).on_conflict_do_nothing()`,
		`36 class:app/main.py#Repo select(Team)`,
		`37 class:app/main.py#Repo sa.insert(Team).from_select(list[str:"id"], select(User.id))`,
		`37 class:app/main.py#Repo select(User.id)`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query sites:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Annotations: only the parameters a chain names (limit and other are
	// named, d is a local); the inner function's chain names its parent's.
	wantAnnotations := []facts.PythonModelAnnotations{{Scope: 1, Parameters: map[string]facts.PythonModelExpr{
		"session": {Kind: "name", Name: "Session"},
		"team":    {Kind: "union", Args: []facts.PythonModelExpr{{Kind: "name", Name: "Team"}, {Kind: "literal", LitKind: "none", Literal: "None"}}},
		"limit":   {Kind: "name", Name: "int"},
		"other":   {Kind: "name", Name: "dict"},
	}}}
	if !reflect.DeepEqual(m.Annotations, wantAnnotations) {
		t.Fatalf("annotations: %+v", m.Annotations)
	}
	if len(m.Classes) != 0 || m.Version != facts.PythonModelsVersion {
		t.Fatalf("model payload: %+v", m)
	}
}

func TestQueryAnnotationForms(t *testing.T) {
	source := `from typing import Annotated, Optional
from sqlalchemy.ext.asyncio import AsyncSession
from fastapi import Depends
from app.models import Item

SessionDep = Annotated[AsyncSession, Depends(get_db)]

async def a(db: SessionDep, item: Optional[Item], *, other: "Item" = None):
    await db.merge(item)
    db.add(other)

def b(session, item: Item):
    session.delete(item)
`
	_, m := querySites(t, "app/routes.py", source)
	if m == nil {
		t.Fatal("no payload")
	}
	got := map[int]map[string]string{}
	for _, a := range m.Annotations {
		got[a.Scope] = map[string]string{}
		for name, e := range a.Parameters {
			got[a.Scope][name] = renderQuery(e) + " " + e.Kind
		}
	}
	want := map[int]map[string]string{
		1: {"db": "SessionDep name", "item": "? subscript", "other": `str:"Item" literal`},
		2: {"item": "Item name"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("annotations: %v", got)
	}
	if e := m.Annotations[0].Parameters["item"]; e.Name != "Optional" || len(e.Args) != 1 || e.Args[0].Name != "Item" {
		t.Fatalf("Optional[Item]: %+v", e)
	}
}

func TestQueryGate(t *testing.T) {
	for _, test := range []struct {
		name, source string
		sites        []string
	}{
		// GQ: no sqlmodel or sqlalchemy import, a session-named receiver.
		{"gq", "from app.db import Thing\n\nasync def h(db_session, t: Thing):\n    db_session.add(t)\n    cache.get(Thing)\n", []string{"4 function:app/h.py#h db_session.add(t)", "5 function:app/h.py#h cache.get(Thing)"}},
		{"gq attribute", "def h(self):\n    self.session.refresh(self.item)\n", []string{"2 function:app/h.py#h self.session.refresh(self.item)"}},
		{"gq add_all", "def h(async_session, items):\n    async_session.add_all(items)\n", []string{"2 function:app/h.py#h async_session.add_all(items)"}},
		// GQ through the receiver's annotation (a DbSession dependency alias).
		{"gq annotation", "from app.utils import DbSession\n\nasync def h(db: DbSession, t):\n    def inner():\n        db.add(t)\n", []string{"5 function:app/h.py#h.inner db.add(t)"}},
		// Not GQ: the receiver is neither session-named nor so annotated, or
		// the method is no session method.
		{"receiver", "def h(db: Database, t):\n    db.add(t)\n", nil},
		{"method", "def h(session, stmt):\n    session.exec(stmt)\n", nil},
		{"literal", "def h(session):\n    session.get(\"key\")\n", nil},
		{"no chain", "x = 1\n", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, m := querySites(t, "app/h.py", test.source)
			if test.sites == nil && m != nil {
				t.Fatalf("payload outside the gate: %+v", m)
			}
			if test.sites != nil && (m == nil || len(m.Classes)+len(m.CoreTables)+len(m.Sites) != 0) {
				t.Fatalf("GQ payload: %+v", m)
			}
			if !reflect.DeepEqual(got, test.sites) {
				t.Fatalf("sites %q want %q", got, test.sites)
			}
		})
	}
}

func TestQueryG1KeepsModelPayload(t *testing.T) {
	source := "from sqlmodel import SQLModel, Field, select\n\nclass Hero(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n\ndef f(session):\n    return session.exec(select(Hero)).all()\n"
	got, m := querySites(t, "app/models.py", source)
	if len(m.Classes) != 1 || !m.Classes[0].MembersCaptured {
		t.Fatalf("classes: %+v", m.Classes)
	}
	if want := []string{"7 function:app/models.py#f select(Hero)"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sites %q", got)
	}
	if len(m.Annotations) != 0 {
		t.Fatalf("unannotated parameters captured: %+v", m.Annotations)
	}
}
