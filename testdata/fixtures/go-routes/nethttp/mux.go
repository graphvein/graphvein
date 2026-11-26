// Package nethttp registers net/http ServeMux routes (GR-R1, GR-K2, GR-K4).
package nethttp

import (
	"log"
	"net/http"

	"example.com/goroutes/consts"
	"example.com/goroutes/handlers"
)

type server struct{}

func (s *server) status(w http.ResponseWriter, r *http.Request) {}

func index(w http.ResponseWriter, r *http.Request) {}

// logging passes the request's URL to log.Println, which no rule reads:
// nothing proves it keeps the path (GR-O7 refuses it; at a serving
// position it is assumption GR-V5).
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println(r.URL)
		next.ServeHTTP(w, r)
	})
}

// Routes builds the main mux.
func Routes(s *server, dir string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", index)
	mux.HandleFunc("GET example.com/items/{id}", index)
	mux.HandleFunc("POST /items", s.status)
	mux.Handle("/files/{path...}", http.HandlerFunc(index))
	mux.Handle("/tree/", http.HandlerFunc(s.status))
	mux.HandleFunc("GET "+consts.Users, handlers.Health)
	var u handlers.Users
	mux.HandleFunc("GET /users/list", u.List)
	h := &handlers.Users{}
	mux.HandleFunc("GET /users/{id}", h.Show)
	n := handlers.NewUsers()
	mux.HandleFunc("DELETE /users/{id}", n.Show)
	mux.HandleFunc("GET /wrapped", handlers.Wrap(index))
	mux.HandleFunc("GET /inline", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /a/{x", index)
	mux.HandleFunc("GET /items/{id}", index)
	mux.HandleFunc("GET /items/{name}", index)
	mux.HandleFunc(dir+"/x", index)
	if dir != "" {
		mux.HandleFunc("PUT /conditional", index)
	}
	return mux
}

// Zero registers on zero-value muxes (GR-K4) and composes them (GR-C3:
// no route for the composition calls).
func Zero() http.Handler {
	var zero http.ServeMux
	zero.HandleFunc("GET /zero", index)
	value := http.ServeMux{}
	value.HandleFunc("GET /value", index)
	ptr := &http.ServeMux{}
	ptr.HandleFunc("GET /ptr", index)
	alloc := new(http.ServeMux)
	alloc.HandleFunc("GET /alloc", index)
	root := http.NewServeMux()
	root.Handle("/zero/", http.StripPrefix("/zero", &zero))
	root.HandleFunc("/ptr/", ptr.ServeHTTP)
	root.Handle("/alloc/", http.Handler(alloc))
	inner := http.NewServeMux()
	inner.HandleFunc("GET /inner", index)
	root.Handle("/inner/", logging(inner))
	return root
}

func init() {
	http.HandleFunc("/default", index)
	http.Handle("/default/tree/", http.HandlerFunc(index))
	http.DefaultServeMux.HandleFunc("GET /via-default", index)
}
