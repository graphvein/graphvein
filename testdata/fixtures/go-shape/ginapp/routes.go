package ginapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Engine registers the gin routes.
func Engine(api *API) *gin.Engine {
	r := gin.New()
	r.GET("/users/:id", api.GetUser)
	r.POST("/users", CreateUserHandler)
	r.PUT("/users/:id", UpdateUser)
	r.POST("/loose", Loose)
	r.POST("/login", LoginHandler)
	r.POST("/mixed", MixedHandler)
	r.GET("/search", Search)
	r.GET("/list", List)
	r.GET("/items/:id", GetItem)
	r.POST("/upload", Upload)
	r.DELETE("/items/:id", DeleteItem)
	r.GET("/deep", Deep)
	r.GET("/find", api.Find)
	r.GET("/iface", ViaInterface)
	r.GET("/external", External)
	r.GET("/funcvalue", FuncValue)
	r.GET("/reassign", Reassign)
	r.GET("/opaque", Opaque)
	r.GET("/cycle", Cycle)
	r.GET("/xml", XMLOut)
	r.GET("/price", PriceHandler)
	r.GET("/conflict", ConflictHandler)
	r.GET("/ua", ReqHeader)
	r.GET("/dynamic", Dynamic)
	r.GET("/shadow", Shadow)
	// A type declared in the enclosing function shadows the package's
	// Audit inside the literal (T029).
	type Audit struct {
		Note string `json:"note"`
	}
	r.GET("/shadowinline", func(c *gin.Context) {
		c.JSON(http.StatusOK, Audit{Note: "x"})
	})
	r.GET("/inline", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"inline": true})
	})
	return r
}

// NewAPI returns an API with an in-memory store.
func NewAPI() *API { return &API{store: memStore{}} }
