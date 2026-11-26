package prisma

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// Tool is the group tool of Prisma tables.
const Tool = "prisma"

// Input carries the repository's link-time sources (every retained file:
// manifests, lockfiles, config files and .prisma files) and the migration
// pass's records for the shared comparison with migration sets.
type Input struct {
	Sources    map[string][]byte
	Migrations []contract.Node
	Edges      []contract.Edge
}

// claimTag is a test-only hook: every claim branch reports its contract
// row ("<row>" or "<row>/<form>").
var claimTag func(row string)

// faultHook is a test-only hook called at the start of every isolated
// step, so a test can make one step fail and check that the others run.
var faultHook func(step, subject string)

func tag(row string) {
	if claimTag != nil {
		claimTag(row)
	}
}

// rows names this contract's rows for the core's shared steps: the table
// form (PR-B3) for the node shapes, PR-R1 for foreign-key resolution. The
// migration comparison has no Prisma row (no tag).
var rows = models.Rows{Emit: "PR-B3", ForeignKeys: "PR-R1"}

// Link discovers every Prisma schema of the repository, claims its tables
// and columns under the claim contract (docs/extract/schema-prisma.md) and
// emits them through the model kit, one group per schema root
// (models/prisma/<schema path>). A failure in one schema, model or table
// never stops the others: it becomes a diagnostic.
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	pkgs, diags := Discover(in.Sources)
	var nodes []contract.Node
	var edges []contract.Edge
	steps := &models.Steps{Hook: faultHook}
	for _, p := range sharedSchemas(pkgs, steps) {
		if p.Files == nil {
			continue
		}
		var s *schema
		steps.Guard(p.Manifest, "schema", "Prisma schema "+p.SchemaPath, func() { s = readSchema(p, in.Sources, steps) })
		if s == nil || len(s.tables) == 0 {
			continue
		}
		g := models.Group{Tool: Tool, Root: p.SchemaPath, Pins: p.Pins, Assumptions: s.baseAssumptions(), Dialect: s.provider, Verification: models.VerificationOracle}
		n, e := models.Link(s.tables, models.LinkInput{Group: g, Rows: rows, Tag: tag, Steps: steps, Sources: in.Sources, Migrations: in.Migrations, Edges: in.Edges})
		nodes = append(nodes, n...)
		edges = append(edges, e...)
	}
	diags = append(diags, steps.Diagnostics...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, edges, diags
}

// Diagnostic codes of the claim pass.
const (
	codeImportFailure = "schema_model_import_failure"
	codeNotClaimed    = "schema_model_not_claimed"
)

func diagf(steps *models.Steps, file, severity, code, format string, args ...any) {
	steps.Diag(file, severity, code, fmt.Sprintf(format, args...))
}

// claimGates are the version gates of the claim pass (schema.since and the
// unproven Prisma 6 minor of schema.multiSchema): two Prisma versions of
// the same major on the same side of each gate give the same facts.
// TestClaimGates checks that every since call names one of them.
var claimGates = [][2]int{{6, 0}, {6, 2}, {6, 13}}

// gateKey sums up the facts a package's Prisma version decides.
func gateKey(p *Package) string {
	s := &schema{major: p.Major, version: p.Version}
	key := fmt.Sprintf("%d/%t", p.Major, p.Version == "")
	for _, g := range claimGates {
		key += fmt.Sprintf("/%t", s.since(g[0], g[1]))
	}
	return key
}

// sharedSchemas applies the rule for a schema that more than one package
// discovers (a workspace root and a
// member naming the same file, say): it is claimed once when every
// package names the same schema and their Prisma versions give the same
// facts (the same major, the same side of every claim gate), else nothing
// of it is claimed, with a diagnostic naming the packages. Identities are
// per file, so claiming it twice would emit every one twice. Packages
// whose schemas share a file form one cluster. The returned packages are
// the ones to claim, in discovery order; a shared schema keeps the first
// package's directory and version, and its pins only when every package
// locks the same version (else null: the version is ambiguous).
func sharedSchemas(pkgs []*Package, steps *models.Steps) []*Package {
	cluster := make([]int, len(pkgs))
	owner := map[string]int{}
	for i, p := range pkgs {
		cluster[i] = i
		for _, f := range p.Files {
			j, seen := owner[f]
			if !seen {
				owner[f] = i
				continue
			}
			from, to := cluster[i], cluster[j]
			for k := range cluster {
				if cluster[k] == from {
					cluster[k] = to
				}
			}
		}
	}
	members := map[int][]*Package{}
	for i, p := range pkgs {
		if p.Files != nil {
			members[cluster[i]] = append(members[cluster[i]], p)
		}
	}
	var out []*Package
	for i, p := range pkgs {
		group := members[cluster[i]]
		if p.Files == nil || group[0] != p {
			continue
		}
		if len(group) == 1 {
			out = append(out, p)
			continue
		}
		var names, versions []string
		same := true
		for _, q := range group {
			names = append(names, q.Manifest)
			v := q.Version
			if v == "" {
				v = fmt.Sprintf("Prisma %d, version not proven", q.Major)
			}
			versions = append(versions, v)
			same = same && q.SchemaPath == p.SchemaPath && strings.Join(q.Files, "\x00") == strings.Join(p.Files, "\x00") && gateKey(q) == gateKey(p)
		}
		if !same {
			diagf(steps, p.SchemaPath, "warning", codeNotClaimed, "Prisma schema %s is discovered by packages %s, whose schemas or Prisma versions (%s) do not give the same facts; nothing of it is claimed", p.SchemaPath, strings.Join(names, " and "), strings.Join(versions, ", "))
			continue
		}
		claim := *p
		for _, q := range group[1:] {
			if q.Version != p.Version {
				claim.Pins = nil
			}
		}
		out = append(out, &claim)
	}
	return out
}
