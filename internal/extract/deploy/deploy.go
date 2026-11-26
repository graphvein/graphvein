// Package deploy states deploy configuration as nodes (T026,
// ADR 0079 option 2A, rows DC of the deploy-config claim contract in
// docs/contract/claims/architecture-graph.md): one level-2 `deploy_service` node
// per compose service and one level-3 `dockerfile` node per Dockerfile.
//
// Every file is read on its own. Compose files are never merged (a
// `compose.override.yml` is a separate set of facts), `extends` and
// `include` are not followed, and environment values are reduced to the
// URL scheme and host so that no secret enters the store. The server maps
// services to components, attaches Traefik TLS to a component's ingress and
// reads database images (rows CD1, CD3, AT2, AE2); this package states only
// what each file says.
package deploy

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// Row is one row of the deploy-config claim contract.
type Row struct {
	ID string
	// Form names the form the row reads, as the task file names it.
	Form string
}

// Contract lists the rows in task-file order. Every form the code reads
// maps to exactly one row; every other file and form is DCD, the single
// unknown branch: nothing is stated.
var Contract = []Row{
	{"DC-S1", "compose service"},
	{"DC-S2", "environment URL"},
	{"DC-T1", "Traefik router label"},
	{"DC-T2", "Traefik entrypoint argument"},
	{"DC-F1", "Dockerfile"},
	{"DCD", "everything else"},
}

// Diagnostic codes of the refusals (DC-S1, DC-F1): the file states no node.
const (
	ComposeUnreadCode    = "deploy_compose_unread"
	DockerfileUnreadCode = "deploy_dockerfile_unread"
)

// claimTag is a test-only hook: every branch reports its row ("<row>" or
// "<row>/<form>").
var claimTag func(row string)

func tag(row string) {
	if claimTag != nil {
		claimTag(row)
	}
}

// ComposeFile reports whether a path has a compose file name of row DC-S1:
// `compose.yml`, `compose.yaml`, `docker-compose*.yml`/`.yaml` and
// `compose.*.yml`/`.yaml`.
func ComposeFile(p string) bool {
	base := path.Base(p)
	ext := path.Ext(base)
	if ext != ".yml" && ext != ".yaml" {
		return false
	}
	stem := strings.TrimSuffix(base, ext)
	return stem == "compose" || strings.HasPrefix(stem, "docker-compose") || strings.HasPrefix(stem, "compose.")
}

// DockerfileName reports whether a path has a Dockerfile name of row DC-F1:
// `Dockerfile`, `Dockerfile.*` and `*.Dockerfile`. `<name>.dockerignore`
// is BuildKit's per-Dockerfile ignore file, not a Dockerfile.
func DockerfileName(p string) bool {
	base := path.Base(p)
	if strings.HasSuffix(base, ".dockerignore") {
		return false
	}
	return base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(base, ".Dockerfile")
}

// Link reads every compose file and Dockerfile among the retained sources
// and returns their nodes and the refusal diagnostics. A failure in one
// file never affects another.
func Link(sources map[string][]byte) ([]contract.Node, []contract.Diagnostic) {
	files := make([]string, 0, len(sources))
	for p := range sources {
		files = append(files, p)
	}
	sort.Strings(files)
	var nodes []contract.Node
	var diags []contract.Diagnostic
	for _, file := range files {
		source := sources[file]
		switch {
		case ComposeFile(file):
			found, err := readCompose(file, source)
			if err != nil {
				tag("DC-S1/refused")
				diags = append(diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: ComposeUnreadCode,
					Message: fmt.Sprintf("%s: %v; no deploy facts from this file (row DC-S1)", file, err)})
				continue
			}
			nodes = append(nodes, found...)
		case DockerfileName(file):
			n, err := readDockerfile(file, source)
			if err != nil {
				tag("DC-F1/refused")
				diags = append(diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: DockerfileUnreadCode,
					Message: fmt.Sprintf("%s: %v; no Dockerfile facts from this file (row DC-F1)", file, err)})
				continue
			}
			nodes = append(nodes, n)
		case strings.HasSuffix(path.Base(file), ".dockerignore") && strings.HasPrefix(path.Base(file), "Dockerfile."):
			tag("DCD/dockerignore")
		}
	}
	// DC-S1 (review fix): Compose reads COMPOSE_FILE from the
	// project directory's .env, which replaces its default file
	// discovery; only whether that line exists is stored, never a value.
	for i := range nodes {
		if nodes[i].Kind != "deploy_service" {
			continue
		}
		nodes[i].Attributes["dotenv_compose_file"] = nil
		env := path.Join(path.Dir(nodes[i].File), ".env")
		if source, ok := sources[env]; ok && setsComposeFile(source) {
			tag("DC-S1/dotenv-compose-file")
			nodes[i].Attributes["dotenv_compose_file"] = env
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, diags
}

// setsComposeFile reports whether a dotenv file has a COMPOSE_FILE line
// (`COMPOSE_FILE=...`, `export COMPOSE_FILE=...` or the `KEY: value`
// form compose-go's dotenv parser also accepts). The value is not read.
func setsComposeFile(source []byte) bool {
	for _, line := range sourceLines(source) {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		rest, ok := strings.CutPrefix(line, "COMPOSE_FILE")
		if !ok {
			continue
		}
		rest = strings.TrimLeft(rest, " \t")
		if strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":") {
			return true
		}
	}
	return false
}

// interpolated reports whether s holds a compose interpolation: `$NAME` or
// `${...}`; `$$` is an escaped dollar sign.
func interpolated(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '$' {
			continue
		}
		next := s[i+1]
		switch {
		case next == '$':
			i++
		case next == '{' || next == '_' || (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z'):
			return true
		}
	}
	return false
}

// imageRef is a parsed image reference: [registry/][path/]name[:tag][@digest].
type imageRef struct {
	name, tag, digest string
}

// parseImage splits an image reference as written. It refuses empty
// names and whitespace; interpolated references are never parsed.
func parseImage(ref string) (imageRef, bool) {
	if ref == "" || strings.ContainsAny(ref, " \t\r\n") || interpolated(ref) {
		return imageRef{}, false
	}
	var r imageRef
	rest := ref
	if at := strings.Index(rest, "@"); at >= 0 {
		r.digest = rest[at+1:]
		rest = rest[:at]
	}
	if colon := strings.LastIndex(rest, ":"); colon > strings.LastIndex(rest, "/") {
		r.tag = rest[colon+1:]
		rest = rest[:colon]
	}
	if rest == "" {
		return imageRef{}, false
	}
	r.name = rest
	return r, true
}

// imageAttrs adds `image`, `image_name`, `image_tag` and `image_digest` to
// attrs (null when absent or not parsed) and names `image` in interp when
// the reference is interpolated.
func imageAttrs(attrs map[string]any, image string, interp *[]string) {
	attrs["image"] = image
	attrs["image_name"], attrs["image_tag"], attrs["image_digest"] = nil, nil, nil
	if interpolated(image) {
		*interp = append(*interp, "image")
		return
	}
	if r, ok := parseImage(image); ok {
		attrs["image_name"] = r.name
		if r.tag != "" {
			attrs["image_tag"] = r.tag
		}
		if r.digest != "" {
			attrs["image_digest"] = r.digest
		}
	}
}

// anyList converts a list to []any (plain JSON), keeping order.
func anyList(list []string) []any {
	out := make([]any, 0, len(list))
	for _, s := range list {
		out = append(out, s)
	}
	return out
}

// sourceLines splits LF-normalized source into lines.
func sourceLines(source []byte) []string {
	text := strings.ReplaceAll(strings.ReplaceAll(string(source), "\r\n", "\n"), "\r", "\n")
	return strings.Split(text, "\n")
}

// spanText returns the normalized source text of lines start..end
// (1-based, inclusive) from column 0 to the end of the last line, the
// half-open span the node's hash covers.
func spanText(lines []string, start, end int) string {
	return strings.Join(lines[start-1:end], "\n")
}

// defaultFileNames and defaultOverrideFileNames are Docker Compose's
// default file discovery, in order of preference (compose-go v2.16.2,
// cli/options.go `DefaultFileNames`, `DefaultOverrideFileNames`): without
// `-f`, Compose loads the first existing default file of the working
// directory and merges the first existing override file into it.
var (
	defaultFileNames         = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}
	defaultOverrideFileNames = []string{"compose.override.yml", "compose.override.yaml", "docker-compose.override.yml", "docker-compose.override.yaml"}
)

// DefaultPair returns the compose files Docker Compose loads by default in
// the repository directory dir: base is "" when no default file exists,
// override is "" when none exists (or no base does). exists reports
// whether a repository-relative path is a stored file. The server uses it
// to tell Compose's own merge from an assumed combination (row CD4).
func DefaultPair(dir string, exists func(string) bool) (base, override string) {
	join := func(name string) string {
		if dir == "." {
			return name
		}
		return dir + "/" + name
	}
	for _, name := range defaultFileNames {
		if exists(join(name)) {
			base = join(name)
			break
		}
	}
	if base == "" {
		return "", ""
	}
	for _, name := range defaultOverrideFileNames {
		if exists(join(name)) {
			return base, join(name)
		}
	}
	return base, ""
}
