package typeorm

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file is the claim pass of steps TOB3 and TOB4: it turns each
// claimable group's reached entities into kit tables (rows TO-K2, TO-K6,
// TO-E1, TO-E2, TO-E3, TO-C1 to TO-C6, TO-X1 to TO-X4, TO-R1 to TO-R4,
// TO-P1, TO-P2 and D): tables, columns, types and defaults (entity.go,
// column.go, column_render.go), relations and junction tables
// (relations.go), indexes, constraints and foreign keys
// (constraints.go).

// rows names this contract's rows for the kit's shared steps.
var rows = models.Rows{Emit: "TO-E1", ForeignKeys: "TO-R1"}

// faultHook is a test-only hook called at the start of every isolated
// step.
var faultHook func(step, subject string)

// Link discovers every TypeORM package of the repository and claims the
// tables of each claimable group's entities under the claim contract
// (docs/extract/schema-typeorm.md), one kit group per data source or
// NestJS root (models/typeorm/<root>). A failure in one entity never stops
// the others: it becomes a diagnostic.
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	d, pkgs := runDiscovery(in)
	diags := d.diags
	steps := d.steps
	var nodes []contract.Node
	var edges []contract.Edge
	for _, p := range pkgs {
		if p.Refused != "" {
			continue
		}
		shared := sharedEntities(p)
		for _, g := range p.Groups {
			if !g.Claimable() {
				continue
			}
			c := &claimer{d: d, g: g, steps: steps, shared: shared}
			steps.Guard(g.File, "group", "TypeORM group "+g.SetKey(), c.read)
			if len(c.tables) == 0 {
				continue
			}
			tables := make([]*models.Table, len(c.tables))
			for i, t := range c.tables {
				tables[i] = t.rec
			}
			n, e := models.Link(tables, models.LinkInput{Group: c.kitGroup(), Rows: rows, Tag: in.Tag, Steps: steps, Sources: in.Sources, Migrations: in.Migrations, Edges: in.Edges})
			nodes = append(nodes, n...)
			edges = append(edges, e...)
		}
	}
	diags = append(diags, steps.Diagnostics...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, edges, diags
}

// sharedEntities are the entities two or more claimable groups of the
// package reach (Maintainerr's CLI data source and NestJS root): their
// tables are claimed in each group, so their identities name the group.
func sharedEntities(p *Package) map[*Entity]bool {
	n := map[*Entity]int{}
	for _, g := range p.Groups {
		if g.Claimable() && len(g.Failures) < len(g.Dialects) {
			for _, e := range g.Entities {
				n[e]++
			}
		}
	}
	out := map[*Entity]bool{}
	for e, k := range n {
		if k > 1 {
			out[e] = true
		}
	}
	return out
}

// claimer reads one group's tables.
type claimer struct {
	d      *discovery
	g      *Group
	steps  *models.Steps
	shared map[*Entity]bool
	// dialects are the group's dialects whose initialize() is not proven
	// to fail; failed holds the failures by dialect (TO-P1).
	dialects []string
	failed   map[string][]string
	tables   []*table
	// byClass finds a built table by its entity class (relation targets);
	// noMigrations caches generatedNames' folder check.
	byClass      map[*typescript.ModelStatement]*table
	noMigrations *bool
}

func (c *claimer) tag(row string) { c.d.in.tag(row) }

// read builds every reached entity's table, then drops the dialects a
// provable failure stops (TO-P1: validation runs for every entity before
// any DDL, and the schema build is one transaction), and finishes the
// tables on the dialects left.
func (c *claimer) read() {
	c.failed = map[string][]string{}
	for d, why := range c.g.Failures {
		c.failed[d] = append(c.failed[d], why)
	}
	var built []*table
	for _, e := range c.g.Entities {
		if !e.Typeorm || e.Form != "Entity" {
			// Row D, reported by discovery.
			continue
		}
		var t *table
		c.steps.Guard(e.File, "entity", "TypeORM entity "+e.Key(), func() { t = c.entity(e) })
		if t != nil {
			built = append(built, t)
		}
	}
	// Relations read the targets' columns, so every entity's own columns
	// come first (TO-R1 to TO-R4); TO-P1 (3) is decided after them.
	junctions := c.relations(built)
	for _, t := range built {
		t.objectFailures()
	}
	built = append(built, junctions...)
	for _, d := range c.g.Dialects {
		if len(c.failed[d]) == 0 {
			c.dialects = append(c.dialects, d)
		}
	}
	for _, d := range sortedKeysList(c.failed) {
		// Discovery reported its own failures (1.x's sqlite type).
		var own []string
		for _, why := range c.failed[d] {
			if why != c.g.Failures[d] {
				own = append(own, why)
			}
		}
		if len(own) == 0 {
			continue
		}
		c.steps.Diag(c.g.File, "info", codeFailure, fmt.Sprintf("%s:%d: %s; initialize() fails on %s, so no table of model group %s is claimed on %s", c.g.File, c.g.Span.StartLine, strings.Join(own, "; "), d, c.g.SetKey(), d))
	}
	if len(c.dialects) == 0 {
		return
	}
	for _, t := range built {
		c.steps.Guard(t.e.File, "table", "TypeORM table "+t.rec.Identity, t.finish)
		c.tables = append(c.tables, t)
	}
}

// fail records a TO-P1 failure of form on the listed dialects (every
// group dialect when none is listed).
func (c *claimer) fail(form, why string, dialects ...string) {
	c.tag("TO-P1/" + form)
	if len(dialects) == 0 {
		dialects = c.g.Dialects
	}
	for _, d := range dialects {
		msg := "TO-P1 " + form + ": " + why
		if !slices.Contains(c.failed[d], msg) {
			c.failed[d] = append(c.failed[d], msg)
		}
	}
}

// weakest is the verification of a dialect-independent fact: mapped when
// any live dialect is mapped (dialect rule, several dialects).
func (c *claimer) weakest() string {
	for _, d := range c.dialects {
		if c.g.Verification[d] != models.VerificationOracle {
			return models.VerificationMapped
		}
	}
	return models.VerificationOracle
}

// kitGroup is the kit group: one dialect, or the run-time dialect set
// (ADR 0065) of the dialects left after the failures.
func (c *claimer) kitGroup() models.Group {
	g := c.g
	assumptions := []string{"TO-V1", "TO-V2", "TO-V3", "TO-V4", "TO-V5"}
	mapped := false
	for _, d := range c.dialects {
		mapped = mapped || g.Verification[d] != models.VerificationOracle
	}
	if mapped {
		assumptions = append(assumptions, "TO-V6")
	}
	for _, t := range c.tables {
		if t.rec.Schema != nil {
			assumptions = append(assumptions, "TO-V7")
			break
		}
	}
	for _, o := range g.Options {
		if o.Config || o.From == "registerAs" {
			assumptions = append(assumptions, "TO-V8")
			break
		}
	}
	assumptions = append(assumptions, "TO-V9")
	if g.Naming == "snake" {
		assumptions = append(assumptions, "TO-V10")
	}
	kg := models.Group{Tool: Tool, Root: g.Root, Pins: g.Package.Pins, Assumptions: assumptions, Verification: c.weakest()}
	if len(c.dialects) == 1 {
		kg.Dialect = c.dialects[0]
	} else {
		kg.Dialects = append([]string(nil), c.dialects...)
	}
	return kg
}

// runtime says the group's dialect is chosen at run time: its
// dialect-dependent facts are stated per dialect (ADR 0065).
func (c *claimer) runtime() bool { return len(c.dialects) > 1 }

func sortedKeysList(m map[string][]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
