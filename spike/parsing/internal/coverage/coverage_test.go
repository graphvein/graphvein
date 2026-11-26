//go:build !native

package coverage

import (
	"path/filepath"
	"testing"
)

func TestGrammarGoldens(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	for _, result := range Run() {
		if result.Error != "" {
			t.Errorf("%s: %s", result.Language, result.Error)
		}
	}
}
