//go:build !native

package parse_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"graphvein/spike/parsing/internal/experiment"
	"graphvein/spike/parsing/internal/parse"
)

func TestInlineAngularGolden(t *testing.T) {
	path := filepath.Join("..", "..", "samples", "angular-inline.ts")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range [][]byte{source, []byte(strings.ReplaceAll(string(source), "\n", "\r\n"))} {
		entries, diagnostics := parse.InlineAngular(source)
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		if err := experiment.CheckGolden(entries, path+".golden.json"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInlineAngularFailureIsolation(t *testing.T) {
	source := []byte("@Component({template: `<p>${name}</p>`}) class Dynamic {}\n" +
		"@Component({template: '<p>\\n</p>'}) class Escaped {}\n" +
		"@Component({template: value}) class Computed {}\n" +
		"@Component({template: '<p>{{</p>'}) class Broken {}\n" +
		"@Other({template: '<aside>ignored</aside>'}) class Other {}\n" +
		"@Component({template: `<p>λ</p>`}) class Valid {}")
	entries, diagnostics := parse.InlineAngular(source)
	if len(diagnostics) != 4 {
		t.Fatalf("got %d diagnostics: %v", len(diagnostics), diagnostics)
	}
	want := []parse.Entry{{Kind: "element", Name: "p", Line: 6}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("got %v, want %v", entries, want)
	}
}
