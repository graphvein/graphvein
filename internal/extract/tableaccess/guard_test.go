package tableaccess

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestStepGuard checks R5-B3: a panic in one file's step is a
// table_access_internal_error diagnostic for that file, the file's partial
// result is dropped, and every other file is still linked as before.
func TestStepGuard(t *testing.T) {
	cases := []struct{ root, step, file string }{
		{fixtureRoot, "sqlx", "app/src/repo.rs"},
		{sqlmodelFixtureRoot, "sqlmodel", "app/crud.py"},
		{prismaFixtureRoot, "prisma", "src/relations.ts"},
		{drizzleFixtureRoot, "drizzle", "src/select.ts"},
	}
	for _, c := range cases {
		t.Run(c.step, func(t *testing.T) {
			in := loadRoot(t, filepath.FromSlash(c.root))
			clean := analyze(in)
			stepHook = func(step, file string) {
				if step == c.step && file == c.file {
					panic("injected")
				}
			}
			t.Cleanup(func() { stepHook = nil })
			failed := analyze(in)
			if len(failed.diagnostics) != 1 {
				t.Fatalf("diagnostics %+v, want one", failed.diagnostics)
			}
			d := failed.diagnostics[0]
			if d.File != c.file || d.Code != InternalErrorCode || !strings.Contains(d.Message, "injected") || !strings.Contains(d.Message, c.step+" step") {
				t.Errorf("diagnostic %+v", d)
			}
			var want, dropped int
			for _, s := range clean.sites {
				if s.file == c.file {
					dropped++
				} else {
					want++
				}
			}
			if dropped == 0 {
				t.Fatalf("%s has no site in the clean run", c.file)
			}
			for _, s := range failed.sites {
				if s.file == c.file {
					t.Errorf("a site of the failed file was kept: %s", siteKey(s))
				}
			}
			if len(failed.sites) != want {
				t.Errorf("%d sites of the other files, want %d", len(failed.sites), want)
			}
			if out := Link(in); len(out.Diagnostics) != 1 {
				t.Errorf("Link diagnostics %+v", out.Diagnostics)
			}
		})
	}
}
