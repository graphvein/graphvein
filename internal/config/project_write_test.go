package config

import (
	"strings"
	"testing"
)

// TestAddProjectConsume covers the writer behind suggest --accept: a new
// consumes list, an existing (flow) list, a scope's list, comments kept,
// and refusals for a member or scope the manifest does not have.
func TestAddProjectConsume(t *testing.T) {
	manifest := "# project comment\nversion: 1\nname: p\nmembers:\n  - repository: web # the frontend\n    path: web\n    scopes:\n      - name: admin\n        path: admin\n  - repository: api\n    path: api\n    consumes: [web]\n"
	c := Consume{Repository: "api", Select: Selector{Bases: []string{"VITE_API"}, Origins: []string{"http://localhost:3100"}}}
	got, err := AddProjectConsume([]byte(manifest), "web", "", c)
	if err != nil {
		t.Fatal(err)
	}
	want := "# project comment\nversion: 1\nname: p\nmembers:\n  - repository: web # the frontend\n    path: web\n    scopes:\n      - name: admin\n        path: admin\n    consumes:\n      - repository: api\n        select:\n          bases: [VITE_API]\n          origins: ['http://localhost:3100']\n  - repository: api\n    path: api\n    consumes: [web]\n"
	if string(got) != want {
		t.Fatalf("new list:\n%s\nwant\n%s", got, want)
	}
	got, err = AddProjectConsume([]byte(manifest), "web", "admin", Consume{Repository: "api", Select: Selector{PathPrefixes: []string{"/admin"}}})
	if err != nil || !strings.Contains(string(got), "        path: admin\n        consumes:\n          - repository: api\n            select:\n              path_prefixes: [/admin]\n") {
		t.Fatalf("scope list:\n%s %v", got, err)
	}
	got, err = AddProjectConsume([]byte(manifest), "api", "", Consume{Repository: "web", Scope: "admin"})
	if err != nil || !strings.Contains(string(got), "consumes: [web, {repository: web, scope: admin}]") {
		t.Fatalf("existing flow list:\n%s %v", got, err)
	}
	// default_scope is written as a boolean and reads back.
	got, err = AddProjectConsume([]byte(manifest), "api", "", Consume{Repository: "web", DefaultScope: true})
	if err != nil || !strings.Contains(string(got), "consumes: [web, {repository: web, default_scope: true}]") {
		t.Fatalf("default scope entry:\n%s %v", got, err)
	}
	if p, err := ParseProject(t.TempDir(), got, nil); err != nil || !p.Members[1].Consumes[1].DefaultScope {
		t.Fatalf("default scope entry does not read back: %+v %v", p, err)
	}
	for _, c := range []struct{ member, scope string }{{"missing", ""}, {"web", "missing"}, {"api", "admin"}} {
		if _, err := AddProjectConsume([]byte(manifest), c.member, c.scope, Consume{Repository: "web"}); err == nil {
			t.Errorf("%v: no error", c)
		}
	}
}

// editManifestFixture has a comment on every level the edits touch.
const editManifestFixture = `# project comment
version: 1
name: p
members:
  # the frontend
  - repository: web # web line
    path: web
    consumes:
      - api # whole api
    scopes:
      - name: admin # admin scope
        path: admin
        consumes: [api]
  - repository: api
    path: api
    scopes:
      - name: users
        path: apps/users
`

// TestManifestEditsKeepComments covers the writers behind the UI's
// manifest edits (T022 B10): each keeps every comment it does not remove,
// writes only what it names, and reads back through ParseProject.
func TestManifestEditsKeepComments(t *testing.T) {
	data := []byte(editManifestFixture)
	cases := []struct {
		name string
		edit func() ([]byte, error)
		want []string // substrings of the result
		gone []string // substrings that must be gone
	}{
		{"select on a scalar entry", func() ([]byte, error) {
			return SetProjectConsumeSelect(data, "web", "", ConsumeTarget{Repository: "api"}, Selector{Bases: []string{"VITE_API"}})
		}, []string{"      - repository: api # whole api\n        select:\n          bases: [VITE_API]\n"}, nil},
		{"select cleared", func() ([]byte, error) {
			d, err := SetProjectConsumeSelect(data, "web", "", ConsumeTarget{Repository: "api"}, Selector{Origins: []string{"http://localhost:3000"}})
			if err != nil {
				return nil, err
			}
			return SetProjectConsumeSelect(d, "web", "", ConsumeTarget{Repository: "api"}, Selector{})
		}, []string{"      - repository: api # whole api\n    scopes:"}, []string{"select"}},
		{"default scope on and off", func() ([]byte, error) {
			return SetProjectConsumeDefaultScope(data, "web", "", "api", true)
		}, []string{"      - repository: api # whole api\n        default_scope: true\n"}, nil},
		{"remove the last entry", func() ([]byte, error) {
			return RemoveProjectConsume(data, "web", "", ConsumeTarget{Repository: "api"})
		}, []string{"    path: web\n    scopes:\n"}, []string{"whole api"}},
		{"remove a scope's entry", func() ([]byte, error) {
			return RemoveProjectConsume(data, "web", "admin", ConsumeTarget{Repository: "api"})
		}, []string{"      - name: admin # admin scope\n        path: admin\n  - repository: api"}, nil},
		{"add a scope", func() ([]byte, error) {
			return AddProjectScope(data, "api", Scope{Name: "orders", Path: "apps/orders"})
		}, []string{"        path: apps/users\n      - name: orders\n        path: apps/orders\n"}, nil},
		{"add the first scope", func() ([]byte, error) {
			d, err := RemoveProjectScope(data, "api", "users")
			if err != nil {
				return nil, err
			}
			return AddProjectScope(d, "api", Scope{Name: "orders", Path: "apps/orders"})
		}, []string{"    path: api\n    scopes:\n      - name: orders\n"}, []string{"users"}},
		{"rename a repository", func() ([]byte, error) {
			return RenameProjectRepository(data, "api", "backend")
		}, []string{"      - backend # whole api\n", "consumes: [backend]", "  - repository: backend\n"}, []string{"- api", "[api]", "repository: api"}},
	}
	for _, c := range cases {
		got, err := c.edit()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		s := string(got)
		for _, comment := range []string{"# project comment", "# the frontend", "# web line", "# admin scope"} {
			if !strings.Contains(s, comment) {
				t.Errorf("%s: lost %q:\n%s", c.name, comment, s)
			}
		}
		for _, w := range c.want {
			if !strings.Contains(s, w) {
				t.Errorf("%s: want %q in:\n%s", c.name, w, s)
			}
		}
		for _, g := range c.gone {
			if strings.Contains(s, g) {
				t.Errorf("%s: %q still in:\n%s", c.name, g, s)
			}
		}
		if _, err := ParseProject(t.TempDir(), got, nil); err != nil {
			t.Errorf("%s: result does not validate: %v\n%s", c.name, err, s)
		}
	}
	// Turning default_scope off restores the original entry's meaning.
	on, _ := SetProjectConsumeDefaultScope(data, "web", "", "api", true)
	off, err := SetProjectConsumeDefaultScope(on, "web", "", "api", false)
	if err != nil || strings.Contains(string(off), "default_scope") {
		t.Errorf("default scope off: %v\n%s", err, off)
	}
	// Refusals name what is missing and write nothing.
	for name, edit := range map[string]func() ([]byte, error){
		"no such entry":  func() ([]byte, error) { return RemoveProjectConsume(data, "api", "", ConsumeTarget{Repository: "web"}) },
		"no such member": func() ([]byte, error) { return RenameProjectRepository(data, "ghost", "x") },
		"no such scope":  func() ([]byte, error) { return RemoveProjectScope(data, "web", "users") },
		"default off without the entry": func() ([]byte, error) {
			return SetProjectConsumeDefaultScope(data, "web", "", "api", false)
		},
	} {
		if got, err := edit(); err == nil {
			t.Errorf("%s: no error:\n%s", name, got)
		}
	}
	// Removing a scope another entry names leaves an invalid manifest that
	// validation reports (the server refuses it before writing).
	d, err := RemoveProjectScope([]byte(strings.Replace(editManifestFixture, "      - api # whole api\n", "      - {repository: api, scope: users}\n", 1)), "api", "users")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProject(t.TempDir(), d, nil); err == nil || !strings.Contains(err.Error(), "does not declare") {
		t.Errorf("dangling scope reference: %v", err)
	}
}
