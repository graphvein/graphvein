package chiapp

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Review holds a goto skipping a Use (row SH-S18, form jump): chi serves
// /c-jump/x without BasicAuth.
func Review(r *chi.Mux) {
	r.Route("/c-jump", func(s chi.Router) {
		goto reg
		s.Use(middleware.BasicAuth("fixture", creds))
	reg:
		s.Get("/x", private)
	})
}
