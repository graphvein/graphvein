package openapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/openapi"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// fixtureDocument indexes a copy of a route fixture (with graphvein.yaml
// set to config when non-empty, or activated from _graphvein.yaml) and
// returns its generated document, with the run's id, time and extractor
// version replaced so the golden survives re-indexing and extractor bumps.
func fixtureDocument(t *testing.T, fixture, cfg string) map[string]any {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("../../testdata/fixtures", fixture))); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "_graphvein.yaml")); err == nil {
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if cfg != "" {
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-20T00:00:00Z", Parser: parse.Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "wazero")}}
	var b bytes.Buffer
	if _, err := index.Emit(ctx, root, s, &b, opt); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.StreamWithWrites(ctx, bytes.NewReader(b.Bytes()), s); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	in, err := openapi.Read(ctx, s, "fixture", c.Explorer.PathValues)
	if err != nil {
		t.Fatal(err)
	}
	doc := openapi.Generate(in)
	if doc.X.Index == nil {
		t.Fatal("no index run")
	}
	doc.X.Index = &openapi.IndexInfo{Run: "RUN", FinishedAt: "FINISHED", Extractor: "graphvein EXTRACTOR"}
	data, err := openapi.Encode(doc, openapi.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestFixtureDocuments checks the generated document of route fixtures
// against their goldens: FastAPI (conditional and settings-default paths,
// a duplicate operation, unmounted routes, one with a path set by you),
// Express (equivalent registrations, wildcards, optional parameters),
// Go (host patterns, wrapper assumptions), Next.js (an unresolved basePath
// set by you for one project), FastAPI auth, imported specs with
// documents_route edges, test-only routes, and every route shape fixture:
// Axum (axum 0.8, 0.3 and the version reader), Express and Pages Router
// API, FastAPI (the main fixture, project exception handlers, keyword
// unpacking), gin/chi/net/http, Next.js App Router and the security rows
// of Express, Axum, gin, chi and Next.js, so the fact -> standard field path runs
// on real framework facts. The goldens are the documents the OpenAPI 3.1
// schema oracle validates.
func TestFixtureDocuments(t *testing.T) {
	cases := []struct{ fixture, config string }{
		{"fastapi", "version: 1\nexplorer:\n  path_values:\n    - key: \"route:app/api/routes/items.py#GET%20%2Flost\"\n      value: /api/lost/{lost_id}\n" +
			"    - key: \"route:app/api/routes/items.py#GET%20%2Fdefault\"\n      value: /never-used\n"},
		{"express", ""},
		{"go-routes", ""},
		{"nextjs-routes", "version: 1\nexplorer:\n  path_values:\n    - key: nextjs_base_path:docs\n      value: /docs\n"},
		{"auth-fastapi", ""},
		{"openapi-routes", ""},
		{"test-scope", ""},
		{"route-shape-axum", ""},
		{"route-shape-axum03", ""},
		{"express-shape", ""},
		{"route-shape-fastapi", ""},
		{"route-shape-fastapi-handlers", ""},
		{"route-shape-fastapi-unpack", ""},
		{"go-shape", ""},
		{"route-shape-axum-versions", ""},
		{"mount-hops", mountHopsConfig},
		{"nextjs-shape", ""},
		{"route-shape-spec", ""},
		{"route-shape-security", ""},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			got := fixtureDocument(t, c.fixture, c.config)
			golden.Check(t, "../../testdata/fixtures/"+c.fixture+".openapi.golden.json", got)
		})
	}
}

// fixtureGoldens are every generated document golden.
func fixtureGoldens(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../testdata/fixtures/*.openapi.golden.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no document goldens (%v)", err)
	}
	return append(files, "testdata/synthetic.golden.json")
}

// TestDocumentInvariants checks every golden document for the path rules
// OpenAPI states and the validator oracle does not check: each operation
// declares exactly the parameters of its path key, no key holds a
// multi-segment wildcard, and no two keys are equal up to parameter names
// (rows OG-11, OG-12, OG-15, OG-26).
func TestDocumentInvariants(t *testing.T) {
	param := regexp.MustCompile(`\{([^{}]*)\}`)
	for _, file := range fixtureGoldens(t) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Paths map[string]map[string]json.RawMessage `json:"paths"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		forms := map[string]string{}
		for key, item := range doc.Paths {
			if strings.Contains(key, "{*") || strings.Contains(key, "...}") {
				t.Errorf("%s: wildcard key %s", file, key)
			}
			form := param.ReplaceAllString(key, "{}")
			if other, dup := forms[form]; dup {
				t.Errorf("%s: equivalent keys %s and %s", file, key, other)
			}
			forms[form] = key
			var want []string
			for _, m := range param.FindAllStringSubmatch(key, -1) {
				want = append(want, m[1])
			}
			for method, raw := range item {
				if strings.HasPrefix(method, "x-") {
					continue
				}
				var op struct {
					Parameters []struct {
						Name     string `json:"name"`
						In       string `json:"in"`
						Required bool   `json:"required"`
					} `json:"parameters"`
				}
				if err := json.Unmarshal(raw, &op); err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, p := range op.Parameters {
					if p.In == "path" {
						if !p.Required {
							t.Errorf("%s: %s %s: path parameter %s not required", file, method, key, p.Name)
						}
						got = append(got, p.Name)
					}
				}
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Errorf("%s: %s %s declares path parameters %v, key has %v", file, method, key, got, want)
				}
			}
		}
	}
}

// TestValidatorOracle validates every golden document with the pinned
// OpenAPI 3.1 validator (scripts/oracle/openapi_validate.mjs). Opt-in:
// GRAPHVEIN_OPENAPI_VALIDATOR names a node_modules directory holding
// @seriousme/openapi-schema-validator@2.11.0 (the script's header says how
// to install it); Node from PATH or GRAPHVEIN_NODE.
func TestValidatorOracle(t *testing.T) {
	modules := os.Getenv("GRAPHVEIN_OPENAPI_VALIDATOR")
	if modules == "" {
		t.Skip("GRAPHVEIN_OPENAPI_VALIDATOR is not set: install the validator as scripts/oracle/openapi_validate.mjs says")
	}
	node := os.Getenv("GRAPHVEIN_NODE")
	if node == "" {
		found, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node is not on PATH and GRAPHVEIN_NODE is not set")
		}
		node = found
	}
	args := append([]string{"../../scripts/oracle/openapi_validate.mjs", modules}, fixtureGoldens(t)...)
	out, err := exec.Command(node, args...).CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
}

// mountHopsConfig sets one value per unresolved mount hop of the
// mount-hops fixture (rows OG-94..OG-96): a chi Route base, one below the
// read prefix /outer, and a gin Group prefix (an empty path below it is the
// prefix itself) with their hashes, a twin hop (ambiguous), a wrapper hop
// from the root with a stale hash (registration changed) and the Handle
// hop of the same routes ("/": it adds nothing), a StripPrefix prefix
// without a hash (warning), a Handle without StripPrefix with a prefix
// (refused: it adds nothing), an Express use prefix whose /items/{id}
// equals a code-read key up to parameter names (refused, and listed as
// may shadow the code-read operation), an Express hop whose only route
// has its own route value (route keys first), an Express hop below the
// read prefix /x, and FastAPI include and router prefixes (one below the
// read include prefix /x),
// and Axum nest prefixes (T029 FU-3): an axum 0.8 crate whose nested `/`
// routes land at the value itself, its twin nests (ambiguous) and an
// axum 0.3 crate whose nested `/` route is not stored, a nest below the
// read prefix /outer (the value is only the hop's part) and a nest below
// another unknown nest (the value is the whole prefix), and a
// nest_service hop whose nested `/` lands at the value with and without a
// trailing slash.
const mountHopsConfig = `version: 1
explorer:
  path_values:
    - key: "mount:goapp/chi.go#Router%20r.Route%28base%29"
      value: /dozzle
      registration: "sha256:45a816f8c72aaa4b56b5958fcba82bbea904667ee50375c56546e568737cec12"
    - key: "mount:goapp/chi.go#Nested%20r.Route%28base%29"
      value: /b
      registration: "sha256:2d958e15aa776baa9408fd92242aafcaa2f239888d0c12b26d8a2a518c625f0a"
    - key: "mount:goapp/chi.go#Twin%20r.Route%28base%29"
      value: /twin
    - key: "mount:goapp/gin.go#Gin%20e.Group%28prefix%29"
      value: /v1
      registration: "sha256:a69607aaa3348d51dd6e2b7903ac9765bade78f715afc28ca6bece46d3d6d94d"
    - key: "mount:goapp/mux.go#ui%20mux"
      value: /stale
      registration: "sha256:0000000000000000000000000000000000000000000000000000000000000000"
    - key: "mount:goapp/mux.go#ServeUI%20ui%28%29"
      value: /
      registration: "sha256:ebf00b7476c4b71a6fa24c14991672c22e7bec6c046012ea80cb193e898d288e"
    - key: "mount:goapp/mux.go#ServeAPI%20root.Handle%28prefix%2B%22%2F%22%29"
      value: /svc
    - key: "mount:goapp/mux.go#ServeNoStrip%20root.Handle%28prefix%2B%22%2F%22%29"
      value: /svc
      registration: "sha256:a1e389fd9de0b34cd43e99e7e54d25e912fa2fbc0de8d818dae058e70554446a"
    - key: "mount:web/app.js#%28module%29%20app.use%28base%29"
      value: /api
      registration: "sha256:24525ad6999912090013fcd042f8ef5712de20a710c3b618b9ff0a653c50cfcc"
    - key: "mount:web/app.js#%28module%29%20app.use%28process.env.ADMIN%29"
      value: /adm
    - key: "route:web/app.js#GET%20%2Fstats"
      value: /admin/stats
    - key: "mount:web/app.js#%28module%29%20outer.use%28process.env.INNER%29"
      value: /in
      registration: "sha256:e239f965e10ba97342e886cef410d616f59fb4d090f067c6fd32ac8f7ef025e0"
    - key: "mount:api/app/main.py#%28module%29%20app.include_router%28prefix%3DPREFIX%29"
      value: /v1
      registration: "sha256:19fed7ab0c812b775072e5ae213277cf0c78525ccd3068c6f94fc27a68cde8df"
    - key: "mount:api/app/main.py#%28module%29%20APIRouter%28prefix%3Dos.environ%5B%22OWN%22%5D%29"
      value: /own
      registration: "sha256:b8f02f4c9f8c314e2392e011e460e05b853afae9c99788c0bc9ceeada0dd1c1b"
    - key: "mount:api/app/main.py#%28module%29%20APIRouter%28prefix%3Dos.environ%5B%22OWN2%22%5D%29"
      value: /own2
      registration: "sha256:abf3b448425b110abdb1c1555188983baa2aee71f1e148169fa39bd94fe8032e"
    - key: "mount:rustapp/src/lib.rs#app%20.nest%28base%29"
      value: /rs
      registration: "sha256:d7d5c6ba3627ef0028bc8eca39ad5c71f1b2556a346514480561b6cea6cffed0"
    - key: "mount:rustapp/src/lib.rs#twin%20r.nest%28%26a%29"
      value: /twin
    - key: "mount:rustold/src/lib.rs#app%20.nest%28%26prefix%29"
      value: /old
      registration: "sha256:9fb73c720b15366140612d5d5a2f0544937a556bfc891590582aaecfb8eacc46"
    - key: "mount:rustapp/src/lib.rs#outer%20.nest%28base%29"
      value: /in
      registration: "sha256:e4a2feb40d91c836330134a17661a3df56c94b204a9cb65007e6198ee3bdbe7c"
    - key: "mount:rustapp/src/lib.rs#stacked%20.nest%28b%29"
      value: /s/t
      registration: "sha256:0053d78b4a9b679bbc3d779ddd2f8b972f7834524ff3f61886c156c8d24af338"
    - key: "mount:rustapp/src/lib.rs#service_hop%20.nest_service%28base%29"
      value: /hs
      registration: "sha256:e938c95cf2745367a7433434065707c85be37d57489ab2483a15cd8c2fb7bb2a"
`
