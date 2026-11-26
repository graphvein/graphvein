package app

import "github.com/gin-gonic/gin"

func ping(c *gin.Context) {}

// Gin groups routes below a configured prefix (UP-1, gin Group); the
// empty path below the group is the prefix itself (UP-10: stored, never
// matched).
func Gin(prefix string) *gin.Engine {
	e := gin.New()
	g := e.Group(prefix)
	g.GET("/api/ping/:id", ping)
	g.GET("", ping)
	return e
}

// RunGin serves the engine.
func RunGin(prefix string) {
	Gin(prefix).Run()
}
