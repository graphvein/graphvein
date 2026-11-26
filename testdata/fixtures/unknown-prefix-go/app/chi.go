package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func h(w http.ResponseWriter, r *http.Request) {}

// items is a router mounted below prefixes read from configuration.
func items() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/v1/items/{id}", h)
	r.Post("/v1/items", h)
	return r
}

// Mounted mounts items below a configured base (UP-1, chi Mount) and
// routes a group below a configured base inside a read prefix (UP-1, chi
// Route, value_covers hop: "above" is /outer).
func Mounted(base string) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", h)
	r.Mount(base, items())
	r.Route("/outer", func(r chi.Router) {
		r.Route(base, func(r chi.Router) {
			r.Get("/inner/{name}", h)
		})
	})
	return r
}

// Twice mounts one router below two configured bases: the route's one
// path below them is one entry (UP-8).
func Twice(a, b string) http.Handler {
	sub := chi.NewRouter()
	sub.Get("/dup/path", h)
	r := chi.NewRouter()
	r.Mount(a, sub)
	r.Mount(b, sub)
	return r
}

// Known mounts one router at a read prefix and below a configured base:
// its route has a path, so it gets no entry (UP-9).
func Known(base string) http.Handler {
	sub := chi.NewRouter()
	sub.Get("/known/path", h)
	r := chi.NewRouter()
	r.Mount("/k", sub)
	r.Mount(base, sub)
	return r
}

// Conditional routes below a configured base only in one branch: the
// entry is conditional.
func Conditional(base string, on bool) http.Handler {
	r := chi.NewRouter()
	if on {
		r.Route(base, func(r chi.Router) {
			r.Get("/maybe/there", h)
		})
	}
	return r
}

// Regexp has a prefix chi refuses to compose (a regexp that can match
// '/', regexp_param): no hop, no entry (row D).
func Regexp() http.Handler {
	r := chi.NewRouter()
	r.Route("/{rest:.*}", func(r chi.Router) {
		r.Get("/regexp/below", h)
	})
	return r
}

// Dynamic registers a pattern with an unknown piece: no route node at
// all (UP-10).
func Dynamic(base string) http.Handler {
	r := chi.NewRouter()
	r.Get("/.well-known/x"+base, h)
	return r
}

// Plugin's router parameter is bound by no resolved call
// (route_unmounted): no entry (UP-10).
func Plugin(r chi.Router) {
	r.Get("/plugin/echo", h)
}

func Serve(base, a, b string) {
	http.ListenAndServe(":9001", Mounted(base))
	http.ListenAndServe(":9002", Twice(a, b))
	http.ListenAndServe(":9003", Known(base))
	http.ListenAndServe(":9004", Conditional(base, true))
	http.ListenAndServe(":9005", Regexp())
	http.ListenAndServe(":9006", Dynamic(base))
}

// scoped is mounted below a configured base by Scoped and at "/t" by a
// test (UP-9 per scope): its test-scope path does not remove
// the production entry.
func scoped() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/scoped/item", h)
	return r
}

// Scoped mounts scoped below a configured base.
func Scoped(base string) http.Handler {
	r := chi.NewRouter()
	r.Mount(base, scoped())
	return r
}
