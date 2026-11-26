//go:build ignore

// Dump the existing SQLite graph through the store interface for replay checks.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func main() {
	if err := dump(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dump() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: dump-index <sqlite-path> <repository>")
	}
	ctx := context.Background()
	s, err := sqlite.OpenRead(ctx, os.Args[1])
	if err != nil {
		return err
	}
	defer s.Close()
	snapshot, err := s.Dump(ctx, os.Args[2])
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(snapshot)
}
