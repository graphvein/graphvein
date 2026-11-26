// Package review holds the forms of the SH-G review (each handler names
// the finding it reproduces).
package review

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Item is validated only as the bound value's element or under dive.
type Item struct {
	SKU string `json:"sku" binding:"required"`
}

// Address is a struct-kind field: required is not checked on it.
type Address struct {
	City string `json:"city"`
}

// Order: A1.
type Order struct {
	Items   []Item  `json:"items"`
	Dived   []Item  `json:"dived" binding:"dive"`
	Address Address `json:"address" binding:"required"`
	Note    string  `json:"note"`
}

// Stamp is a defined type of time.Time: B3.
type Stamp time.Time

// Event writes a Stamp.
type Event struct {
	At Stamp `json:"at"`
}

// Ev embeds time.Time, whose MarshalJSON is promoted: B2.
type Ev struct {
	time.Time
	Name string `json:"name"`
}

// Form is bound by Content-Type: B5.
type Form struct {
	Name string `json:"name" form:"name" binding:"required"`
}

// Out is a response body.
type Out struct {
	ID int `json:"id"`
}

// Base is embedded through a pointer: B4.
type Base struct {
	ID int `json:"id" binding:"required"`
}

// WithBase embeds *Base.
type WithBase struct {
	*Base
	X int `json:"x"`
}

// Inner is required inside an optional pointer field.
type Inner struct {
	V string `json:"v" binding:"required"`
}

// PtrReq has a nested required field.
type PtrReq struct {
	P *Inner `json:"p"`
}

// Since reads a unix time from the query: C3.
type Since struct {
	From time.Time `form:"from" time_format:"unix" binding:"required"`
}

// Types lists the review types for the encoding/json oracle.
var Types = map[string]any{"Order": Order{}, "Event": Event{}, "Ev": Ev{}, "Form": Form{}, "WithBase": WithBase{}, "PtrReq": PtrReq{}, "Out": Out{}}

// Engine registers the review's gin routes.
func Engine() *gin.Engine {
	r := gin.New()
	r.POST("/orders", CreateOrder)
	r.POST("/items", ListItems)
	r.POST("/form", FormH)
	r.DELETE("/form", FormH)
	r.GET("/event", GetEvent)
	r.GET("/ev", GetEv)
	r.GET("/ginstatus", GinStatus)
	r.GET("/statusjson", StatusThenJSON)
	r.POST("/nocontent", NoContent)
	r.GET("/lower", Lower)
	r.GET("/wrapped", Wrapped)
	r.POST("/embptr", EmbPtr)
	r.POST("/ptrreq", PtrReqH)
	r.POST("/validated", Validated)
	r.GET("/since", SinceH)
	return r
}

// CreateOrder: A1 (Items elements and Address not validated, Dived is).
func CreateOrder(c *gin.Context) {
	var o Order
	if err := c.ShouldBindJSON(&o); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad"})
		return
	}
	c.JSON(http.StatusCreated, Out{ID: 1})
}

// ListItems binds a slice: gin validates its elements.
func ListItems(c *gin.Context) {
	var items []Item
	if err := c.ShouldBindJSON(&items); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"n": len(items)})
}

// FormH: B5 (the form binder reads the query too).
func FormH(c *gin.Context) {
	var f Form
	if err := c.ShouldBind(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad"})
		return
	}
	c.JSON(http.StatusOK, f)
}

// GetEvent: B3.
func GetEvent(c *gin.Context) { c.JSON(http.StatusOK, Event{}) }

// GetEv: B2.
func GetEv(c *gin.Context) { c.JSON(http.StatusOK, Ev{Name: "x"}) }

// GinStatus: B7 (c.Status then Encode on c.Writer sends 201).
func GinStatus(c *gin.Context) {
	c.Status(http.StatusCreated)
	json.NewEncoder(c.Writer).Encode(Out{ID: 2})
}

// StatusThenJSON: B7 (200 replaces 202).
func StatusThenJSON(c *gin.Context) {
	c.Status(http.StatusAccepted)
	c.JSON(http.StatusOK, Out{ID: 3})
}

// NoContent: B7 (204 has no body).
func NoContent(c *gin.Context) { c.JSON(http.StatusNoContent, Out{ID: 4}) }

func query(c *gin.Context, key string) string {
	key = strings.ToLower(key)
	return c.Query(key)
}

// Lower: B1 (the name is lowered before the read).
func Lower(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"v": query(c, "Page")}) }

func write(c *gin.Context, code int, v any) {
	v = gin.H{"data": v}
	c.JSON(code, v)
}

// Wrapped: B1 (the value is wrapped before it is written).
func Wrapped(c *gin.Context) { write(c, http.StatusOK, Out{ID: 5}) }

// EmbPtr: B4.
func EmbPtr(c *gin.Context) {
	var v WithBase
	if err := c.ShouldBindJSON(&v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PtrReqH: a nested required field.
func PtrReqH(c *gin.Context) {
	var v PtrReq
	if err := c.ShouldBindJSON(&v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// FormValidator binds through a method, as realworld-gin's validators.
type FormValidator struct {
	Form Form
}

// NewFormValidator returns a validator.
func NewFormValidator() FormValidator { return FormValidator{} }

// Bind binds the JSON body.
func (v *FormValidator) Bind(c *gin.Context) error {
	return c.ShouldBindJSON(&v.Form)
}

// Validated calls a method of a value returned by a module function.
func Validated(c *gin.Context) {
	v := NewFormValidator()
	if err := v.Bind(c); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "bad"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"name": v.Form.Name})
}

// SinceH: C3.
func SinceH(c *gin.Context) {
	var s Since
	if err := c.ShouldBindQuery(&s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Mux registers the review's net/http routes.
func Mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("POST /late", LateHeader)
	m.HandleFunc("POST /decode", DecodeH)
	m.HandleFunc("POST /decodechecked", DecodeChecked)
	m.HandleFunc("GET /after", AfterWrite)
	m.HandleFunc("GET /helper", HelperThenEncode)
	m.HandleFunc("GET /cond", CondHeader)
	return m
}

// LateHeader: A2 (the header is set after WriteHeader: not sent).
func LateHeader(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Out{ID: 6})
}

func readJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return err
	}
	return nil
}

// DecodeH: B6 (the helper checks the error, the handler ignores it).
func DecodeH(w http.ResponseWriter, r *http.Request) {
	var o Out
	readJSON(r, &o)
	json.NewEncoder(w).Encode(o)
}

// DecodeChecked checks the helper's error.
func DecodeChecked(w http.ResponseWriter, r *http.Request) {
	var o Out
	if err := readJSON(r, &o); err != nil {
		http.Error(w, "bad", http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(o)
}

// AfterWrite: B7 (WriteHeader after the body is never sent).
func AfterWrite(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(Out{ID: 7})
	w.WriteHeader(http.StatusTeapot)
}

func setCreated(w http.ResponseWriter) { w.WriteHeader(http.StatusCreated) }

// HelperThenEncode writes the status in a helper.
func HelperThenEncode(w http.ResponseWriter, r *http.Request) {
	setCreated(w)
	json.NewEncoder(w).Encode(Out{ID: 8})
}

// CondHeader sets the header in a branch.
func CondHeader(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("x") != "" {
		w.Header().Set("Content-Type", "application/json")
	}
	json.NewEncoder(w).Encode(Out{ID: 9})
}
