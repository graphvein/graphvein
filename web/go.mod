// This module exists only to fence web/ off from the Graphvein Go module:
// web/node_modules contains Go files (flatted ships golang/pkg/flatted), and
// without this file `go list ./...`, `go vet ./...` and `go test ./...` at
// the repository root would build them (task T016c F2, ADR 0041).
module graphvein.invalid/web

go 1.27
