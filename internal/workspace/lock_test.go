package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// helperEnv makes the test binary act as a separate graphvein process
// that adds entries to a registry (TestRegistryHelperProcess).
const helperEnv = "GRAPHVEIN_TEST_WORKSPACE_HELPER"

// TestRegistryConcurrentWriters checks WS-3: writers that do not share a
// Registry value (as two serve processes do not) never lose an entry,
// because each read, change and replace happens under the lock file's
// operating-system lock.
func TestRegistryConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	const writers, each = 6, 8
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := Open(path) // a separate value: no shared mutex
			for i := 0; i < each; i++ {
				name := fmt.Sprintf("g%d-%d", w, i)
				if _, _, err := r.Add(Entry{Kind: KindRepository, Name: name, Path: filepath.Join(filepath.Dir(path), "src", name)}); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	entries, err := Open(path).Entries()
	if err != nil || len(entries) != writers*each {
		t.Fatalf("%d entries, want %d: %v", len(entries), writers*each, err)
	}
}

// TestRegistryProcesses checks WS-3 across real processes: several
// copies of the test binary add entries to one registry at once and none
// is lost.
func TestRegistryProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("starts processes")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skip("no test executable:", err)
	}
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	const procs, each = 4, 15
	cmds := make([]*exec.Cmd, procs)
	for p := range cmds {
		cmd := exec.Command(exe, "-test.run=^TestRegistryHelperProcess$")
		cmd.Env = append(os.Environ(), helperEnv+"="+path+"|"+strconv.Itoa(p)+"|"+strconv.Itoa(each))
		cmds[p] = cmd
	}
	outs := make([][]byte, procs)
	errs := make([]error, procs)
	var wg sync.WaitGroup
	for p, cmd := range cmds {
		wg.Add(1)
		go func(p int, cmd *exec.Cmd) {
			defer wg.Done()
			outs[p], errs[p] = cmd.CombinedOutput()
		}(p, cmd)
	}
	wg.Wait()
	for p := range cmds {
		if errs[p] != nil {
			t.Fatalf("process %d: %v\n%s", p, errs[p], outs[p])
		}
	}
	entries, err := Open(path).Entries()
	if err != nil || len(entries) != procs*each {
		t.Fatalf("%d entries, want %d: %v", len(entries), procs*each, err)
	}
}

// TestRegistryHelperProcess is the body of one process of
// TestRegistryProcesses; it does nothing in a normal test run.
func TestRegistryHelperProcess(t *testing.T) {
	spec := os.Getenv(helperEnv)
	if spec == "" {
		return
	}
	parts := strings.Split(spec, "|")
	if len(parts) != 3 {
		t.Fatalf("bad %s %q", helperEnv, spec)
	}
	each, _ := strconv.Atoi(parts[2])
	r := Open(parts[0])
	for i := 0; i < each; i++ {
		name := fmt.Sprintf("p%s-%d", parts[1], i)
		if _, _, err := r.Add(Entry{Kind: KindRepository, Name: name, Path: filepath.Join(filepath.Dir(parts[0]), "src", name)}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRegistryLockHeld checks WS-3's refusal: while another holder keeps
// the lock, a write waits and then fails without touching the file; once
// the holder releases it (as the kernel does when a process exits), the
// lock file left behind blocks nobody.
func TestRegistryLockHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	r := Open(path)
	if _, _, err := r.Add(Entry{Kind: KindRepository, Name: "a", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	held, err := lockFile(path+LockSuffix, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r.lockWait = 50 * time.Millisecond
	if _, _, err := r.Add(Entry{Kind: KindRepository, Name: "b", Path: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "held by another process") {
		t.Fatalf("add under a held lock: %v", err)
	}
	if _, err := r.Remove(EntryID(KindRepository, "x", "a")); err == nil {
		t.Fatal("remove under a held lock succeeded")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("the registry changed under a held lock")
	}
	if entries, err := r.Entries(); err != nil || len(entries) != 1 {
		t.Fatalf("reads do not wait for the lock: %v %v", entries, err)
	}
	if err := held.unlock(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + LockSuffix); err != nil {
		t.Fatalf("lock file: %v", err)
	}
	if _, added, err := r.Add(Entry{Kind: KindRepository, Name: "b", Path: t.TempDir()}); err != nil || !added {
		t.Fatalf("add after release: %v %v", added, err)
	}
}
