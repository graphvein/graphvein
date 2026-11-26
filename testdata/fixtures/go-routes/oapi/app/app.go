package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"

	"example.com/goroutes/oapi/basechi"
	"example.com/goroutes/oapi/petchi"
	"example.com/goroutes/oapi/petgin"
	"example.com/goroutes/oapi/petstd"
)

// Pets implements the ServerInterface of the generated chi, gin and
// net/http servers (row GR-G1: the handlers are its methods).
type Pets struct{}

func (p *Pets) ListPets(w http.ResponseWriter, r *http.Request) {}

func (p *Pets) ShowPetById(w http.ResponseWriter, r *http.Request, petId string) {}

// GinPets implements the gin server's ServerInterface.
type GinPets struct{}

func (p GinPets) ListPets(c *gin.Context) {}

func (p GinPets) ShowPetById(c *gin.Context, petId string) {}

// petsAPI is the BaseURL of the net/http server (a constant, GR-P1).
const petsAPI = "/std"

// Serve instantiates the chi server three times, all with BaseURL "": on
// a new router served at its root, on a router served directly and on a
// router mounted under /api.
func Serve() error {
	srv := &Pets{}
	go http.ListenAndServe(":8101", petchi.Handler(srv))

	r := chi.NewRouter()
	petchi.HandlerWithOptions(srv, petchi.ChiServerOptions{BaseRouter: r})
	go http.ListenAndServe(":8102", r)

	root := chi.NewRouter()
	sub := chi.NewRouter()
	petchi.HandlerFromMux(srv, sub)
	root.Mount("/api", sub)
	return http.ListenAndServe(":8103", root)
}

// ServeGin registers the gin server under the BaseURL /g.
func ServeGin() error {
	e := gin.New()
	petgin.RegisterHandlersWithOptions(e, GinPets{}, petgin.GinServerOptions{BaseURL: "/g"})
	return e.Run(":8104")
}

// ServeStd registers the net/http server under a constant BaseURL.
func ServeStd() error {
	mux := http.NewServeMux()
	petstd.HandlerWithOptions(&Pets{}, petstd.StdHTTPServerOptions{BaseURL: petsAPI, BaseRouter: mux})
	return http.ListenAndServe(":8105", mux)
}

// ServeBase gives the second chi server two different BaseURLs (one not a
// constant): its routes keep their nodes without a path
// (route_prefix_unresolved). si is an interface value, so the handlers
// stay unresolved.
func ServeBase(si basechi.ServerInterface, prefix string) error {
	r := chi.NewRouter()
	basechi.HandlerWithOptions(si, basechi.ChiServerOptions{BaseURL: "/v2", BaseRouter: r})
	basechi.HandlerFromMuxWithBaseURL(si, r, prefix)
	return http.ListenAndServe(":8106", r)
}
