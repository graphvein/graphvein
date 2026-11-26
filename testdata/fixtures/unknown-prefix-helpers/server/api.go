package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func h(w http.ResponseWriter, r *http.Request) {}

// API serves every group below a base read from configuration, as dozzle's
// createRouter does with r.Route(base, ...): the routes have no path, only
// unresolved_prefix_paths entries (row UP-1).
func API(base, other string) http.Handler {
	r := chi.NewRouter()
	r.Route(base, func(r chi.Router) {
		r.Get("/upc/api/version", h)
		r.Patch("/upc/api/profile", h)
		r.Get("/upc/hosts/{host}/logs", h)
		r.Get("/upchealth", h)
		r.Get("/upc/shared/list", h)
	})
	r.Route(other, func(r chi.Router) {
		r.Get("/upc/shared/list", h)
	})
	r.Route("/outer", func(r chi.Router) {
		r.Route(base, func(r chi.Router) {
			r.Get("/upc/inner/{name}", h)
		})
	})
	return r
}

// Known mounts a router at a read prefix: its route has a full path.
func Known() http.Handler {
	sub := chi.NewRouter()
	sub.Get("/upc/known/items", h)
	r := chi.NewRouter()
	r.Mount("/k", sub)
	return r
}

func Serve(base, other string) {
	http.ListenAndServe(":9111", API(base, other))
	http.ListenAndServe(":9112", Known())
}
