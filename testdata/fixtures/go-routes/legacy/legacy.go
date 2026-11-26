// Package legacy is a Go 1.21 module: net/http patterns are read with the
// old semantics (legacy_mux_patterns).
package legacy

import "net/http"

func handle(w http.ResponseWriter, r *http.Request) {}

// Routes registers on a mux.
func Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/legacy", handle)
	return mux
}
