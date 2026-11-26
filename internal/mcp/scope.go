package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/testscope"
)

// walkScope is the test-scope filter of callers and impact (row MC-31).
// It classifies each reached node with the code views' one rule
// (server.TestScoped: the architecture chain's CH10 test, used by the
// chain and table tools too) and the node's own repository's test rules.
// Production scope leaves out the results that are test code; all keeps
// every result and marks those in test code test_code. It filters the
// walk's results, not the walk: a kept node whose stored path passes
// through test code is marked via_test_code.
type walkScope struct {
	s     *Server
	scope string
	// rules holds each repository's test rules, read once per call.
	rules map[string]testscope.Rules
	// test holds the classification of every reached node by key.
	test map[string]bool
	// nodes caches the stored nodes read for the items the scope does
	// not filter (possible callers, possibly missing call sites), by key.
	nodes map[string]*contract.Node
}

// newWalkScope starts the scope filter of one callers or impact call.
func newWalkScope(s *Server, scope string) *walkScope {
	return &walkScope{s: s, scope: scope, rules: map[string]testscope.Rules{}, test: map[string]bool{}, nodes: map[string]*contract.Node{}}
}

// walkScopeParam reads the scope argument of callers and impact: the
// values of the chain, table and overview tools, production by default.
func walkScopeParam(scope string) (string, error) {
	scope = orDefault(scope, "production")
	if scope != "production" && scope != "all" {
		return "", badRequest("scope must be production or all, not %q", scope)
	}
	return scope, nil
}

// nodeKey names a node across a project's members.
func nodeKey(repository, id string) string { return repository + "\x00" + id }

// testRules returns the test rules of a repository as the views read
// them: the graphvein.yaml of its checkout, or the built-in rules only
// for a project member without a checkout.
func (w *walkScope) testRules(repository string) (testscope.Rules, error) {
	if r, ok := w.rules[repository]; ok {
		return r, nil
	}
	root := w.s.opt.Root
	if w.s.opt.Project != nil {
		root = ""
		for _, m := range w.s.opt.Project.Members {
			if m.Repository == repository && m.Checkout {
				root = m.Path
			}
		}
	}
	rules := config.Defaults().Tests
	if root != "" {
		cfg, err := config.Load(root)
		if err != nil {
			return testscope.Rules{}, badRequest("reading the test rules of %s: %v", orDefault(repository, w.s.opt.Repository), err)
		}
		rules = cfg.Tests
	}
	w.rules[repository] = rules
	return rules, nil
}

// classify records the test-scope classification of every reached node.
func (w *walkScope) classify(results []any) error {
	for _, r := range results {
		m, _ := r.(map[string]any)
		node, _ := m["node"].(map[string]any)
		repository := str(node["repository"])
		rules, err := w.testRules(repository)
		if err != nil {
			return err
		}
		attrs, _ := node["attributes"].(map[string]any)
		n := contract.Node{Identity: str(node["identity"]), File: str(node["file"]), Attributes: attrs}
		w.test[nodeKey(repository, n.Identity)] = server.TestScoped(n, rules)
	}
	return nil
}

// hidden reports whether production scope leaves out a reached node.
func (w *walkScope) hidden(repository, id string) bool {
	return w.scope == "production" && w.test[nodeKey(repository, id)]
}

// isTest reports whether a reached node is test code.
func (w *walkScope) isTest(repository, id string) bool {
	return w.test[nodeKey(repository, id)]
}

// viaTest reports whether a result's stored path passes through a node
// classified as test code, the target and the result itself excepted.
func (w *walkScope) viaTest(result map[string]any, target, self string) bool {
	for _, st := range listOf(result["path"]) {
		step, _ := st.(map[string]any)
		for _, end := range [][2]string{{str(step["source_repository"]), str(step["source"])}, {str(step["target_repository"]), str(step["target"])}} {
			key := nodeKey(end[0], end[1])
			if key != target && key != self && w.test[key] {
				return true
			}
		}
	}
	return false
}

// storeRepository is the repository a node is stored under: a project
// member's name, or the served repository (whose nodes carry none).
func (w *walkScope) storeRepository(repository string) string {
	return orDefault(repository, w.s.opt.Repository)
}

// load reads the stored nodes of identities in repository that are not
// cached yet, in one store call.
func (w *walkScope) load(ctx context.Context, repository string, identities []string) error {
	var missing []string
	seen := map[string]bool{}
	for _, id := range identities {
		key := nodeKey(repository, id)
		if _, ok := w.nodes[key]; ok || id == "" || seen[id] {
			continue
		}
		seen[id] = true
		missing = append(missing, id)
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	nodes, err := w.s.opt.Store.NodesByIdentity(ctx, w.storeRepository(repository), missing)
	if err != nil {
		return fmt.Errorf("reading the nodes of %s for the test scope: %w", w.storeRepository(repository), err)
	}
	for _, id := range missing {
		w.nodes[nodeKey(repository, id)] = nil
	}
	for i := range nodes {
		w.nodes[nodeKey(repository, nodes[i].Identity)] = &nodes[i]
	}
	return nil
}

// itemTest classifies an item the scope does not filter by the same rule
// (server.TestScoped): its stored node when identity names one, else its
// file alone (a call site at a file's top level).
func (w *walkScope) itemTest(ctx context.Context, repository, identity, file string) (bool, error) {
	rules, err := w.testRules(repository)
	if err != nil {
		return false, err
	}
	if identity != "" {
		if err := w.load(ctx, repository, []string{identity}); err != nil {
			return false, err
		}
		if n := w.nodes[nodeKey(repository, identity)]; n != nil {
			return server.TestScoped(*n, rules), nil
		}
	}
	return server.TestScoped(contract.Node{File: file}, rules), nil
}

// markTest sets test_code on an item in test code (MC-31); labelTest
// lists its legend entry once the item is shown.
func markTest(item map[string]any, test bool) {
	if test {
		item["test_code"] = true
	}
}

// labelTest records the test_scope labels the shown items use (MC-5):
// a response lists only the labels of what it shows.
func labelTest(e *Envelope, items []any) {
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["test_code"] == true {
			e.label("test_scope", "test_code")
		}
		if m["via_test_code"] == true {
			e.label("test_scope", "via_test_code")
		}
	}
}

// markNodeTest marks a near miss: its served node carries the identity,
// file and scope attribute the rule reads.
func (w *walkScope) markNodeTest(item map[string]any, repository string, node map[string]any) error {
	rules, err := w.testRules(repository)
	if err != nil {
		return err
	}
	attrs, _ := node["attributes"].(map[string]any)
	markTest(item, server.TestScoped(contract.Node{Identity: str(node["identity"]), File: str(node["file"]), Attributes: attrs}, rules))
	return nil
}

// markSites marks the possibly missing call sites in test code: a site's
// caller is its enclosing node, a site without one is classified by its
// file.
func (w *walkScope) markSites(ctx context.Context, sites []any) error {
	byRepo := map[string][]string{}
	for _, site := range sites {
		m, _ := site.(map[string]any)
		byRepo[str(m["repository"])] = append(byRepo[str(m["repository"])], str(m["caller"]))
	}
	for _, repository := range sortedKeys(byRepo) {
		if err := w.load(ctx, repository, byRepo[repository]); err != nil {
			return err
		}
	}
	for _, site := range sites {
		m, _ := site.(map[string]any)
		test, err := w.itemTest(ctx, str(m["repository"]), str(m["caller"]), str(m["file"]))
		if err != nil {
			return err
		}
		markTest(m, test)
	}
	return nil
}

// markPossibleCaller marks a possible caller of another member whose
// source is test code.
func (w *walkScope) markPossibleCaller(ctx context.Context, pc map[string]any) error {
	source, _ := pc["source"].(map[string]any)
	repository := str(source["repository"])
	test, err := w.itemTest(ctx, repository, str(source["identity"]), "")
	if err != nil {
		return err
	}
	markTest(pc, test)
	return nil
}

// resultFileUnknownCalls counts the unresolved call sites in the files of
// the shown results (MC-16) when production scope left results out, so
// "these results" never counts the files of hidden test results; the
// query counts the files of every reached node.
func (w *walkScope) resultFileUnknownCalls(ctx context.Context, files map[string]map[string]bool) (int, error) {
	total := 0
	for _, repository := range sortedKeys(files) {
		var ids []string
		for _, file := range sortedKeys(files[repository]) {
			id, err := contract.Identity("file", file, file)
			if err != nil {
				return 0, fmt.Errorf("naming the file node of %s: %w", file, err)
			}
			ids = append(ids, id)
		}
		nodes, err := w.s.opt.Store.NodesByIdentity(ctx, w.storeRepository(repository), ids)
		if err != nil {
			return 0, fmt.Errorf("reading the result files of %s: %w", w.storeRepository(repository), err)
		}
		for _, n := range nodes {
			if v, ok := n.Attributes["unresolved_calls_unknown"]; ok {
				if count, err := strconv.Atoi(fmt.Sprint(v)); err == nil && count > 0 {
					total += count
				}
			}
		}
	}
	return total, nil
}

// report states what production scope left out (MC-31): the results and
// walk ends that are test code, with how to get them, and, on every
// production-scope answer, the parts the scope does not filter.
func (w *walkScope) report(e *Envelope, results, ends int) {
	if w.scope != "production" {
		return
	}
	if results+ends > 0 {
		text := fmt.Sprintf("%d results and %d '? not modelled' ends in test code not shown (production scope; the test-scope rule of the chain view): call again with scope all", results, ends)
		e.unknown("MC-31", "test_scope_hidden", text, map[string]any{"results": results, "ends": ends, "how": "scope: all"})
	}
	e.unknown("MC-31", "test_scope_unfiltered", testScopeUnfiltered, map[string]any{"unfiltered": []string{"bounds", "possibly_missing_callers", "near_miss", "possible_caller"}})
}

// testScopeUnfiltered is the text of the MC-31 unknown every production
// scope answer of callers and impact carries.
const testScopeUnfiltered = "production scope leaves out the results and '? not modelled' ends in test code only: the walk itself still follows test code, so the bounds, the possibly missing callers (blind spots), the near misses and the possible callers of other members count it too; their items in test code say test_code: true, and a result reached through test code says via_test_code: true"
