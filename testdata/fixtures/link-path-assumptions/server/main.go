// Package server serves net/http routers through a wrapper that changes
// the path (no proof): their paths rest on assumption GR-V5.
package server

import (
	"net/http"
	"os"
	"strings"
)

func index(w http.ResponseWriter, r *http.Request) {}

// rewrite changes the path before serving: not path-preserving.
func rewrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimSuffix(r.URL.Path, "/")
		next.ServeHTTP(w, r)
	})
}

// Prefixed strips a prefix the caller passes in front of a mux: its route
// is stored below an unknown prefix (unresolved_prefix_paths).
func Prefixed(prefix string) http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /gp/orders/list", index)
	root := http.NewServeMux()
	root.Handle(prefix+"/", http.StripPrefix(prefix, api))
	return root
}

// Serve serves G1 (GR-V5, exact tier), G2 (GR-V5 below a prefix read from
// the environment: unknown-prefix tier) and G3 (one mux served through the
// wrapper and, below /direct, without it).
func Serve() {
	wrapped := http.NewServeMux()
	wrapped.HandleFunc("GET /gw/items/all", index)
	go http.ListenAndServe(":8101", rewrite(wrapped))

	go http.ListenAndServe(":8102", rewrite(Prefixed(os.Getenv("BASE"))))

	both := http.NewServeMux()
	both.HandleFunc("GET /gb/items/all", index)
	go http.ListenAndServe(":8103", rewrite(both))
	go http.ListenAndServe(":8104", http.StripPrefix("/direct", both))
}
