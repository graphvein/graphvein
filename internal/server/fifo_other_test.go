//go:build !unix

package server

// makeFIFO reports false: this OS has no named pipes in the file tree.
func makeFIFO(string) bool { return false }
