// Package tg tags handlers: each writes its tag in the X-Route header, so
// the source oracle sees which registration answered a request.
package tg

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// T returns a net/http or chi handler that writes tag.
func T(tag string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("X-Route", tag) }
}

// G returns a gin handler that writes tag.
func G(tag string) gin.HandlerFunc {
	return func(c *gin.Context) { c.Header("X-Route", tag) }
}

// Mark writes tag (a named handler's own name).
func Mark(w http.ResponseWriter, tag string) { w.Header().Set("X-Route", tag) }
