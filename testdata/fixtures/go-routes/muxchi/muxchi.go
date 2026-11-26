// Package muxchi holds the ServeMux -> chi hop (rows GR-C7 and GR-O8,
// task T023): a chi router at a ServeMux's "/" pattern, directly
// or through a closure that forwards the request, with the sibling
// patterns, redirects and closure cases that take some of its paths.
package muxchi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func status(w http.ResponseWriter, r *http.Request) {}

func api() *chi.Mux {
	a := chi.NewRouter()
	a.Get("/version", status)
	return a
}

// Start is owncast's shape: the chi root is forwarded from a closure
// registered at "/" beside a subtree pattern; the closure answers
// /debug/vars and /embed/chat itself.
func Start(admin http.Handler) error {
	r := chi.NewRouter()
	r.Get("/status", status)
	r.Get("/items/{id}", status)
	r.Get("/debug/vars", status)
	r.Get("/embed/chat", status)
	r.Get("/plugins/{name}", status)
	r.Get("/plugins", status)
	r.Mount("/api/", api())
	r.Route("/plugins/more", func(sub chi.Router) {
		sub.Get("/list", status)
	})

	rootHandler := r
	m := http.NewServeMux()
	m.Handle("/plugins/", admin)
	m.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/debug/vars":
			w.WriteHeader(http.StatusNotFound)
			return
		case "/embed/chat":
			http.Redirect(w, req, "/embed/chat/readonly", http.StatusTemporaryRedirect)
		default:
			rootHandler.ServeHTTP(w, req)
		}
	})
	return http.ListenAndServe(":8080", m)
}

// Direct registers a chi router at "/" itself; a POST sibling leaves the
// GET route its path, a GET sibling takes the other one.
func Direct() error {
	r := chi.NewRouter()
	r.Get("/direct", status)
	r.Get("/taken", status)
	m := http.NewServeMux()
	m.HandleFunc("POST /direct", status)
	m.HandleFunc("GET /taken", status)
	m.Handle("/", r)
	return http.ListenAndServe(":8081", m)
}

// Refused holds the forms that claim no path: a forwarded variable written
// twice, a closure that serves only one path, a chi router at a pattern
// other than "/" and a mux with a pattern no rule reads.
func Refused(pattern string) {
	r := chi.NewRouter()
	r.Get("/rewritten", status)
	target := r
	if pattern == "" {
		target = chi.NewRouter()
	}
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		target.ServeHTTP(w, req)
	})
	http.ListenAndServe(":8082", m)

	only := chi.NewRouter()
	only.Get("/only", status)
	m2 := http.NewServeMux()
	m2.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/only" {
			only.ServeHTTP(w, req)
		}
	})
	http.ListenAndServe(":8083", m2)

	sub := chi.NewRouter()
	sub.Get("/sub", status)
	m3 := http.NewServeMux()
	m3.Handle("/sub/", sub)
	http.ListenAndServe(":8084", m3)

	dyn := chi.NewRouter()
	dyn.Get("/dyn", status)
	m4 := http.NewServeMux()
	m4.HandleFunc(pattern, status)
	m4.Handle("/", dyn)
	http.ListenAndServe(":8085", m4)
}
