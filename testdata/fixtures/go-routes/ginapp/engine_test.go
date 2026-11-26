package ginapp

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestEngine(t *testing.T) {
	r := gin.Default()
	r.GET("/test-only", getUser)
	r.PATCH("/test-only", getUser)
	r.PATCH("/test-only", getUser)
	register(r.Group("/t"))
}
