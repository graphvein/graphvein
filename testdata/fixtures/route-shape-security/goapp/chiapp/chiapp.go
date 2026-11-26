// Package chiapp holds the chi routes of the route-shape-security fixture
// (T029): chi Use in groups and routes, With, jwtauth and chi's
// BasicAuth.
package chiapp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/jwtauth/v5"
)

// TokenAuth signs and verifies the fixture's tokens.
var TokenAuth = jwtauth.New("HS256", []byte("fixture-secret"), nil)

var creds = map[string]string{"user": "pass"}

// Router builds the fixture's chi routes.
func Router() *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Get("/public", public)
	r.Group(func(r chi.Router) {
		r.Use(jwtauth.Verifier(TokenAuth))
		r.Use(jwtauth.Authenticator(TokenAuth))
		r.Get("/chi-jwt", private)
	})
	r.Group(func(r chi.Router) {
		r.Use(jwtauth.Verifier(TokenAuth))
		r.Get("/optional", private)
	})
	r.With(middleware.BasicAuth("fixture", creds)).Get("/chi-basic", private)
	r.Route("/query", func(r chi.Router) {
		r.Use(jwtauth.Verify(TokenAuth, jwtauth.TokenFromQuery), jwtauth.Authenticator(TokenAuth))
		r.Get("/", private)
	})
	r.Group(func(r chi.Router) {
		r.Use(middleware.Logger, requireKey)
		r.Get("/chi-custom", private)
	})
	Review(r)
	// Row SH-S24: the later /c16 replaces the first, and the parent's
	// static /c18/x is matched before sub18's: chi serves both without
	// BasicAuth.
	r.With(middleware.BasicAuth("fixture", creds)).Get("/c16", private)
	r.Get("/c16", public)
	r.Get("/c18/x", public)
	sub18 := chi.NewRouter()
	sub18.Use(middleware.BasicAuth("fixture", creds))
	sub18.Get("/x", private)
	r.Mount("/c18", sub18)
	return r
}

// requireKey is a repository auth middleware: no row reads it.
func requireKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "fixture" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func public(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func private(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
