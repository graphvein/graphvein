//go:build !native || !cgo

package parse

import "fmt"

func newNative(language string) (Parser, error) {
	return nil, fmt.Errorf("native backend requires a C compiler and CGO_ENABLED=1 go build -tags native")
}
