package cases

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/gvsource/tg"
)

// Escaped routers (T023, row GR-C4): each sub-router below is
// served under a prefix through a use no contract row reads, so it is no
// root and its routes have no path.

// mount is a mount helper whose handler parameter is http.Handler.
func mount(r chi.Router, p string, h http.Handler) { r.Mount(p, h) }

// R1 mounts a chi router through mount.
func R1() http.Handler {
	sub := chi.NewRouter()
	sub.Get("/r1users", tg.T("R1/users"))
	r := chi.NewRouter()
	mount(r, "/api", sub)
	return r
}

// mountMux mounts a ServeMux with StripPrefix through an http.Handler
// parameter.
func mountMux(root *http.ServeMux, h http.Handler) {
	root.Handle("/api/", http.StripPrefix("/api", h))
}

// R2 mounts a ServeMux through mountMux.
func R2() http.Handler {
	sub := http.NewServeMux()
	sub.HandleFunc("/r2users", tg.T("R2/users"))
	root := http.NewServeMux()
	mountMux(root, sub)
	return root
}

// newAPI is a constructor with (router, error) results.
func newAPI() (*chi.Mux, error) {
	r := chi.NewRouter()
	r.Get("/r6users", tg.T("R6/users"))
	if r == nil {
		return nil, errors.New("no router")
	}
	return r, nil
}

// R6 serves newAPI's router under /api.
func R6() http.Handler {
	api, _ := newAPI()
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", api))
	return m
}

// S keeps a router in a struct field.
type S struct{ router chi.Router }

func newS() *S {
	s := &S{}
	r := chi.NewRouter()
	r.Get("/r9users", tg.T("R9/users"))
	s.router = r
	return s
}

// R9 mounts a router kept in a struct field.
func R9() http.Handler {
	s := newS()
	root := chi.NewRouter()
	root.Mount("/api", s.router)
	return root
}

// Handler returns the field's router.
func (s *S) Handler() http.Handler { return s.router }

func newS10() *S {
	r := chi.NewRouter()
	r.Get("/r10users", tg.T("R10/users"))
	s := new(S)
	s.router = r
	return s
}

// R10 serves a router returned by a method under /api.
func R10() http.Handler {
	s := newS10()
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", s.Handler()))
	return m
}

// Listen serves a handler (the oracle replaces it).
var Listen = func(addr string, h http.Handler) error { return http.ListenAndServe(addr, h) }

// serve is a serving helper with an http.Handler parameter.
func serve(h http.Handler) error { return Listen("127.0.0.1:0", http.StripPrefix("/base", h)) }

// R11 serves a router through serve.
func R11() {
	r := chi.NewRouter()
	r.Get("/r11users", tg.T("R11/users"))
	_ = serve(r)
}

// Global holds a router in a package-level variable.
var Global http.Handler

// R12init stores a router in Global.
func R12init() {
	r := chi.NewRouter()
	r.Get("/r12users", tg.T("R12/users"))
	Global = r
}

// R12 serves Global under /api.
func R12() http.Handler {
	R12init()
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", Global))
	return m
}
