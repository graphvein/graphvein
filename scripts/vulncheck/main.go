// Command vulncheck is the gate over govulncheck's binary-mode scan of the
// graphvein binary (ADR 0086). It reads `govulncheck -format json` output and
// fails when a vulnerable symbol is linked into the binary and the finding is
// not covered by a reviewed, unexpired entry in the allowlist. Package- and
// module-level findings are reported but do not fail. scripts/vulncheck.sh
// runs it:
//
//	go run ./scripts/vulncheck -allowlist scripts/vulncheck/allowlist.txt scan.json
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"
)

func main() {
	allowPath := flag.String("allowlist", "scripts/vulncheck/allowlist.txt", "reviewed allowlist file")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: vulncheck [-allowlist file] govulncheck.json")
		os.Exit(2)
	}
	failed, err := run(*allowPath, flag.Arg(0), time.Now().UTC())
	if err != nil {
		slog.Error("vulncheck", "err", err)
		os.Exit(1)
	}
	if failed {
		os.Exit(1)
	}
}

func run(allowPath, scanPath string, today time.Time) (bool, error) {
	af, err := os.Open(allowPath)
	if err != nil {
		return false, fmt.Errorf("opening allowlist: %w", err)
	}
	defer af.Close()
	allow, err := ParseAllowlist(af)
	if err != nil {
		return false, fmt.Errorf("parsing %s: %w", allowPath, err)
	}
	sf, err := os.Open(scanPath)
	if err != nil {
		return false, fmt.Errorf("opening scan: %w", err)
	}
	defer sf.Close()
	scan, err := ReadScan(sf)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", scanPath, err)
	}
	res := Evaluate(scan, allow, today)
	Report(os.Stdout, scan, res)
	return res.Failed(), nil
}
