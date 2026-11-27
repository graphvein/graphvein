package router

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAbout(t *testing.T) {
	engine := gin.New()
	SetApiRouter(engine)
	engine.GET("/api/debug", debug)
}

func debug(c *gin.Context) {}
