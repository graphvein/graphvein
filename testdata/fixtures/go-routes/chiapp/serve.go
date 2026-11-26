package chiapp

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/goroutes/mw"
)

// Serve serves a chi router under a StripPrefix prefix (GR-C6).
func Serve(a *api) error {
	r := chi.NewRouter()
	r.Get("/items/{id}", a.list)
	r.Route("/admin", func(r chi.Router) {
		r.Post("/", a.list)
	})
	c := chi.NewRouter()
	c.Get("/ctx", a.list)
	go http.ListenAndServe(":8097", mw.WithUser(c))
	return http.ListenAndServe(":8090", http.StripPrefix("/chi", r))
}
