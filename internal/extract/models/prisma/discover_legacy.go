package prisma

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models/lockfile"
)

// discoverLegacy finds the schema of a Prisma 5 or 6 package as that CLI
// does (developer decision 2026-11-09: Prisma 5 and 6 are read too). The
// rules are verified in the prisma/prisma sources at 5.22.0 and 6.19.3
// (packages/internals/src/cli/getSchema.ts, packages/config) and in the
// history between 6.0.0 and 6.19.3 for the points where 6.x changed:
//
//   - Config files: none before 6.4.0; 6.4.0 to 6.12.x read only
//     `prisma.config.ts` (with early-access rules this reader does not
//     model, so such a file leaves the schema unproven); from 6.13.0 the
//     same names as Prisma 7.9.1 (c12), whose literal `schema` comes first.
//   - package.json `"prisma": {"schema": ...}` (read-package-up from the
//     package directory), resolved against the package directory: a file,
//     or (from 6.7.0, prismaSchemaFolder GA) a directory. Before 6.7.0 a
//     directory needs the prismaSchemaFolder preview feature, which this
//     rule does not read: not proven.
//   - Default lookup relative to the package directory: `schema.prisma`,
//     then `prisma/schema.prisma`; before 6.6.0 a `prisma/schema` folder
//     can also be the schema (with the preview feature) and conflicts with
//     `prisma/schema.prisma`, so a `prisma/schema` directory leaves the
//     second and third rules unproven there.
//
// Without an exact version (pins null) only what every 5.x or 6.x release
// agrees on is proven: no config candidate may exist (6.x), a
// package.json schema must name a file, and `prisma/schema.prisma` needs
// no `prisma/schema` directory beside it.
func discoverLegacy(sources map[string][]byte, p *Package, pkgPrisma json.RawMessage) {
	exact := p.Version != "" && lockfile.Major(p.Version) == p.Major
	since := func(minor int) bool { return exact && lockfile.AtLeast(p.Version, p.Major, minor) }
	if p.Major == 6 {
		switch c := firstConfig(sources, p.Dir, legacyConfigs); {
		case c == "":
		case !exact:
			p.Reason = fmt.Sprintf("Prisma 6 without an exact locked version: whether %s is loaded (6.4.0 and later) is not proven", c)
			return
		case since(13):
			p.Config = c
			state, value := readConfigSchema(string(sources[c]))
			switch state {
			case configUnreadable:
				p.Reason = c + " is not an `export default defineConfig({...})` or object literal the reader can read"
				return
			case configNotLiteral:
				p.Reason = "the schema setting of " + c + " is not a string literal"
				return
			case configLiteral:
				resolveConfigSchema(sources, p, path.Dir(c), value)
				return
			}
		case since(4):
			p.Reason = fmt.Sprintf("Prisma %s reads %s with early-access rules the reader does not model (6.4.0 to 6.12.x)", p.Version, c)
			return
		}
	}
	folderGA := p.Major == 6 && since(7)
	if len(pkgPrisma) > 0 {
		var member map[string]json.RawMessage
		if json.Unmarshal(pkgPrisma, &member) == nil {
			if raw, ok := member["schema"]; ok && string(raw) != "null" {
				var value string
				if json.Unmarshal(raw, &value) != nil {
					p.Reason = p.Manifest + `: "prisma.schema" is not a string (the CLI refuses it)`
					return
				}
				if value != "" {
					resolvePackageSchema(sources, p, value, folderGA)
					return
				}
			}
		}
	}
	base := p.Dir
	folder := path.Join(base, "prisma/schema")
	folderExists := isDir(sources, folder)
	oldLookup := !(p.Major == 6 && since(6))
	if f := path.Join(base, "schema.prisma"); hasFile(sources, f) {
		p.SchemaPath, p.Files = f, []string{f}
		return
	}
	if f := path.Join(base, "prisma/schema.prisma"); hasFile(sources, f) {
		if folderExists && oldLookup {
			p.Reason = fmt.Sprintf("%s and the folder %s both exist; before Prisma 6.6.0 the folder can be the schema (prismaSchemaFolder) and conflicts with the file", f, folder)
			return
		}
		p.SchemaPath, p.Files = f, []string{f}
		return
	}
	if folderExists && oldLookup {
		p.Reason = fmt.Sprintf("the folder %s is a schema only with the prismaSchemaFolder preview feature before Prisma 6.6.0, which discovery does not read", folder)
		return
	}
	p.Reason = "neither " + path.Join(base, "schema.prisma") + " nor " + path.Join(base, "prisma/schema.prisma") + " exists"
}

// resolvePackageSchema resolves a Prisma 5 or 6 package.json schema path
// against the package directory: a file, or a directory when the
// prismaSchemaFolder feature is GA at the locked version (6.7.0).
func resolvePackageSchema(sources map[string][]byte, p *Package, value string, folderGA bool) {
	if path.IsAbs(value) || strings.Contains(value, `\`) || len(value) > 1 && value[1] == ':' {
		p.Reason = fmt.Sprintf("the schema path %q of %s cannot be placed in the repository", value, p.Manifest)
		return
	}
	target := path.Join(p.Dir, value)
	if target == ".." || strings.HasPrefix(target, "../") {
		p.Reason = fmt.Sprintf("the schema path %q of %s is outside the repository", value, p.Manifest)
		return
	}
	if hasFile(sources, target) {
		p.SchemaPath, p.Files = target, []string{target}
		return
	}
	if !isDir(sources, target) {
		p.Reason = fmt.Sprintf("the schema path %s named by %s does not exist", target, p.Manifest)
		return
	}
	if !folderGA {
		p.Reason = fmt.Sprintf("the schema path %s named by %s is a folder, which needs the prismaSchemaFolder preview feature before Prisma 6.7.0 (not read by discovery)", target, p.Manifest)
		return
	}
	// The directory rule is the config rule's (schema-files-loader).
	saved := p.Config
	p.Config = p.Manifest
	resolveConfigSchema(sources, p, p.Dir, value)
	p.Config = saved
}

func hasFile(sources map[string][]byte, f string) bool {
	_, ok := sources[f]
	return ok
}

// isDir says whether any retained file is under dir.
func isDir(sources map[string][]byte, dir string) bool {
	prefix := dir + "/"
	if dir == "." {
		return len(sources) > 0
	}
	for f := range sources {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return false
}
