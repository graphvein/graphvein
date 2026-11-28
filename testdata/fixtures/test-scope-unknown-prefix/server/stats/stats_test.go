package stats

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStats(t *testing.T) {
	engine := gin.New()
	Register(engine)
}
