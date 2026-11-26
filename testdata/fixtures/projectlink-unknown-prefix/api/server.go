package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func h(w http.ResponseWriter, r *http.Request) {}

// Routes serves every group below bases read from configuration: no route
// has a path, only unresolved_prefix_paths entries.
func Routes(base, other string) http.Handler {
	r := chi.NewRouter()
	r.Route(base, func(r chi.Router) {
		r.Get("/upd/orders/list", h)
		r.Get("/upd/order/{id}", h)
		r.Get("/upd/shared/items", h)
		r.Get("/upd/tie/items", h)
		r.Get("/updping", h)
	})
	r.Route(other, func(r chi.Router) {
		r.Get("/upd/tie/items", h)
	})
	return r
}

func Serve(base, other string) {
	http.ListenAndServe(":9121", Routes(base, other))
}
