package diesel

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// readJoinables reads every joinable! of the file at any module depth
// (row DI-J1) once every table is read.
func (g *group) readJoinables(items []Item) {
	for i := range items {
		it := &items[i]
		switch {
		case it.Kind == ItemModule && it.Name != "sql_types":
			g.readJoinables(it.Items)
		case it.Kind == ItemJoinable && it.Unread == "":
			g.steps.Guard(g.f.Path, "joinable", fmt.Sprintf("Diesel joinable! at %s:%d", g.f.Path, it.Line), func() { g.readJoinable(it) })
		}
	}
}

// readJoinable is the DI-J1 branch: a generated joinable!(child -> parent
// (column)) between two tables of the group whose facts are known, the
// column a generated column of the child and the parent's primary key a
// single generated column, is a foreign key from that column to the
// parent's key (print-schema writes one only for such a constraint,
// foreign_keys.rs:8-71). Everything else claims no foreign key.
func (g *group) readJoinable(it *Item) {
	j := it.Joinable
	g.in.tag(rowJ1)
	if it.Macro == "joinable" {
		g.in.tag("DI-J1/joinable!(child -> parent (column));")
	} else {
		g.in.tag("DI-J1/diesel::joinable!(...)")
	}
	if len(j.Child) != 1 || len(j.Parent) != 1 {
		g.in.tag("DI-J1/a::b")
		return
	}
	if g.prov.HandWritten(it.Line) || g.prov.HandWritten(j.Line) {
		// A Rust-level join hint: no foreign key.
		return
	}
	child, parent := g.byRust[rustKey(it.Module, j.Child[0])], g.byRust[rustKey(it.Module, j.Parent[0])]
	if g.before21 && g.f.Dialect != "sqlite" {
		// diesel_cli 2.0 writes a joinable! for one column pair of a
		// composite foreign key too (2.0.1 foreign_keys.rs has no
		// single-column filter; information_schema.rs:360-369 takes the
		// first key_column_usage row): in a file that does not prove
		// diesel_cli 2.1 or later output, whatever the pin, no joinable! is
		// a foreign key (Revision 2, A1 and addendum).
		//
		// SQLite is exempt (Revision 2 addendum 2): 2.0.1 and 2.1.1 both
		// load one constraint per PRAGMA foreign_key_list row (2.0.1
		// sqlite.rs:65-108, 2.1.1 sqlite.rs:57-88), so a composite key's
		// rows share a table pair and the duplicate-pair filter drops them
		// in either release; the joinable! sets agree.
		if child != nil && child.cols[j.Column] != nil {
			g.columnReason(child.cols[j.Column].rec, rowJ1, models.Fact("foreign_keys"), "the joinable! at line %d is not claimed as a foreign key: %s, and diesel_cli 2.0 also writes a joinable! for one column of a composite foreign key", j.Line, pinText(g.f.Version))
		}
		return
	}
	if child == nil || parent == nil || !child.known || !parent.pkKnown || len(parent.t.Keys) != 1 {
		return
	}
	col, key := child.cols[j.Column], parent.cols[parent.t.Keys[0]]
	if col == nil || !col.known || key == nil || !key.known {
		return
	}
	fk := &models.ForeignKey{
		Schema: parent.rec.Schema, Table: parent.rec.Name, Column: key.rec.Name,
		Columns: []string{col.rec.Name}, RefColumns: []string{key.rec.Name},
		File: g.f.Path, Line: j.Line,
	}
	col.rec.ForeignKeys = append(col.rec.ForeignKeys, fk)
	col.rec.ForeignKeysUnknown = false
	col.rec.Claims["foreign_keys"] = models.ClaimSource{Row: rowJ1, Line: j.Line}
}

// tableList writes the markers that keep the group's table list from
// reading complete (DI-T1: complete only when every table is generated,
// no filter is set and no patch hunk removes a table! block). Each is a
// schema_model_unread diagnostic on the schema file, which the server
// counts into the group's table_list reasons.
func (g *group) tableList() {
	f := g.f
	mark := func(line int, format string, args ...any) {
		g.steps.Diag(f.Path, "info", codeUnread, fmt.Sprintf("%s:%d: ", f.Path, line)+fmt.Sprintf(format, args...))
	}
	switch {
	case !f.Configured:
		mark(1, "the schema file is not configured by diesel.toml (DI-U2, DI-V7): names only; its table list is not proven complete")
		return
	case !g.prov.Generated:
		mark(1, "the schema file is read as hand-written (%s): names only; its table list is not proven complete", g.prov.Reason)
		return
	}
	for _, t := range g.tables {
		if g.prov.Added[t.ModelLine] {
			mark(t.ModelLine, "table %s is added by the patch_file %s (hand-written, names only); the table list is not proven complete", t.Name, f.Section.PatchPath)
		}
	}
	if f.Section != nil && f.Section.Filter {
		mark(1, "the diesel.toml section that configures this file sets filter: tables print-schema left out are not listed; the table list is not proven complete")
	}
	for _, r := range g.prov.Removals {
		for _, l := range r.Lines {
			if containsTableMacro(l) {
				mark(max(r.After, 1), "the patch_file %s removes a table! line (hunk %d); the table list is not proven complete", f.Section.PatchPath, r.Hunk+1)
				break
			}
		}
	}
}

func containsTableMacro(l string) bool { return strings.Contains(l, "table!") }

// pinText says why the diesel_cli 2.0 rules apply to a group: the file
// shows no diesel_cli 2.1 output, and the pin does not prove which
// diesel_cli wrote it (Revision 2 addendum).
func pinText(v string) string {
	pin := "the diesel pin is not proven"
	if v != "" {
		pin = "the diesel pin " + v + " does not prove which diesel_cli wrote it"
	}
	return "the file shows no diesel_cli 2.1 or later output (no #[max_length] and no database comment on a generated line; " + pin + ")"
}
