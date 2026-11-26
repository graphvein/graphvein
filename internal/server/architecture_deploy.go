package server

import (
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/deploy"
)

// This file holds the annotation rows that read deploy-config facts (T026,
// ADR 0079): AT2, Traefik TLS at a component's ingress (O3
// (a): never the link's TLS cell, which keeps "?" with a pointer), and
// AE2, a database image a component's compose service names. The facts
// are the `deploy_service` nodes of internal/extract/deploy, each read
// from one compose file; nothing here combines two files.

// cellAsConfigured is the state of a cell whose value a deploy-config file
// states ("as configured in <file>"); it never renders as proven.
const cellAsConfigured = "as_configured"

// archIngress is one Traefik router with `tls=true` on a compose service
// that CD3 maps to the component (AT2).
type archIngress struct {
	Router string `json:"router"`
	// Basis is set when the service maps by CD4 (Compose's default merge
	// or an assumed combination of files).
	Basis     string            `json:"basis,omitempty"`
	Service   string            `json:"service"`
	File      string            `json:"file"`
	AsWritten archRouterWritten `json:"as_written"`
	Row       string            `json:"row"`
	Text      string            `json:"text"`
}

// archRouterWritten are a router's label values as the compose file writes
// them (stored facts, not the server's wording).
type archRouterWritten struct {
	Rule         *string `json:"rule"`
	Entrypoints  *string `json:"entrypoints"`
	CertResolver *string `json:"cert_resolver"`
}

// tlsRouters returns a service's routers whose `tls` label parsed as true.
func tlsRouters(n contract.Node) []map[string]any {
	var out []map[string]any
	for _, r := range objects(n.Attributes["traefik_routers"]) {
		if enabled, ok := r["tls_enabled"].(bool); ok && enabled {
			out = append(out, r)
		}
	}
	return out
}

func optString(m map[string]any, key string) *string {
	if s, ok := m[key].(string); ok {
		return &s
	}
	return nil
}

// ingress applies row AT2 to the components: a compose service CD3 maps to
// a component carries its TLS routers to that component's ingress. A
// service CD3 maps nowhere lists its TLS routers in the Not proven list.
func (m *archModel) ingress() {
	for _, n := range m.nodesByKind["deploy_service"] {
		routers := tlsRouters(n)
		if len(routers) == 0 {
			continue
		}
		mp := m.serviceMapping(n)
		dir := mp.dir
		if reason := m.traefikIgnored(n, mp); reason != "" {
			// DC-T1/DC-T2: Traefik's Docker provider ignores
			// the container, so its routers prove nothing.
			for _, r := range routers {
				name, _ := r["router"].(string)
				tagRow("AT2/ignored")
				m.unmapped = append(m.unmapped, archNotProven{Kind: "ingress_ignored", Row: "AT2", Text: "TLS router " + name + " in " + n.File + " (service " + n.Name + ") not stated: " + reason, Count: 1})
			}
			continue
		}
		for _, r := range routers {
			name, _ := r["router"].(string)
			text := "ingress TLS at router " + name + " as configured in " + n.File + " (service " + n.Name + ")"
			if c := m.components[dir]; dir != "" && c != nil {
				tagRow("AT2")
				if mp.basis != "" {
					tagRow("AT2/" + mp.row)
					text += ", " + mp.basis
				}
				c.Ingress = append(c.Ingress, archIngress{Router: name, Service: n.Name, File: n.File, Row: "AT2", Text: text, Basis: mp.basis,
					AsWritten: archRouterWritten{Rule: optString(r, "rule"), Entrypoints: optString(r, "entrypoints"), CertResolver: optString(r, "tls_certresolver")}})
				continue
			}
			tagRow("AT2/not-mapped")
			m.unmapped = append(m.unmapped, archNotProven{Kind: "ingress_not_mapped", Row: "AT2", Text: text + ", service not mapped to a component", Count: 1})
		}
	}
	for _, c := range m.components {
		sort.Slice(c.Ingress, func(i, j int) bool {
			if c.Ingress[i].File != c.Ingress[j].File {
				return c.Ingress[i].File < c.Ingress[j].File
			}
			return c.Ingress[i].Router < c.Ingress[j].Router
		})
	}
}

// traefikIgnored is why Traefik's Docker provider may ignore service n,
// "" when it is enabled: `traefik.enable` parses false (its own label, or
// for a CD4 service the label of the same-name services it combines
// with, when they agree), or a Traefik service of a compose file in the
// same directory sets `--providers.docker.exposedbydefault=false` (or a
// value not read) and n has no `traefik.enable=true`.
func (m *archModel) traefikIgnored(n contract.Node, mp serviceMap) string {
	enabled, written := n.Attributes["traefik_enabled"], n.Attributes["traefik_enable"] != nil
	if !written && mp.row == "CD4" {
		var values []any
		for _, sib := range m.nodesByKind["deploy_service"] {
			if sib.Name == n.Name && sib.File != n.File && path.Dir(sib.File) == path.Dir(n.File) && sib.Attributes["traefik_enable"] != nil {
				values = append(values, sib.Attributes["traefik_enabled"])
			}
		}
		if len(values) > 0 {
			written, enabled = true, values[0]
			for _, v := range values[1:] {
				if v != enabled {
					enabled = nil
				}
			}
		}
	}
	if written {
		b, ok := enabled.(bool)
		switch {
		case !ok:
			tagRow("AT2/enable-unknown")
			return "traefik.enable is not read as a boolean"
		case !b:
			tagRow("AT2/enable-false")
			return "traefik.enable=false (Traefik ignores the container)"
		}
		return ""
	}
	for _, t := range m.nodesByKind["deploy_service"] {
		if path.Dir(t.File) != path.Dir(n.File) {
			continue
		}
		switch v := t.Attributes["traefik_exposed_by_default"]; {
		case v == false:
			tagRow("AT2/exposed-by-default")
			return "--providers.docker.exposedbydefault=false in " + t.File + " (service " + t.Name + ") and no traefik.enable=true"
		case v != nil && v != true:
			tagRow("AT2/exposed-by-default")
			return "--providers.docker.exposedbydefault in " + t.File + " (service " + t.Name + ") is not read and no traefik.enable=true"
		}
	}
	return ""
}

// dotenvComposeFile is the .env file a service list's compose directory
// stores with a COMPOSE_FILE line (DC-S1), "" when none.
func dotenvComposeFile(nodes []contract.Node) string {
	for _, n := range nodes {
		if f, ok := n.Attributes["dotenv_compose_file"].(string); ok {
			return f
		}
	}
	return ""
}

// ingressPointers is O3 (a): an HTTP link into a component with ingress
// TLS keeps its TLS cell unknown (the file configures the hop to the
// proxy, which the graph has no node for) and points to that ingress.
func ingressPointers(m *archModel, links []*archLink) {
	for _, l := range links {
		if l.Family != familyHTTP {
			continue
		}
		c := m.components[l.Target]
		if c == nil || len(c.Ingress) == 0 {
			continue
		}
		var files []string
		for _, in := range c.Ingress {
			files = append(files, in.File)
		}
		tagRow("AT2/pointer")
		l.Cells.TLS.Source += "; " + l.Target + " has ingress TLS as configured in " + strings.Join(distinct(files), ", ") + " (see the component), not proven for this hop"
		l.Cells.TLS.Rows = append(l.Cells.TLS.Rows, "AT2")
	}
}

// databaseImages is AE2's closed list of database images (official
// images, Docker Hub registry and `library/` stripped).
var databaseImages = map[string]bool{"postgres": true, "mysql": true, "mariadb": true, "mongo": true}

// databaseImage returns the official image name when n's image is on the
// list.
func databaseImage(n contract.Node) (string, bool) {
	name := stringAttr(n, "image_name")
	if name == nil {
		return "", false
	}
	image := *name
	for _, p := range []string{"docker.io/", "index.docker.io/", "registry-1.docker.io/"} {
		image = strings.TrimPrefix(image, p)
	}
	image = strings.TrimPrefix(image, "library/")
	return image, databaseImages[image]
}

// engineFact is one AE2 fact: a database service of the same compose file
// that a component's mapped service names.
type engineFact struct {
	value, file, service string
}

func (f engineFact) source() string {
	return "as configured in " + f.file + " (service " + f.service + ")"
}

// deployEngines collects AE2's facts for a card's accessors and the
// database services of every compose file that no accessor's service
// names (listed on the card as not tied).
func deployEngines(m *archModel, card *archDatabase) ([]engineFact, []string) {
	accessors := map[string]bool{}
	for _, a := range card.Accessors {
		accessors[a.Component] = true
	}
	byFile := map[string]map[string]contract.Node{}
	for _, n := range m.nodesByKind["deploy_service"] {
		if byFile[n.File] == nil {
			byFile[n.File] = map[string]contract.Node{}
		}
		byFile[n.File][n.Name] = n
	}
	tied := map[string]bool{}
	var facts []engineFact
	for _, n := range m.nodesByKind["deploy_service"] {
		mp := m.serviceMapping(n)
		if mp.row != "CD3" || !accessors[mp.dir] {
			continue
		}
		names := stringList(n.Attributes["depends_on"])
		for _, u := range objects(n.Attributes["environment_urls"]) {
			if host, ok := u["host"].(string); ok {
				names = append(names, host)
			}
		}
		for _, name := range distinct(names) {
			target, ok := byFile[n.File][name]
			if !ok {
				continue
			}
			image, ok := databaseImage(target)
			if !ok {
				continue
			}
			value := image
			if tag := stringAttr(target, "image_tag"); tag != nil {
				value += " " + *tag
			} else {
				value += " (no tag written)"
			}
			tied[target.Identity] = true
			if m.tiedTo == nil {
				m.tiedTo = map[string]string{}
			}
			if prev, ok := m.tiedTo[target.Identity]; !ok || card.ID < prev {
				m.tiedTo[target.Identity] = card.ID
			}
			facts = append(facts, engineFact{value: value, file: target.File, service: target.Name})
		}
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].file != facts[j].file {
			return facts[i].file < facts[j].file
		}
		return facts[i].service < facts[j].service
	})
	var untied []string
	for _, n := range m.nodesByKind["deploy_service"] {
		if _, ok := databaseImage(n); ok && !tied[n.Identity] {
			untied = append(untied, untiedText(n)+" to this card")
		}
	}
	sort.Strings(untied)
	return dedupeFacts(facts), untied
}

func untiedText(n contract.Node) string {
	image, _ := n.Attributes["image"].(string)
	return "deploy config names " + image + " (" + n.File + ", service " + n.Name + "), not tied"
}

func dedupeFacts(facts []engineFact) []engineFact {
	seen := map[engineFact]bool{}
	var out []engineFact
	for _, f := range facts {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// engineFromDeploy applies AE2 to a card on top of AE1's result. Without
// any stored dialect, one distinct engine value gives the cell. With AE1
// proven and agreeing, or with stored dialects that prove nothing (they
// disagree or cover only some tables), the facts are evidence lines and
// the cell stays. Disagreeing facts, or a fact that disagrees with AE1,
// make the cell unknown with every fact listed (AED).
func engineFromDeploy(m *archModel, card *archDatabase, cell archCell, evidence []string, dialectsStored bool) (archCell, []string) {
	facts, untied := deployEngines(m, card)
	for _, u := range untied {
		tagRow("AE2/untied")
		evidence = append(evidence, u)
	}
	if len(facts) == 0 {
		return cell, evidence
	}
	values := map[string]bool{}
	var listed []string
	for _, f := range facts {
		values[f.value] = true
		listed = append(listed, f.value+" "+f.source())
	}
	agreesWithAE1 := cell.State == cellProven
	for _, f := range facts {
		if !strings.HasPrefix(f.value, "postgres ") {
			agreesWithAE1 = false
		}
	}
	switch {
	case len(values) == 1 && cell.State != cellProven && !dialectsStored:
		tagRow("AE2")
		return archCell{Value: facts[0].value, State: cellAsConfigured, Source: facts[0].source(), Rows: []string{"AE2"}}, evidence
	case len(values) == 1 && agreesWithAE1:
		tagRow("AE2/evidence")
		return cell, append(evidence, "deploy config: "+listed[0])
	case cell.State != cellProven && dialectsStored:
		tagRow("AE2/evidence")
		for _, l := range listed {
			evidence = append(evidence, "deploy config: "+l)
		}
		return cell, evidence
	}
	tagRow("AED/conflict")
	reason := "unknown: deploy config facts conflict (" + strings.Join(listed, "; ") + ")"
	if cell.State == cellProven {
		reason = "unknown: deploy config (" + strings.Join(listed, "; ") + ") disagrees with the " + strings.TrimPrefix(cell.Source, "proven: ")
	}
	return archCell{Value: "engine ?", State: cellUnknown, Source: reason, Rows: []string{"AED"}}, append(evidence, listed...)
}

// untiedDatabases lists database services when no card exists to carry
// them (AE2: "deploy config names <image>, not tied").
func untiedDatabases(m *archModel, g *archGraph) {
	if len(g.databases) > 0 {
		return
	}
	var texts []string
	for _, n := range m.nodesByKind["deploy_service"] {
		if _, ok := databaseImage(n); ok {
			texts = append(texts, untiedText(n)+" to a database card")
		}
	}
	sort.Strings(texts)
	for _, t := range texts {
		tagRow("AE2/untied")
		g.notProven = append(g.notProven, archNotProven{Kind: "database_not_tied", Row: "AE2", Text: t, Count: 1})
	}
}

// serviceMap is a compose service's mapping to a component (rows CD3,
// CD4): dir is "" when none, with the reason; basis is CD4's stated basis.
type serviceMap struct {
	dir, row, tag, basis, reason string
}

// serviceMapping returns n's CD3/CD4 mapping, computed once per model.
func (m *archModel) serviceMapping(n contract.Node) serviceMap {
	if m.services == nil {
		m.mapServices()
	}
	return m.services[n.Identity]
}

// directMapping is row CD3. A written Dockerfile decides alone: its
// resolved directory must be a component directory (AG-B3-2 (b): no
// fallback to the context). Without one, the resolved context must be a
// component directory. A service with neither build nor image is left to
// CD4 (reason "no build or image of its own").
func (m *archModel) directMapping(n contract.Node) serviceMap {
	_, hasContext := n.Attributes["build_context"]
	hasBuild := hasContext || stringAttr(n, "build_context_as_written") != nil || stringAttr(n, "build_dockerfile_as_written") != nil
	if !hasBuild {
		if stringAttr(n, "image") != nil {
			return serviceMap{reason: "image only, no build"}
		}
		return serviceMap{reason: "no build or image of its own"}
	}
	if stringAttr(n, "build_dockerfile_as_written") != nil {
		f := stringAttr(n, "build_dockerfile")
		if f == nil {
			return serviceMap{reason: "Dockerfile path interpolated, absolute or outside the repository"}
		}
		if _, ok := m.components[path.Dir(*f)]; ok {
			return serviceMap{dir: path.Dir(*f), row: "CD3", tag: "CD3/dockerfile"}
		}
		tagRow("CDD/dockerfile-outside")
		return serviceMap{reason: "Dockerfile " + *f + " is in no component directory"}
	}
	ctx := stringAttr(n, "build_context")
	if ctx == nil {
		return serviceMap{reason: "build context interpolated, remote or outside the repository"}
	}
	if _, ok := m.components[*ctx]; ok {
		return serviceMap{dir: *ctx, row: "CD3", tag: "CD3/context"}
	}
	return serviceMap{reason: "build context " + *ctx + " is no component directory"}
}

// mapServices applies CD3 to every service, then CD4 (AG-B3-1 (b)) to the
// services with neither build nor image: the same-name services of the
// other compose files in the same directory that CD3 maps must all map to
// one component. The basis is stated: Compose's default merge when the two
// files are the directory's default file and override file (DefaultPair),
// else the assumption that the files are combined.
func (m *archModel) mapServices() {
	m.services = map[string]serviceMap{}
	stored := map[string]bool{}
	for _, f := range m.files {
		stored[f] = true
	}
	exists := func(p string) bool { return stored[p] }
	type key struct{ dir, name string }
	byName := map[key][]contract.Node{}
	for _, n := range m.nodesByKind["deploy_service"] {
		m.services[n.Identity] = m.directMapping(n)
		k := key{path.Dir(n.File), n.Name}
		byName[k] = append(byName[k], n)
	}
	for _, n := range m.nodesByKind["deploy_service"] {
		if m.services[n.Identity].reason != "no build or image of its own" {
			continue
		}
		dir := path.Dir(n.File)
		dirs := map[string]bool{}
		var files []string
		defaultMerge := ""
		base, override := deploy.DefaultPair(dir, exists)
		for _, sib := range byName[key{dir, n.Name}] {
			mp := m.services[sib.Identity]
			if sib.File == n.File || mp.row != "CD3" {
				continue
			}
			dirs[mp.dir] = true
			files = append(files, sib.File)
			if override != "" && ((sib.File == base && n.File == override) || (sib.File == override && n.File == base)) {
				defaultMerge = sib.File
			}
		}
		sort.Strings(files)
		switch {
		case len(dirs) == 0:
			tagRow("CD4/none")
			m.services[n.Identity] = serviceMap{reason: "no build or image of its own, and no same-name service of another compose file in its directory maps"}
		case len(dirs) > 1:
			tagRow("CD4/several")
			m.services[n.Identity] = serviceMap{reason: "same-name services of other compose files (" + strings.Join(files, ", ") + ") map to different components"}
		case defaultMerge != "" && dotenvComposeFile(byName[key{dir, n.Name}]) != "":
			// Review fix: the directory's .env sets COMPOSE_FILE,
			// which replaces the default discovery.
			var target string
			for d := range dirs {
				target = d
			}
			tagRow("CD4/compose-file-env")
			m.services[n.Identity] = serviceMap{dir: target, row: "CD4", tag: "CD4/assumed", basis: "assumption: " + n.File + " is combined with " + strings.Join(files, " or ") + " (" + dotenvComposeFile(byName[key{dir, n.Name}]) + " sets COMPOSE_FILE, which replaces Compose's default file discovery)"}
		case defaultMerge != "":
			var target string
			for d := range dirs {
				target = d
			}
			m.services[n.Identity] = serviceMap{dir: target, row: "CD4", tag: "CD4/default-merge", basis: "merged with service " + n.Name + " of " + defaultMerge + " by Compose's default file discovery"}
		default:
			var target string
			for d := range dirs {
				target = d
			}
			m.services[n.Identity] = serviceMap{dir: target, row: "CD4", tag: "CD4/assumed", basis: "assumption: " + n.File + " is combined with " + strings.Join(files, " or ")}
		}
	}
}
