// Package mixed holds two package clauses in built files, so the go
// command reports an error for the directory and nothing in it is read.
package mixed

// Thing would be a model if its package could be decided.
type Thing struct{ ID uint }
