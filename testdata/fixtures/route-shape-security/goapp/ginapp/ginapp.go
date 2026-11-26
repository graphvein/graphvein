// Package ginapp holds the gin routes of the route-shape-security fixture
// (T029): router-level middleware (Group arguments, Use before
// and after a route, Use in a called function, a conditional Use) and
// gin's BasicAuth.
package ginapp

import (
	"os"

	"github.com/gin-gonic/gin"
)

var accounts = gin.Accounts{"user": "pass"}

// Engine builds the fixture's gin routes.
func Engine() *gin.Engine {
	r := gin.Default()
	r.GET("/gin-open", open)
	admin := r.Group("/admin", gin.BasicAuth(accounts))
	admin.GET("/stats", stats)
	realm := r.Group("/realm")
	realm.Use(gin.BasicAuthForRealm(accounts, "fixture"))
	realm.GET("/info", stats)
	late := r.Group("/late")
	late.GET("/before", open)
	late.Use(gin.BasicAuth(accounts))
	late.GET("/after", stats)
	custom := r.Group("/custom")
	custom.Use(requireKey())
	custom.GET("/me", stats)
	cond := r.Group("/cond")
	if os.Getenv("GV_FIXTURE_AUTH") != "" {
		cond.Use(gin.BasicAuth(accounts))
	}
	cond.GET("/maybe", stats)
	r.GET("/inline", gin.BasicAuth(accounts), stats)
	registerAPI(r.Group("/gin-api"))
	setup := r.Group("/setup")
	setupAuth(setup)
	setup.GET("/done", stats)
	More(r)
	Jumps(r)
	JumpHere(r)
	return r
}

func registerAPI(g *gin.RouterGroup) {
	g.Use(gin.BasicAuth(accounts))
	g.GET("/items", stats)
}

func setupAuth(g *gin.RouterGroup) {
	g.Use(gin.BasicAuth(accounts))
}

// requireKey is a repository auth middleware: no row reads it.
func requireKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("X-Key") != "fixture" {
			c.AbortWithStatus(401)
			return
		}
		c.Next()
	}
}

func open(c *gin.Context) {
	c.String(200, "open")
}

func stats(c *gin.Context) {
	c.String(200, "ok")
}

// More router-middleware forms (row SH-S18): a Use in a function
// literal and a function registering on groups with and without auth.
func More(r *gin.Engine) {
	lazy := r.Group("/lazy")
	func() { lazy.Use(gin.BasicAuth(accounts)) }()
	lazy.GET("/lazy-x", stats)
	registerShared(r.Group("/shared-auth", gin.BasicAuth(accounts)))
	registerShared(r.Group("/shared-open"))
}

func registerShared(g *gin.RouterGroup) {
	g.GET("/shared", stats)
}
