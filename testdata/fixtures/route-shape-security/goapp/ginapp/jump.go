package ginapp

import "github.com/gin-gonic/gin"

// Jumps calls a function whose goto skips its Use (row SH-S18, form
// jump): gin serves /m-ret/x without BasicAuth.
func Jumps(r *gin.Engine) {
	ret := r.Group("/m-ret")
	setupMaybe(ret, false)
	ret.GET("/x", stats)
}

func setupMaybe(g *gin.RouterGroup, on bool) {
	if !on {
		goto end
	}
	g.Use(gin.BasicAuth(accounts))
end:
}

// JumpHere skips a Use in its own body with a goto: gin serves
// /m-jump/x without BasicAuth.
func JumpHere(r *gin.Engine) {
	j := r.Group("/m-jump")
	goto reg
	j.Use(gin.BasicAuth(accounts))
reg:
	j.GET("/x", stats)
}
