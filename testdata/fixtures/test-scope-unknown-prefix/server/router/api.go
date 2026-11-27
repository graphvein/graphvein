package router

import "github.com/gin-gonic/gin"

func SetApiRouter(router *gin.Engine) {
	api := router.Group("/api")
	api.GET("/about", about)
	api.POST("/token/", createToken)
}

func about(c *gin.Context)       {}
func createToken(c *gin.Context) {}
