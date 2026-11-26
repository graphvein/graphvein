package cases

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"example.com/gvsource/tg"
)

// Wrappers (row GR-O7) that answer some requests themselves (T023)
// or change the path, and one that keeps it (PW8).

// W1 answers one path itself.
func W1(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("X-Route", "W1/health")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// W2 redirects trailing slashes away.
func W2(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, strings.TrimSuffix(r.URL.Path, "/"), http.StatusMovedPermanently)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// W3 replaces the URL.
func W3(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*r.URL = url.URL{Path: "/rewritten"}
		h.ServeHTTP(w, r)
	})
}

func lower(r *http.Request) { r.URL.Path = strings.ToLower(r.URL.Path) }

// W4 rewrites the path through a helper.
func W4(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lower(r)
		h.ServeHTTP(w, r)
	})
}

// W5 sends a prefix to another handler.
func W5(h http.Handler) http.Handler {
	fs := tg.T("W5/static")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			fs.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// W7 answers CORS preflight requests itself.
func W7(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("X-Route", "W7/preflight")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// W8 only reads the request before serving it.
func W8(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Deny") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// PW1 composes through W1.
func PW1() http.Handler {
	s := http.NewServeMux()
	s.HandleFunc("/health", tg.T("PW1/health"))
	s.HandleFunc("/x1/", tg.T("PW1/x"))
	s.HandleFunc("OPTIONS /o1", tg.T("PW1/options"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", W1(s)))
	return m
}

// PW2 composes through W2.
func PW2() http.Handler {
	s := http.NewServeMux()
	s.HandleFunc("/x2/", tg.T("PW2/x"))
	s.HandleFunc("/health2", tg.T("PW2/health"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", W2(s)))
	return m
}

// PW3 composes through W3.
func PW3() http.Handler {
	s := http.NewServeMux()
	s.HandleFunc("/rewritten", tg.T("PW3/rewritten"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", W3(s)))
	return m
}

// PW4 composes through W4.
func PW4() http.Handler {
	s := http.NewServeMux()
	s.HandleFunc("/Low", tg.T("PW4/low"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", W4(s)))
	return m
}

// PW5 composes through W5.
func PW5() http.Handler {
	s := http.NewServeMux()
	s.HandleFunc("/static/a5", tg.T("PW5/static"))
	s.HandleFunc("/health5", tg.T("PW5/health"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", W5(s)))
	return m
}

// PW7 composes through W7: OPTIONS and ANY routes get no path.
func PW7() http.Handler {
	s := http.NewServeMux()
	s.HandleFunc("GET /health7", tg.T("PW7/health"))
	s.HandleFunc("/any7", tg.T("PW7/any"))
	s.HandleFunc("OPTIONS /o7", tg.T("PW7/options"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", W7(s)))
	return m
}

// PW8 composes through W8 (a proven wrapper) and W7 around chi.
func PW8() http.Handler {
	s := http.NewServeMux()
	s.HandleFunc("GET /items", tg.T("PW8/items"))
	c := chi.NewRouter()
	c.Get("/c", tg.T("PW8/chi"))
	c.Options("/c", tg.T("PW8/chi-options"))
	m := chi.NewRouter()
	m.Mount("/api", W8(c))
	m.Mount("/cors", W7(chi.NewRouter()))
	mux := http.NewServeMux()
	mux.Handle("/v1/", http.StripPrefix("/v1", W8(s)))
	mux.Handle("/", m)
	return mux
}

// PC1 mounts chi through W1.
func PC1() http.Handler {
	s := chi.NewRouter()
	s.Get("/health", tg.T("PC1/health"))
	r := chi.NewRouter()
	r.Mount("/api", W1(s))
	return r
}
