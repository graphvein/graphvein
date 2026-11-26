package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStrip registers below a configured prefix in a test file: the entry
// has scope test.
func TestStrip(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /t/only/test", h)
	root := http.NewServeMux()
	root.Handle(t.Name()+"/", http.StripPrefix(t.Name(), inner))
	httptest.NewServer(root)
}
