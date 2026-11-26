package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// structureMaxFileSize is the indexer's default parsing limit (row TS-F3).
const structureMaxFileSize = 2 << 20

// structureExtraFiles are the fixture cases the repository cannot store as
// written (it normalises line endings to LF) or should not store (a file
// over the parsing limit): the tests write them into the temporary copy
// (TestTypeScriptStructureFixture's golden included, through
// export_test.go).
var structureExtraFiles = map[string]string{
	// CRLF line endings: spans and hashes as for LF (TS-D1, TS-D10).
	"src/crlf.ts": "export function crlf() {\r\n  return 1;\r\n}\r\nexport type CR = string;\r\n",
	// A lone CR ends the // comment for tree-sitter and the compiler alike.
	"src/lonecr.ts": "// note\rexport function afterCR() {}\n",
	// A byte order mark, a two-byte and a four-byte character before a
	// declaration (UTF-8 byte columns).
	"src/bom.ts": "\xef\xbb\xbf// \xc3\xa9 \xf0\x9f\x98\x80\nexport function bom() {}\n",
	// Over the parsing limit: the file node only (TS-F3).
	"src/large.js": "export function large() {}\n" + strings.Repeat("// padding line for the parsing limit\n", structureMaxFileSize/38+1),
}

// writeStructureExtraFiles writes structureExtraFiles below root.
func writeStructureExtraFiles(t *testing.T, root string) {
	t.Helper()
	for name, content := range structureExtraFiles {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
