package app

import "net/http"

// guard answers one path itself: the router behind it is not proven to
// see every path (UP-4, a wrapped router, miniflux's form).
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked" {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WrappedMux serves a mux behind guard at the root mux's "/".
func WrappedMux() http.Handler {
	ui := http.NewServeMux()
	ui.HandleFunc("GET /ui/page/{id}", h)
	root := http.NewServeMux()
	root.Handle("/", guard(ui))
	return root
}

type holder struct{ h http.Handler }

// ServeEscaped stores a mux in a struct field: it escapes (UP-4).
func ServeEscaped() holder {
	m := http.NewServeMux()
	m.HandleFunc("GET /escaped/one/{id}", h)
	return holder{h: m}
}

// StripMux strips a configured prefix (UP-2, StripPrefix prefix).
func StripMux(prefix string) http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /v1/status/{id}", h)
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", h)
	root.Handle(prefix+"/", http.StripPrefix(prefix, api))
	return root
}

// OuterMux hands a mux on below a configured outer pattern without
// StripPrefix (UP-2, outer pattern; the hop adds nothing).
func OuterMux(pattern string) http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /v2/outer/status", h)
	root := http.NewServeMux()
	root.Handle(pattern, api)
	return root
}

// BracesMux strips a constant prefix holding template syntax: the hop
// is stored, but no entry comes from it (UP-2's refusal, row D).
func BracesMux() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /v3/braces/status", h)
	root := http.NewServeMux()
	root.Handle("/{tenant}/", http.StripPrefix("/{tenant}", api))
	return root
}

// ServeStripped serves a mux through a StripPrefix of a configured
// prefix at the serving position: no hop is stored there, so no entry
// (row D).
func ServeStripped(prefix string) {
	api := http.NewServeMux()
	api.HandleFunc("GET /v4/served/status", h)
	http.ListenAndServe(":9105", http.StripPrefix(prefix, api))
}

// TailMux strips a configured prefix below the outer pattern prefix +
// "/api/": the inner mux sees only paths starting
// with /api/, so /api/items/list has an entry and /other/thing/one has
// none (GR-C3's tail check below the unread prefix).
func TailMux(prefix string) http.Handler {
	sub := http.NewServeMux()
	sub.HandleFunc("GET /api/items/list", h)
	sub.HandleFunc("GET /other/thing/one", h)
	root := http.NewServeMux()
	root.Handle(prefix+"/api/", http.StripPrefix(prefix, sub))
	return root
}

// ServeMuxes serves the muxes above.
func ServeMuxes(prefix, pattern string) {
	http.ListenAndServe(":9106", TailMux(prefix))
	http.ListenAndServe(":9101", WrappedMux())
	http.ListenAndServe(":9102", StripMux(prefix))
	http.ListenAndServe(":9103", OuterMux(pattern))
	http.ListenAndServe(":9104", BracesMux())
}
