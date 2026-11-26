package cases

import (
	"net/http"

	"example.com/gvsource/tg"
)

// net/http compositions (rows GR-R1, GR-C3) and their
// siblings (T023).

// P1 strips a prefix with a trailing slash: no path.
func P1() http.Handler {
	sub := http.NewServeMux()
	sub.HandleFunc("/users", tg.T("P1/users"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api/", sub))
	return m
}

// P2 has a more specific sibling on the outer mux that takes /api/users.
func P2() http.Handler {
	sub := http.NewServeMux()
	sub.HandleFunc("GET /users", tg.T("P2/users"))
	sub.HandleFunc("GET /items", tg.T("P2/items"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", sub))
	m.HandleFunc("/api/users", tg.T("P2/outer"))
	return m
}

// P6 composes {$} and a multi-segment wildcard under StripPrefix.
func P6() http.Handler {
	sub := http.NewServeMux()
	sub.HandleFunc("GET /{$}", tg.T("P6/index"))
	sub.HandleFunc("/files/{p...}", tg.T("P6/files"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", sub))
	return m
}

// P12 registers a host pattern and a method pattern.
func P12() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET example.com/h", tg.T("P12/host"))
	m.HandleFunc("POST /p", tg.T("P12/post"))
	return m
}

// P13 registers a sibling through a struct field: the mux escapes.
func P13() http.Handler {
	sub := http.NewServeMux()
	sub.HandleFunc("GET /users", tg.T("P13/users"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", sub))
	s := muxServer{}
	s.mux = m
	s.mux.HandleFunc("/api/users", tg.T("P13/outer"))
	return m
}

// P18 nests two StripPrefix compositions beside a disjoint sibling and a
// sibling subtree whose redirect takes the bare path.
func P18() http.Handler {
	v2 := http.NewServeMux()
	v2.HandleFunc("GET /status", tg.T("P18/status"))
	v2.HandleFunc("GET /docs", tg.T("P18/docs"))
	api := http.NewServeMux()
	api.Handle("/v2/", http.StripPrefix("/v2", v2))
	api.HandleFunc("/v2/docs/", tg.T("P18/docs-tree"))
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", api))
	m.HandleFunc("/health", tg.T("P18/health"))
	return m
}

// usersHandler is a named handler (row GR-H1).
func usersHandler(w http.ResponseWriter, _ *http.Request) { tg.Mark(w, "usersHandler") }

// Pets has a method handler (row GR-H2).
type Pets struct{}

// List answers the pet list.
func (p *Pets) List(w http.ResponseWriter, _ *http.Request) { tg.Mark(w, "Pets.List") }

// P19 registers named handlers, a method value and http.HandlerFunc.
func P19() http.Handler {
	m := http.NewServeMux()
	p := &Pets{}
	m.HandleFunc("GET /users", usersHandler)
	m.HandleFunc("GET /pets", p.List)
	m.Handle("GET /pets2", http.HandlerFunc(p.List))
	return m
}
