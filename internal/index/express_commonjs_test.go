package index_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// TestExpressCommonJSGolden indexes the express-commonjs fixture (T036): a
// router a module exports with CommonJS (`module.exports = router`,
// `module.exports = { router }`, `exports.router = router`) and another
// module mounts with `app.use(p, require("./m"))` composes its paths as
// through an ESM import; every write of the exports object the rows do not
// read, and every require that does not name one module, keeps no path.
func TestExpressCommonJSGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/express-commonjs")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/express-commonjs.golden.json", goRoutesSnapshot(t, snapshot))
}

// TestExpressCommonJSUnscannedGolden indexes the express-commonjs-unscanned
// fixture (T033 review) with its inert names restored (its
// README): a relative require whose earlier candidate in Node's loader
// order exists on disk but was not scanned (ignored by .gitignore or
// .graphveinignore, or a name the scanner always skips) gives no value,
// and the mount's route_mount_unresolved diagnostic names that entry; a
// scanned first candidate still composes.
func TestExpressCommonJSUnscannedGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/express-commonjs-unscanned")); err != nil {
		t.Fatal(err)
	}
	for from, to := range map[string]string{"_graphveinignore": ".graphveinignore", "web/_gitignore": "web/.gitignore", "web/_vendor": "web/vendor"} {
		if err := os.Rename(filepath.Join(root, filepath.FromSlash(from)), filepath.Join(root, filepath.FromSlash(to))); err != nil {
			t.Fatal(err)
		}
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	var mounts []string
	for _, d := range snapshot.Diagnostics {
		if d.Code == "route_mount_unresolved" {
			mounts = append(mounts, d.Message)
		}
	}
	want := []string{
		`require("./routes/u1") (loads web/routes/u1.js, not scanned) at 7:0-7:38`,
		`require("./routes/u2") (loads web/routes/u2.js, not scanned) at 9:0-9:38`,
		`require("./vendor") (loads web/vendor, not scanned) at 12:0-12:35`,
	}
	sort.Strings(mounts)
	if !reflect.DeepEqual(mounts, want) {
		t.Errorf("route_mount_unresolved %q, want %q", mounts, want)
	}
	golden.Check(t, "../../testdata/fixtures/express-commonjs-unscanned.golden.json", goRoutesSnapshot(t, snapshot))
}
