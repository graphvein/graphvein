package cases

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/gvsource/tg"
)

// Confirmation-pass forms: a router returned by a
// method called through a module interface (A1-r1), a router appended to
// a slice (A1-r2) and a forwarding closure that answers by host (B6). None
// of their chi routes has a path.

// provider returns a router through an interface method.
type provider interface{ Router() http.Handler }

type impl struct{}

// Router builds the router a provider serves.
func (impl) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/v2users", tg.T("R13/users"))
	return r
}

// R13 serves a provider's router under /api.
func R13() http.Handler {
	var p provider = impl{}
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", p.Router()))
	return m
}

// R14 mounts a router taken from a slice.
func R14() http.Handler {
	r := chi.NewRouter()
	r.Get("/v9users", tg.T("R14/users"))
	var hs []http.Handler
	hs = append(hs, r)
	root := chi.NewRouter()
	root.Mount("/api", hs[0])
	return root
}

// H14 forwards from a closure that answers one host itself.
func H14() http.Handler {
	r := chi.NewRouter()
	r.Get("/v14", tg.T("H14/v14"))
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.Host == "example.org" {
			w.Header().Set("X-Route", "H14/closure")
			return
		}
		r.ServeHTTP(w, req)
	})
	return m
}
