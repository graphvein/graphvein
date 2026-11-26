package server

import (
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestBase registers below a configured base in a test file: the entry has
// scope test.
func TestBase(t *testing.T) {
	r := chi.NewRouter()
	r.Route(t.Name(), func(r chi.Router) {
		r.Get("/upb/testonly/items", h)
	})
	httptest.NewServer(r)
}
