package tableaccess

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/models/drizzle"
	"github.com/graphvein/graphvein/internal/extract/models/prisma"
	"github.com/graphvein/graphvein/internal/extract/models/sqlmodel"
	"github.com/graphvein/graphvein/internal/extract/models/supabasetypes"
	"github.com/graphvein/graphvein/internal/extract/schema"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
	"github.com/graphvein/graphvein/internal/lang/rust"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
)

const fixtureRoot = "../../../testdata/fixtures/table-access-sqlx"

// loadRoot builds the link input for a repository root the way the
// indexer does for Rust files and migration sets: Rust files parsed and
// extracted, every manifest and SQL file retained, the schema records
// linked. Directories the indexer never reads for these inputs (.git,
// target, node_modules, .graphvein) are skipped.
func loadRoot(t testing.TB, root string) Input {
	t.Helper()
	sources := map[string][]byte{}
	var rustFiles, pythonFiles, tsFiles []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (name == ".git" || name == "target" || name == "node_modules" || name == ".graphvein") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".rs") && !strings.HasSuffix(name, ".sql") && !strings.HasSuffix(name, ".toml") && !strings.HasSuffix(name, ".py") && name != "setup.cfg" &&
			tsLanguage(name) == "" && !strings.HasSuffix(name, ".prisma") && !prismaManifests[name] && !tsConfig(name) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		sources[rel] = data
		if strings.HasSuffix(name, ".rs") {
			rustFiles = append(rustFiles, rel)
		}
		if strings.HasSuffix(name, ".py") {
			pythonFiles = append(pythonFiles, rel)
		}
		if tsLanguage(name) != "" {
			tsFiles = append(tsFiles, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(rustFiles)
	jobs := make([]parse.Job, len(rustFiles))
	for i, f := range rustFiles {
		jobs[i] = parse.Job{Language: "rust", Source: sources[f]}
	}
	results, err := parse.Batch(context.Background(), jobs, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Rust: map[string]facts.Facts{}, Sources: sources}
	for i, f := range rustFiles {
		if results[i].Err != nil || results[i].Root == nil {
			t.Logf("%s: not parsed: %v", f, results[i].Err)
			continue
		}
		in.Rust[f] = rust.Extract(f, sources[f], results[i].Root)
	}
	in.Schema, in.SchemaEdges, _ = schema.Link(schema.Input{Sources: sources, Root: root})
	sort.Strings(pythonFiles)
	pyJobs := make([]parse.Job, len(pythonFiles))
	for i, f := range pythonFiles {
		pyJobs[i] = parse.Job{Language: "python", Source: sources[f]}
	}
	pyResults, err := parse.Batch(context.Background(), pyJobs, parse.Options{CacheDir: filepath.Join(t.TempDir(), "pycache")})
	if err != nil {
		t.Fatal(err)
	}
	in.Python = map[string]facts.Facts{}
	for i, f := range pythonFiles {
		if pyResults[i].Err != nil || pyResults[i].Root == nil {
			t.Logf("%s: not parsed: %v", f, pyResults[i].Err)
			continue
		}
		in.Python[f] = python.Extract(f, sources[f], pyResults[i].Root)
	}
	in.Resolver = python.NewModuleResolver(in.Python, sources)
	in.Models, _, _ = sqlmodel.Link(sqlmodel.Input{Python: in.Python, Resolver: in.Resolver, Sources: sources, Migrations: in.Schema})
	sort.Strings(tsFiles)
	tsJobs := make([]parse.Job, len(tsFiles))
	for i, f := range tsFiles {
		tsJobs[i] = parse.Job{Language: tsLanguage(f), Source: sources[f]}
	}
	tsResults, err := parse.Batch(context.Background(), tsJobs, parse.Options{CacheDir: filepath.Join(t.TempDir(), "tscache")})
	if err != nil {
		t.Fatal(err)
	}
	in.TypeScript = map[string]facts.Facts{}
	in.DrizzleModels = map[string]*typescript.ModelSyntax{}
	for i, f := range tsFiles {
		if tsResults[i].Err != nil || tsResults[i].Root == nil {
			t.Logf("%s: not parsed: %v", f, tsResults[i].Err)
			continue
		}
		in.TypeScript[f] = typescript.Extract(f, sources[f], tsResults[i].Root)
		if m := typescript.CaptureModels(f, tsResults[i].Root, in.TypeScript[f]); m != nil {
			in.DrizzleModels[f] = m
		}
	}
	in.Prisma, _, _ = prisma.Link(prisma.Input{Sources: sources, Migrations: in.Schema})
	// The module resolver reads package.json and tsconfig files listed in
	// the TypeScript map (with empty facts), as the indexer lists every
	// file there.
	for f := range sources {
		if name := path.Base(f); name == "package.json" || tsConfig(name) {
			if _, ok := in.TypeScript[f]; !ok {
				in.TypeScript[f] = facts.Facts{Exports: map[string]string{}}
			}
		}
	}
	in.Modules = typescript.NewModuleResolver(in.TypeScript, sources)
	in.Drizzle, _, _ = drizzle.Link(drizzle.Input{TypeScript: in.TypeScript, Models: in.DrizzleModels, Resolver: in.Modules, Sources: sources, Migrations: in.Schema})
	in.SupabaseTypes, _, _ = supabasetypes.Link(supabasetypes.Input{Sources: sources, Migrations: in.Schema, Edges: in.SchemaEdges})
	return in
}

// prismaManifests are the files Prisma discovery reads besides schemas
// and configs (manifests, lockfiles and workspace files).
var prismaManifests = map[string]bool{"package.json": true, "package-lock.json": true, "pnpm-lock.yaml": true, "pnpm-workspace.yaml": true, "yarn.lock": true, "bun.lock": true}

// tsConfig reports a tsconfig file (the module resolver reads its paths).
func tsConfig(name string) bool {
	return strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json") || name == "jsconfig.json"
}

// tsLanguage is the parser language of a TypeScript or JavaScript file,
// "" for any other file.
func tsLanguage(file string) string {
	switch {
	case strings.HasSuffix(file, ".d.ts"):
		return ""
	case strings.HasSuffix(file, ".ts"), strings.HasSuffix(file, ".mts"), strings.HasSuffix(file, ".cts"):
		return "typescript"
	case strings.HasSuffix(file, ".tsx"):
		return "tsx"
	case strings.HasSuffix(file, ".js"), strings.HasSuffix(file, ".mjs"), strings.HasSuffix(file, ".cjs"), strings.HasSuffix(file, ".jsx"):
		return "javascript"
	}
	return ""
}

const sqlmodelFixtureRoot = "../../../testdata/fixtures/table-access-sqlmodel"

const prismaFixtureRoot = "../../../testdata/fixtures/table-access-prisma"

const drizzleFixtureRoot = "../../../testdata/fixtures/table-access-drizzle"

const supabaseFixtureRoot = "../../../testdata/fixtures/table-access-supabase"
