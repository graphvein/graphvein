package stats

import "github.com/gin-gonic/gin"

func Register(r gin.IRouter) {
	r.GET("/stats", stats)
}

func stats(c *gin.Context) {}
