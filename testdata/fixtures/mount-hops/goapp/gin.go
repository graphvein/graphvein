package mounts

import "github.com/gin-gonic/gin"

func ping(c *gin.Context) {}

// Gin groups its routes below a prefix read from configuration.
func Gin(prefix string) {
	e := gin.New()
	e.GET("/ready", ping)
	g := e.Group(prefix)
	g.GET("/ping", ping)
	g.GET("/", ping)
	// An empty path below the group is the prefix itself (no slash).
	g.GET("", ping)
	e.Run()
}
