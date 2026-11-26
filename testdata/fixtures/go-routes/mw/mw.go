// Package mw holds middleware the go-routes fixture's routers are served
// through (contract row GR-O7).
package mw

import (
	"context"
	"net/http"
	"strings"
)

type key struct{}

// CORS keeps the path: it reads the request and answers OPTIONS itself
// (a terminating branch, so its write never reaches the serving call).
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			r.URL.Path = "/preflight"
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WithUser keeps the URL but replaces the context (r.WithContext), so it
// passes no chi router through.
func WithUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), key{}, ClientIP(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Rewrite changes the path before serving: no proof.
func Rewrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimSuffix(r.URL.Path, "/")
		next.ServeHTTP(w, r)
	})
}

// Audit passes the request to a helper that changes its method: no proof.
func Audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		normalize(r)
		next.ServeHTTP(w, r)
	})
}

// ClientIP only reads the request (row RQ-request), through a helper.
func ClientIP(r *http.Request) string {
	if ip := forwarded(r); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

func forwarded(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
}

func normalize(r *http.Request) {
	r.Method = strings.ToUpper(r.Method)
}
