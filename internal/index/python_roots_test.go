package index_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

func TestPythonImplicitRootsGoldenAndInitializerReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/python-implicit-roots")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	before, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/python-implicit-roots.golden.json", moduleSnapshot(t, snapshot))
	// Verify the handler edge too; the compact module golden records its route.
	found := false
	for _, e := range snapshot.Edges {
		if e.Kind == "exposes_route" && e.Source == "function:mysite/polls/views.py#view" && e.Target == "route:mysite/polls/api.py#GET%20%2Flist" {
			found = true
		}
	}
	if !found {
		t.Fatal("implicit handler did not expose its route")
	}
	again, warm, writes := emit(t, root, s, opt)
	if string(before) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatalf("warm replay: %+v %+v", warm, writes)
	}
	// Making levels a regular package removes the deeper implicit root. Only
	// that new file is parsed; deleting it must restore links from cached callers.
	initializer := filepath.Join(root, "two/levels/__init__.py")
	for _, add := range []bool{true, false} {
		if add {
			if err := os.WriteFile(initializer, []byte("# new parent package\n"), 0644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Remove(initializer); err != nil {
			t.Fatal(err)
		}
		_, changed, _ := emit(t, root, s, opt)
		wantParsed := 0
		if add {
			wantParsed = 1
		}
		if changed.Parsed != wantParsed || changed.Reused != first.Parsed {
			t.Fatalf("initializer add=%v: %+v", add, changed)
		}
		after, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		linked := false
		for _, e := range after.Edges {
			if e.Kind == "calls" && e.Source == "function:two/levels/deep/caller.py#run" && e.Target == "function:two/levels/deep/tools.py#work" {
				linked = true
			}
		}
		if linked == add {
			t.Fatalf("stale implicit root after initializer add=%v", add)
		}
		fresh := open(t)
		emit(t, root, fresh, opt)
		complete, err := fresh.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(after, complete) {
			t.Fatalf("initializer add=%v: replay differs from fresh index", add)
		}
	}
}

func TestPythonSelfIndexLostCallSites(t *testing.T) {
	// Nest the existing fixture exactly as in self-index, without indexing the
	// whole repository during unit tests.
	root := t.TempDir()
	nested := filepath.Join(root, "testdata/repos/python")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(nested, os.DirFS("../../testdata/repos/python")); err != nil {
		t.Fatal(err)
	}
	// Include this task's metadata fixture: explicit sibling roots participate
	// even when the importer belongs to a different nested fixture.
	implicitFixture := filepath.Join(root, "testdata/fixtures/python-implicit-roots")
	if err := os.MkdirAll(implicitFixture, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(implicitFixture, os.DirFS("../../testdata/fixtures/python-implicit-roots")); err != nil {
		t.Fatal(err)
	}
	s := open(t)
	emit(t, root, s, options(t))
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{40: "function:testdata/repos/python/pkg/util.py#help", 41: "function:testdata/repos/python/pkg/util.py#help", 43: "function:testdata/repos/python/pkg/util.py#help", 46: "method:testdata/repos/python/pkg/util.py#Helper.method"}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" && e.Source == "function:testdata/repos/python/pkg/main.py#run" {
			for _, evidence := range e.Evidence {
				if target, ok := want[evidence.Span.StartLine]; ok && target == e.Target {
					delete(want, evidence.Span.StartLine)
				}
			}
		}
	}
	if len(want) != 0 {
		t.Fatal("lost self-index sites", want)
	}
}
