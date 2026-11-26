package mounts

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func list(w http.ResponseWriter, r *http.Request) {}

// Router serves every API route below a base read from configuration:
// one value for the r.Route(base, ...) hop places all of them.
func Router(base string) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", list)
	r.Route(base, func(r chi.Router) {
		r.Get("/", list)
		r.Get("/api/items", list)
		r.Route("/api/users", func(r chi.Router) {
			r.Get("/{id}", list)
		})
	})
	return r
}

// Twin has two hops with the same text in one function: no value can be
// tied to either.
func Twin(base string) http.Handler {
	r := chi.NewRouter()
	r.Route(base, func(r chi.Router) {
		r.Get("/first", list)
	})
	r.Route(base, func(r chi.Router) {
		r.Get("/second", list)
	})
	return r
}

// Nested has its hop below a constant prefix: the value is only the
// hop's part; /outer is read and composed above it.
func Nested(base string) http.Handler {
	r := chi.NewRouter()
	r.Route("/outer", func(r chi.Router) {
		r.Route(base, func(r chi.Router) {
			r.Get("/n", list)
		})
	})
	return r
}

func Serve(base string) {
	http.ListenAndServe(":8080", Router(base))
	http.ListenAndServe(":8081", Twin(base))
	http.ListenAndServe(":8084", Nested(base))
}
