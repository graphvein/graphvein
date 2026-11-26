//go:build ignore

// Native replay supports Windows, where CreateProcess cannot run shell scripts.
// Build explicitly for the manual round-trip check, never from a test.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	if err := replay(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func replay() error {
	path := os.Getenv("GV_REPLAY_STREAM")
	if path == "" {
		return fmt.Errorf("set GV_REPLAY_STREAM to the snapshot file")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening replay stream %s: %w", path, err)
	}
	defer f.Close()
	if _, err := io.Copy(os.Stdout, f); err != nil {
		return fmt.Errorf("replaying stream: %w", err)
	}
	return nil
}
