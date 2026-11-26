package mounts

import "net/http"

// guard answers one path itself: it is not proven to pass every path on,
// so the router behind it gets no path (miniflux's middleware form).
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked" {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ui() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /page/{id}", list)
	mux.HandleFunc("POST /form", list)
	return guard(mux)
}

func ServeUI() {
	root := http.NewServeMux()
	root.Handle("/", ui())
	http.ListenAndServe(":8082", root)
}

// ServeAPI strips a prefix read from configuration.
func ServeAPI(prefix string) {
	api := http.NewServeMux()
	api.HandleFunc("GET /v1/status", list)
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", list)
	root.Handle(prefix+"/", http.StripPrefix(prefix, api))
	http.ListenAndServe(":8083", root)
}

// ServeNoStrip hands the router on without StripPrefix: the paths below
// the hop are the served paths, so the hop adds nothing.
func ServeNoStrip(prefix string) {
	api := http.NewServeMux()
	api.HandleFunc("GET /v2/status", list)
	root := http.NewServeMux()
	root.Handle(prefix+"/", api)
	http.ListenAndServe(":8085", root)
}
