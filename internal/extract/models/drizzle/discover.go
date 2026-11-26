package drizzle

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/lockfile"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Input is what the Drizzle front end reads at link time: the TypeScript
// facts of every file (imports and export clauses), the cached model
// payloads of the gated files, the shared module resolver (relative
// imports, tsconfig paths, workspace packages) and the retained sources
// (package.json, lockfiles, JSON configs).
type Input struct {
	TypeScript map[string]typescript.Facts
	Models     map[string]*typescript.ModelSyntax
	Resolver   *typescript.ModuleResolver
	Sources    map[string][]byte
	// Migrations and Edges are the migration pass's records: a Drizzle
	// table whose name a migration set also defines gets the kit's
	// schema_model_migration_overlap diagnostic (the sources are never
	// merged), as SQLModel's and Prisma's do.
	Migrations []contract.Node
	Edges      []contract.Edge
	// Tag reports a contract row ("<row>" or "<row>/<form>") to the
	// contract test; nil outside tests.
	Tag func(row string)
}

func (in Input) tag(row string) {
	if in.Tag != nil {
		in.Tag(row)
	}
}

// Drizzle's packages: the dependency names that make a directory a
// Drizzle package, and the pins (DR-V1).
const (
	pkgORM = "drizzle-orm"
	pkgKit = "drizzle-kit"
)

// Package is one directory with a package.json whose dependencies or
// devDependencies name drizzle-orm or drizzle-kit.
type Package struct {
	// Dir is the repository-relative package directory ("." for the root);
	// Manifest its package.json.
	Dir, Manifest string
	// Versions are the locked exact versions of drizzle-orm and drizzle-kit
	// ("" when not proven); Lockfile the lockfile(s) read.
	Versions map[string]string
	Lockfile string
	// Pins is {"drizzle-orm": v, "drizzle-kit": v} when both are proven,
	// else nil (DR-V1 is still listed).
	Pins map[string]any
	// Refused says why no claim is made for the package (drizzle-orm 1.x);
	// its configs are then not read.
	Refused string
	// Configs are the drizzle-kit configurations found for the package.
	Configs []*Config
}

// Config is one drizzle-kit configuration: a config file named by the
// default name or by a literal --config in a package script, or a
// script's literal --schema and --dialect flags. A proven config is one
// model group.
type Config struct {
	// Path is the config file, or the package.json for a flag
	// configuration; Root is the group root (the config path, or
	// "<package.json>#<script>" for flags).
	Path, Root string
	Package    *Package
	// Named is how the config is found: "default", "--config" or "flags";
	// Scripts are the scripts that run drizzle-kit with it.
	Named   string
	Scripts []string
	// Dialect is the literal dialect; Unsupported is set for singlestore
	// and gel (row D).
	Dialect     string
	Unsupported bool
	// Schema holds the schema patterns as written.
	Schema []string
	// Casing is the literal casing ("" when absent); CasingKnown is false
	// when the key is not a literal drizzle-kit value (DR-K2: derived
	// names unknown).
	Casing      string
	CasingKnown bool
	// SchemaFilter is the literal schemaFilter (nil when absent);
	// SchemaFilterKnown is false when it is not literal.
	SchemaFilter      []string
	SchemaFilterKnown bool
	// Files are the schema files drizzle-kit loads (sorted); nil when the
	// config is unproven, with the reason in Reason.
	Files  []string
	Reason string
	// NotRead are matched files with an extension drizzle-kit's loader is
	// not documented for; they are reported and not read.
	NotRead []string
	// Out is drizzle-kit's migrations folder (the literal `out` key, else
	// drizzle-kit's default "drizzle"), resolved against the package
	// directory like the schema paths (A5); "" when the key is not literal,
	// the config cannot be read, or the folder is outside the repository.
	// No pass reads it: it is reported as a schema_model_migrations marker
	// so the server's unread-tool rule can name it.
	Out string
	// Modules are the schema files and the modules their exports reach,
	// in discovery order; Unresolved the re-exports that resolve to no
	// file of the repository.
	Modules    []Module
	Unresolved []string
	// flags are a flag configuration's literal options.
	flags map[string]string
}

// SetKey is the config's model group key.
func (c *Config) SetKey() string { return models.SetKey(Tool, c.Root) }

// Proven reports a config whose schema files are proven.
func (c *Config) Proven() bool { return c.Files != nil }

// Diagnostic codes of discovery (info).
const (
	codeNotProven = "schema_model_not_proven"
	codeUnread    = "schema_model_unread"
	// codeMigrations marks a config's drizzle-kit migrations folder (the
	// code Prisma's front end uses for its migrations directory); its
	// message ends with migrationsMarker and the folder.
	codeMigrations = "schema_model_migrations"
)

// migrationsMarker separates the config path from the folder in a
// schema_model_migrations message ("<config>: <marker><dir>"); the server
// reads the folder back from it and lists it as the unread tool
// drizzle-kit when it holds meta/_journal.json or a .sql file.
const migrationsMarker = "drizzle-kit migrations folder (not read): "

// defaultConfigs are drizzle-kit's default config names in the order it
// tries them (bin.cjs:15840-15846).
var defaultConfigs = []string{"drizzle.config.ts", "drizzle.config.js", "drizzle.config.json"}

type manifest struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Scripts         map[string]string `json:"scripts"`
}

// Discover finds every Drizzle package of the repository, its pins and
// its drizzle-kit configurations, and for each proven configuration the
// schema files and the modules their exports reach. Packages are sorted
// by directory. Diagnostics: schema_model_not_proven for a package or
// config without claims (no config, drizzle-orm 1.x, an unproven config);
// schema_model_unread markers for config files drizzle-kit is not shown to
// load, for files of the group's packages that call a table builder but
// are not reached, and for matched schema files with another extension.
func Discover(in Input) ([]*Package, []contract.Diagnostic) {
	var diags []contract.Diagnostic
	diag := func(file, code, msg string) {
		diags = append(diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: msg})
	}
	files := make([]string, 0, len(in.Sources))
	for p := range in.Sources {
		files = append(files, p)
	}
	sort.Strings(files)
	var manifests []string
	for _, p := range files {
		if path.Base(p) == "package.json" && !underNodeModules(p) {
			manifests = append(manifests, p)
		}
	}
	builders := builderFiles(in)
	var out []*Package
	used, marked := map[string]bool{}, map[string]bool{}
	for _, m := range manifests {
		var man manifest
		if json.Unmarshal(in.Sources[m], &man) != nil {
			continue
		}
		spec := map[string]string{}
		for _, name := range []string{pkgORM, pkgKit} {
			if v, ok := man.Dependencies[name]; ok {
				spec[name] = v
			} else if v, ok := man.DevDependencies[name]; ok {
				spec[name] = v
			}
		}
		if len(spec) == 0 {
			continue
		}
		in.tag("DR-K1/package dependency")
		p := &Package{Dir: path.Dir(m), Manifest: m, Versions: map[string]string{}}
		out = append(out, p)
		pins(in.Sources, p, spec)
		if p.Refused != "" {
			diag(m, codeNotProven, m+": "+p.Refused)
			continue
		}
		findConfigs(in, p, man.Scripts, diag)
		for _, c := range p.Configs {
			if c.Named != "flags" {
				used[c.Path] = true
			}
			readConfig(in, c, files)
			if c.Out != "" && !marked[c.Path+"\x00"+c.Out] {
				marked[c.Path+"\x00"+c.Out] = true
				diag(c.Path, codeMigrations, c.Path+": "+migrationsMarker+c.Out)
			}
			if !c.Proven() {
				if c.Unsupported {
					diag(c.Path, codeUnread, c.Reason)
				} else {
					diag(c.Path, codeNotProven, "no Drizzle schema is proven for "+c.Path+": "+c.Reason)
				}
				continue
			}
			if len(c.NotRead) > 0 {
				diag(c.Path, codeUnread, fmt.Sprintf("%s: %d matched schema file(s) without a .ts, .js, .mjs, .cjs, .mts or .cts extension are not read: %s", c.Path, len(c.NotRead), strings.Join(c.NotRead, ", ")))
			}
			for _, u := range c.Unresolved {
				diag(c.Path, codeUnread, c.Path+": "+u)
			}
		}
		if len(p.Configs) == 0 {
			if list := ownedBuilders(builders, manifests, map[string]bool{p.Dir: true}, nil); len(list) > 0 {
				diag(m, codeNotProven, fmt.Sprintf("%s: no drizzle-kit config is found for this package (no default config name, no --config in its scripts); %s not read", m, describeBuilders(list, builders)))
			}
		}
	}
	// Files outside every group's schema that call a table builder.
	for _, p := range out {
		for _, c := range p.Configs {
			if !c.Proven() {
				continue
			}
			reached := map[string]bool{}
			owners := map[string]bool{p.Dir: true}
			for _, mod := range c.Modules {
				reached[mod.File] = true
				owners[owner(manifests, mod.File)] = true
			}
			if list := ownedBuilders(builders, manifests, owners, reached); len(list) > 0 {
				in.tag("DR-P2")
				diag(c.Path, codeUnread, fmt.Sprintf("%s: %s outside the configured schema; not read", c.Path, describeBuilders(list, builders)))
			}
		}
	}
	for _, f := range files {
		if isConfigName(path.Base(f)) && !used[f] && !underNodeModules(f) {
			diag(f, codeUnread, f+": not drizzle-kit's default config of a Drizzle package and not named by a literal --config in its scripts; not read")
		}
	}
	return out, diags
}

func underNodeModules(p string) bool {
	return strings.HasPrefix(p, "node_modules/") || strings.Contains(p, "/node_modules/")
}

// pins reads the locked versions (DR-V1) and refuses drizzle-orm 1.x and
// its release candidates (developer decision 2026-11-09: 0.x is read).
func pins(sources map[string][]byte, p *Package, spec map[string]string) {
	var lockfiles []string
	for _, name := range []string{pkgORM, pkgKit} {
		s, ok := spec[name]
		if !ok {
			continue
		}
		v, lf, _ := lockfile.Locked(sources, p.Dir, name, s)
		p.Versions[name] = v
		if lf != "" && !containsString(lockfiles, lf) {
			lockfiles = append(lockfiles, lf)
		}
	}
	p.Lockfile = strings.Join(lockfiles, ", ")
	if p.Versions[pkgORM] != "" && p.Versions[pkgKit] != "" {
		p.Pins = map[string]any{pkgORM: p.Versions[pkgORM], pkgKit: p.Versions[pkgKit]}
	}
	if s, ok := spec[pkgORM]; ok {
		v := p.Versions[pkgORM]
		if v == "" {
			v = s
		}
		if lockfile.Major(v) >= 1 {
			p.Refused = "drizzle-orm " + v + ": the claim contract is verified for drizzle-orm 0.x only; no claim is made until a corpus pins 1.0"
		}
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// findConfigs lists a package's configurations: the default config name
// in the package directory, every literal --config of a drizzle-kit
// command in its scripts (resolved against the package directory, DR-V5),
// and every literal --schema/--dialect flag set. Script commands whose
// configuration cannot be proven get a marker.
func findConfigs(in Input, p *Package, scripts map[string]string, diag func(file, code, msg string)) {
	byPath := map[string]*Config{}
	add := func(file, root, named, script string) *Config {
		c := byPath[root]
		if c == nil {
			c = &Config{Path: file, Root: root, Package: p, Named: named}
			byPath[root] = c
			p.Configs = append(p.Configs, c)
		}
		if script != "" && !containsString(c.Scripts, script) {
			c.Scripts = append(c.Scripts, script)
		}
		return c
	}
	for _, name := range defaultConfigs {
		f := path.Join(p.Dir, name)
		if _, ok := in.Sources[f]; ok {
			add(f, f, "default", "")
			in.tag("DR-K1/default")
			break
		}
	}
	names := make([]string, 0, len(scripts))
	for n := range scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	var defaultScripts []string
	flagSets := map[string]string{}
	for _, inv := range kitInvocations(scripts, names) {
		switch {
		case inv.Unproven != "":
			diag(p.Manifest, codeNotProven, p.Manifest+": "+inv.Unproven+"; its drizzle-kit configuration is not read")
		case inv.Flags != nil:
			if inv.Flags["schema"] == "" || inv.Flags["dialect"] == "" {
				diag(p.Manifest, codeNotProven, p.Manifest+": script "+quote(inv.Script)+" passes drizzle-kit options without a literal --schema and --dialect; its configuration is not read")
				continue
			}
			key := inv.Flags["schema"] + "\x00" + inv.Flags["dialect"] + "\x00" + inv.Flags["casing"]
			root, ok := flagSets[key]
			if !ok {
				root = p.Manifest + "#" + inv.Script
				flagSets[key] = root
			}
			c := add(p.Manifest, root, "flags", inv.Script)
			c.flags = inv.Flags
		case inv.Config != "":
			f := path.Join(p.Dir, inv.Config)
			if !placeable(inv.Config) || f == ".." || strings.HasPrefix(f, "../") {
				diag(p.Manifest, codeNotProven, p.Manifest+": script "+quote(inv.Script)+" names config "+quote(inv.Config)+", which cannot be placed in the repository")
				continue
			}
			if _, ok := in.Sources[f]; !ok {
				diag(p.Manifest, codeNotProven, p.Manifest+": script "+quote(inv.Script)+" names config "+f+", which does not exist")
				continue
			}
			add(f, f, "--config", inv.Script)
			in.tag("DR-K1/`--config`")
		default:
			defaultScripts = append(defaultScripts, inv.Script)
		}
	}
	for _, c := range p.Configs {
		if c.Named == "default" {
			c.Scripts = append(c.Scripts, defaultScripts...)
		}
	}
}

// outDir is a config's migrations folder relative to the repository: the
// literal `out` (drizzle-kit's default "drizzle", bin.cjs:15299 and
// :15325; the folder holds meta/_journal.json and the .sql migrations,
// prepareOutFolder bin.cjs:8127) joined to the package directory, where drizzle-kit runs (A5). "" when it
// is not known or leaves the repository.
func outDir(obj configObject, dir string) string {
	if obj.Unread != "" {
		return ""
	}
	out := "drizzle"
	if v, ok := obj.Keys["out"]; ok {
		if !v.Literal || v.List || !placeable(v.Strings[0]) {
			return ""
		}
		out = v.Strings[0]
	}
	p := path.Join(dir, out)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

// placeable reports a relative path with forward slashes only.
func placeable(value string) bool {
	return value != "" && !path.IsAbs(value) && !strings.Contains(value, `\`) && (len(value) < 2 || value[1] != ':')
}

// readConfig reads a config's keys and resolves its schema files and the
// modules they reach.
func readConfig(in Input, c *Config, files []string) {
	var obj configObject
	switch {
	case c.Named == "flags":
		obj = flagObject(c.flags)
	case strings.HasSuffix(c.Path, ".json"):
		obj = readConfigJSON(in.Sources[c.Path])
	default:
		obj = readConfigPayload(in.Models[c.Path], in.TypeScript[c.Path])
	}
	c.Out = outDir(obj, c.Package.Dir)
	obj.apply(c, in.tag)
	if c.Reason != "" {
		return
	}
	matched, why := resolveSchema(files, c.Package.Dir, c.Schema)
	if why != "" {
		c.Reason = why
		return
	}
	for _, s := range c.Schema {
		in.tag("DR-K1/" + globForm(s))
	}
	var read []string
	for _, f := range matched {
		if schemaExtension(f) {
			read = append(read, f)
		} else {
			c.NotRead = append(c.NotRead, f)
		}
	}
	if len(read) == 0 {
		c.Reason = fmt.Sprintf("schema %s matches no .ts, .js, .mjs, .cjs, .mts or .cts file in %s (drizzle-kit exits on no match, bin.cjs:19731)", strings.Join(c.Schema, ", "), c.Package.Dir)
		return
	}
	c.Files = append([]string{}, read...)
	reach(in, c)
}
