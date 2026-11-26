package mux

import (
	"encoding/json"
	"net/http"
)

// Mux registers the net/http routes.
func Mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /items/{id}", getItem)
	m.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
	})
	m.Handle("GET /health", http.HandlerFunc(health))
	return m
}

func getItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	writeItem(w, id)
}

// writeItem encodes without a status of its own: the status depends on
// what its caller wrote.
func writeItem(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func health(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
