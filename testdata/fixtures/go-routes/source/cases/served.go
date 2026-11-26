package cases

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/gvsource/tg"
)

// The gated half of wiring.go's cases.

func mkU1() http.Handler { r := chi.NewRouter(); r.Get("/u1users", tg.T("U1/users")); return r }
func mkU2() http.Handler { r := chi.NewRouter(); r.Get("/u2users", tg.T("U2/users")); return r }

// prefixed serves a handler under /api.
func prefixed(h any) http.Handler {
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", h.(http.Handler)))
	return m
}

// serveRoot builds a server for a handler at its own paths.
func serveRoot(h http.Handler) *http.Server { return &http.Server{Handler: h} }
