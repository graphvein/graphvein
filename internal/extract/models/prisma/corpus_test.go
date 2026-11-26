package prisma

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpusCounts are the inventory's counts (the T018g1 task record (internal))
// for the pinned corpora's schema files: models, enums, fields plus enum
// values, and field plus block attributes.
var corpusCounts = map[string][4]int{
	"umami/prisma/schema.prisma":                                  {26, 0, 289, 601},
	"hoppscotch/packages/hoppscotch-backend/prisma/schema.prisma": {23, 4, 226, 160},
}

// TestPrismaCorpusParse parses every .prisma file under the directory
// GRAPHVEIN_PRISMA_CORPUS names (checkouts of the pinned corpora, umami
// and hoppscotch) and expects no parse error and, for the corpora's
// schema files, the inventory's counts. Skipped without it.
func TestPrismaCorpusParse(t *testing.T) {
	root := os.Getenv("GRAPHVEIN_PRISMA_CORPUS")
	if root == "" {
		t.Skip("GRAPHVEIN_PRISMA_CORPUS is not set")
	}
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".prisma") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files++
		f := Parse(p, src)
		blocks := map[string]int{}
		fields, attrs := 0, 0
		for _, b := range f.Blocks {
			blocks[b.Keyword]++
			fields += len(b.Fields) + len(b.Values)
			attrs += len(b.Attributes)
			for _, fd := range b.Fields {
				attrs += len(fd.Attributes)
			}
		}
		t.Logf("%s: blocks %v, fields and values %d, attributes %d, errors %d", p, blocks, fields, attrs, len(f.Errors))
		for suffix, want := range corpusCounts {
			if strings.HasSuffix(filepath.ToSlash(p), suffix) {
				if got := [4]int{blocks[KeywordModel], blocks[KeywordEnum], fields, attrs}; got != want {
					t.Errorf("%s: counts %v, want %v", p, got, want)
				}
			}
		}
		for _, e := range f.Errors {
			t.Errorf("%s:%d:%d: %s", p, e.Line, e.Col, e.Message)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatalf("no .prisma file under %s", root)
	}
}

// corpusDiscovery is what discovery must find in each pinned corpus
// (the T018g1 task record (internal) "Discovery as the pinned CLI does it").
var corpusDiscovery = map[string]string{
	"umami":      ". 7.10.0 prisma.config.ts prisma/schema.prisma",
	"hoppscotch": "packages/hoppscotch-backend 7.9.1 packages/hoppscotch-backend/prisma.config.ts packages/hoppscotch-backend/prisma/schema.prisma",
}

// TestPrismaCorpusDiscover runs discovery over each corpus checkout under
// GRAPHVEIN_PRISMA_CORPUS (the files discovery reads: manifests,
// lockfiles, configs and .prisma files). Skipped without it.
func TestPrismaCorpusDiscover(t *testing.T) {
	root := os.Getenv("GRAPHVEIN_PRISMA_CORPUS")
	if root == "" {
		t.Skip("GRAPHVEIN_PRISMA_CORPUS is not set")
	}
	for name, want := range corpusDiscovery {
		sources := corpusSources(t, filepath.Join(root, name))
		pkgs, diags := Discover(sources)
		var got []string
		for _, p := range pkgs {
			got = append(got, strings.Join(append([]string{p.Dir, p.Version, p.Config}, p.Files...), " "))
		}
		if len(pkgs) != 1 || got[0] != want || len(diags) != 0 {
			t.Errorf("%s: packages %q, diagnostics %+v; want %q", name, got, diags, want)
		}
	}
}

// corpusSources reads the files of a corpus checkout that discovery and
// the claim pass read: manifests, lockfiles, configs and .prisma files.
func corpusSources(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	sources := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		base := d.Name()
		if d.IsDir() || !(base == "package.json" || base == "pnpm-lock.yaml" || base == "package-lock.json" || base == "yarn.lock" ||
			strings.HasSuffix(base, ".prisma") || strings.HasPrefix(base, "prisma.config.") || strings.HasPrefix(base, "prisma7.config.")) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		sources[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

// corpusClaims are the claim pass's counts on each corpus: tables,
// columns, columns with every fact read (type, nullability, default or its
// proven absence), complete tables, and claims by verification. The the investigation
// coverage target is every table complete: 26 tables / 229 columns
// (umami) and 23 / 171 (hoppscotch), reached at step G1B3 with row PR-R1.
var corpusClaims = map[string]string{
	"umami":      "tables 26 columns 229 typed 229 defaults 229 complete 26 oracle 2234",
	"hoppscotch": "tables 23 columns 171 typed 171 defaults 171 complete 23 oracle 1598",
}

// corpusBaseline is the investigation oracle baseline in baselineCounts' terms
// (umami: 123 NOT NULL, 31 defaults, 26 PKs, 11 unique and 81 other
// indexes, no foreign key and 30 emulated relations under relationMode
// "prisma"; hoppscotch: 133, 24, 21, 19, 5, enums on 8 columns, 22 foreign
// keys).
var corpusBaseline = map[string]string{
	"umami":      "not_null 123 defaults 31 pks 26 unique 11 indexes 81 enum_columns 0 foreign_keys 0 emulated 30",
	"hoppscotch": "not_null 133 defaults 24 pks 21 unique 19 indexes 5 enum_columns 8 foreign_keys 22 emulated 0",
}

// TestPrismaCorpusLink runs the claim pass over each corpus checkout under
// GRAPHVEIN_PRISMA_CORPUS and checks the counts above and the claim
// checks. Skipped without it.
func TestPrismaCorpusLink(t *testing.T) {
	root := os.Getenv("GRAPHVEIN_PRISMA_CORPUS")
	if root == "" {
		t.Skip("GRAPHVEIN_PRISMA_CORPUS is not set")
	}
	for name, want := range corpusClaims {
		nodes, _, diags := Link(Input{Sources: corpusSources(t, filepath.Join(root, name))})
		checkRecords(t, nodes)
		got := countRecords(nodes)
		t.Logf("%s: %s; %d diagnostics", name, got, len(diags))
		for _, d := range diags {
			t.Logf("%s: %s %s", name, d.Code, d.Message)
		}
		if got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
		if got := baselineCounts(nodes); got != corpusBaseline[name] {
			t.Errorf("%s: %s, oracle baseline %s", name, got, corpusBaseline[name])
		}
	}
}
