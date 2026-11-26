package sqlite

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/store"
)

// TestLockStateCreatesNoFolder checks that probing the index lock of a
// store whose folder is gone (a moved repository) answers free and creates
// nothing (T038 review): a read path never recreates a folder.
func TestLockStateCreatesNoFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "moved")
	l := IndexLocker(filepath.Join(root, ".graphvein", "index.db"))
	defer l.Close()
	state, err := l.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state != store.LockFree {
		t.Fatalf("state %s, want free", state)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the probe created %s (stat error %v)", root, err)
	}
}
