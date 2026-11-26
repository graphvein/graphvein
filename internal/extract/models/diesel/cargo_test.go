package diesel

import (
	"strings"
	"testing"
)

// lockOf writes a Cargo.lock: each entry is "name version dep,dep"
// (a "-" for no dependencies), registry-sourced unless the name starts
// with "ws:" (a workspace or path package).
func lockOf(entries ...string) string {
	var b strings.Builder
	b.WriteString("version = 4\n\n")
	for _, e := range entries {
		f := strings.Fields(e)
		name := f[0]
		b.WriteString("[[package]]\nname = \"" + strings.TrimPrefix(name, "ws:") + "\"\nversion = \"" + f[1] + "\"\n")
		if !strings.HasPrefix(name, "ws:") {
			b.WriteString("source = \"registry+https://github.com/rust-lang/crates.io-index\"\n")
		}
		if len(f) > 2 && f[2] != "-" {
			b.WriteString("dependencies = [\n")
			for _, d := range strings.Split(f[2], ",") {
				b.WriteString(" \"" + d + "\",\n")
			}
			b.WriteString("]\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// TestDialectCompanions covers the Revision 1 addendum OP2: backends that
// the known companion crates enable count in the dialect union, and a
// crate whose feature map is not known makes the dialect unknown.
func TestDialectCompanions(t *testing.T) {
	const app = "[package]\nname = \"app\"\n[dependencies]\n"
	base := func(manifest, lock string, extra ...string) map[string]string {
		m := map[string]string{"diesel.toml": "[print_schema]\nfile = \"src/schema.rs\"\n", "src/schema.rs": schemaRS,
			"Cargo.toml": app + manifest, "Cargo.lock": lock}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}
	cases := []struct {
		name          string
		files         map[string]string
		dialect, why  string
		wantAssumeDia bool
	}{
		{"diesel-async postgres feature", base("diesel = \"2.3\"\ndiesel-async = { version = \"0.9\", features = [\"postgres\", \"deadpool\"] }\n",
			lockOf("diesel 2.3.14 -", "diesel-async 0.9.2 diesel", "ws:app 0.1.0 diesel,diesel-async")), "postgres", "", true},
		{"diesel-async renamed, sqlite since 0.5", base("diesel = \"2.3\"\nda = { package = \"diesel-async\", version = \"0.5\", features = [\"sqlite\"] }\n",
			lockOf("diesel 2.2.0 -", "diesel-async 0.5.2 diesel", "ws:app 0.1.0 diesel,diesel-async")), "sqlite", "", true},
		{"diesel-async feature forwarded by [features]", base("diesel = { version = \"2.3\", features = [\"postgres\"] }\ndiesel-async = \"0.9\"\n[features]\nmy = [\"diesel-async/mysql\"]\n",
			lockOf("diesel 2.3.14 -", "diesel-async 0.9.2 diesel", "ws:app 0.1.0 diesel,diesel-async")), "unknown", "several Diesel backends are enabled in the workspace (mysql, postgres)", false},
		{"diesel-async with no backend feature adds none", base("diesel = { version = \"2.3\", features = [\"mysql\"] }\ndiesel-async = \"0.9\"\n",
			lockOf("diesel 2.3.14 -", "diesel-async 0.9.2 diesel", "ws:app 0.1.0 diesel,diesel-async")), "mysql", "", true},
		{"diesel_full_text_search always postgres", base("diesel = \"2.3\"\ndiesel_full_text_search = { version = \"2.3\", default-features = false }\n",
			lockOf("diesel 2.3.14 -", "diesel_full_text_search 2.3.1 diesel", "ws:app 0.1.0 diesel,diesel_full_text_search")), "postgres", "", true},
		{"diesel_full_text_search with sqlite", base("diesel = { version = \"2.3\", features = [\"sqlite\"] }\ndiesel_full_text_search = \"2.3\"\n",
			lockOf("diesel 2.3.14 -", "diesel_full_text_search 2.3.1 diesel", "ws:app 0.1.0 diesel,diesel_full_text_search")), "unknown", "several Diesel backends are enabled in the workspace (postgres, sqlite)", false},
		{"diesel_migrations postgres feature enables nothing", base("diesel = { version = \"2.3\", features = [\"sqlite\"] }\ndiesel_migrations = { version = \"2.3\", features = [\"postgres\"] }\n",
			lockOf("diesel 2.3.14 -", "diesel_migrations 2.3.2 diesel,migrations_internals", "migrations_internals 2.3.0 -", "ws:app 0.1.0 diesel,diesel_migrations")), "sqlite", "", true},
		{"diesel_migrations used by a crate outside the workspace", base("diesel = { version = \"2.3\", features = [\"sqlite\"] }\n",
			lockOf("diesel 2.3.14 -", "diesel_migrations 2.3.2 diesel", "other 1.0.0 diesel_migrations", "ws:app 0.1.0 diesel,other")), "sqlite", "", true},
		{"unknown crate depending on diesel", base("diesel = { version = \"2.3\", features = [\"postgres\"] }\ndiesel-derive-enum = \"2\"\n",
			lockOf("diesel 2.3.14 -", "diesel-derive-enum 2.1.0 diesel,heck", "ws:app 0.1.0 diesel,diesel-derive-enum")), "unknown",
			"Cargo.lock locks diesel-derive-enum 2.1.0, which depends on diesel; the Diesel backend features it enables are not known", false},
		{"diesel 2.4 mariadb alone", base("diesel = { version = \"2.4\", features = [\"mariadb\"] }\n",
			lockOf("diesel 2.4.0 -", "ws:app 0.1.0 diesel")), "unknown",
			"the workspace enables diesel's MariaDB backend (diesel 2.4), a dialect the claim contract does not read", false},
		{"diesel 2.4 mariadb with postgres", base("diesel = { version = \"2.4\", features = [\"postgres\", \"mariadb_backend\"] }\n",
			lockOf("diesel 2.4.0 -", "ws:app 0.1.0 diesel")), "unknown",
			"several Diesel backends are enabled in the workspace (mariadb, postgres)", false},
		{"diesel 2.4 sqlite-no-std", base("diesel = { version = \"2.4\", features = [\"sqlite-no-std\"] }\n",
			lockOf("diesel 2.4.0 -", "ws:app 0.1.0 diesel")), "sqlite", "", true},
		{"companion at a version not read", base("diesel = { version = \"2.4\", features = [\"postgres\"] }\ndiesel-async = \"0.10\"\n",
			lockOf("diesel 2.4.0 -", "diesel-async 0.10.0 diesel", "ws:app 0.1.0 diesel,diesel-async")), "unknown",
			"Cargo.lock locks diesel-async 0.10.0, which depends on diesel; the Diesel backend features it enables are not known", false},
		{"conditional companion used outside the workspace", base("diesel = { version = \"2.3\", features = [\"postgres\"] }\npool = \"1\"\n",
			lockOf("diesel 2.3.14 -", "diesel-async 0.9.2 diesel", "pool 1.0.0 diesel-async", "ws:app 0.1.0 diesel,pool")), "unknown",
			"Cargo.lock locks pool 1.0.0, which depends on diesel-async; the Diesel backend features it turns on there are not known", false},
		{"two versions of a conditional companion", base("diesel = { version = \"2.3\", features = [\"postgres\"] }\ndiesel-async = \"0.9\"\n",
			lockOf("diesel 2.3.14 -", "diesel-async 0.9.2 diesel", "diesel-async 0.8.0 diesel", "ws:app 0.1.0 diesel,diesel-async")), "unknown",
			"Cargo.lock locks 2 versions of diesel-async; the Diesel backend features each enables are not known", false},
		{"path package outside the workspace depending on diesel", base("diesel = { version = \"2.3\", features = [\"postgres\"] }\nlocal = { path = \"../local\" }\n",
			lockOf("diesel 2.3.14 -", "ws:local 0.1.0 diesel", "ws:app 0.1.0 diesel,local")), "unknown",
			"Cargo.lock locks local 0.1.0, which depends on diesel; the Diesel backend features it enables are not known", false},
		{"member's companion entry inherited from the workspace", base("diesel = { version = \"2.3\" }\n[workspace]\nmembers = [\"worker\"]\n[workspace.dependencies]\ndiesel-async = { version = \"0.9\", features = [\"postgres\"] }\n",
			lockOf("diesel 2.3.14 -", "diesel-async 0.9.2 diesel", "ws:app 0.1.0 diesel", "ws:worker 0.1.0 diesel,diesel-async"),
			"worker/Cargo.toml", "[package]\nname = \"worker\"\n[dependencies]\ndiesel = \"2.3\"\ndiesel-async = { workspace = true }\n"), "postgres", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, diags, _ := discover(t, tc.files)
			if len(d.Files) != 1 {
				t.Fatalf("files %+v diags %q", d.Files, messages(diags))
			}
			f := d.Files[0]
			if f.Dialect != tc.dialect || f.DialectReason != tc.why {
				t.Errorf("dialect %q (%q), want %q (%q)", f.Dialect, f.DialectReason, tc.dialect, tc.why)
			}
			has := false
			for _, a := range f.Assumptions {
				has = has || a == AssumeDialect
			}
			if has != tc.wantAssumeDia {
				t.Errorf("assumptions %q", f.Assumptions)
			}
		})
	}
}

// TestCompanionTable keeps the companion feature maps well formed: every
// dialect is one the backend features name, and no version is listed
// twice for a crate.
func TestCompanionTable(t *testing.T) {
	dialects := map[string]bool{}
	for _, d := range backendFeatures {
		dialects[d] = true
	}
	for name, list := range companions {
		seen := map[string]bool{}
		for _, c := range list {
			for _, v := range c.versions {
				if seen[v] {
					t.Errorf("%s %s listed twice", name, v)
				}
				seen[v] = true
			}
			if c.always != "" && !dialects[c.always] {
				t.Errorf("%s: always %q", name, c.always)
			}
			for f, d := range c.features {
				if !dialects[d] {
					t.Errorf("%s: feature %s maps to %q", name, f, d)
				}
			}
			for _, f := range c.defaults {
				if _, ok := c.features[f]; !ok {
					t.Errorf("%s: default %s is not in the feature map", name, f)
				}
			}
		}
	}
}
