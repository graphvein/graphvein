package nethttp

import (
	"net/http"
	"testing"
)

func TestProd(t *testing.T) {
	srv := &http.Server{Handler: http.StripPrefix("/test", Prod())}
	_ = srv
}
