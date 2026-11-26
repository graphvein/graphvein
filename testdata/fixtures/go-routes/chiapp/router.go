// Package chiapp registers chi v5 routes (GR-R2, GR-O1, GR-O2, GR-O4,
// GR-O5).
package chiapp

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/goroutes/consts"
	"example.com/goroutes/handlers"
)

const local = "/local"

type api struct {
	router chi.Router
}

func (a *api) list(w http.ResponseWriter, r *http.Request) {}

func auth(next http.Handler) http.Handler { return next }

func logged(next http.Handler) http.Handler { return next }

// NewRouter is a factory: every return is a proven chi object (GR-O5).
func NewRouter(a *api, method string, options *Options) http.Handler {
	r := chi.NewRouter()
	r.Get("/", a.list)
	r.Post(consts.APIPrefix+"/items", a.list)
	r.Put(local, a.list)
	r.Handle("PUT /handled", http.HandlerFunc(a.list))
	r.HandleFunc("/any", a.list)
	r.Method(http.MethodPatch, "/patch", http.HandlerFunc(a.list))
	r.MethodFunc("get", "/lower", a.list)
	r.MethodFunc(method, "/dynamic", a.list)
	r.MethodFunc("FOO", "/foo", a.list)
	r.Get("relative", a.list)
	r.Get("/files/{id:[0-9]+}", a.list)
	r.Get("/raw/{path:.*}", a.list)
	r.With(auth).Get("/admin", a.list)
	r.With(auth).With(logged).Put("/admin/{id}", a.list)
	admin := r.With(logged)
	admin.Delete("/admin/{id}", a.list)
	r.Route("/v1", func(r chi.Router) {
		r.Get("/ping", a.list)
		r.Group(func(r chi.Router) {
			r.Get("/grouped", a.list)
		})
	})
	r.Route("/v2", v2)
	r.Route("/v3", options.Register)
	sub := r.Route("/v4", func(chi.Router) {})
	sub.Get("/returned", a.list)
	mount(r)
	mux := chi.NewMux()
	mux.Get("/mux", a.list)
	r.Mount("/mux", mux)
	return r
}

func v2(r chi.Router) {
	r.Get("/two", handlers.Health)
}

func mount(r chi.Router) {
	r.Get("/helper", handlers.Health)
}

// Options carries a router the code may or may not have (the generated
// server shape: receiver_unproven).
type Options struct {
	BaseRouter chi.Router
}

// Register is a method callback: not a module function (callback_unread).
func (o *Options) Register(r chi.Router) {}

// Generated reads its router from a struct field first (receiver_unproven).
func Generated(options Options, a *api) {
	r := options.BaseRouter
	if r == nil {
		r = chi.NewRouter()
	}
	r.Get("/generated", a.list)
	r.Group(func(r chi.Router) {
		r.Get("/generated-group", a.list)
	})
	a.router.Get("/field", a.list)
}

// Rebound reassigns its router parameter (param_rebound).
func Rebound(r chi.Router, a *api) {
	if r == nil {
		r = chi.NewRouter()
	}
	r.Get("/rebound", a.list)
}

// Escapes stores its router in a struct literal (escaped_router).
func Escapes(a *api) *api {
	r := chi.NewRouter()
	r.Get("/escaped", a.list)
	return &api{router: r}
}

type factory struct{ r chi.Router }

func (f *factory) Router() chi.Router { return chi.NewRouter() }

// Field returns a field of its receiver: no router built inside it.
func (f *factory) Field() chi.Router { return f.r }

func newFactory() *factory { return &factory{} }

// Chain registers on a method called on a call result: Router builds its
// router inside (GR-O9), Field returns a field
// (factory_method_chain).
func Chain(a *api) {
	newFactory().Router().Get("/chain", a.list)
	f := newFactory()
	f.Router().Get("/typed-factory", a.list)
	newFactory().Field().Get("/field-chain", a.list)
}
