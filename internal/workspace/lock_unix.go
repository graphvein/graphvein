//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package workspace

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive flock(2) lock without waiting. flock locks
// belong to the open file, so two Registry values in one process exclude
// each other as two processes do.
func tryLock(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EWOULDBLOCK):
			return errLockBusy
		}
		return err
	}
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
