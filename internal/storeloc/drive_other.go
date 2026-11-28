//go:build !windows

package storeloc

// remoteDrive is Windows-only: elsewhere a network file system is a
// mounted folder, which a path does not reveal.
func remoteDrive(string) bool { return false }
