package rust

import (
	"context"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/scan"
)

// WriteStructureExtraFiles lets the external test package write the
// fixture cases the repository cannot store (structureExtraFiles).
var WriteStructureExtraFiles = writeStructureExtraFiles

// StructureRoot is one crate root Graphvein reads (row RS-M1), for the
// structure oracle's comparison with cargo metadata.
type StructureRoot struct {
	Manifest, File, Kind string
}

// StructureRoots scans root as the indexer does and returns the crate
// roots NewModuleResolver reads from its manifests.
func StructureRoots(t *testing.T, root string) []StructureRoot {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]facts.Facts{}
	sources := map[string][]byte{}
	for _, f := range scanned.Files {
		sources[f.Record.Path] = f.Source
		if f.Record.Language == "rust" {
			files[f.Record.Path] = facts.Facts{}
		}
	}
	var out []StructureRoot
	for _, r := range NewModuleResolver(files, sources).Roots() {
		out = append(out, StructureRoot(r))
	}
	return out
}
