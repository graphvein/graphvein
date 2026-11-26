// Package main serves the chi catch-all of the link precision fixture.
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func file(w http.ResponseWriter, r *http.Request) {}

func main() {
	r := chi.NewRouter()
	r.Get("/t035-go/files/readme", file)
	r.Get("/t035-go/files/*", file)
	_ = http.ListenAndServe("127.0.0.1:8080", r)
}
