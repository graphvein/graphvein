package ginapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func recoverer(h http.Handler) http.Handler { return h }

// Serve serves a gin engine through a wrapper and one under a StripPrefix
// prefix (GR-C6, GR-V5).
func Serve() {
	e := gin.New()
	e.GET("/wrapped/:id", getUser)
	srv := &http.Server{Handler: recoverer(e)}
	go srv.ListenAndServe()
	s := gin.New()
	s.GET("/s/*rest", getUser)
	go http.ListenAndServe(":8091", http.StripPrefix("/gin", s))
}
