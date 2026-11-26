//go:build native

package parse

import "fmt"

func newWASM(language string) (Parser, error) {
	return nil, fmt.Errorf("WASM backend requires a build without -tags native")
}
