package drizzle

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Diagnostic codes of the claim pass.
const (
	codeImportFailure = "schema_model_import_failure"
	codeNotClaimed    = "schema_model_not_claimed"
)

// rows names this contract's rows for the kit's shared steps: the table
// form (DR-T1) for the node shapes, DR-C2 for foreign-key resolution. The
// migration comparison (schema_model_migration_overlap) has no Drizzle row
// (no tag), as for Prisma.
var rows = models.Rows{Emit: "DR-T1", ForeignKeys: "DR-C2"}

// faultHook is a test-only hook called at the start of every isolated
// step.
var faultHook func(step, subject string)

// Link discovers every Drizzle package and config of the repository,
// claims the tables and columns of each proven config's schema modules
// under the claim contract (docs/extract/schema-drizzle.md) and emits them
// through the model kit, one group per config (models/drizzle/<config
// path>). A failure in one config, table or column never stops the
// others: it becomes a diagnostic.
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	pkgs, diags := Discover(in)
	steps := &models.Steps{Hook: faultHook}
	var groups []*group
	for _, p := range pkgs {
		for _, c := range p.Configs {
			if c.Proven() {
				groups = append(groups, &group{in: in, c: c, dialect: rulesDialect(c.Dialect), steps: steps})
			}
		}
	}
	groups = sameConfig(groups, steps)
	sharedDeclarations(groups, steps)
	var nodes []contract.Node
	var edges []contract.Edge
	for _, g := range groups {
		steps.Guard(g.c.Path, "group", "Drizzle config "+g.c.Root, g.read)
		if len(g.tables) == 0 {
			continue
		}
		tables := make([]*models.Table, len(g.tables))
		for i, t := range g.tables {
			tables[i] = t.rec
		}
		verification := models.VerificationOracle
		assumptions := []string{"DR-V1", "DR-V2", "DR-V4", "DR-V5"}
		if g.dialect == "mysql" {
			verification = models.VerificationMapped
			assumptions = append(assumptions, "DR-V6")
		}
		mg := models.Group{Tool: Tool, Root: g.c.Root, Pins: g.c.Package.Pins, Assumptions: assumptions, Dialect: g.c.Dialect, Verification: verification}
		n, e := models.Link(tables, models.LinkInput{Group: mg, Rows: rows, Tag: in.Tag, Steps: steps, Sources: in.Sources, Migrations: in.Migrations, Edges: in.Edges})
		nodes = append(nodes, n...)
		edges = append(edges, e...)
	}
	diags = append(diags, steps.Diagnostics...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, edges, diags
}

// sameConfig merges the groups of one config file that several packages
// run (a package's default config that another package's scripts also
// name with --config): it is one config and one model group. drizzle-kit
// resolves the schema against the working directory (DR-V5), so the runs
// agree only when they read the same files with the same keys; when they
// do not, which tables the config creates depends on the directory, and
// none is claimed (T018g2 review).
func sameConfig(groups []*group, steps *models.Steps) []*group {
	byRoot := map[string][]*group{}
	var order []string
	for _, g := range groups {
		if byRoot[g.c.Root] == nil {
			order = append(order, g.c.Root)
		}
		byRoot[g.c.Root] = append(byRoot[g.c.Root], g)
	}
	var out []*group
	for _, root := range order {
		list := byRoot[root]
		first := list[0]
		same := true
		var dirs []string
		for _, g := range list {
			dirs = append(dirs, g.c.Package.Dir)
			same = same && agree(first.c, g.c) == "" && strings.Join(first.c.Files, "\x00") == strings.Join(g.c.Files, "\x00")
		}
		if same {
			out = append(out, first)
			continue
		}
		steps.Diag(first.c.Path, "info", codeNotProven, fmt.Sprintf("%s: the config is run from the package directories %s, which resolve its schema to different files (DR-V5: drizzle-kit reads the schema against its working directory); no table is claimed", first.c.Path, strings.Join(dirs, " and ")))
	}
	return out
}

// agree names the first key in which two configs differ that changes the
// claims of a table both read: dialect, casing or schemaFilter; "" when
// they agree.
func agree(a, b *Config) string {
	switch {
	case a.Dialect != b.Dialect:
		return "dialect"
	case a.CasingKnown != b.CasingKnown || a.Casing != b.Casing:
		return "casing"
	case a.SchemaFilterKnown != b.SchemaFilterKnown || strings.Join(a.SchemaFilter, "\x00") != strings.Join(b.SchemaFilter, "\x00") || (a.SchemaFilter == nil) != (b.SchemaFilter == nil):
		return "schemaFilter"
	}
	return ""
}

// sharedDeclarations handles the declarations that several groups read
// (two configs whose schemas reach the same module). Identities are per
// file, so a table is one node in one group. When the configs agree on
// dialect, casing and schemaFilter, the table's facts are the same in
// each, and it is claimed in the group of the first config (by root); the
// other groups get a diagnostic naming that group, and their table list
// is may_be_incomplete. When they differ, the facts depend on the config
// and the table is claimed in none (T018g2 review,
// decision: option b within per-file identities).
func sharedDeclarations(groups []*group, steps *models.Steps) {
	owners := map[*typescript.ModelDeclaration][]*group{}
	where := map[*typescript.ModelDeclaration]string{}
	var decls []*typescript.ModelDeclaration
	kept := len(steps.Diagnostics)
	for _, g := range groups {
		g.collectVisible()
		for _, v := range g.order {
			if owners[v.decl] == nil {
				decls = append(decls, v.decl)
			}
			owners[v.decl] = append(owners[v.decl], g)
			where[v.decl] = v.file
		}
		g.visible, g.order = nil, nil
	}
	// collectVisible's markers (a module without a payload, a
	// default-exported table) are written when each group reads its
	// tables; this first pass only lists the declarations.
	steps.Diagnostics = steps.Diagnostics[:kept]
	for _, d := range decls {
		list := owners[d]
		if len(list) < 2 {
			continue
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].c.Root < list[j].c.Root })
		_, _, isTable := list[0].tableBuilder(where[d], d.Value)
		differ := ""
		for _, g := range list[1:] {
			if differ == "" {
				differ = agree(list[0].c, g.c)
			}
		}
		var roots []string
		for _, g := range list {
			roots = append(roots, g.c.Root)
		}
		for i, g := range list {
			if differ == "" && i == 0 {
				continue
			}
			if g.skip == nil {
				g.skip = map[*typescript.ModelDeclaration]bool{}
			}
			g.skip[d] = true
			if !isTable {
				continue
			}
			head := fmt.Sprintf("%s:%d: %s is read by the configs %s", where[d], d.Span.StartLine, d.Name, strings.Join(roots, " and "))
			msg := head + fmt.Sprintf(", which differ in %s, so its facts depend on the config; it is not claimed", differ)
			if differ == "" {
				msg = head + fmt.Sprintf(", which agree on dialect, casing and schemaFilter; a table is one node in one group (identities are per file), so it is claimed in model group %s only", list[0].c.SetKey())
			}
			steps.Diag(where[d], "info", codeNotClaimed, msg+ListMarker(g.c.SetKey()))
		}
	}
	sort.SliceStable(steps.Diagnostics, func(i, j int) bool { return steps.Diagnostics[i].Message < steps.Diagnostics[j].Message })
}
