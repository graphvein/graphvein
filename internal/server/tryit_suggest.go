package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/projectsuggest"
)

// This file holds base URL suggestions (rows TI-31..TI-41): labelled
// candidates for an environment's base_url, each with its source and the
// reason. A suggestion is never dialable and never used: the UI shows it,
// and only the user's pick, written through the guarded edit (TI-30),
// makes an environment.

// suggestionNotice is shown with every list.
const suggestionNotice = "suggested, not used until you pick one; picking writes an environment to a file you review first"

// Limits of the code and config files read for port literals.
const (
	maxSuggestFiles    = 5000
	maxSuggestFileSize = 1 << 20
)

type suggestionsDoc struct {
	APIVersion  int             `json:"api_version"`
	Repository  string          `json:"repository"`
	Notice      string          `json:"notice"`
	Suggestions []suggestionDoc `json:"suggestions"`
}

// suggestionDoc is one candidate base URL.
type suggestionDoc struct {
	URL string `json:"url"`
	// SourceKind names the form read; Row the claim row.
	SourceKind string `json:"source_kind"`
	Row        string `json:"row"`
	// File and Line locate the evidence; null for a framework default.
	File   *string `json:"file"`
	Line   *int    `json:"line"`
	Reason string  `json:"reason"`
}

// suggestionKinds orders the kinds: facts about this deployment first,
// literals in code next, defaults last.
var suggestionKinds = []string{"compose_published_port", "traefik_router", "openapi_server", "frontend_env",
	"listen_call", "uvicorn_run", "go_listen", "package_json_script", "procfile", "dockerfile_expose", "framework_default"}

func (t *tryItState) suggestionsEndpoint(w http.ResponseWriter, r *http.Request) error {
	s, err := t.scope(r.URL.Query().Get("repository"))
	if err != nil {
		return err
	}
	list, err := suggest(r.Context(), s)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, suggestionsDoc{Repository: s.opt.Repository, Notice: suggestionNotice, Suggestions: list})
	return nil
}

// suggest collects every suggestion for the scope's repository.
func suggest(ctx context.Context, s tryScope) ([]suggestionDoc, error) {
	rd := &reader{ctx: ctx, s: s.opt.Store, repo: s.opt.Repository}
	out := []suggestionDoc{}
	add := func(kind, row, url, file string, line int, reason string) {
		d := suggestionDoc{URL: url, SourceKind: kind, Row: row, Reason: reason}
		if file != "" {
			d.File = newPtr(file)
			if line > 0 {
				d.Line = newPtr(line)
			}
		}
		out = append(out, d)
	}
	services, err := rd.nodes("deploy_service")
	if err != nil {
		return nil, err
	}
	for _, n := range services {
		composeSuggestions(n, add)
	}
	dockerfiles, err := rd.nodes("dockerfile")
	if err != nil {
		return nil, err
	}
	for _, n := range dockerfiles {
		for _, st := range objects(n.Attributes["stages"]) {
			line := intAttr(st["line"])
			for _, word := range stringsOf(st["expose"]) {
				port, proto, _ := strings.Cut(word, "/")
				if p, ok := portNumber(port); ok && (proto == "" || strings.EqualFold(proto, "tcp")) {
					add("dockerfile_expose", rowSuggestDockerfile, loopbackURL("http", p), n.File, line,
						fmt.Sprintf("EXPOSE %s in the stage starting at this line: a container port, reachable from this machine only when it is published", word))
				}
			}
		}
	}
	specs, err := rd.nodes("api_spec")
	if err != nil {
		return nil, err
	}
	for _, n := range specs {
		for _, srv := range objects(n.Attributes["servers"]) {
			u, _ := srv["url"].(string)
			if b, ok := suggestedBase(u); ok {
				add("openapi_server", rowSuggestSpecServer, b.Text, n.File, nodeLine(n), "a server URL of the committed API spec "+n.File)
			}
		}
	}
	if s.project != nil {
		if err := frontendSuggestions(ctx, s, add); err != nil {
			return nil, err
		}
	}
	if !s.opt.noCheckout && s.opt.Root != "" {
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		scanFiles(s.opt.Root, hashes, add)
	}
	routes, err := rd.nodes("route")
	if err != nil {
		return nil, err
	}
	frameworks := map[string]bool{}
	for _, n := range routes {
		if f, ok := n.Attributes["framework"].(string); ok && n.Attributes["scope"] != "test" {
			frameworks[f] = true
		}
	}
	for _, d := range frameworkDefaults {
		if frameworks[d.framework] {
			add("framework_default", rowSuggestDefault, loopbackURL("http", d.port), "", 0,
				fmt.Sprintf("framework default, not read from your code: %s", d.why))
		}
	}
	return orderSuggestions(out), nil
}

// frameworkDefaults are the ports a framework's own tooling uses when the
// code names none (row TI-41). Axum, chi and net/http have no default.
var frameworkDefaults = []struct {
	framework string
	port      int
	why       string
}{
	{"fastapi", 8000, "uvicorn and fastapi dev listen on 8000"},
	{"express", 3000, "Express apps conventionally listen on 3000 (Express itself has no default)"},
	{"nextjs", 3000, "next dev and next start listen on 3000"},
	{"gin", 8080, "gin's Run() without an address listens on :8080"},
}

func orderSuggestions(list []suggestionDoc) []suggestionDoc {
	rank := map[string]int{}
	for i, k := range suggestionKinds {
		rank[k] = i
	}
	key := func(d suggestionDoc) string {
		f, l := "", 0
		if d.File != nil {
			f = *d.File
		}
		if d.Line != nil {
			l = *d.Line
		}
		return fmt.Sprintf("%03d\x00%s\x00%s\x00%09d\x00%s", rank[d.SourceKind], d.URL, f, l, d.Reason)
	}
	sort.Slice(list, func(i, j int) bool { return key(list[i]) < key(list[j]) })
	out := list[:0]
	var last string
	for _, d := range list {
		if k := key(d); k != last {
			out = append(out, d)
			last = k
		}
	}
	return out
}

// nodeLine is a node's first line, or 0 without a span.
func nodeLine(n contract.Node) int {
	if l := lineOf(n); l != nil {
		return *l
	}
	return 0
}

// intAttr reads a stored integer attribute, whichever number type the
// store decoded it to; 0 when absent.
func intAttr(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func stringsOf(v any) []string {
	items, _ := v.([]any)
	var out []string
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func portNumber(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0 && n <= 65535 && !strings.HasPrefix(s, "0")
}

// loopbackURL is a base URL on this machine. 127.0.0.1 is used, not
// localhost, whose lookup may answer ::1 where the server is not bound.
func loopbackURL(scheme string, port int) string {
	return fmt.Sprintf("%s://127.0.0.1:%d", scheme, port)
}

// suggestedBase returns the origin and path of an http(s) URL without
// templates, user info, query or fragment, as an environment would hold it.
func suggestedBase(u string) (config.BaseURL, bool) {
	if strings.ContainsAny(u, "{}$") {
		return config.BaseURL{}, false
	}
	b, err := config.ParseBaseURL(u)
	return b, err == nil
}

// composeSuggestions reads a deploy_service node (rows TI-31, TI-32).
// Only services built from this repository are read for ports: an image
// service (a database, a cache) is not where the API runs.
func composeSuggestions(n contract.Node, add func(kind, row, url, file string, line int, reason string)) {
	service, _ := n.Attributes["service"].(string)
	line := nodeLine(n)
	_, built := n.Attributes["build_context_as_written"].(string)
	if !built {
		_, built = n.Attributes["build_context"].(string)
	}
	if built {
		for _, p := range objects(n.Attributes["ports"]) {
			if p["interpolated"] == true {
				continue
			}
			host, published, target, ok := composePublished(p)
			if !ok {
				continue
			}
			add("compose_published_port", rowSuggestCompose, fmt.Sprintf("http://%s:%d", host, published), n.File, line,
				fmt.Sprintf("Compose service %s publishes container port %s on port %d of this machine", service, target, published))
		}
	}
	for _, rt := range objects(n.Attributes["traefik_routers"]) {
		if rt["interpolated"] == true {
			continue
		}
		rule, _ := rt["rule"].(string)
		host, ok := traefikHost(rule)
		if !ok {
			continue
		}
		scheme := "http"
		if rt["tls_enabled"] == true || rt["tls_certresolver"] != nil {
			scheme = "https"
		}
		name, _ := rt["router"].(string)
		add("traefik_router", rowSuggestTraefik, scheme+"://"+host, n.File, line,
			fmt.Sprintf("Traefik router %s of Compose service %s matches Host(`%s`)", name, service, host))
	}
}

var composeShort = regexp.MustCompile(`^(?:(\[[0-9a-fA-F:.]+\]|[0-9.]+):)?(\d{1,5}):(\d{1,5})(?:/(tcp|TCP))?$`)

// composePublished reads a short ("8000:80", "127.0.0.1:8000:80") or long
// ({published, target}) ports entry with one literal published port.
// Ranges, UDP and entries without a published port give nothing.
func composePublished(p map[string]any) (host string, published int, target string, ok bool) {
	host = "127.0.0.1"
	if text, isShort := p["as_written"].(string); isShort {
		m := composeShort.FindStringSubmatch(strings.Trim(text, `"'`))
		if m == nil {
			return "", 0, "", false
		}
		if ip := strings.Trim(m[1], "[]"); ip != "" {
			addr, err := netip.ParseAddr(ip)
			if err != nil {
				return "", 0, "", false
			}
			if !addr.IsUnspecified() {
				host = m[1]
			}
		}
		published, ok = portNumber(m[2])
		return host, published, m[3], ok
	}
	if proto, _ := p["protocol"].(string); proto != "" && !strings.EqualFold(proto, "tcp") {
		return "", 0, "", false
	}
	switch v := p["published"].(type) {
	case string:
		published, ok = portNumber(v)
	case float64:
		published, ok = int(v), v > 0 && v <= 65535 && v == float64(int(v))
	}
	switch v := p["target"].(type) {
	case string:
		target = v
	case float64:
		target = strconv.Itoa(int(v))
	}
	return host, published, target, ok
}

var traefikHostRule = regexp.MustCompile("^Host\\(`([A-Za-z0-9.-]+)`\\)(?:\\s*&&\\s*PathPrefix\\(`[^`]*`\\))?$")

// traefikHost reads a rule naming exactly one literal host.
func traefikHost(rule string) (string, bool) {
	m := traefikHostRule.FindStringSubmatch(strings.TrimSpace(rule))
	if m == nil {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

// frontendSuggestions reads, in project mode, the addresses other members
// call this member at, as `graphvein project suggest` reads them (env
// files, dev-server proxies, define constants): an address only becomes a
// suggestion here when the suggestion pass matched it to this member
// (row TI-35).
func frontendSuggestions(ctx context.Context, s tryScope, add func(kind, row, url, file string, line int, reason string)) error {
	calls, err := projectsuggest.ReadCalls(ctx, s.opt.Store, *s.project)
	if err != nil {
		return fmt.Errorf("reading stored calls: %w", err)
	}
	res, err := projectsuggest.Suggest(ctx, *s.project, calls)
	if err != nil {
		return fmt.Errorf("reading suggestions: %w", err)
	}
	for _, p := range res.Proposals {
		if p.Provider != s.opt.Repository {
			continue
		}
		for _, ev := range p.Evidence {
			if ev.Member != p.Member || ev.Row == projectsuggest.RowComposeEnvironment {
				// The provider's own ports, and Compose service names only
				// containers resolve, are not addresses for this machine.
				continue
			}
			b, ok := suggestedBase(ev.Value)
			if !ok {
				continue
			}
			add("frontend_env", rowSuggestFrontend, b.Origin(), ev.Member+"/"+ev.File, ev.Line,
				fmt.Sprintf("%s calls %s at this address (%s, read as for project suggestions)", ev.Member, s.opt.Repository, ev.Key))
		}
	}
	return nil
}

// Literal forms read from code and config files (rows TI-36..TI-40). Each
// is matched on one line; anything else is not read (TI-D).
var (
	jsListen      = regexp.MustCompile(`\.listen\(\s*(\d{2,5})\s*[,)]`)
	uvicornRun    = regexp.MustCompile(`\buvicorn\.run\(.*\bport\s*=\s*(\d{2,5})\b`)
	goListen      = regexp.MustCompile(`(ListenAndServe(TLS)?|\.Run)\(\s*"[A-Za-z0-9.\-]*:(\d{2,5})"`)
	cliPort       = regexp.MustCompile(`(?:--port[= ]|(?:^|\s)-p\s+)(\d{2,5})\b`)
	procfileBind  = regexp.MustCompile(`(?:--bind[= ]|(?:^|\s)-b\s+)[A-Za-z0-9.\-]*:(\d{2,5})\b`)
	jsExtensions  = map[string]bool{".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".mts": true, ".cts": true}
	testPathParts = []string{"test", "tests", "__tests__", "spec", "specs", "e2e", "testdata", "fixtures"}
)

// scanFiles reads the indexed files that may hold port literals. Only files
// the stored index lists are read, at most maxSuggestFiles, each at most
// maxSuggestFileSize; test files are skipped.
func scanFiles(root string, hashes map[string]string, add func(kind, row, url, file string, line int, reason string)) {
	files := make([]string, 0, len(hashes))
	for f := range hashes {
		files = append(files, f)
	}
	sort.Strings(files)
	read := 0
	for _, f := range files {
		base := path.Base(f)
		ext := path.Ext(f)
		var kind string
		switch {
		case base == "Procfile":
			kind = "procfile"
		case base == "package.json":
			kind = "package_json_script"
		case jsExtensions[ext]:
			kind = "listen_call"
		case ext == ".py":
			kind = "uvicorn_run"
		case ext == ".go" && !strings.HasSuffix(f, "_test.go"):
			kind = "go_listen"
		default:
			continue
		}
		if testPath(f) {
			continue
		}
		if read++; read > maxSuggestFiles {
			return
		}
		data, err := readSmall(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			continue // a file that cannot be read gives no suggestion
		}
		switch kind {
		case "package_json_script":
			packageScripts(f, data, add)
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 0, 64<<10), maxSuggestFileSize)
		for line := 1; sc.Scan(); line++ {
			text := sc.Text()
			switch kind {
			case "procfile":
				for _, re := range []*regexp.Regexp{cliPort, procfileBind} {
					if m := re.FindStringSubmatch(text); m != nil {
						if p, ok := portNumber(m[1]); ok {
							add(kind, rowSuggestProcfile, loopbackURL("http", p), f, line, "a Procfile process listens on port "+m[1])
						}
					}
				}
			case "listen_call":
				if m := jsListen.FindStringSubmatch(text); m != nil {
					if p, ok := portNumber(m[1]); ok {
						add(kind, rowSuggestListen, loopbackURL("http", p), f, line, "listen("+m[1]+") in "+path.Base(f))
					}
				}
			case "uvicorn_run":
				if m := uvicornRun.FindStringSubmatch(text); m != nil {
					if p, ok := portNumber(m[1]); ok {
						add(kind, rowSuggestUvicorn, loopbackURL("http", p), f, line, "uvicorn.run(..., port="+m[1]+") in "+path.Base(f))
					}
				}
			case "go_listen":
				if m := goListen.FindStringSubmatch(text); m != nil {
					if p, ok := portNumber(m[3]); ok {
						scheme := "http"
						if m[2] != "" {
							scheme = "https"
						}
						add(kind, rowSuggestGoListen, loopbackURL(scheme, p), f, line, m[1]+"(\":"+m[3]+"\") in "+path.Base(f))
					}
				}
			}
		}
	}
}

func testPath(f string) bool {
	for _, part := range strings.Split(path.Dir(f), "/") {
		for _, t := range testPathParts {
			if part == t {
				return true
			}
		}
	}
	base := path.Base(f)
	return strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
}

func readSmall(p string) ([]byte, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSuggestFileSize {
		return nil, fmt.Errorf("%s: not a regular file of at most %d bytes", p, maxSuggestFileSize)
	}
	return os.ReadFile(p)
}

// packageScripts reads package.json scripts with a literal --port N or
// -p N (row TI-39).
func packageScripts(file string, data []byte, add func(kind, row, url, file string, line int, reason string)) {
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return
	}
	names := make([]string, 0, len(pkg.Scripts))
	for name := range pkg.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := cliPort.FindStringSubmatch(pkg.Scripts[name])
		if m == nil {
			continue
		}
		p, ok := portNumber(m[1])
		if !ok {
			continue
		}
		add("package_json_script", rowSuggestPackage, loopbackURL("http", p), file, lineOfText(data, strconv.Quote(name)+":"),
			fmt.Sprintf("package.json script %q passes port %s", name, m[1]))
	}
}

// lineOfText is the 1-based line of the first occurrence of needle, or 0.
func lineOfText(data []byte, needle string) int {
	i := bytes.Index(data, []byte(needle))
	if i < 0 {
		return 0
	}
	return bytes.Count(data[:i], []byte("\n")) + 1
}
