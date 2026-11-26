// Package ginapp registers gin v1 routes (GR-R3, GR-R4, GR-O3, GR-O4).
package ginapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func auth(c *gin.Context)    {}
func getUser(c *gin.Context) {}

// Engine builds the engine.
func Engine(method string) *gin.Engine {
	g := gin.New()
	g.GET("version", getUser)
	api := g.Group("/api", auth)
	api.GET("/users/:id", auth, getUser)
	api.POST("/users", getUser)
	api.Any("/any", getUser)
	api.Handle("PATCH", "/handled", getUser)
	api.Handle(method, "/dynamic", getUser)
	api.Handle("get", "/lower", getUser)
	api.Match([]string{http.MethodGet, "POST", http.MethodGet}, "/match", getUser)
	g.Group("/chain").Use(auth).DELETE("/x", getUser)
	g.Static("/static", "./public")
	g.StaticFS("/image", http.Dir("img"))
	g.StaticFile("/favicon.ico", "./favicon.ico")
	g.StaticFileFS("/robots.txt", "robots.txt", http.Dir("."))
	g.Static("/bad/:x", "./public")
	register(api.Group("/v1"))
	d := gin.Default()
	d.OPTIONS("/options", func(c *gin.Context) {})
	var typed *gin.Engine = gin.Default()
	typed.GET("/typed", getUser)
	var untyped = gin.New()
	untyped.GET("/untyped", getUser)
	return g
}

func register(r *gin.RouterGroup) {
	r.PUT("/x/*rest", getUser)
}

// Unbound is never called: its registrations stay without a path.
func Unbound(r gin.IRouter) {
	r.HEAD("/unbound", getUser)
}
