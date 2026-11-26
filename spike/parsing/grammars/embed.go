// Package grammars embeds reviewed release and CI assets so the spike runs offline.
package grammars

import (
	"embed"
	"fmt"
)

//go:embed wasitter-*.wasm
var assets embed.FS

// Available lists the reviewed grammars exercised by the offline golden suite.
func Available() []string {
	return []string{"javascript", "typescript", "tsx", "rust", "go", "python", "java", "html", "angular", "c_sharp", "dart"}
}

// Load restricts the experiment to audited bundled assets.
func Load(language string) ([]byte, error) {
	found := false
	for _, name := range Available() {
		found = found || name == language
	}
	if !found {
		return nil, fmt.Errorf("unsupported language %q", language)
	}
	data, err := assets.ReadFile("wasitter-" + language + ".wasm")
	if err != nil {
		return nil, fmt.Errorf("reading %s grammar: %w", language, err)
	}
	return data, nil
}
