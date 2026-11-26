package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()
	r.POST("/orders", createOrder)
	r.GET("/orders/:id", getOrder)
	r.GET("/health", health)
	_ = r.Run()
}

func createOrder(c *gin.Context) {
	var in NewOrder
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Order{ID: "1"})
}

func getOrder(c *gin.Context) {
	c.JSON(http.StatusOK, Order{ID: c.Param("id")})
}

func health(c *gin.Context) {
	c.JSON(http.StatusOK, Status{OK: true})
}
