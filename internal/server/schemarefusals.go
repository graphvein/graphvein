package server

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// This file serves the schema summary's refused_sources (T070): the model
// sources a linker found and refused as a whole, so the schema views can
// say "found, not read" with the reason instead of "no model tables found".
// They come from stored diagnostics only; no extraction runs here.

// codeModelNotProven is the model linkers' code for a package, module,
// config or schema they found but do not read because a fact it needs is
// not proven (docs/extract/schema-models.md and the per-tool documents).
const codeModelNotProven = "schema_model_not_proven"

// schemaRefusedSource is one whole-source refusal: the tool whose linker
// wrote it, the stored file it is filed on, its code and its message
// verbatim (the message names the reason).
type schemaRefusedSource struct {
	Tool    string `json:"tool"`
	File    string `json:"file"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// refusalForm is one row of the refusal table: a message form one model
// linker writes when it refuses a whole source (a package, a module, a
// config or a schema file), never a single class, column or call site.
// Each row names the linker's form it matches (internal/extract/models/
// <tool>), and TestRefusalForms checks every row against that form's text.
type refusalForm struct {
	id    string
	tool  string
	code  string
	match func(file, message string) bool
}

func msgHas(parts ...string) func(string, string) bool {
	return func(_, m string) bool {
		for _, p := range parts {
			if !strings.Contains(m, p) {
				return false
			}
		}
		return true
	}
}

func msgEnds(s string) func(string, string) bool {
	return func(_, m string) bool { return strings.HasSuffix(m, s) }
}

// refusalForms lists the whole-source refusals the schema views name. A
// form missing here is not listed: the views then fall back to what the
// other summary fields prove, as before T070.
var refusalForms = []refusalForm{
	// gorm/discover.go: "<go.mod>: GORM module <path> is not read: <why>".
	{"gorm-module", "gorm", codeModelNotProven, msgHas(": GORM module ", " is not read: ")},
	// prisma/discover.go: "<package.json>: no Prisma schema is proven for
	// this package: <why>".
	{"prisma-package", "prisma", codeModelNotProven, msgHas(": no Prisma schema is proven for this package: ")},
	// prisma/discover.go: "<n> .prisma file(s) are outside every
	// discovered Prisma schema and not read: <files>".
	{"prisma-outside", "prisma", codeModelUnread, msgHas(" .prisma file(s) are outside every discovered Prisma schema and not read: ")},
	// prisma/schema.go: "<file>:<line>: provider mongodb: collections are
	// out of scope, no table is claimed".
	{"prisma-mongodb", "prisma", codeModelUnread, msgEnds(": provider mongodb: collections are out of scope, no table is claimed")},
	// drizzle/discover.go: "<package.json>: drizzle-orm <v>: the claim
	// contract is verified for drizzle-orm 0.x only; ...".
	{"drizzle-version", "drizzle", codeModelNotProven, msgHas(": drizzle-orm ", ": the claim contract is verified for drizzle-orm 0.x only")},
	// drizzle/discover.go: "no Drizzle schema is proven for <config>: <why>".
	{"drizzle-config", "drizzle", codeModelNotProven, func(_, m string) bool { return strings.HasPrefix(m, "no Drizzle schema is proven for ") }},
	// drizzle/discover.go: "<package.json>: no drizzle-kit config is found
	// for this package (...); <builders> not read".
	{"drizzle-no-config", "drizzle", codeModelNotProven, msgHas(": no drizzle-kit config is found for this package ")},
	// drizzle/discover.go: "<package.json>: <why>; its drizzle-kit
	// configuration is not read" and "<package.json>: script <s> passes
	// drizzle-kit options without a literal --schema and --dialect; its
	// configuration is not read".
	{"drizzle-script", "drizzle", codeModelNotProven, func(_, m string) bool {
		return strings.Contains(m, "drizzle-kit") && strings.HasSuffix(m, "configuration is not read")
	}},
	// drizzle/discover.go: "<package.json>: script <s> names config <c>,
	// which cannot be placed in the repository" / "which does not exist".
	{"drizzle-script-config", "drizzle", codeModelNotProven, func(_, m string) bool {
		return strings.Contains(m, ": script ") && strings.Contains(m, " names config ") &&
			(strings.HasSuffix(m, ", which cannot be placed in the repository") || strings.HasSuffix(m, ", which does not exist"))
	}},
	// drizzle/link.go (DR-V5): "<config>: the config is run from the
	// package directories ..., which resolve its schema to different files
	// (DR-V5: ...); no table is claimed".
	{"drizzle-dirs", "drizzle", codeModelNotProven, msgHas("(DR-V5: drizzle-kit reads the schema against its working directory); no table is claimed")},
	// drizzle/discover.go: "<config>: not drizzle-kit's default config of a
	// Drizzle package and not named by a literal --config in its scripts;
	// not read".
	{"drizzle-unused-config", "drizzle", codeModelUnread, msgEnds(": not drizzle-kit's default config of a Drizzle package and not named by a literal --config in its scripts; not read")},
	// sequelize/discover.go: "<package.json>: sequelize <v>: ... the claim
	// contract is verified for sequelize 6 only ...".
	{"sequelize-version", "sequelize", codeModelNotProven, msgHas(": sequelize ", "the claim contract is verified for sequelize 6 only")},
	// sequelize/discover.go: "<package.json>: no module-level `new
	// Sequelize(...)` instance is found in this package; its models are
	// not read".
	{"sequelize-no-instance", "sequelize", codeModelNotProven, msgEnds(": no module-level `new Sequelize(...)` instance is found in this package; its models are not read")},
	// typeorm/discover.go: "<package.json>: typeorm <v>: ... the claim
	// contract is verified for typeorm 0.3.x and 1.x only ...".
	{"typeorm-version", "typeorm", codeModelNotProven, msgHas(": typeorm ", "the claim contract is verified for typeorm 0.3.x and 1.x only")},
	// typeorm/discover.go: "<package.json>: no TypeORM data source,
	// ormconfig or TypeOrmModule root is found in this package; its
	// entities are not read".
	{"typeorm-no-source", "typeorm", codeModelNotProven, msgEnds(": no TypeORM data source, ormconfig or TypeOrmModule root is found in this package; its entities are not read")},
	// typeorm/discover.go: "<file>:<line>: the options of TypeORM <form>
	// <name> are not read (<why>); its entities are not claimed...".
	{"typeorm-options", "typeorm", codeModelNotProven, msgHas(": the options of TypeORM ", "; its entities are not claimed")},
	// typeorm/discover.go: "<file>:<line>: TypeORM <form> <name> is not
	// read: <why> (row D)".
	{"typeorm-source", "typeorm", codeModelUnread, func(_, m string) bool {
		return strings.Contains(m, ": TypeORM ") && strings.Contains(m, " is not read: ") && strings.HasSuffix(m, " (row D)")
	}},
	// diesel/discover.go: every not-proven form on a diesel.toml refuses
	// the config or one of its print_schema sections (DI-K1).
	{"diesel-config", "diesel", codeModelNotProven, func(f, _ string) bool { return path.Base(f) == "diesel.toml" }},
	// diesel/discover.go (O2): "<schema.rs>: a schema.rs with table! that
	// no diesel.toml names; it is not read: <why>" / "... could not be
	// read (<err>)".
	{"diesel-unnamed", "diesel", codeModelUnread, msgHas(": a schema.rs with table! that no diesel.toml names")},
}

// refusalTool returns the tool of the first refusal form d matches, or "".
func refusalTool(d contract.Diagnostic) string {
	for _, r := range refusalForms {
		if d.Code == r.code && r.match(d.File, d.Message) {
			return r.tool
		}
	}
	return ""
}

// refusedSources keeps the diagnostics of stored files (hashes' keys) that
// match a refusal form, one entry per distinct (file, code, message),
// ordered by file, tool, code, then message. [] when none.
func refusedSources(diags []contract.Diagnostic, hashes map[string]string) []schemaRefusedSource {
	out := []schemaRefusedSource{}
	seen := map[schemaRefusedSource]bool{}
	for _, d := range diags {
		if _, stored := hashes[d.File]; !stored {
			continue
		}
		tool := refusalTool(d)
		if tool == "" {
			continue
		}
		r := schemaRefusedSource{Tool: tool, File: d.File, Code: d.Code, Message: d.Message}
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Tool != b.Tool {
			return a.Tool < b.Tool
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	return out
}

// refusedSources reads the stored refusals; nil (null) when not indexed.
func (rd *reader) refusedSources(hashes map[string]string, last *lastRun) ([]schemaRefusedSource, error) {
	if last == nil {
		return nil, nil
	}
	diags, err := rd.s.DiagnosticsWithCodes(rd.ctx, rd.repo, []string{codeModelNotProven, codeModelUnread}, nil)
	if err != nil {
		return nil, fmt.Errorf("reading schema refusals: %w", err)
	}
	return refusedSources(diags, hashes), nil
}
