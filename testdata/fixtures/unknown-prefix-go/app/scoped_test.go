package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// scopedHarness mounts the production router scoped at a read prefix: a
// test-scope path (UP-9 applies per scope).
func scopedHarness() http.Handler {
	r := chi.NewRouter()
	r.Mount("/t", scoped())
	return r
}
