package nethttp

import (
	"log"
	"net/http"
	"time"
)

// Serving positions (GR-C6): a root served under StripPrefix,
// through a wrapper (assumption GR-V5) or through a closure.

// ServeAll serves muxes under StripPrefix prefixes, through wrappers and
// the path-preserving net/http handlers.
func ServeAll(base string) {
	logged := http.NewServeMux()
	logged.HandleFunc("GET /l", index)
	log.Fatal(http.ListenAndServe(":8081", http.StripPrefix("/logged", logged)))

	nested := http.NewServeMux()
	nested.HandleFunc("GET /n/{id}", index)
	h := http.StripPrefix("/outer", http.StripPrefix("/inner", nested))
	srv := &http.Server{Handler: http.TimeoutHandler(h, time.Second, "")}
	_ = srv.ListenAndServe()

	wrapped := http.NewServeMux()
	wrapped.HandleFunc("GET /w", index)
	go http.ListenAndServe(":8082", logging(http.MaxBytesHandler(wrapped, 1<<20)))

	both := http.NewServeMux()
	both.HandleFunc("GET /both", index)
	go http.ListenAndServe(":8083", both)
	go http.ListenAndServe(":8084", logging(both))

	dyn := http.NewServeMux()
	dyn.HandleFunc("GET /dyn", index)
	go http.ListenAndServe(":8085", http.StripPrefix(base, dyn))

	slash := http.NewServeMux()
	slash.HandleFunc("GET /s", index)
	go http.ListenAndServe(":8086", http.StripPrefix("/slash/", slash))

	closed := http.NewServeMux()
	closed.HandleFunc("GET /c", index)
	go http.ListenAndServe(":8087", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		closed.ServeHTTP(w, r)
	}))

	helped := http.NewServeMux()
	helped.HandleFunc("GET /h", index)
	serveUnder(helped)
}

// serveUnder serves its parameter under a prefix: the caller's mux is
// served there.
func serveUnder(m *http.ServeMux) error {
	return http.ListenAndServe(":8088", http.StripPrefix("/helper", m))
}

// Prod builds a production mux that only a test serves under a prefix: the
// test's serving position does not move its production paths.
func Prod() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /p", index)
	return m
}
