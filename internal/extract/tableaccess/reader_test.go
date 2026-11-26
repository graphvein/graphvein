package tableaccess

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// refsText renders a reading's references as sorted "r:name" / "w:name"
// entries (CTE references as "cte:name"), so cases read like the SQL.
func refsText(r Reading) []string {
	var out []string
	for _, ref := range r.Refs {
		name := ref.Name
		if ref.Schema != "" {
			name = ref.Schema + "." + ref.Name
		}
		switch {
		case ref.CTE:
			out = append(out, "cte:"+name)
		case ref.Write:
			out = append(out, "w:"+name+":"+ref.Operation)
		default:
			out = append(out, "r:"+name)
		}
	}
	sort.Strings(out)
	return out
}

func TestReaderReferences(t *testing.T) {
	cases := []struct {
		sql  string
		want []string
	}{
		{"select id from accounts where id = $1", []string{"r:accounts"}},
		{"SELECT a.id FROM Accounts a JOIN posts p ON p.author_id = a.id LEFT OUTER JOIN tags t USING (id), post_tags pt;", []string{"r:accounts", "r:post_tags", "r:posts", "r:tags"}},
		{`select * from "Mixed" m, public."Mixed"`, []string{"r:Mixed", "r:public.Mixed"}},
		{"select * from audit.events", []string{"r:audit.events"}},
		{"insert into tags (name) values ($1) on conflict (name) do update set name = excluded.name returning id", []string{"w:tags:upsert"}},
		{"insert into tags as t (name) values ($1) on conflict do nothing", []string{"w:tags:insert"}},
		{"insert into post_tags (post_id, tag_id) select $1::int, id from tags where name = any($2)", []string{"r:tags", "w:post_tags:insert"}},
		{"update posts set title = $1 from accounts a where a.id = posts.author_id", []string{"r:accounts", "w:posts:update"}},
		{"update posts p set views = q.views from posts q where q.id = p.id", []string{"r:posts", "w:posts:update"}},
		{"delete from only comments c using posts p where p.id = c.post_id returning c.id", []string{"r:posts", "w:comments:delete"}},
		{"with recent as (select id from posts) select * from recent join accounts on true", []string{"cte:recent", "r:accounts", "r:posts"}},
		{"with moved as (delete from comments where id = $1 returning *) insert into comments_archive select * from moved", []string{"cte:moved", "w:comments:delete", "w:comments_archive:insert"}},
		{"with recursive t(n) as (select 1 union all select n + 1 from t where n < 3) select * from t", []string{"cte:t", "cte:t"}},
		// Non-recursive: a CTE does not see itself, so the inner name is a table.
		{"with t as (select * from t) select * from t", []string{"cte:t", "r:t"}},
		{"select (select count(*) from comments c where c.post_id = p.id) from posts p where exists (select 1 from tags) and p.id in (select post_id from post_tags)", []string{"r:comments", "r:post_tags", "r:posts", "r:tags"}},
		{"select id from accounts where id = $1 for update", []string{"r:accounts"}},
		{"select * from unnest($1::int[]) as u(id) join posts on posts.id = u.id", []string{"r:posts"}},
		{"select * from lateral (select 1) x, accounts", []string{"r:accounts"}},
		{"select extract(year from created_at) from posts where a is distinct from b", []string{"r:posts"}},
		{"select now()", nil},
		{"select x::timestamp with time zone from posts", []string{"r:posts"}},
		{"select * from (accounts a join posts p on true)", []string{"r:accounts", "r:posts"}},
		{"select 1 from a union select 2 from b except select 3 from c", []string{"r:a", "r:b", "r:c"}},
		{"-- comment ; here\nselect /* nested /* c */ ; */ 'a;b', $$x;y$$, E'\\';' from posts", []string{"r:posts"}},
		{"select * from posts where title = 'from accounts'", []string{"r:posts"}},
		// R5-B1: a reserved word Postgres reads as a function is no table;
		// quoted, or after a schema, it is a name.
		{"select * from user", nil},
		{"select * from current_user u join posts on true, session_user, current_date, localtimestamp, system_user", []string{"r:posts"}},
		{"select current_schema from current_schema()", nil},
		{`select id from "user"`, []string{"r:user"}},
		{"select id from public.user", []string{"r:public.user"}},
		{`insert into "user" (id) select 1 from user`, []string{"w:user:insert"}},
	}
	for _, c := range cases {
		r := readSQL(c.sql)
		if r.Refusal != "" {
			t.Errorf("%q refused: %s", c.sql, r.Refusal)
			continue
		}
		if got := refsText(r); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %v, want %v", c.sql, got, c.want)
		}
	}
}

func TestReaderRefusals(t *testing.T) {
	cases := map[string]string{
		"create table x (id int)": "statement kind create",
		"truncate posts":          "statement kind truncate",
		"merge into t using s on true when matched then delete": "statement kind merge",
		"select 1; select 2":                                          "several statements",
		"select * from a.b.c":                                         "a 3-part name",
		"select * into newtab from posts":                             "SELECT INTO",
		"select * from posts tablesample system (1)":                  "tablesample in a FROM list",
		"select 'unterminated":                                        "unterminated string",
		"select * from lateral foo":                                   "LATERAL form",
		"with t as (select 1) search depth first by n set o select 1": "CTE search clause",
		"(select 1)":                            "statement kind",
		"values (1)":                            "statement kind values",
		"insert into t select 1 from (select 1": "unbalanced parentheses",
		"":                                      "no statement",
		"insert into user (id) values (1)":      "user is a function, not a table",
		"update current_user set x = 1":         "current_user is a function, not a table",
		"select * from user.accounts":           "user is a function, not a table",
	}
	for sql, want := range cases {
		r := readSQL(sql)
		if !strings.Contains(r.Refusal, want) || r.Refs != nil {
			t.Errorf("%q: refusal %q refs %v, want %q", sql, r.Refusal, r.Refs, want)
		}
	}
}

func TestReaderForms(t *testing.T) {
	r := readSQL("select id from accounts for no key update")
	if !reflect.DeepEqual(r.Forms, []string{"FROM", "NO KEY UPDATE", "SELECT"}) {
		t.Errorf("forms %v", r.Forms)
	}
	r = readSQL("select id from accounts for key share")
	if !reflect.DeepEqual(r.Forms, []string{"FROM", "KEY SHARE", "SELECT"}) {
		t.Errorf("forms %v", r.Forms)
	}
}
