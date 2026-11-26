package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/packages"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

const architectureTask = "../../docs/contract/claims/architecture-graph.md"

// componentsFixtureRoot copies testdata/fixtures/components to a temporary
// repository and restores the inert names of its README.
func componentsFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/components")); err != nil {
		t.Fatal(err)
	}
	for from, to := range map[string]string{"_graphvein.yaml": "graphvein.yaml", "crates/app/_tauri.conf.json": "crates/app/tauri.conf.json"} {
		if err := os.Rename(filepath.Join(root, filepath.FromSlash(from)), filepath.Join(root, filepath.FromSlash(to))); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// archSynthStream is the crafted snapshot "archsynth" for the rows the
// indexed fixture cannot reach before the deploy-config and IPC steps:
// a Tauri command and its calls (CK2, CE2 and its ghost, AP2, AT1), deploy
// nodes (CD1, CD3 by Dockerfile and by context, an unmapped service), a
// schema set with conflicting dialects (AED), an inferred table access with
// an unbound site (CE3 tier, "may also use"), a test-scope in-process call
// (hidden) and a renders edge (CED).
func archSynthStream(t *testing.T) (*streamWriter, int) {
	w := &streamWriter{t: t, count: map[string]int{}}
	w.rec(map[string]any{"type": "header", "contract_version": 1, "indexer": map[string]any{"name": "crafted", "version": "t026"}, "repository": "archsynth", "generated_at": "2026-11-18T00:00:00Z"})
	for _, f := range []string{"app/Cargo.toml", "app/src/main.rs", "ui/package.json", "ui/src/main.ts", "svc/package.json", "svc/Dockerfile", "svc/src/repo.ts", "svc/src/repo.test.ts", "svc/src/server.ts", "svc/openapi.json", "deploy/compose.yml", "deploy/compose.override.yml", "deploy/compose.deploy.yml", "env/compose.yml", "env/compose.override.yml", "env/.env", "db/schema.sql", "db/store.sql", "db/ledger.sql", "db/models.ts"} {
		w.rec(map[string]any{"type": "file", "path": f, "language": "unknown", "precision": "best_effort", "size": 0, "content_hash": contract.ContentHash(nil)})
	}
	pkg := func(file, name string, attrs map[string]any) {
		attrs["name"], attrs["directory"] = name, path.Dir(file)
		id := w.id("package", file, name)
		w.rec(map[string]any{"type": "node", "identity": id, "kind": "package", "level": 2, "name": name, "qualified_name": name,
			"file": file, "span": w.span(), "content_hash": contract.ContentHash(nil), "attributes": attrs})
	}
	pkg("app/Cargo.toml", "app", map[string]any{"ecosystem": "cargo", "bins": []any{}, "lib": false, "autobins": true, "dependencies": []any{"tokio-postgres"}})
	pkg("ui/package.json", "ui", map[string]any{"ecosystem": "npm", "dependencies": []any{}})
	pkg("svc/package.json", "svc", map[string]any{"ecosystem": "npm", "dependencies": []any{"mysql2"}})
	var edges [][5]any
	edge := func(kind, source, target, provenance string, confidence float64) {
		edges = append(edges, [5]any{kind, source, target, provenance, confidence})
	}
	command := w.node("ipc_command", "app/src/main.rs", "main.COMMAND greet", map[string]any{"framework": "tauri", "command": "greet", "transport": "tauri_ipc"})
	run := w.node("function", "ui/src/main.ts", "run", nil)
	call := w.node("ipc_call", "ui/src/main.ts", "run.INVOKE 'greet'", map[string]any{"framework": "tauri", "command": "greet", "status": "linked", "transport": "tauri_ipc"})
	w.node("ipc_call", "ui/src/main.ts", "run.INVOKE name", map[string]any{"framework": "tauri", "status": "dynamic", "transport": "tauri_ipc"})
	edge("contains", run, call, "fact", 1)
	edge("invokes_command", call, command, "fact", 1)
	// Deploy nodes in the shape internal/extract/deploy emits (the
	// deploy-compose fixture's golden): svc maps by Dockerfile (CD3) and
	// carries a TLS router (AT2) and a URL host naming db (AE2); core maps
	// app by context and names two engines (AED conflict); edge is
	// image-only with a TLS router (AT2 not mapped); docs is an untied
	// database image (AE2 untied).
	w.node("dockerfile", "svc/Dockerfile", "Dockerfile", map[string]any{"stages": []any{}})
	serviceIn := func(file, name string, attrs map[string]any) {
		base := map[string]any{"service": name, "image": nil, "image_name": nil, "image_tag": nil, "image_digest": nil, "build_context_as_written": nil,
			"build_dockerfile_as_written": nil, "depends_on": []any{}, "ports": []any{}, "environment_urls": []any{}, "traefik_routers": []any{}, "interpolated": []any{}}
		for k, v := range attrs {
			base[k] = v
		}
		w.node("deploy_service", file, name, base)
	}
	service := func(name string, attrs map[string]any) { serviceIn("deploy/compose.yml", name, attrs) }
	router := func(name string, tls any) map[string]any {
		return map[string]any{"router": name, "rule": "Host(`example.test`)", "entrypoints": "websecure", "tls": "true", "tls_enabled": tls, "tls_certresolver": "le", "interpolated": []any{}}
	}
	service("svc", map[string]any{"build_context_as_written": "..", "build_dockerfile_as_written": "svc/Dockerfile", "build_dockerfile": "svc/Dockerfile", "build_context": ".",
		"environment_urls": []any{map[string]any{"name": "DATABASE_URL", "scheme": "postgresql", "host": "db", "interpolated": []any{}}},
		"traefik_routers":  []any{router("svc-https", true), router("svc-http", nil)}})
	service("web", map[string]any{"build_context_as_written": "../ui", "build_context": "ui"})
	service("core", map[string]any{"build_context_as_written": "../app", "build_context": "app", "depends_on": []any{"db", "cache"}})
	service("edge", map[string]any{"image": "example/edge:1", "image_name": "example/edge", "image_tag": "1", "traefik_routers": []any{router("edge-https", true)}})
	service("db", map[string]any{"image": "postgres:17", "image_name": "postgres", "image_tag": "17"})
	service("cache", map[string]any{"image": "docker.io/library/mysql:8.0", "image_name": "docker.io/library/mysql", "image_tag": "8.0"})
	service("docs", map[string]any{"image": "mongo", "image_name": "mongo"})
	// CD3 with a Dockerfile in no component directory (no context
	// fallback); CD4: an overlay service tied by name, by Compose's default
	// merge (compose.override.yml) or by assumption (compose.deploy.yml),
	// with none or several candidates.
	service("mig", map[string]any{"build_context_as_written": "..", "build_context": ".", "build_dockerfile_as_written": "db/migrate/Dockerfile", "build_dockerfile": "db/migrate/Dockerfile"})
	service("multi", map[string]any{"build_context_as_written": "../svc", "build_context": "svc"})
	serviceIn("deploy/compose.override.yml", "multi", map[string]any{"build_context_as_written": "../ui", "build_context": "ui"})
	serviceIn("deploy/compose.override.yml", "web", map[string]any{})
	serviceIn("deploy/compose.deploy.yml", "svc", map[string]any{"traefik_routers": []any{router("svc-deploy-https", true)}})
	serviceIn("deploy/compose.deploy.yml", "lonely", map[string]any{})
	serviceIn("deploy/compose.deploy.yml", "multi", map[string]any{})
	// Review fix: a TLS router on a service with
	// traefik.enable=false, a Traefik proxy with exposedbydefault=false in
	// another directory whose services carry no traefik.enable=true, and
	// that directory's .env setting COMPOSE_FILE (CD4 then assumed).
	service("gated", map[string]any{"build_context_as_written": "../ui", "build_context": "ui", "traefik_enable": "false", "traefik_enabled": false, "traefik_routers": []any{router("gated-https", true)}})
	env := map[string]any{"dotenv_compose_file": "env/.env"}
	serviceIn("env/compose.yml", "envweb", map[string]any{"build_context_as_written": "../ui", "build_context": "ui", "dotenv_compose_file": "env/.env"})
	serviceIn("env/compose.yml", "proxy", map[string]any{"image": "traefik:v3", "image_name": "traefik", "image_tag": "v3", "traefik_exposed_by_default": false, "dotenv_compose_file": "env/.env"})
	serviceIn("env/compose.override.yml", "envweb", map[string]any{"traefik_routers": []any{router("envweb-https", true)}, "dotenv_compose_file": env["dotenv_compose_file"]})
	a := w.node("table", "db/schema.sql", "public.a", map[string]any{"set": "db", "dialect": "postgres", "tool": "sql"})
	w.node("table", "db/schema.sql", "public.b", map[string]any{"set": "db", "dialect": "sqlite", "tool": "sql"})
	stock := w.node("table", "db/store.sql", "stock", map[string]any{"set": "store", "tool": "sql"})
	ledger := w.node("table", "db/ledger.sql", "entries", map[string]any{"set": "ledger", "tool": "sql"})
	// AE1 from ORM model tables: the source names the models.
	w.node("table", "db/models.ts", "users", map[string]any{"set": "db/models.ts", "dialect": "postgres", "tool": "drizzle", "source": "model"})
	repo := w.node("function", "svc/src/repo.ts", "load", map[string]any{"table_access_unknown": []any{map[string]any{"row": "CT-S2", "tool": "sql", "reason": "sql_not_literal", "candidates": nil}},
		"table_access": []any{map[string]any{"kind": "reads_table", "target": w.id("table", "db/store.sql", "stock"), "role": "builds"}}})
	edge("reads_table", repo, a, "inferred", 0.9)
	edge("reads_table", repo, stock, "fact", 1)
	books := w.node("function", "app/src/main.rs", "books", nil)
	edge("writes_table", books, ledger, "fact", 1)
	edge("calls", repo, run, "fact", 1)
	testFn := w.node("function", "svc/src/repo.test.ts", "check", nil)
	edge("calls", testFn, run, "fact", 1)
	edge("renders", run, repo, "fact", 1)
	// An HTTP link whose members have no stored auth form (AAD) and whose
	// documenting operations disagree (AA2 mixed).
	fastapiRoute := w.node("route", "svc/src/server.ts", "GET /x", map[string]any{"framework": "fastapi", "method": "GET", "pattern": "/x"})
	expressRoute := w.node("route", "svc/src/server.ts", "GET /y", map[string]any{"framework": "express", "method": "GET", "pattern": "/y", "middleware": []any{}})
	// A FastAPI route declaring two schemes (AA3, one optional) with an
	// unread step, called by a hey-api call whose generated options carry
	// security (AA4).
	declared := w.node("route", "svc/src/server.ts", "GET /z", map[string]any{"framework": "fastapi", "method": "GET", "pattern": "/z",
		"security_dependencies": []any{
			map[string]any{"class": "OAuth2PasswordBearer", "scheme_name": "OAuth2PasswordBearer", "auto_error": true, "instance": "svc/src/deps.py#oauth2", "chain": []any{}},
			map[string]any{"class": "APIKeyHeader", "scheme_name": "Header key", "auto_error": false, "instance": "svc/src/deps.py#key", "chain": []any{}},
		},
		"security_dependencies_unknown": []any{map[string]any{"reason": "unresolved dependency", "expression": "helper.check", "chain": []any{}}}})
	// An Express route with mounted middleware (AA5, one from an auth
	// library, one unread form) and a declaring route argument (AA1), and
	// an Axum route with layers (AA6, a closed-list layer and an unread
	// conditional layer).
	mounted := w.node("route", "svc/src/server.ts", "GET /m", map[string]any{"framework": "express", "method": "GET", "pattern": "/m",
		"middleware":          []any{"checks.required"},
		"middleware_declares": []any{map[string]any{"index": 0, "name": "checks.required", "library": "express-jwt", "detail": "expressjwt", "option": "credentialsRequired", "optional": false}},
		"mounted_middleware": []any{
			map[string]any{"name": "logger", "prefix": "/", "file": "svc/src/server.ts", "span": w.span()},
			map[string]any{"name": "passport.authenticate('jwt')", "prefix": "/m", "file": "svc/src/server.ts", "span": w.span(), "declares": map[string]any{"library": "passport", "detail": "jwt"}},
		},
		"mounted_middleware_unknown": []any{map[string]any{"reason": "passed to configure, which may register middleware", "expression": "configure", "file": "svc/src/server.ts", "span": w.span()},
			map[string]any{"reason": "earlier route handler requireAuth (ALL /*) may run first", "expression": "requireAuth", "file": "svc/src/server.ts", "span": w.span()}}})
	layered := w.node("route", "svc/src/server.ts", "GET /l", map[string]any{"framework": "axum", "method": "GET", "pattern": "/l", "middleware": []any{},
		"layers": []any{
			map[string]any{"expression": "ValidateRequestHeaderLayer::bearer(\"token\")", "method": "route_layer", "file": "svc/src/server.ts", "span": w.span(), "declares": map[string]any{"library": "tower-http", "detail": "ValidateRequestHeaderLayer::bearer"}},
			map[string]any{"expression": "axum::middleware::from_fn(check)", "method": "layer", "file": "svc/src/server.ts", "span": w.span(), "middleware_function": "check"},
			map[string]any{"expression": "ServiceBuilder::new().layer(TraceLayer::new_for_http()).layer(CompressionLayer::new()).layer(CorsLayer::permissive())", "method": "layer", "file": "svc/src/server.ts", "span": w.span()},
		},
		"layers_unknown": []any{map[string]any{"reason": "conditional layer", "expression": "CorsLayer::permissive()", "method": "layer", "file": "svc/src/server.ts", "span": w.span()}}})
	for i, target := range []string{fastapiRoute, expressRoute, declared, mounted, layered} {
		attrs := map[string]any{"status": "linked", "paths": []any{}}
		if target == declared {
			attrs["library"] = "hey-api"
			attrs["security"] = []any{map[string]any{"key": "BearerAuth", "scheme": "bearer", "type": "http"}, map[string]any{"in": "query", "name": "key", "type": "apiKey"}}
		}
		c := w.node("client_call", "ui/src/main.ts", "run.GET "+[]string{"/x", "/y", "/z", "/m", "/l"}[i], attrs)
		edge("calls_route", c, target, "inferred", 0.9)
	}
	// A Next.js server action (CE7, AP5; TOP-2 of 2026-11-18): called from
	// another component (a link) and from its own (inside, not drawn).
	action := w.node("server_action", "svc/src/server.ts", "saveDraft", map[string]any{"framework": "nextjs", "transport": "server_action", "action": "saveDraft", "action_scope": "module"})
	for _, file := range []string{"ui/src/main.ts", "svc/src/repo.ts"} {
		c := w.node("action_call", file, "run.ACTION saveDraft", map[string]any{"framework": "nextjs", "transport": "server_action", "status": "linked", "action": action, "form": "form_action"})
		edge("calls_action", c, action, "fact", 1)
	}
	for i, security := range [][]any{{"apiKey"}, {}} {
		op := w.node("api_operation", "svc/openapi.json", []string{"GET /y", "GET /y@2"}[i], map[string]any{"security": security})
		edge("documents_route", op, expressRoute, "inferred", 0.9)
	}
	for _, e := range edges {
		w.edge(e[0].(string), e[1].(string), e[2].(string), e[3].(string), e[4].(float64))
	}
	return w, w.count["node"]
}

// archStaleStream is "archstale": an index without package nodes (an older
// indexer), so its manifest directories are unread (CM-U1).
func archStaleStream(t *testing.T) (*streamWriter, int) {
	w := &streamWriter{t: t, count: map[string]int{}}
	w.rec(map[string]any{"type": "header", "contract_version": 1, "indexer": map[string]any{"name": "crafted", "version": "old"}, "repository": "archstale", "generated_at": "2026-11-18T00:00:00Z"})
	for _, f := range []string{"a/package.json", "a/x.ts", "b.ts"} {
		w.rec(map[string]any{"type": "file", "path": f, "language": "unknown", "precision": "best_effort", "size": 0, "content_hash": contract.ContentHash(nil)})
	}
	w.node("function", "a/x.ts", "x", nil)
	return w, w.count["node"]
}

func ingestArchStreams(t *testing.T, s store.Store) {
	t.Helper()
	for _, build := range []func(*testing.T) (*streamWriter, int){archSynthStream, archStaleStream} {
		w, nodes := build(t)
		var repo string
		for _, line := range strings.SplitN(w.buf.String(), "\n", 2)[:1] {
			var h struct{ Repository string }
			if err := json.Unmarshal([]byte(line), &h); err != nil {
				t.Fatal(err)
			}
			repo = h.Repository
		}
		w.rec(map[string]any{"type": "footer", "counts": map[string]any{"records": w.count["file"] + w.count["node"] + w.count["edge"], "file": w.count["file"], "node": w.count["node"], "edge": w.count["edge"], "diagnostic": 0}})
		ingestStream(t, s, &w.buf, repo, nodes)
	}
}

func archCases() []apiCase {
	return []apiCase{
		{"architecture-components", "fixture", "/api/v0/architecture"},
		{"architecture-components-all", "fixture", "/api/v0/architecture?scope=all"},
		{"architecture-component-web", "fixture", "/api/v0/architecture/component?id=web"},
		{"architecture-component-app", "fixture", "/api/v0/architecture/component?id=" + url.QueryEscape("crates/app")},
		{"architecture-synthetic", "archsynth", "/api/v0/architecture"},
		{"architecture-stale", "archstale", "/api/v0/architecture"},
	}
}

// archBodies serves every architecture golden on both backends and
// returns the decoded bodies by backend and case; tags collects the rows
// the requests reached.
func archBodies(t *testing.T, tags map[string]bool, check bool) map[string]map[string]any {
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	archTag = func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	}
	t.Cleanup(func() { archTag = nil })
	backends(t, func(t *testing.T, w, s store.Store) {
		root := componentsFixtureRoot(t)
		indexRoot(t, root, w)
		ingestArchStreams(t, w)
		empty := t.TempDir()
		got := map[string]any{}
		for _, tc := range archCases() {
			opt := testOptions(root, s)
			base := root
			if tc.repo != "fixture" {
				opt = testOptions(empty, s)
				opt.Repository = tc.repo
				base = empty
			}
			rec := get(t, opt, http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", tc.path, rec.Code, rec.Body.String())
			}
			v := normalizeAPI(t, rec.Body.Bytes())
			if check {
				golden.CheckRoot(t, filepath.Join("testdata", tc.name+".golden.json"), v, base)
			}
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			var plain any
			if err := json.Unmarshal([]byte(golden.ReplaceRoot(string(data), filepath.ToSlash(base))), &plain); err != nil {
				t.Fatal(err)
			}
			got[tc.name] = plain
		}
		mu.Lock()
		bodies[filepath.Base(t.Name())] = got
		mu.Unlock()
	})
	return bodies
}

// TestArchitectureGoldens checks every architecture golden on both
// backends and that both backends produce the same bodies.
func TestArchitectureGoldens(t *testing.T) {
	bodies := archBodies(t, map[string]bool{}, true)
	if bodies["sqlite"] != nil && bodies["postgres"] != nil && !reflect.DeepEqual(bodies["sqlite"], bodies["postgres"]) {
		t.Error("SQLite and Postgres bodies differ")
	}
}

// TestArchitectureErrors checks the parameter errors.
func TestArchitectureErrors(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := componentsFixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		for path, code := range map[string]int{
			"/api/v0/architecture?scope=test":          http.StatusBadRequest,
			"/api/v0/architecture?scope=a&scope=b":     http.StatusBadRequest,
			"/api/v0/architecture/component":           http.StatusBadRequest,
			"/api/v0/architecture/component?id=nope":   http.StatusNotFound,
			"/api/v0/architecture/component?id=e2e":    http.StatusOK,
			"/api/v0/architecture/component?id=.":      http.StatusOK,
			"/api/v0/architecture?scope=production":    http.StatusOK,
			"/api/v0/architecture/component?id=crates": http.StatusNotFound,
		} {
			if rec := get(t, opt, http.MethodGet, path, nil); rec.Code != code {
				t.Errorf("%s: status %d, want %d (%s)", path, rec.Code, code, rec.Body.String())
			}
		}
	})
}

// contractSection returns the row ids of the task file's table under the
// heading that starts with title, in document order.
func contractSection(t *testing.T, title string) []string {
	t.Helper()
	data, err := os.ReadFile(architectureTask)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## "+title)
	if start < 0 {
		t.Fatalf("no section %q in the task file", title)
	}
	section := text[start+1:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	var ids []string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| id |") {
			ids = append(ids, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	return ids
}

func rowIDs(rows []archRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// checkReached requires every non-pending row of rows to be tagged, and
// every tag to name a row of rows or of another family (tag form
// "<row>" or "<row>/<form>").
func checkReached(t *testing.T, rows []archRow, tags map[string]bool) {
	t.Helper()
	reached := map[string]bool{}
	for tag := range tags {
		row, _, _ := strings.Cut(tag, "/")
		reached[row] = true
	}
	for _, r := range rows {
		switch {
		case r.Pending == "" && !reached[r.ID]:
			t.Errorf("row %s is not reached by the fixtures", r.ID)
		case r.Pending != "" && reached[r.ID]:
			t.Errorf("row %s is pending %s but has a branch", r.ID, r.Pending)
		}
	}
}

// TestComponentContract keeps the components table and the code 1:1: the
// task file's rows equal the indexer's rows (internal/extract/packages)
// followed by the server's, in order, and the fixtures reach every server
// row (the indexer's are reached by that package's own test).
func TestComponentContract(t *testing.T) {
	var want []string
	for _, r := range packages.Contract {
		want = append(want, r.ID)
	}
	want = append(want, rowIDs(componentRows)...)
	// CM-U1 is an indexer row the server also reads (unread components).
	if got := contractSection(t, "Claim contract: components"); !reflect.DeepEqual(got, want) {
		t.Errorf("task rows %v\ncode rows %v", got, want)
	}
	tags := map[string]bool{}
	archBodies(t, tags, false)
	checkReached(t, componentRows, tags)
	for _, form := range []string{"CM-U1", "CM-U1/stale", "CK5/cargo-main", "CK5/npm-bin", "CK5/python-scripts", "CK5/go-main", "CD3/dockerfile", "CD3/context", "CDD/service", "CDD/dockerfile-outside", "CD4/assumed", "CD4/default-merge", "CD4/none", "CD4/several", "CD4/compose-file-env"} {
		if !tags[form] {
			t.Errorf("form %s is not reached", form)
		}
	}
}

// TestComponentLinkContract does the same for rows CE.
func TestComponentLinkContract(t *testing.T) {
	if got := contractSection(t, "Claim contract: component links"); !reflect.DeepEqual(got, rowIDs(linkRows)) {
		t.Errorf("task rows %v\ncode rows %v", got, rowIDs(linkRows))
	}
	tags := map[string]bool{}
	archBodies(t, tags, false)
	// CE8 is reached in project mode only (architecture_project_test.go).
	archProjectBodies(t, tags, false)
	checkReached(t, linkRows, tags)
	for _, form := range []string{"CE1/ghost", "CE2/ghost", "CE3/may-also-use", "CE3/builds", "CE5/no-route", "CE8/documents", "CED/inside"} {
		if !tags[form] {
			t.Errorf("form %s is not reached", form)
		}
	}
}

// TestAnnotationContract does the same for rows AP, AA, AT and AE. Rows
// whose facts a later unit adds are pending: no branch, cells stay on the
// default row.
func TestAnnotationContract(t *testing.T) {
	if got := contractSection(t, "Claim contract: annotations"); !reflect.DeepEqual(got, rowIDs(annotationRows)) {
		t.Errorf("task rows %v\ncode rows %v", got, rowIDs(annotationRows))
	}
	tags := map[string]bool{}
	archBodies(t, tags, false)
	checkReached(t, annotationRows, tags)
	for _, form := range []string{"AA2/mixed", "AA3/unknown-steps", "AAD/http", "AAD/tauri", "AAD/sql", "AAD/in-process", "AAD/server-action", "AT3/server",
		"AT2/pointer", "AT2/not-mapped", "AT2/CD4", "AT2/enable-false", "AT2/exposed-by-default", "AA5/earlier-route", "AE2/untied", "AE2/evidence", "AED/conflict"} {
		if !tags[form] {
			t.Errorf("form %s is not reached", form)
		}
	}
}

var forbiddenWording = regexp.MustCompile(`(?i)protected|secure\b|authenticated|encrypted|enforces`)

// TestArchitectureWording checks every string the architecture endpoints
// emit (gate: wording) and that a cell is "proven" only for a proven
// source (gate: glyph).
func TestArchitectureWording(t *testing.T) {
	bodies := archBodies(t, map[string]bool{}, false)
	cells := 0
	var walk func(name string, v any)
	walk = func(name string, v any) {
		switch x := v.(type) {
		case string:
			if forbiddenWording.MatchString(x) {
				t.Errorf("%s: %q uses a forbidden word", name, x)
			}
		case []any:
			for _, item := range x {
				walk(name, item)
			}
		case map[string]any:
			if state, ok := x["state"].(string); ok {
				if source, ok := x["source"].(string); ok {
					cells++
					proven := strings.HasPrefix(source, "proven")
					if (state == cellProven) != proven {
						t.Errorf("%s: cell %v: state %q with source %q", name, x["value"], state, source)
					}
				}
			}
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if k == "node" || k == "calls" || k == "routes" || k == "tables" || k == "commands" || k == "as_written" {
					continue // stored nodes and values, as written by extractors
				}
				walk(name, x[k])
			}
		}
	}
	for _, backend := range bodies {
		for name, body := range backend {
			walk(name, body)
		}
	}
	for _, backend := range archProjectBodies(t, map[string]bool{}, false) {
		for name, body := range backend {
			walk("project "+name, body)
		}
	}
	if cells == 0 {
		t.Fatal("no annotation cell checked")
	}
}
