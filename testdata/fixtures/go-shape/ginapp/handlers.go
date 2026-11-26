package ginapp

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const pageParam = "page"

// Store has two implementations: a call through it is ambiguous.
type Store interface {
	Find(c *gin.Context)
}

type memStore struct{}

func (memStore) Find(c *gin.Context) { c.Status(http.StatusOK) }

type diskStore struct{}

func (diskStore) Find(c *gin.Context) { c.Status(http.StatusOK) }

// API holds the handlers that are methods.
type API struct {
	store Store
}

// GetUser reads a converted path parameter, query, header and cookie.
func (a *API) GetUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	verbose := c.Query("verbose")
	_ = c.DefaultQuery("lang", "en")
	_ = c.Query(pageParam)
	_ = c.GetHeader("X-Request-ID")
	_, _ = c.Cookie("session")
	u := User{Name: verbose}
	u.Base.ID = id
	c.JSON(http.StatusOK, u)
}

// CreateUserHandler binds a JSON body and checks the error.
func CreateUserHandler(c *gin.Context) {
	var req CreateUser
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name == "taken" {
		fail(c, http.StatusConflict, "exists")
		return
	}
	c.JSON(http.StatusCreated, User{Name: req.Name})
}

// fail answers with a status its caller passes.
func fail(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg})
}

// UpdateUser uses BindJSON, which answers 400 itself.
func UpdateUser(c *gin.Context) {
	var req CreateUser
	if c.BindJSON(&req) != nil {
		return
	}
	c.Status(http.StatusNoContent)
}

// Loose ignores the bind error: the type is not enforced.
func Loose(c *gin.Context) {
	var req CreateUser
	_ = c.ShouldBindJSON(&req)
	c.String(http.StatusOK, "ok")
}

// LoginHandler binds by Content-Type (H18).
func LoginHandler(c *gin.Context) {
	var in Login
	if err := c.ShouldBind(&in); err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": "t", "expires": 3600, "ok": true, "scope": nil})
}

// MixedHandler binds a type whose JSON and form names differ.
func MixedHandler(c *gin.Context) {
	var m Mixed
	if err := c.Bind(&m); err != nil {
		return
	}
	c.Status(http.StatusNoContent)
}

// Search uses Bind on a GET route: gin binds the query.
func Search(c *gin.Context) {
	var q ListQuery
	if err := c.Bind(&q); err != nil {
		return
	}
	c.JSON(http.StatusOK, []Role{})
}

// List binds the query string.
func List(c *gin.Context) {
	var q ListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad query"})
		return
	}
	c.JSON(http.StatusOK, Page[Role]{})
}

// GetItem binds the path.
func GetItem(c *gin.Context) {
	var uri ItemURI
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, &Node{Name: "root"})
}

// Upload reads form fields.
func Upload(c *gin.Context) {
	name := c.PostForm("name")
	file, err := c.FormFile("file")
	if err != nil {
		c.String(http.StatusBadRequest, "no file")
		return
	}
	_ = file
	c.Redirect(http.StatusFound, "/files/"+name)
}

// withID reads a path parameter whose name its caller passes (H20).
func withID(c *gin.Context, name string, f func(id uint)) {
	if id, err := strconv.ParseUint(c.Param(name), 10, 64); err == nil {
		f(uint(id))
	} else {
		c.AbortWithError(http.StatusBadRequest, errors.New("invalid id"))
	}
}

// DeleteItem reads its parameter through withID.
func DeleteItem(c *gin.Context) {
	withID(c, "id", func(id uint) {
		c.Status(http.StatusNoContent)
	})
}

// Deep calls a chain seven functions deep.
func Deep(c *gin.Context) { level1(c) }

func level1(c *gin.Context) { level2(c) }

func level2(c *gin.Context) { level3(c) }

func level3(c *gin.Context) {
	_ = c.Query("q3")
	level4(c)
}

func level4(c *gin.Context) { level5(c) }

func level5(c *gin.Context) { level6(c) }

func level6(c *gin.Context) { level7(c) }

func level7(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"depth": 7}) }

// Find calls through a field (receiver type not read) and a method of
// its own receiver.
func (a *API) Find(c *gin.Context) {
	a.store.Find(c)
	a.render(c)
}

func (a *API) render(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"found": true}) }

// ViaInterface calls through an interface with two implementations.
func ViaInterface(c *gin.Context) {
	var s Store = memStore{}
	s.Find(c)
}

// External passes the context to a library function.
func External(c *gin.Context) {
	fmt.Println(c)
	c.Status(http.StatusOK)
}

func pick() func(*gin.Context) { return func(*gin.Context) {} }

// FuncValue calls a function value.
func FuncValue(c *gin.Context) {
	h := pick()
	h(c)
}

// Reassign replaces the context before reading it.
func Reassign(c *gin.Context) {
	c = func() *gin.Context { return c }()
	c.JSON(http.StatusOK, gin.H{})
}

func logAny(v any) { _ = v }

// Opaque passes the context to a parameter of another type.
func Opaque(c *gin.Context) {
	logAny(c)
	c.Status(http.StatusOK)
}

// Cycle calls itself through another function.
func Cycle(c *gin.Context) { ping(c, 0) }

func ping(c *gin.Context, n int) {
	if n < 3 {
		pong(c, n)
	}
}

func pong(c *gin.Context, n int) { ping(c, n+1) }

// XMLOut writes XML, which no row reads.
func XMLOut(c *gin.Context) { c.XML(http.StatusOK, gin.H{}) }

// PriceHandler writes a type with a promoted MarshalJSON.
func PriceHandler(c *gin.Context) { c.JSON(http.StatusOK, Price{}) }

// ConflictHandler writes types whose embedded fields conflict.
func ConflictHandler(c *gin.Context) {
	if c.Query("tagged") != "" {
		c.JSON(http.StatusAccepted, Tagged{})
		return
	}
	c.JSON(http.StatusOK, Conflict{})
}

// ReqHeader reads a header through c.Request.
func ReqHeader(c *gin.Context) {
	ua := c.Request.Header.Get("User-Agent")
	c.String(http.StatusOK, ua)
}

// Dynamic reads a query parameter whose name is not a literal.
func Dynamic(c *gin.Context) {
	key := c.Query("key")
	_ = c.Query(key)
	c.Status(http.StatusOK)
}

// Shadow returns a handler-local User, not the package's User (T029):
// the shape reader leaves it unresolved.
func Shadow(c *gin.Context) {
	type User struct {
		Inner string `json:"inner"`
	}
	c.JSON(http.StatusOK, User{Inner: "x"})
}
