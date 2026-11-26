//go:build !unix

package index_test

// makeFIFO reports false: this OS has no named pipes in the file tree.
func makeFIFO(string) bool { return false }
