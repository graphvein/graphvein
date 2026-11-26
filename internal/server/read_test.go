package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/graphvein/graphvein/internal/store"
)

// TestReader checks the in-process reader of T030 D3: a view answers the
// same JSON as over HTTP, a view's error answer is a *ReadError with its
// code, and a path outside ReadPaths (the re-index runner, a writing
// change endpoint, try-it) is refused before anything is read.
func TestReader(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := fixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		r := NewReader(opt)
		defer r.Close()
		got, err := r.Read(context.Background(), "/api/v0/status", nil)
		if err != nil {
			t.Fatal(err)
		}
		want := get(t, opt, http.MethodGet, "/api/v0/status", nil)
		var a, b any
		if json.Unmarshal(got, &a) != nil || json.Unmarshal(want.Body.Bytes(), &b) != nil {
			t.Fatal("status bodies do not decode")
		}
		if !jsonEqual(a, b) {
			t.Errorf("reader status %s\nHTTP status %s", got, want.Body.String())
		}
		_, err = r.Read(context.Background(), "/api/v0/api/endpoint", url.Values{"id": {"not an identity"}})
		var re *ReadError
		if !errors.As(err, &re) || re.Code != "bad_request" || re.Status != http.StatusBadRequest {
			t.Errorf("bad endpoint id: %v", err)
		}
		for _, path := range []string{indexRunPath, "/api/v0/changes/pins", "/api/v0/changes/pins/delete", "/api/v0/changes/refs", "/api/v0/tryit/send", "/api/v0/project/manifest", "/api/v0/explorer/path-values"} {
			_, err := r.Read(context.Background(), path, nil)
			if !errors.As(err, &re) || re.Code != "not_found" {
				t.Errorf("%s: %v, want refused", path, err)
			}
		}
	})
	// No readable view accepts a write: none is a POST path.
	for _, p := range ReadPaths {
		if postPaths[p] {
			t.Errorf("ReadPaths lists %s, which accepts POST", p)
		}
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
