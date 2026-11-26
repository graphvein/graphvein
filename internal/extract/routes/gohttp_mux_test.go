package routes

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// goMuxPatterns are patterns over every branch of the transcribed parser
// and conflict check, valid and invalid, in registration order.
var goMuxPatterns = []string{
	"/", "/a", "/a/", "/a/{$}", "/a/{x}", "/a/{y}", "/a/{x}/b", "/a/{x...}", "/a/{x...}/b",
	"GET /a", "HEAD /a", "POST /a/{x}", "GET /a/{y}/b", "CONNECT /a/../b", "GET /a/../b",
	"example.com/", "example.com/a/{x}", "{host}/a", "GET example.com/a",
	"", "GET", "G@T /a", "get /lower", "GET\t/tab", "GET  /two-spaces",
	"/{x}/{x}", "/{}", "/{1x}", "/{x-y}", "/b{x}", "/{x", "/{$}/more", "/{x...}/more",
	"/%41", "/%zz", "/{é}", "/c/{$}", "/c/", "/c/{d...}", "PUT /c/{d}/", "/c/{e}/",
	"DELETE /d/{id}", "DELETE /d/{name}", "GET /e", "GET /e/", "GET /f/{a}/x", "GET /f/x/{b}",
}

// TestGoMuxPatternOracle checks the transcribed ServeMux pattern parser
// and conflict check (gohttp_mux.go) against Go's own ServeMux: the same
// patterns are accepted, and registering them in order on one mux panics
// on exactly the patterns the transcription reports as conflicting.
func TestGoMuxPatternOracle(t *testing.T) {
	real := http.NewServeMux()
	ours := &goMux{}
	for _, p := range goMuxPatterns {
		var realErr string
		func() {
			defer func() {
				if v := recover(); v != nil {
					realErr = fmt.Sprint(v)
				}
			}()
			real.Handle(p, http.NotFoundHandler())
		}()
		conflict, err := ours.register(p)
		switch {
		case p == "":
			if realErr == "" || err == nil {
				t.Errorf("%q: real %q, ours %v", p, realErr, err)
			}
		case err != nil:
			if !strings.Contains(realErr, err.Error()) {
				t.Errorf("%q: ours %v, real %q", p, err, realErr)
			}
		case conflict != "":
			if !strings.Contains(realErr, "conflicts with pattern \""+conflict+"\"") {
				t.Errorf("%q: ours conflicts with %q, real %q", p, conflict, realErr)
			}
		default:
			if realErr != "" {
				t.Errorf("%q: ours accepts, real %q", p, realErr)
			}
		}
	}
}
