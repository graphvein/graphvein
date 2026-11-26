package lockfile

import "testing"

// doc is one pnpm 9 lockfile document with one importer.
func doc(importer, orm, kit string) string {
	return "lockfileVersion: '9.0'\n\nimporters:\n\n  " + importer + ":\n    dependencies:\n      drizzle-orm:\n        specifier: ^" + orm + "\n        version: " + orm + "(postgres@3.4.7)\n    devDependencies:\n      drizzle-kit:\n        specifier: ^" + kit + "\n        version: " + kit + "\n"
}

// env is pnpm's environment document, written before the lockfile
// document by recent pnpm versions (kaneo, kftray).
const env = "---\nlockfileVersion: '9.0'\n\nimporters:\n\n  .:\n    configDependencies: {}\n    packageManagerDependencies:\n      pnpm:\n        specifier: 10.18.0\n        version: 10.18.0\n\n---\n"

// TestPnpmDocuments: every YAML document is read; the documents that lock
// the dependency must agree; broken YAML gives no version.
func TestPnpmDocuments(t *testing.T) {
	data := []byte(env + doc("apps/api", "0.45.4", "0.31.11"))
	if v := Pnpm(data, "apps/api", "drizzle-orm"); v != "0.45.4" {
		t.Errorf("drizzle-orm %q", v)
	}
	if v := Pnpm(data, "apps/api", "drizzle-kit"); v != "0.31.11" {
		t.Errorf("drizzle-kit %q", v)
	}
	if v := Pnpm([]byte(doc(".", "0.45.2", "0.31.9")), ".", "drizzle-orm"); v != "0.45.2" {
		t.Errorf("single document %q", v)
	}
	two := []byte(doc("apps/api", "0.45.4", "0.31.11") + "---\n" + doc("apps/api", "0.45.2", "0.31.11"))
	if v := Pnpm(two, "apps/api", "drizzle-orm"); v != "" {
		t.Errorf("disagreeing documents give %q", v)
	}
	if v := Pnpm([]byte("a: [\n"), "apps/api", "drizzle-orm"); v != "" {
		t.Errorf("broken YAML gives %q", v)
	}
	if v := Pnpm(nil, ".", "drizzle-orm"); v != "" {
		t.Errorf("empty lockfile gives %q", v)
	}
}

// TestLocked: the lockfile beside the package wins, else the nearest one
// above it reads the package's importer.
func TestLocked(t *testing.T) {
	sources := map[string][]byte{"pnpm-lock.yaml": []byte(env + doc("apps/api", "0.45.4", "0.31.11"))}
	v, lf, why := Locked(sources, "apps/api", "drizzle-orm", "^0.45.4")
	if v != "0.45.4" || lf != "pnpm-lock.yaml" || why != "" {
		t.Errorf("workspace root: %q %q %q", v, lf, why)
	}
	if v, _, why := Locked(map[string][]byte{}, "apps/api", "drizzle-orm", "^0.45.4"); v != "" || why == "" {
		t.Errorf("no lockfile: %q %q", v, why)
	}
}

func TestMajorAndAtLeast(t *testing.T) {
	for v, want := range map[string]int{"0.45.4": 0, "^1.0.0-rc.1": 1, "~7.1": 7, ">=1": -1, "latest": -1} {
		if got := Major(v); got != want {
			t.Errorf("Major(%q) = %d, want %d", v, got, want)
		}
	}
	if !AtLeast("7.10.0", 7, 10) || AtLeast("7.9.1", 7, 10) || AtLeast("^7.10.0", 7, 10) {
		t.Error("AtLeast")
	}
}

// TestYarnKeyLists: classic quotes each key of a list, Berry quotes the
// whole list (Maintainerr's typescript entry, T018g6).
func TestYarnKeyLists(t *testing.T) {
	berry := "__metadata:\n  version: 8\n\n\"typescript@npm:^6.0.3, typescript@npm:~6.0.2\":\n  version: 6.0.3\n  resolution: \"typescript@npm:6.0.3\"\n\n\"typescript@patch:typescript@npm%3A^6.0.3#optional!builtin<compat/typescript>\":\n  version: 6.0.3\n"
	classic := "\"@babel/core@^7.0.0\", \"@babel/core@^7.12.3\":\n  version \"7.12.3\"\n"
	for _, c := range []struct{ data, name, spec, want string }{
		{berry, "typescript", "^6.0.3", "6.0.3"},
		{berry, "typescript", "~6.0.2", "6.0.3"},
		{classic, "@babel/core", "^7.12.3", "7.12.3"},
		{classic, "@babel/core", "^7.0.1", ""},
	} {
		if got := yarnVersion([]byte(c.data), c.name, c.spec); got != c.want {
			t.Errorf("%s@%s = %q, want %q", c.name, c.spec, got, c.want)
		}
	}
}

// TestParseGoMod: module, single-line and block requires with the indirect
// comment, quoted paths, replace directives in both forms, and lines it
// cannot read skipped.
func TestParseGoMod(t *testing.T) {
	src := "// comment\nmodule \"example.com/app/v3\"\n\ngo 1.24\n\nrequire gorm.io/gorm v1.31.2\n\nrequire (\n\tgorm.io/driver/sqlite v1.6.0\n\tgithub.com/jinzhu/inflection v1.0.0 // indirect\n\tbroken\n)\n\nreplace gorm.io/driver/mysql => ../mysql\nreplace (\n\tgorm.io/gorm v1.31.2 => gorm.io/gorm v1.30.0\n)\nexclude gorm.io/gorm v1.20.0\n"
	m := ParseGoMod([]byte(src))
	if m.Module != "example.com/app/v3" || m.Line != 2 {
		t.Errorf("module %q at %d", m.Module, m.Line)
	}
	want := []GoRequire{
		{Path: "gorm.io/gorm", Version: "v1.31.2", Line: 6},
		{Path: "gorm.io/driver/sqlite", Version: "v1.6.0", Line: 9},
		{Path: "github.com/jinzhu/inflection", Version: "v1.0.0", Indirect: true, Line: 10},
	}
	if len(m.Requires) != len(want) {
		t.Fatalf("requires %+v", m.Requires)
	}
	for i, r := range want {
		if m.Requires[i] != r {
			t.Errorf("require %d: %+v, want %+v", i, m.Requires[i], r)
		}
	}
	reps := []GoReplace{
		{Path: "gorm.io/driver/mysql", NewPath: "../mysql", Line: 14},
		{Path: "gorm.io/gorm", Version: "v1.31.2", NewPath: "gorm.io/gorm", NewVersion: "v1.30.0", Line: 16},
	}
	if len(m.Replaces) != len(reps) || m.Replaces[0] != reps[0] || m.Replaces[1] != reps[1] {
		t.Errorf("replaces %+v", m.Replaces)
	}
}
