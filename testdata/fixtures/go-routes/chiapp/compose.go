package chiapp

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/goroutes/mw"
)

// cors serves r.WithContext, which replaces the context chi keeps its
// routing state in: GR-O7 passes no chi router through it.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(r.Context()))
	})
}

// Compose mounts chi sub-routers (GR-C1, GR-C4, GR-C5).
func Compose(a *api, base string, ok bool) chi.Router {
	r := chi.NewRouter()
	r.Route("/nested/", func(r chi.Router) {
		r.Get("/", a.list)
		r.Get("/{id:[0-9]+}", a.list)
	})
	sub := chi.NewRouter()
	sub.Get("/", a.list)
	sub.Post("/items", a.list)
	r.Mount("/sub", sub)
	r.Mount("/sub2/", newSub(a))
	w := chi.NewRouter()
	w.Get("/w", a.list)
	r.Mount("/wrapped", cors(w))
	c := chi.NewRouter()
	c.Get("/c", a.list)
	r.Mount("/cors", mw.CORS(c))
	r.Mount("/proxy", http.NotFoundHandler())
	d := chi.NewRouter()
	d.Get("/d", a.list)
	r.Mount(base, d)
	r.Route("relative", func(r chi.Router) {
		r.Get("/x", a.list)
	})
	r.Route("/re/{p:.+}", func(r chi.Router) {
		r.Get("/x", a.list)
	})
	chained := r.With(auth).With(logged)
	chained.Get("/chained", a.list)
	if ok {
		r.Route("/opt", func(r chi.Router) {
			r.Get("/x", a.list)
		})
	}
	m := http.NewServeMux()
	m.HandleFunc("GET /m", a.list)
	r.Mount("/mux", m)
	return r
}

func newSub(a *api) chi.Router {
	s := chi.NewRouter()
	s.Get("/x", a.list)
	return s
}
