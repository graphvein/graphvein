//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly || windows)

package workspace

import "os"

// tryLock has no operating-system lock on these platforms (none is a
// release target): writes are serialized within one process only (ADR
// 0087 "Known gaps").
func tryLock(*os.File) error { return nil }

func unlockFile(*os.File) error { return nil }
