package nethttp

import "net/http"

const apiPrefix = "/api"

// Serve composes sub-muxes into a root served by an http.Server (GR-C3,
// GR-C4, GR-C5).
func Serve(ok bool, base string) *http.Server {
	api := http.NewServeMux()
	api.HandleFunc("GET /users", index)
	api.HandleFunc("POST /users/{id}", index)
	v2 := http.NewServeMux()
	v2.HandleFunc("GET /status", index)
	api.Handle("/v2/", http.StripPrefix("/v2", v2))
	root := http.NewServeMux()
	root.Handle(apiPrefix+"/", http.StripPrefix(apiPrefix, api))
	getOnly := http.NewServeMux()
	getOnly.HandleFunc("GET /read", index)
	getOnly.HandleFunc("POST /write", index)
	root.Handle("GET /ro/", http.StripPrefix("/ro", getOnly))
	hosted := http.NewServeMux()
	hosted.HandleFunc("GET /h", index)
	root.Handle("example.com/hosted/", http.StripPrefix("/hosted", hosted))
	direct := http.NewServeMux()
	direct.HandleFunc("GET /direct/x", index)
	root.Handle("/direct/", direct)
	var m *http.ServeMux
	if ok {
		m = http.NewServeMux()
	} else {
		m = http.NewServeMux()
	}
	m.HandleFunc("GET /either", index)
	root.Handle("/either", m)
	// A pattern that is not constant is on a mux of its own: a sibling of
	// unknown pattern refuses the other compositions of its mux
	// (GR-C3), as fixed's here.
	dyn := http.NewServeMux()
	dyn.HandleFunc("GET /d", index)
	droot := http.NewServeMux()
	droot.Handle(base+"/", http.StripPrefix(base, dyn))
	fixed := http.NewServeMux()
	fixed.HandleFunc("GET /f", index)
	droot.Handle("/fixed/", http.StripPrefix("/fixed", fixed))
	root.HandleFunc("/conflict/", index)
	clash := http.NewServeMux()
	clash.HandleFunc("GET /c", index)
	root.Handle("/conflict/", clash)
	if ok {
		cond := http.NewServeMux()
		cond.HandleFunc("GET /c2", index)
		root.Handle("/cond/", http.StripPrefix("/cond", cond))
	}
	return &http.Server{Handler: root}
}

// Stripped serves a mux under a prefix outside any mux registration: no
// rule composes it, so it is no root (wrapped_router).
func Stripped() error {
	sm := http.NewServeMux()
	sm.HandleFunc("GET /s", index)
	return http.ListenAndServe(":8080", http.StripPrefix("/base", sm))
}

// API returns a mux wrapped in middleware.
func API() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me", index)
	return logging(mux)
}

// Outer mounts the wrapped API and forwards to a mux from a closure: both
// muxes reach their mount only through another call or a closure
// (wrapped_router).
func Outer() *http.ServeMux {
	outer := http.NewServeMux()
	outer.Handle("/v1/", API())
	fwd := http.NewServeMux()
	fwd.HandleFunc("GET /fwd", index)
	outer.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fwd.ServeHTTP(w, r)
	})
	return outer
}
