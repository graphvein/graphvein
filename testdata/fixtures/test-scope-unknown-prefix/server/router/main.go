package router

import "github.com/gin-gonic/gin"

// SetRouter passes the engine on after mounting: the dispatcher escapes
// to NoRoute, so no production path below it is proven.
func SetRouter(router *gin.Engine) {
	SetApiRouter(router)
	dispatcher := SetPluginRouter(router)
	router.NoRoute(dispatcher)
}

func SetPluginRouter(router *gin.Engine) gin.HandlerFunc {
	router.GET("/plugins", listPlugins)
	return func(c *gin.Context) {}
}

func listPlugins(c *gin.Context) {}
