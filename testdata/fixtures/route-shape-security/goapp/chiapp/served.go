package chiapp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Served mounts a chi router at "/" of a ServeMux through a wrapper
// (row SH-S18: the wrapper may run first).
func Served() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.BasicAuth("fixture", creds))
	r.Get("/served", private)
	mux := http.NewServeMux()
	mux.Handle("/", logRequests(r))
	return mux
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(w, req)
	})
}
