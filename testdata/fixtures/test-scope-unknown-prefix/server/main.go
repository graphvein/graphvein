package main

import (
	"example.com/server/router"
	"example.com/server/stats"

	"github.com/gin-gonic/gin"
)

func main() {
	server := gin.New()
	stats.Register(server.Group("/admin"))
	router.SetRouter(server)
	server.Run(":3000")
}
