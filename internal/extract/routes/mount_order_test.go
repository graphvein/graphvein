package routes

import "testing"

// TestStarletteMountTakes pins the prefix rule against the oracle's
// answers (FastAPI 0.121.1/Starlette 0.49.3, 0.165.0/Starlette 1.10.0).
func TestStarletteMountTakes(t *testing.T) {
	cases := []struct {
		prefix, template string
		want             bool
	}{
		{"/x", "/x/a", true},
		{"/x", "/x", false},
		{"/x", "/x/", true},
		{"/x/", "/x/a", true},
		{"/x", "/{v}/c", false},
		{"/x", "/y/a", false},
		{"/t/{tenant}", "/t/acme/z", true},
		{"", "/", true},
		{"", "/late", true},
		{"/x/b", "/x/a", false},
	}
	for _, c := range cases {
		if got := starletteMountTakes(c.prefix, c.template); got != c.want {
			t.Errorf("starletteMountTakes(%q, %q) = %t, want %t", c.prefix, c.template, got, c.want)
		}
	}
}

// TestEntryOrder checks that order is known only within one function body
// of one file, and that a routes= list entry comes first.
func TestEntryOrder(t *testing.T) {
	a := entrySite{file: "m.py", line: 3}
	b := entrySite{file: "m.py", line: 9}
	if before, known := entryOrder(a, b); !before || !known {
		t.Errorf("same body: before=%t known=%t", before, known)
	}
	if _, known := entryOrder(a, entrySite{file: "n.py", line: 9}); known {
		t.Error("another file: order known")
	}
	if _, known := entryOrder(a, entrySite{file: "m.py", domain: 2, line: 9}); known {
		t.Error("another function: order known")
	}
	if before, known := entryOrder(entrySite{file: "m.py", line: 20, first: true}, a); !before || !known {
		t.Error("routes= list entry not first")
	}
}
