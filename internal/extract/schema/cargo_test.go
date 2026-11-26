package schema

import "testing"

func TestReadSqlxDialect(t *testing.T) {
	root := readCargo("Cargo.toml", []byte(`[workspace]
members = ["a"]

[workspace.dependencies]
sqlx = {
  version = "0.9",
  default-features = false,
  features = [
    "runtime-tokio", # comment
    "sqlite",
  ]
}
db = { package = "sqlx", features = ["postgres"] }
`))
	if !root.readable || !root.workspace || root.pkg {
		t.Fatalf("workspace root: %+v", root)
	}
	for _, tc := range []struct {
		name, manifest, dialect string
		found                   bool
	}{
		{"inline", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { version = \"0.8\", features = [\"postgres\", \"uuid\"] }\n", "postgres", true},
		{"table", "[package]\nname = \"a\"\n[dependencies.sqlx]\nversion = \"0.8\"\nfeatures = [\n  \"mysql\",\n]\n", "mysql", true},
		{"dotted", "[package]\nname = \"a\"\n[dependencies]\nsqlx.version = \"0.8\"\nsqlx.features = [\"sqlite-unbundled\"]\n", "sqlite", true},
		{"renamed", "[package]\nname = \"a\"\n[dev-dependencies]\ndatabase = { package = \"sqlx\", version = \"0.8\", features = [\"postgres\"] }\n", "postgres", true},
		{"not sqlx", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { package = \"other\", features = [\"postgres\"] }\n", "", false},
		{"target", "[package]\nname = \"a\"\n[target.'cfg(unix)'.dependencies]\nsqlx = { version = \"0.8\", features = [\"postgres\"] }\n", "postgres", true},
		{"several", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { version = \"0.8\", features = [\"postgres\"] }\n[dev-dependencies]\nsqlx = { version = \"0.8\", features = [\"sqlite\"] }\n", "unknown", true},
		{"all", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { version = \"0.8\", features = [\"all-databases\"] }\n", "unknown", true},
		{"none", "[package]\nname = \"a\"\n[dependencies]\nsqlx = \"0.8\"\n", "unknown", true},
		{"unreadable features", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { version = \"0.8\", features = \"postgres\" }\n", "unknown", true},
		{"workspace", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { workspace = true }\n", "sqlite", true},
		{"workspace renamed plus member feature", "[package]\nname = \"a\"\n[dependencies]\ndb = { workspace = true, features = [\"postgres\"] }\n", "postgres", true},
		{"workspace adds a second dialect", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { workspace = true, features = [\"postgres\"] }\n", "unknown", true},
		// Revision 3 G7: package features that forward to sqlx.
		{"feature forwards another dialect", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { version = \"0.8\", features = [\"postgres\"] }\n[features]\nlite = [\"sqlx/sqlite\"]\n", "unknown", true},
		{"weak feature forwards another dialect", "[package]\nname = \"a\"\n[dependencies]\ndb = { package = \"sqlx\", version = \"0.8\", features = [\"postgres\"] }\n[features]\nmy = [\"db?/mysql\"]\n", "unknown", true},
		{"feature forwards the same dialect", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { version = \"0.8\", features = [\"postgres\"] }\n[features]\npg = [\"sqlx/postgres\", \"other/sqlite\"]\n", "postgres", true},
		{"unreadable features table", "[package]\nname = \"a\"\n[dependencies]\nsqlx = { version = \"0.8\", features = [\"postgres\"] }\n[features]\npg = \"sqlx/postgres\"\n", "unknown", true},
	} {
		m := readCargo("a/Cargo.toml", []byte(tc.manifest))
		if !m.readable || !m.pkg {
			t.Fatalf("%s: unreadable manifest", tc.name)
		}
		got := readSqlx(m, &root)
		if got.found != tc.found || tc.found && got.dialect != tc.dialect {
			t.Fatalf("%s: %+v", tc.name, got)
		}
	}
	if m := readCargo("x/Cargo.toml", []byte("[package\nname = 1")); m.readable {
		t.Fatal("broken manifest must be unreadable")
	}
	manifests := map[string]manifest{"Cargo.toml": root}
	member := readCargo("crates/a/Cargo.toml", []byte("[package]\nname = \"a\"\n"))
	if ws := findWorkspaceRoot(member, manifests); ws == nil || ws.path != "Cargo.toml" {
		t.Fatalf("workspace root not found: %+v", ws)
	}
}
