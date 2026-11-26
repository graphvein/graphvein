// Package oldchi imports chi before v5 (unsupported_module) and gin with a
// dot import (dot_import).
package oldchi

import (
	"github.com/go-chi/chi"
	. "github.com/gin-gonic/gin"
)

// Routes registers on an older chi router: not read.
func Routes() {
	r := chi.NewRouter()
	r.Get("/old", nil)
	g := New()
	g.GET("/dot", nil)
}
