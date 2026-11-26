package cases

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/gvsource/tg"
)

// Router factories used as values: every use of
// a factory's name other than a call the linker reads makes its routers
// escape, so none of the V routes below has a path, although each is
// really served under /api.

func mkV1() http.Handler { r := chi.NewRouter(); r.Get("/v1users", tg.T("V1/users")); return r }
func mkV2() http.Handler { r := chi.NewRouter(); r.Get("/v2users", tg.T("V2/users")); return r }
func mkV3() http.Handler { r := chi.NewRouter(); r.Get("/v3users", tg.T("V3/users")); return r }
func mkV4() http.Handler { r := chi.NewRouter(); r.Get("/v4users", tg.T("V4/users")); return r }
func mkV5() http.Handler { r := chi.NewRouter(); r.Get("/v5users", tg.T("V5/users")); return r }
func mkV6() http.Handler { r := chi.NewRouter(); r.Get("/v6users", tg.T("V6/users")); return r }
func mkV7() http.Handler { r := chi.NewRouter(); r.Get("/v7users", tg.T("V7/users")); return r }
func mkV8() http.Handler { r := chi.NewRouter(); r.Get("/v8users", tg.T("V8/users")); return r }

// Ctors is a constructor registry (a map element).
var Ctors = map[string]func() http.Handler{"api": mkV1}

// V1 serves a factory taken from a map.
func V1() http.Handler {
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", Ctors["api"]()))
	return m
}

// V2 serves a factory taken from a slice.
func V2() http.Handler {
	fs := []func() http.Handler{mkV2}
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", fs[0]()))
	return m
}

type ctorHolder struct{ mk func() http.Handler }

// V3 serves a factory kept in a struct field.
func V3() http.Handler {
	h := ctorHolder{mk: mkV3}
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", h.mk()))
	return m
}

func callIt(f func() http.Handler) http.Handler { return f() }

// V4 serves a factory passed as an argument.
func V4() http.Handler {
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", callIt(mkV4)))
	return m
}

func pick() func() http.Handler { return mkV5 }

// V5 serves a factory returned by a function.
func V5() http.Handler {
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", pick()()))
	return m
}

// V6 serves a factory assigned to a variable.
func V6() http.Handler {
	f := mkV6
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", f()))
	return m
}

// V7 serves a router built in a deferred call.
func V7() (h http.Handler) {
	defer func() {
		m := http.NewServeMux()
		m.Handle("/api/", http.StripPrefix("/api", mkV7()))
		h = m
	}()
	return nil
}

// V8 serves a router compared through its factory before the call.
func V8() http.Handler {
	var f func() http.Handler = mkV8
	if f != nil {
		m := http.NewServeMux()
		m.Handle("/api/", http.StripPrefix("/api", f()))
		return m
	}
	return nil
}

func mkV9() http.Handler { r := chi.NewRouter(); r.Get("/v9users", tg.T("V9/users")); return r }

// V9 serves a factory's chi router, called directly, under StripPrefix at
// a subtree pattern: no row composes a chi router there (wrapped_router).
func V9() http.Handler {
	m := http.NewServeMux()
	m.Handle("/api/", http.StripPrefix("/api", mkV9()))
	return m
}
