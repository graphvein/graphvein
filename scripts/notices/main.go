// Command notices writes THIRD_PARTY_NOTICES at the repository root and
// checks every bundled dependency's licence against the allowlist
// (ADR 0080). Run it from the repository root:
//
//	go run ./scripts/notices          # regenerate THIRD_PARTY_NOTICES
//	go run ./scripts/notices -check   # fail if the file is stale or a licence is not allowed
//
// It runs offline: the Go module set comes from `go list` with GOPROXY=off,
// so `go mod download` must have filled the module cache first.
// It also writes the copies of NOTICE and THIRD_PARTY_NOTICES that the
// server embeds (internal/server/ui/legal). TestThirdPartyNotices runs the
// same check in `go test ./...`.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// OutFile is the generated notice file, relative to the repository root.
const OutFile = "THIRD_PARTY_NOTICES"

func main() {
	check := flag.Bool("check", false, "compare with the committed file instead of writing it")
	flag.Parse()
	if err := run(".", *check); err != nil {
		slog.Error("third-party notices", "err", err)
		os.Exit(1)
	}
}

func run(root string, check bool) error {
	text, problems, err := Generate(root)
	if err != nil {
		return err
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "licence check:", p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d licence problem(s)", len(problems))
	}
	if check {
		current, err := os.ReadFile(OutFile)
		if err != nil {
			return fmt.Errorf("reading %s: %w", OutFile, err)
		}
		if string(current) != text {
			return fmt.Errorf("%s is stale; run go run ./scripts/notices", OutFile)
		}
		return nil
	}
	if err := os.WriteFile(OutFile, []byte(text), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", OutFile, err)
	}
	// The server embeds copies of both notice files (internal/server/ui).
	notice, err := os.ReadFile("NOTICE")
	if err != nil {
		return fmt.Errorf("reading NOTICE: %w", err)
	}
	for name, data := range map[string][]byte{"THIRD_PARTY_NOTICES.txt": []byte(text), "NOTICE.txt": notice} {
		path := filepath.Join("internal", "server", "ui", "legal", name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}
