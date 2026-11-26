//go:build unix

package main

import "syscall"

// makeFIFO creates a named pipe at path and reports whether it could.
func makeFIFO(path string) bool { return syscall.Mkfifo(path, 0o644) == nil }
