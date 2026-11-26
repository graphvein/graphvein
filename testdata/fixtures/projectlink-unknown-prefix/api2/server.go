package api2

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func h(w http.ResponseWriter, r *http.Request) {}

// Routes serves below a base read from configuration; one template is also
// a route of the other API.
func Routes(base string) http.Handler {
	r := chi.NewRouter()
	r.Route(base, func(r chi.Router) {
		r.Get("/upd/shared/items", h)
	})
	return r
}

func Serve(base string) {
	http.ListenAndServe(":9122", Routes(base))
}
