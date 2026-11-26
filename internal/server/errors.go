package server

import (
	"encoding/json"
	"net/http"
)

// errorEnvelope is the one error shape of HTTP API v0, so clients decode
// every failure the same way: {"error": {"code": ..., "message": ...}}.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Details is set only by answers that document it (try-it: the
	// certificate of a TLS failure, a missing auth variable, the limit a
	// request exceeded).
	Details any `json:"details,omitempty"`
}

// writeError writes an API error. Messages come from store and scan errors,
// which never contain the Postgres URL.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

// writeJSON writes an uncached JSON response. Freshness changes with every
// edit, so no API response may be reused.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// An encoding failure here means the client went away mid-write; the
	// status line is already sent, so there is nothing left to report.
	_ = json.NewEncoder(w).Encode(value)
}
