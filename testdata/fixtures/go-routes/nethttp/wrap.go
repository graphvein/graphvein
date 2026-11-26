package nethttp

import (
	"context"
	"net/http"

	"example.com/goroutes/mw"
)

// Path-preserving wrappers (GR-O7) at composition and serving
// positions, and the servers whose Handler is set by an assignment
// (GR-C6).

type auth struct{ realm string }

type authKey struct{}

func newAuth(realm string) *auth { return &auth{realm} }

// require keeps the path: it reads the request, rebinds it to
// r.WithContext and passes it to a method of its own type that only reads.
func (a *auth) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.allowed(r) {
			http.Error(w, a.realm, http.StatusUnauthorized)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), authKey{}, true))
		next.ServeHTTP(w, r)
	})
}

func (a *auth) allowed(r *http.Request) bool {
	user, _, ok := r.BasicAuth()
	return ok && user != "" && mw.ClientIP(r) != ""
}

// identity returns its handler.
func identity(h http.Handler) http.Handler { return h }

// Wrapped composes sub-muxes through proven wrappers (paths claimed) and
// through refused ones (no path, wrapped_router).
func Wrapped() *http.ServeMux {
	root := http.NewServeMux()

	api := http.NewServeMux()
	api.HandleFunc("GET /api/items", index)
	a := newAuth("api")
	root.Handle("/api/", mw.CORS(a.require(identity(api))))

	users := http.NewServeMux()
	users.HandleFunc("GET /users/{id}", index)
	root.Handle("/users/", mw.WithUser(users))

	rewritten := http.NewServeMux()
	rewritten.HandleFunc("GET /rw/x", index)
	root.Handle("/rw/", mw.Rewrite(rewritten))

	audited := http.NewServeMux()
	audited.HandleFunc("GET /audit/x", index)
	root.Handle("/audit/", mw.Audit(audited))
	return root
}

// ServeWrapped serves roots through a proven wrapper (no assumption) and
// a refused one (GR-V5), and sets servers' handlers by assignment.
func ServeWrapped(srv *http.Server) {
	direct := http.NewServeMux()
	direct.HandleFunc("GET /direct", index)
	go http.ListenAndServe(":8093", mw.CORS(direct))

	assumed := http.NewServeMux()
	assumed.HandleFunc("GET /assumed", index)
	go http.ListenAndServe(":8094", mw.Rewrite(assumed))

	inner := http.NewServeMux()
	inner.HandleFunc("GET /inner-assumed", index)
	go http.ListenAndServe(":8096", mw.CORS(logging(inner)))

	assigned := http.NewServeMux()
	assigned.HandleFunc("GET /assigned", index)
	s := &http.Server{Addr: ":8095"}
	s.Handler = http.StripPrefix("/srv", assigned)
	go s.ListenAndServe()

	param := http.NewServeMux()
	param.HandleFunc("GET /param", index)
	srv.Handler = http.StripPrefix("/param", param)

	other := http.NewServeMux()
	other.HandleFunc("GET /other", index)
	var notServer struct{ Handler http.Handler }
	notServer.Handler = http.StripPrefix("/not", other)
}

// Maintained composes a mux that maintenance mode may replace with a plain
// handler (no router): the composition holds through the mux binding,
// conditional. A replacement that forwards to a router is no plain
// handler and blocks the composition.
func Maintained(down bool) *http.ServeMux {
	app := http.NewServeMux()
	app.HandleFunc("GET /app/x", index)
	var h http.Handler = app
	if down {
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
	}
	root := http.NewServeMux()
	root.Handle("/app/", identity(h))

	fwd := http.NewServeMux()
	fwd.HandleFunc("GET /fwd/x", index)
	var f http.Handler = fwd
	if down {
		f = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fwd.ServeHTTP(w, r)
		})
	}
	root.Handle("/fwd/", f)
	return root
}
