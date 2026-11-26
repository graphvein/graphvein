package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func h(w http.ResponseWriter, r *http.Request) {}

// API routes every group below a base read from configuration (rows
// UP-1 of the route side; the matcher rows UP-13..UP-20 read the entries).
func API(base, other string) http.Handler {
	r := chi.NewRouter()
	r.Route(base, func(r chi.Router) {
		r.Get("/upb/orders/{id}", h)
		r.Post("/upb/orders", h)
		r.Get("/upbping", h)
		r.Get("/upb/shared/list", h)
		r.Get("/upb/stats/daily", h)
		r.Get("/daily", h)
		r.Get("/{org}/upb/settings", h)
		r.Get("/upb/files/", h)
		r.Get("/upb/known/items", h)
	})
	r.Route(other, func(r chi.Router) {
		r.Get("/upb/shared/list", h)
	})
	r.Route("/outer", func(r chi.Router) {
		r.Route(base, func(r chi.Router) {
			r.Get("/upb/inner/{name}", h)
		})
	})
	return r
}

// Known mounts a router at a read prefix: its route has a full path.
func Known() http.Handler {
	sub := chi.NewRouter()
	sub.Get("/upb/known/items", h)
	r := chi.NewRouter()
	r.Mount("/k", sub)
	return r
}

// Reports strips a configured prefix in front of a net/http mux, whose GET
// routes also answer HEAD.
func Reports(prefix string) http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /upb/reports/{id}", h)
	root := http.NewServeMux()
	root.Handle(prefix+"/", http.StripPrefix(prefix, api))
	return root
}

func Serve(base, other, prefix string) {
	http.ListenAndServe(":9101", API(base, other))
	http.ListenAndServe(":9102", Known())
	http.ListenAndServe(":9103", Reports(prefix))
}

// Widgets routes below a configured base, and Fallback serves a known
// final catch-all (an SPA fallback) at /k/app: a call the catch-all takes
// that a widgets route matches below the unknown prefix is ambiguous (row
// UP-22).
func Widgets(base string) http.Handler {
	r := chi.NewRouter()
	r.Route(base, func(r chi.Router) {
		r.Get("/upb/widgets/list", h)
	})
	return r
}

func Fallback() http.Handler {
	sub := chi.NewRouter()
	sub.Get("/app/*", h)
	r := chi.NewRouter()
	r.Mount("/k", sub)
	return r
}
