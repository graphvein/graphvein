package changes_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/store"
)

// TestTableAccessRelink covers T019 Revision 4 on both backends with a
// model removed under unchanged code (a ref comparison of two commits):
// the owner's lost reads_table edge is reported on the owner's C2 item
// (changed, table_access_changed; cause code, as the edge's footprint
// holds the changed model file), never as D; a file node's new
// reads_table edge is a C16 edge item, or context of the table's item
// when the table has one; the removed table's impact takes its
// first hop through the removed edge to the owner (I8, removed, "reads
// this table") and lists that owner as a use found; a removed column of a
// remaining table reaches that table's users.
func TestTableAccessRelink(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	backends(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		s, err := b.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		root := filepath.Join(t.TempDir(), "repo")
		cache := filepath.Join(t.TempDir(), "cache")
		model := "from typing import Optional\n\nfrom sqlmodel import Field, SQLModel\n\n\n"
		hero := "class Hero(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n\n\n"
		guild := "class Guild(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n\n\n"
		team := "class Team(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n"
		writeFiles(t, root, map[string]string{
			"models/m.py": model + hero + guild + team + "    name: str\n",
			"models/q.py": "from sqlmodel import Session, select\n\nfrom .m import Hero, Team\n\n\ndef heroes(session: Session):\n    return session.exec(select(Hero)).all()\n\n\n" +
				"def teams(session: Session):\n    return session.exec(select(Team)).all()\n",
		})
		git(t, root, "init", "-q")
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "one")
		writeFiles(t, root, map[string]string{
			"models/m.py": model + guild + team,
			"models/p.py": "from sqlmodel import select\n\nfrom .m import Guild, Team\n\nselect(Guild)\nselect(Team)\n",
		})
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "two")
		indexRun(t, ctx, b, root, cache)
		indexRefs(t, ctx, b, root, cache, git(t, root, "rev-parse", "HEAD~1"), git(t, root, "rev-parse", "HEAD"))
		c := compare(t, ctx, s, "ref:HEAD~1..HEAD", root)
		for _, v := range c.Violations() {
			t.Error(v)
		}

		owner := "function:models/q.py#heroes"
		lost := store.EdgeKey("reads_table", owner, "table:models/m.py#Hero")
		gained := store.EdgeKey("reads_table", "file:models/p.py#models%2Fp.py", "table:models/m.py#Guild")
		// Team has a C11 item (its column was removed): the file node's
		// edge to it is that item's context (R3-3's rule), no claim.
		contextEdge := store.EdgeKey("reads_table", "file:models/p.py#models%2Fp.py", "table:models/m.py#Team")
		var ownerItem *changes.Item
		gainedSeen, teamItem := false, false
		for _, it := range c.AllItems() {
			switch {
			case it.Record == store.RecordNode && it.Identity == owner:
				ownerItem = &it
			case it.Identity == lost:
				t.Errorf("the lost edge is an item of its own (%s), not context of its owner", it.Row)
			case it.Identity == gained:
				gainedSeen = true
				if it.Row != "C16" || it.Change != changes.Added {
					t.Errorf("file node edge: row %s change %s, want C16 added", it.Row, it.Change)
				}
			case it.Record == store.RecordNode && it.Identity == "table:models/m.py#Team":
				teamItem = true
			case it.Identity == contextEdge:
				t.Errorf("the file node's edge to a table with an item is a %s item, not context", it.Row)
			case it.Row == "D":
				t.Errorf("D item %s %s", it.Kind, it.Identity)
			}
		}
		if !teamItem {
			t.Errorf("Team has no item; the context case is not exercised")
		}
		if !gainedSeen {
			t.Errorf("no item for the file node's reads_table edge")
		}
		if ownerItem == nil {
			t.Fatalf("no item for %s", owner)
		}
		if ownerItem.Row != "C2" || ownerItem.Change != changes.Changed || len(ownerItem.Labels) != 1 ||
			ownerItem.Labels[0] != changes.LabelTableAccessChanged || str(ownerItem.Cause) != changes.CauseCode {
			t.Errorf("owner item: row %s change %s labels %v cause %v", ownerItem.Row, ownerItem.Change, ownerItem.Labels, str(ownerItem.Cause))
		}

		r, err := changes.Impact(ctx, s, c, changes.ImpactRequest{ID: "table:models/m.py#Hero"})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range changes.ImpactViolations(r) {
			t.Error(v)
		}
		var found bool
		for _, p := range r.Paths {
			if p.Node == owner {
				found = true
				st := p.Steps[0]
				if p.Relation != "I8" || !st.Removed || p.Wording != "reads this table" || st.Access == nil || st.Access.Tool != "sqlmodel" {
					t.Errorf("path to the owner: relation %s removed %t wording %q access %+v", p.Relation, st.Removed, p.Wording, st.Access)
				}
			}
		}
		if !found {
			t.Errorf("the removed table reaches no owner: %+v", r.Paths)
		}
		if len(r.TableUses) != 1 || len(r.TableUses[0].Found) != 1 || r.TableUses[0].Found[0] != owner || r.TableUses[0].Wording != "uses found" {
			t.Errorf("table uses = %+v", r.TableUses)
		}
		for _, ne := range r.Unknowns.NotExtracted {
			if ne.Relation == "I8" {
				t.Errorf("I8 not_extracted for a SQLModel table: %v", ne.Nodes)
			}
		}

		// A removed column of a remaining table reaches the table's users
		// in the graph that holds the dependents (the base namespace).
		r, err = changes.Impact(ctx, s, c, changes.ImpactRequest{ID: "column:models/m.py#Team.name"})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range changes.ImpactViolations(r) {
			t.Error(v)
		}
		found = false
		for _, p := range r.Paths {
			if p.Node == "function:models/q.py#teams" {
				a := p.Steps[0].Access
				found = p.Relation == "I8" && a != nil && a.ColumnUse != nil && *a.ColumnUse == "not_extracted"
			}
		}
		if !found {
			t.Errorf("the removed column does not reach its table's user with column_use not_extracted: %+v", r.Paths)
		}
	})
}
