package ginapp

import "github.com/gin-gonic/gin"

// Groups nests gin groups (GR-C2, GR-C5).
func Groups(prefix string, ok bool) *gin.Engine {
	e := gin.New()
	v1 := e.Group("/v1/")
	v1.GET("", getUser)
	users := v1.Group("users")
	users.GET("/:id/", getUser)
	users.POST("", getUser)
	dyn := e.Group(prefix)
	dyn.GET("/dyn", getUser)
	var g *gin.RouterGroup = e.Group("/a")
	if ok {
		g = e.Group("/b")
	}
	g.GET("/both", getUser)
	register(e.Group("/r"))
	if ok {
		register(e.Group("/c"))
	}
	return e
}
