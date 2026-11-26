package cases

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"example.com/gvsource/tg"
)

// gin groups (rows GR-R3, GR-R4, GR-O3, GR-C2).

// P20 registers on nested groups with trailing slashes, Any and Static.
func P20() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	v1 := e.Group("/v1/")
	v1.GET("", tg.G("P20/root"))
	v1.GET("/a/", tg.G("P20/a"))
	g2 := v1.Group("b")
	g2.Any("/c", tg.G("P20/c"))
	e.Static("/s", ".")
	return e
}

// P22 registers "" and "/" on one group as two routes (realworld-gin's
// form) and "" alone on another: gin serves each form by its own route
// and redirects the other form of /p22/only (RedirectTrailingSlash).
func P22() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	g := e.Group("/p22/articles")
	g.GET("", tg.G("P22/bare"))
	g.GET("/", tg.G("P22/slash"))
	o := e.Group("/p22/only")
	o.POST("", tg.G("P22/only"))
	return e
}
