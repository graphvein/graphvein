package workspace

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// LockSuffix is appended to the registry file's name to name its lock
// file. The lock file stays next to the registry and is never removed:
// removing it while another process waits on it would let two processes
// hold locks on two different files.
const LockSuffix = ".lock"

// lockTimeout bounds the wait for another process's registry write.
// Writes take milliseconds, so a longer wait means that process hangs.
const lockTimeout = 10 * time.Second

// lockRetry is the pause between attempts to take the lock.
const lockRetry = 10 * time.Millisecond

// errLockBusy is returned by tryLock when another open file holds the lock.
var errLockBusy = errors.New("the lock is held")

// fileLock is an exclusive operating-system lock on the registry's lock
// file. The kernel releases it when its holder exits or crashes, so a lock
// file left behind is never stale: it blocks nobody once its process is
// gone (ADR 0087 "Registry writes").
type fileLock struct {
	f *os.File
}

// lockFile takes the exclusive lock on path, creating the file when
// needed, and waits at most timeout for another holder to release it.
func lockFile(path string, timeout time.Duration) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the workspace registry lock %s: %w", path, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		err := tryLock(f)
		if err == nil {
			return &fileLock{f: f}, nil
		}
		if !errors.Is(err, errLockBusy) {
			f.Close()
			return nil, fmt.Errorf("locking the workspace registry %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("the workspace registry lock %s is held by another process for more than %s; try again", path, timeout)
		}
		time.Sleep(lockRetry)
	}
}

// unlock releases the lock and closes the lock file (closing alone would
// release it too; unlocking first makes the order explicit).
func (l *fileLock) unlock() error {
	uerr := unlockFile(l.f)
	cerr := l.f.Close()
	return errors.Join(uerr, cerr)
}
