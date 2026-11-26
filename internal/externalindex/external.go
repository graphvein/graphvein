// Package externalindex streams user-supplied producers through the fact contract.
package externalindex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
)

// ResolveExecutable treats every input as a path, never a PATH lookup.
func ResolveExecutable(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving indexer %s: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("checking indexer %s: %w", abs, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("indexer %s is not a regular executable file", abs)
	}
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(filepath.Ext(abs), ".exe") && !strings.EqualFold(filepath.Ext(abs), ".com") {
			return "", fmt.Errorf("indexer %s must be a native executable on Windows", abs)
		}
	} else if info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("indexer %s is not executable", abs)
	}
	return abs, nil
}

// Run uses an absolute executable and root; emit optionally receives an exact copy.
func Run(ctx context.Context, executable, root, repository string, timeout time.Duration, s store.Store, emit io.Writer) (store.Writes, error) {
	return run(ctx, executable, nil, root, repository, timeout, s, emit)
}

func run(ctx context.Context, executable string, prefix []string, root, repository string, timeout time.Duration, s store.Store, emit io.Writer) (store.Writes, error) {
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()
	cmd := command(ctx, executable, prefix, root, repository)
	return consume(ctx, cancel, cmd, repository, s, emit)
}

func command(ctx context.Context, executable string, prefix []string, root, repository string) *exec.Cmd {
	args := append(append([]string{}, prefix...), "--repository", repository, root)
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = root
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if !strings.HasPrefix(strings.ToUpper(name), "GRAPHVEIN_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	return cmd
}

func consume(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, repository string, s store.Store, emit io.Writer) (store.Writes, error) {
	var tail tailBuffer
	cmd.Stderr = &tail
	// An explicit pipe lets us reach EOF before Wait, without StdoutPipe's
	// requirement to finish reads before Wait closes its descriptor.
	reader, writer, err := os.Pipe()
	if err != nil {
		return store.Writes{}, fmt.Errorf("opening indexer stdout: %w", err)
	}
	defer reader.Close()
	cmd.Stdout = writer
	if err := cmd.Start(); err != nil {
		writer.Close()
		return store.Writes{}, fmt.Errorf("starting external indexer: %w", err)
	}
	writer.Close()
	// Cancellation also unblocks a read if a descendant inherited stdout.
	stop := context.AfterFunc(ctx, func() { reader.Close() })
	defer stop()
	waited := false
	wait := func() error {
		waited = true
		err := cmd.Wait()
		if ctx.Err() != nil {
			return fmt.Errorf("external indexer: %w", ctx.Err())
		}
		if err != nil {
			return fmt.Errorf("external indexer exit: %w", err)
		}
		return nil
	}
	var input io.Reader = reader
	if emit != nil {
		input = io.TeeReader(reader, emit)
	}
	writes, ingestErr := ingest.StreamWithOptions(ctx, input, s, ingest.Options{Repository: repository, PreserveFactCache: true, BeforeCommit: wait})
	if !waited {
		cancel()
		// Bound stderr draining if an inherited descriptor outlives the child.
		cmd.WaitDelay = time.Second
		waitErr := wait()
		ingestErr = errors.Join(ingestErr, waitErr)
	}
	slog.Debug("external indexer stderr", "stderr", string(tail.data))
	if ingestErr != nil {
		if lines := tail.lastLines(20); lines != "" {
			return writes, fmt.Errorf("%w\nexternal indexer stderr (last 20 lines):\n%s", ingestErr, lines)
		}
		return writes, ingestErr
	}
	return writes, nil
}

type tailBuffer struct{ data []byte }

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	const limit = 64 << 10
	if len(p) >= limit {
		b.data = append(b.data[:0], p[len(p)-limit:]...)
	} else {
		if len(b.data)+len(p) > limit {
			b.data = b.data[len(b.data)+len(p)-limit:]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (b *tailBuffer) lastLines(n int) string {
	lines := strings.Split(strings.TrimSuffix(string(b.data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
