package cases

import (
	"net/http"
	_ "net/http/pprof"

	"github.com/go-chi/chi/v5"

	"example.com/gvsource/tg"
)

// The ServeMux -> chi hop (row GR-C7) beside siblings the gate cannot see
// (T023), forwarding closures that decide through helpers or
// by method (B1, B4), and the hops whose paths hold (H5, H6).

type registrar interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

func addHealth(x registrar) { x.HandleFunc("/api/", tg.T("H1/sibling")) }

// H1 registers a sibling through an interface-typed helper parameter.
func H1() http.Handler {
	r := chi.NewRouter()
	r.Get("/api/users", tg.T("H1/users"))
	r.Get("/home", tg.T("H1/home"))
	m := http.NewServeMux()
	m.Handle("/", r)
	addHealth(m)
	return m
}

type muxServer struct{ mux *http.ServeMux }

// H2 registers a sibling through a struct field.
func H2() http.Handler {
	r := chi.NewRouter()
	r.Get("/api/users", tg.T("H2/users"))
	m := http.NewServeMux()
	m.Handle("/", r)
	var s muxServer
	s.mux = m
	s.mux.HandleFunc("/api/", tg.T("H2/sibling"))
	return m
}

// H3 serves chi from the DefaultServeMux, which net/http/pprof's init
// registers on.
func H3() http.Handler {
	r := chi.NewRouter()
	r.Get("/debug/pprof/heap2", tg.T("H3/pprof"))
	r.Get("/ok", tg.T("H3/ok"))
	http.Handle("/", r)
	return http.DefaultServeMux
}

// H4 forwards from a closure that answers OPTIONS itself.
func H4() http.Handler {
	r := chi.NewRouter()
	r.Options("/x", tg.T("H4/options"))
	r.Get("/x", tg.T("H4/get"))
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodOptions {
			w.Header().Set("X-Route", "H4/closure")
			return
		}
		r.ServeHTTP(w, req)
	})
	return m
}

// H5 forwards from a closure with a path shadow.
func H5() http.Handler {
	r := chi.NewRouter()
	r.Get("/health", tg.T("H5/health"))
	r.Get("/other", tg.T("H5/other"))
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/health" {
			w.Header().Set("X-Route", "H5/closure")
			return
		}
		r.ServeHTTP(w, req)
	})
	return m
}

// H6 serves chi at "/" beside a disjoint sibling subtree.
func H6() http.Handler {
	r := chi.NewRouter()
	r.Get("/users/{id}", tg.T("H6/user"))
	r.Get("/plugins", tg.T("H6/plugins"))
	m := http.NewServeMux()
	m.Handle("/", r)
	m.HandleFunc("/admin/", tg.T("H6/admin"))
	return m
}

// H7 registers a sibling through a method value.
func H7() http.Handler {
	r := chi.NewRouter()
	r.Get("/api/users", tg.T("H7/users"))
	m := http.NewServeMux()
	m.Handle("/", r)
	hf := m.HandleFunc
	hf("/api/", tg.T("H7/sibling"))
	return m
}

// Hook may register anything on a mux.
var Hook func(*http.ServeMux)

// H8 passes the hop mux to a function value.
func H8() http.Handler {
	r := chi.NewRouter()
	r.Get("/api/users", tg.T("H8/users"))
	m := http.NewServeMux()
	m.Handle("/", r)
	if Hook != nil {
		Hook(m)
	}
	return m
}

func isStatic(r *http.Request) bool { return len(r.URL.Path) > 8 && r.URL.Path[:8] == "/static/" }

// H9 answers /static/ in the closure, deciding through a helper.
func H9() http.Handler {
	r := chi.NewRouter()
	r.Get("/static/app.js", tg.T("H9/static"))
	r.Get("/page", tg.T("H9/page"))
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if isStatic(req) {
			w.Header().Set("X-Route", "H9/closure")
			return
		}
		r.ServeHTTP(w, req)
	})
	return m
}

func isAPI(r *http.Request) bool { return len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" }

// H10 forwards only when a helper says so.
func H10() http.Handler {
	r := chi.NewRouter()
	r.Get("/api/x", tg.T("H10/api"))
	r.Get("/web", tg.T("H10/web"))
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if isAPI(req) {
			r.ServeHTTP(w, req)
		}
	})
	return m
}
