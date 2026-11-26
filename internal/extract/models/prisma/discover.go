package prisma

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/lockfile"
)

// Package is one package (a directory with a package.json) that depends
// on Prisma, with the schema the pinned Prisma CLI would load for it
// (the T018g1 task record (internal), "Discovery", verified against the
// 7.9.1 and 7.10.0 CLI sources).
type Package struct {
	// Dir is the repository-relative package directory ("." for the root);
	// Manifest its package.json.
	Dir, Manifest string
	// Version is the locked version of the prisma package (empty when not
	// proven) and Lockfile the lockfile(s) it was read from. Pins is
	// {"prisma": Version}, or nil when the version is not proven (plan
	// shared piece 6: the assumption PR-V1 is still listed).
	Version, Lockfile string
	Pins              map[string]any
	// Major is the Prisma major version the discovery rule is applied
	// for: the locked prisma version's, else the locked @prisma/client
	// version's, else a package.json range's with a fixed major; -1 when
	// none is proven.
	Major int
	// Config is the config file the CLI loads, "" when there is none.
	Config string
	// SchemaPath is the file or directory the rule names; Folder is set
	// when it is a directory (a multi-file schema).
	SchemaPath string
	Folder     bool
	// Files are the schema's .prisma files, sorted; nil when no schema is
	// proven, with the reason in Reason.
	Files  []string
	Reason string
	// Migrations is the directory a loaded Prisma 7 config names in a
	// literal `migrations: { path: ... }` (resolved against the config's
	// directory), "" when there is none or it is not literal. No pass
	// reads it; it is reported as a schema_model_migrations marker so the
	// server's unread-tool rule can name it (T018g1 G1G).
	Migrations string
}

// Diagnostic codes of discovery (info): a package whose schema is not
// proven, and the marker for what is never read.
const (
	codeNotProven = "schema_model_not_proven"
	codeUnread    = "schema_model_unread"
	// codeMigrations marks the migrations directory a config names; its
	// message ends with migrationsMarker and the directory.
	codeMigrations = "schema_model_migrations"
)

// migrationsMarker separates the config path from the directory in a
// schema_model_migrations message ("<config>: <marker><dir>"); the server
// reads the directory back from it.
const migrationsMarker = "Prisma Migrate migrations directory (not read): "

type manifest struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Prisma          json.RawMessage   `json:"prisma"`
}

// Discover finds every package of the repository that depends on prisma
// or @prisma/client and the schema the pinned CLI loads for it, from the
// repository's sources (repository-relative paths). Packages are sorted by
// directory. A package whose schema is not proven has no Files and a
// schema_model_not_proven diagnostic; .prisma files outside every
// discovered schema and package.json "prisma" keys (not read by Prisma 7)
// get schema_model_unread markers.
func Discover(sources map[string][]byte) ([]*Package, []contract.Diagnostic) {
	var out []*Package
	var diags []contract.Diagnostic
	diag := func(file, code, msg string) {
		diags = append(diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: msg})
	}
	var manifests []string
	for p := range sources {
		if path.Base(p) == "package.json" && !underNodeModules(p) {
			manifests = append(manifests, p)
		}
	}
	sort.Strings(manifests)
	marked := map[string]bool{} // configs with a migrations marker
	for _, m := range manifests {
		var pkg manifest
		if json.Unmarshal(sources[m], &pkg) != nil {
			continue
		}
		spec := map[string]string{}
		for _, name := range []string{"prisma", "@prisma/client"} {
			dev, inDev := pkg.DevDependencies[name]
			dep, inDeps := pkg.Dependencies[name]
			switch {
			case inDeps:
				spec[name] = dep
			case inDev:
				spec[name] = dev
			}
		}
		if len(spec) == 0 {
			if len(pkg.Prisma) > 0 {
				diag(m, codeUnread, fmt.Sprintf("%s: the \"prisma\" key of package.json is not read: the package does not depend on Prisma", m))
			}
			continue
		}
		p := &Package{Dir: path.Dir(m), Manifest: m, Major: -1}
		out = append(out, p)
		discoverPackage(sources, p, spec, pkg.Prisma)
		if len(pkg.Prisma) > 0 && p.Major != 5 && p.Major != 6 {
			// Prisma 5 and 6 read package.json "prisma.schema"; 7 does not.
			diag(m, codeUnread, fmt.Sprintf("%s: the \"prisma\" key of package.json is not read by Prisma 7 and is ignored", m))
		}
		if p.Files == nil {
			diag(m, codeNotProven, fmt.Sprintf("%s: no Prisma schema is proven for this package: %s", m, p.Reason))
		}
		if p.Migrations != "" && !marked[p.Config] {
			marked[p.Config] = true
			diag(p.Config, codeMigrations, p.Config+": "+migrationsMarker+p.Migrations)
		}
	}
	// .prisma files outside every discovered schema are never read.
	read := map[string]bool{}
	for _, p := range out {
		for _, f := range p.Files {
			read[f] = true
		}
	}
	var unread []string
	for p := range sources {
		if strings.HasSuffix(p, ".prisma") && !read[p] && !underNodeModules(p) {
			unread = append(unread, p)
		}
	}
	if len(unread) > 0 {
		sort.Strings(unread)
		list := unread
		if len(list) > 5 {
			list = list[:5]
		}
		diag(unread[0], codeUnread, fmt.Sprintf("%d .prisma file(s) are outside every discovered Prisma schema and not read: %s", len(unread), strings.Join(list, ", ")))
	}
	return out, diags
}

func underNodeModules(p string) bool {
	return strings.HasPrefix(p, "node_modules/") || strings.Contains(p, "/node_modules/")
}

// discoverPackage fills a package's pins, config and schema. pkgPrisma is
// the package.json "prisma" member (read by Prisma 5 and 6 only).
func discoverPackage(sources map[string][]byte, p *Package, spec map[string]string, pkgPrisma json.RawMessage) {
	var why string
	if _, ok := spec["prisma"]; ok {
		p.Version, p.Lockfile, why = lockfile.Locked(sources, p.Dir, "prisma", spec["prisma"])
	} else {
		why = "the package does not depend on the prisma CLI package"
	}
	if p.Version != "" {
		p.Pins = map[string]any{"prisma": p.Version}
		p.Major = lockfile.Major(p.Version)
	} else if _, ok := spec["@prisma/client"]; ok {
		if v, _, _ := lockfile.Locked(sources, p.Dir, "@prisma/client", spec["@prisma/client"]); v != "" {
			p.Major = lockfile.Major(v)
		}
	}
	if p.Major < 0 {
		for _, name := range []string{"prisma", "@prisma/client"} {
			if s, ok := spec[name]; ok && lockfile.Major(s) >= 0 {
				p.Major = lockfile.Major(s)
				break
			}
		}
	}
	switch {
	case p.Major < 0:
		p.Reason = "the Prisma version is not proven (" + why + ")"
		return
	case p.Major == 5 || p.Major == 6:
		discoverLegacy(sources, p, pkgPrisma)
		return
	case p.Major != 7:
		p.Reason = fmt.Sprintf("Prisma %d: the discovery rule and the claim contract are verified for Prisma 5, 6 and 7 only", p.Major)
		return
	}
	// The config file: prisma7.config.* only from 7.10.0 on; when the
	// version is not proven and such a file exists, which config the CLI
	// loads is not proven.
	if c := firstConfig(sources, p.Dir, prisma7Configs); c != "" {
		switch {
		case p.Version == "":
			p.Reason = "the Prisma version is not proven and " + c + " is loaded only by Prisma 7.10.0 and later"
			return
		case lockfile.AtLeast(p.Version, 7, 10):
			p.Config = c
		}
	}
	if p.Config == "" {
		p.Config = firstConfig(sources, p.Dir, legacyConfigs)
	}
	base := p.Dir
	if p.Config != "" {
		base = path.Dir(p.Config)
		p.Migrations = configMigrations(sources, p.Config, base)
		state, value := readConfigSchema(string(sources[p.Config]))
		switch state {
		case configUnreadable:
			p.Reason = p.Config + " is not an `export default defineConfig({...})` or object literal the reader can read"
			return
		case configNotLiteral:
			p.Reason = "the schema setting of " + p.Config + " is not a string literal"
			return
		case configLiteral:
			resolveConfigSchema(sources, p, base, value)
			return
		}
	}
	for _, c := range []string{"schema.prisma", "prisma/schema.prisma"} {
		f := path.Join(base, c)
		if _, ok := sources[f]; ok {
			p.SchemaPath, p.Files = f, []string{f}
			return
		}
	}
	p.Reason = "neither " + path.Join(base, "schema.prisma") + " nor " + path.Join(base, "prisma/schema.prisma") + " exists"
}

// configMigrations returns the repository-relative directory of a
// config's literal migrations.path, resolved against the config's
// directory as Prisma 7 resolves it, or "" when the setting is absent, not
// literal, or cannot be placed in the repository (the server's default
// rule, a migrations/ directory beside the schema, still applies).
func configMigrations(sources map[string][]byte, config, base string) string {
	state, value := readConfigMigrations(string(sources[config]))
	if state != configLiteral || !placeable(value) {
		return ""
	}
	target := path.Join(base, value)
	if target == "." || target == ".." || strings.HasPrefix(target, "../") {
		return ""
	}
	return target
}

// placeable reports a relative path with forward slashes only: no empty,
// absolute, backslash or drive-letter path.
func placeable(value string) bool {
	return value != "" && !path.IsAbs(value) && !strings.Contains(value, `\`) && (len(value) < 2 || value[1] != ':')
}

// resolveConfigSchema resolves a config's literal schema path against the
// config file's directory: a file, or a directory whose .prisma files
// (recursively, as Prisma's schema-files-loader reads them) form one
// schema.
func resolveConfigSchema(sources map[string][]byte, p *Package, base, value string) {
	if value == "" || path.IsAbs(value) || strings.Contains(value, `\`) || len(value) > 1 && value[1] == ':' {
		p.Reason = fmt.Sprintf("the schema path %q of %s cannot be placed in the repository", value, p.Config)
		return
	}
	target := path.Join(base, value)
	if target == ".." || strings.HasPrefix(target, "../") {
		p.Reason = fmt.Sprintf("the schema path %q of %s is outside the repository", value, p.Config)
		return
	}
	if _, ok := sources[target]; ok {
		p.SchemaPath, p.Files = target, []string{target}
		return
	}
	prefix := target + "/"
	if target == "." {
		prefix = ""
	}
	var files []string
	isDir := false
	for f := range sources {
		if strings.HasPrefix(f, prefix) {
			isDir = true
			if strings.HasSuffix(f, ".prisma") {
				files = append(files, f)
			}
		}
	}
	if !isDir {
		p.Reason = fmt.Sprintf("the schema path %s named by %s does not exist", target, p.Config)
		return
	}
	if len(files) == 0 {
		p.Reason = fmt.Sprintf("the schema directory %s named by %s holds no .prisma file", target, p.Config)
		return
	}
	sort.Strings(files)
	p.SchemaPath, p.Folder, p.Files = target, true, files
}
