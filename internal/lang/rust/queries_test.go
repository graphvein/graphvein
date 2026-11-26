package rust

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// rustSites renders a file's query sites as "line owner path[!] sql".
func rustSites(t *testing.T, file, source string) []string {
	t.Helper()
	f := extract(t, file, source)
	if !f.Rust.QueriesValid() {
		t.Fatalf("invalid queries: %+v", f.Rust.Queries)
	}
	var out []string
	for _, q := range f.Rust.Queries {
		path := strings.Join(q.Path, "::")
		if q.Macro {
			path += "!"
		}
		sql := "-"
		if q.Literal {
			sql = fmt.Sprintf("%q@%d:%d", q.SQL, q.SQLSpan.StartLine, q.SQLSpan.StartColumn)
		}
		scope := f.Rust.Scopes[q.Scope]
		out = append(out, fmt.Sprintf("%d %s %s %s scope=%s", q.Span.StartLine, q.Owner, path, sql, scope.Function))
	}
	return out
}

func TestSqlxQuerySites(t *testing.T) {
	source := `use sqlx::{query, query_as, PgPool};

struct Repo;

impl Repo {
    async fn find(&self, db: &PgPool) {
        let user = sqlx::query_as!(User, "select id from users where id = $1" /* a, comment */, 1).fetch_one(db).await;
        query!(
            // a line comment, "not sql"
            "insert into users (name) " + r#"values ("a, b")"#,
            name
        ).execute(db).await;
        let n = sqlx::query_scalar_unchecked!("select count(*) from users").fetch_one(db).await;
        let rows = query_as::<_, User>(r"select * from users").fetch_all(db).await;
        let dynamic = sqlx::query(&format!("select * from {}", t)).execute(db).await;
        let constant = sqlx::query!(SQL);
        let concat = sqlx::query!(concat!("select ", "1"));
        app.route("/", get(|| async { sqlx::query("delete from users").execute(db).await }));
    }
}

fn helper() {
    let r = ::sqlx::query_as_with::<_, User, _>("update users set name = $1", args);
    other::query("not sqlx");
    db.query("a method");
    sqlx::query_file!("queries/a.sql");
}
`
	got := rustSites(t, "src/repo.rs", source)
	m := "method:src/repo.rs#Repo.find"
	h := "function:src/repo.rs#helper"
	want := []string{
		`7 ` + m + ` sqlx::query_as! "select id from users where id = $1"@7:41 scope=` + m,
		`8 ` + m + ` query! "insert into users (name) values (\"a, b\")"@10:12 scope=` + m,
		`13 ` + m + ` sqlx::query_scalar_unchecked! "select count(*) from users"@13:46 scope=` + m,
		`14 ` + m + ` query_as "select * from users"@14:39 scope=` + m,
		`15 ` + m + ` sqlx::query - scope=` + m,
		`16 ` + m + ` sqlx::query! - scope=` + m,
		`17 ` + m + ` sqlx::query! - scope=` + m,
		`18 ` + m + ` sqlx::query "delete from users"@18:50 scope=` + m,
		`23 ` + h + ` sqlx::query_as_with "update users set name = $1"@23:48 scope=` + h,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sites:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSqlxQueryGate(t *testing.T) {
	// A single-segment query without a sqlx use is not captured; a sqlx
	// path or a sqlx use passes the gate.
	if got := rustSites(t, "src/a.rs", `fn f() { query("select 1"); query!("select 1"); }`); got != nil {
		t.Fatalf("ungated file captured: %q", got)
	}
	if got := rustSites(t, "src/a.rs", `use sqlx::postgres::PgPool; fn f() { query("select 1"); }`); len(got) != 1 {
		t.Fatalf("sqlx use: %q", got)
	}
	if got := rustSites(t, "src/a.rs", `fn f() { query("select 1"); sqlx::query("select 2"); }`); len(got) != 2 {
		t.Fatalf("sqlx path: %q", got)
	}
	if got := rustSites(t, "src/a.rs", `use sqlx::query; fn f() { let x = 1; }`); got != nil {
		t.Fatalf("no site: %q", got)
	}
}

func TestSqlxQueryImportScope(t *testing.T) {
	f := extract(t, "src/lib.rs", "mod db {\n    use sqlx::query;\n    pub fn run() {\n        { query!(\"select 1\"); }\n    }\n}\n")
	if len(f.Rust.Queries) != 1 {
		t.Fatalf("queries: %+v", f.Rust.Queries)
	}
	q := f.Rust.Queries[0]
	found := false
	for s := q.Scope; s >= 0; s = f.Rust.Scopes[s].Parent {
		for _, imp := range f.Rust.Scopes[s].Imports {
			found = found || imp.Bindings["query"] == "sqlx::query"
		}
	}
	if !found || q.Owner != "function:src/lib.rs#db.run" {
		t.Fatalf("use sqlx::query not reachable from the site scope: %+v", q)
	}
}
