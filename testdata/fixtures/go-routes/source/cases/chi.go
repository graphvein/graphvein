package cases

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/gvsource/tg"
)

// chi compositions (rows GR-O1, GR-O2, GR-C1) and a parent route that
// takes a mounted path (T023).

// P3 has a static parent route that shadows the mounted child's route.
func P3() http.Handler {
	sub := chi.NewRouter()
	sub.Get("/users", tg.T("P3/users"))
	sub.Get("/items", tg.T("P3/items"))
	r := chi.NewRouter()
	r.Get("/api/users", tg.T("P3/parent"))
	r.Mount("/api", sub)
	return r
}

// P4 has a parameter parent route that takes every mounted path of one
// segment for GET.
func P4() http.Handler {
	sub := chi.NewRouter()
	sub.Get("/users", tg.T("P4/users"))
	sub.Post("/users", tg.T("P4/create"))
	r := chi.NewRouter()
	r.Get("/api/{name}", tg.T("P4/parent"))
	r.Mount("/api", sub)
	return r
}

// P5 mounts with a trailing slash: the child "/" answers /api/ only.
func P5() http.Handler {
	sub := chi.NewRouter()
	sub.Get("/", tg.T("P5/index"))
	r := chi.NewRouter()
	r.Mount("/api/", sub)
	return r
}

func passthrough(h http.Handler) http.Handler { return h }

// P14 nests Route, With and Group with trailing slashes and a regexp.
func P14() http.Handler {
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.With(passthrough).Get("/a/", tg.T("P14/a"))
		r.Group(func(r chi.Router) {
			r.Get("/b/{id:[0-9]+}", tg.T("P14/b"))
		})
		r.Route("/c/", func(r chi.Router) {
			r.Get("/", tg.T("P14/c"))
		})
	})
	return r
}

// P15 registers a sub-router with Handle (no Mount): no path.
func P15() http.Handler {
	sub := chi.NewRouter()
	sub.Get("/x", tg.T("P15/x"))
	r := chi.NewRouter()
	r.Handle("/api/*", sub)
	return r
}

// P16 mounts StripPrefix of a chi router: no path.
func P16() http.Handler {
	sub := chi.NewRouter()
	sub.Get("/x", tg.T("P16/x"))
	r := chi.NewRouter()
	r.Mount("/api", http.StripPrefix("/api", sub))
	return r
}

// P17 registers a method pattern through Handle.
func P17() http.Handler {
	r := chi.NewRouter()
	r.Handle("POST /m", tg.T("P17/post"))
	r.Get("/h", tg.T("P17/get"))
	return r
}

// routes registers through a helper bound to the router (row GR-O4).
func routes(r chi.Router) {
	r.Get("/helper", tg.T("P21/helper"))
}

// P21 binds a helper's chi.Router parameter and mounts a factory's router.
func P21() http.Handler {
	r := chi.NewRouter()
	routes(r)
	r.Mount("/admin", adminRouter())
	return r
}

func adminRouter() chi.Router {
	a := chi.NewRouter()
	a.Get("/stats", tg.T("P21/stats"))
	return a
}

// P23 serves a "/" route under Route and Mount prefixes without a
// trailing slash (answering prefix and prefix/), beside a plain route
// whose slash form chi does not answer.
func P23() http.Handler {
	r := chi.NewRouter()
	r.Route("/p23/articles", func(r chi.Router) {
		r.Get("/", tg.T("P23/index"))
		r.Get("/{slug}", tg.T("P23/slug"))
	})
	sub := chi.NewRouter()
	sub.Post("/", tg.T("P23/mounted"))
	r.Mount("/p23/sub", sub)
	return r
}
