package routes

import "testing"

func TestFastAPINormalizationPreservesLiteralPunctuation(t *testing.T) {
	for source, want := range map[string]string{
		"//api//{p:path}/":      "/api/{*p}",
		"/{full_path:path}":     "/{*full_path}",
		"/files/{p:path}/meta":  "/files/{p}/meta",
		"/files/x{p:path}":      "/files/x{p}",
		"/files/{p:int}":        "/files/{p}",
		"/time:zone/*/":         "/time:zone/*",
		"/Case/%2F/../{id:int}": "/Case/%2F/../{id}",
		"/":                     "/",
	} {
		if got := pythonNormalize(source); got != want {
			t.Errorf("%q: %q, want %q", source, got, want)
		}
	}
}
