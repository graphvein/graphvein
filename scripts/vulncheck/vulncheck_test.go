package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A trimmed govulncheck v1.10.0 JSON stream: GO-2026-0001 is called (a
// function in its trace), GO-2026-0002 only required, GO-2026-0003 called.
const sampleScan = `{"config": {"protocol_version": "v1.0.0", "db": "https://vuln.go.dev", "db_last_modified": "2026-11-23T20:05:14Z", "scan_mode": "binary"}}
{"progress": {"message": "Scanning your binary for known vulnerabilities..."}}
{"osv": {"id": "GO-2026-0001", "summary": "Called bug"}}
{"osv": {"id": "GO-2026-0002", "summary": "Required bug"}}
{"osv": {"id": "GO-2026-0003", "summary": "Stdlib bug"}}
{"finding": {"osv": "GO-2026-0001", "fixed_version": "v1.2.0", "trace": [{"module": "example.com/m", "version": "v1.1.0"}]}}
{"finding": {"osv": "GO-2026-0001", "fixed_version": "v1.2.0", "trace": [{"module": "example.com/m", "version": "v1.1.0", "package": "example.com/m/p", "function": "F"}]}}
{"finding": {"osv": "GO-2026-0002", "fixed_version": "v0.3.0", "trace": [{"module": "example.com/n", "version": "v0.2.0"}]}}
{"finding": {"osv": "GO-2026-0003", "fixed_version": "go1.27.4", "trace": [{"module": "stdlib", "version": "go1.27.1", "package": "net/http", "function": "Error"}]}}
`

func mustScan(t *testing.T) Scan {
	t.Helper()
	scan, err := ReadScan(strings.NewReader(sampleScan))
	if err != nil {
		t.Fatal(err)
	}
	return scan
}

func TestReadScanMergesTraces(t *testing.T) {
	scan := mustScan(t)
	if scan.DBLastModified != "2026-11-23T20:05:14Z" {
		t.Errorf("db_last_modified = %q", scan.DBLastModified)
	}
	want := []Finding{
		{ID: "GO-2026-0001", Summary: "Called bug", Module: "example.com/m", Version: "v1.1.0", Fixed: "v1.2.0", Symbol: true},
		{ID: "GO-2026-0002", Summary: "Required bug", Module: "example.com/n", Version: "v0.2.0", Fixed: "v0.3.0"},
		{ID: "GO-2026-0003", Summary: "Stdlib bug", Module: "stdlib", Version: "go1.27.1", Fixed: "go1.27.4", Symbol: true},
	}
	if len(scan.Findings) != len(want) {
		t.Fatalf("findings = %+v", scan.Findings)
	}
	for i := range want {
		if scan.Findings[i] != want[i] {
			t.Errorf("finding %d = %+v, want %+v", i, scan.Findings[i], want[i])
		}
	}
}

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestEvaluate(t *testing.T) {
	scan := mustScan(t)
	tests := []struct {
		name                                      string
		allow                                     string
		today                                     string
		blocking, allowed, unreached, exp, unused int
	}{
		{"no allowlist: called findings block", "", "2026-11-24", 2, 0, 1, 0, 0},
		{"allowed through expiry day", "GO-2026-0001 2026-11-24 r\nGO-2026-0003 2026-12-01 r", "2026-11-24", 0, 2, 1, 0, 0},
		{"expired entry blocks and fails itself", "GO-2026-0001 2026-11-23 r\nGO-2026-0003 2026-12-01 r", "2026-11-24", 1, 1, 1, 1, 0},
		{"entry for an uncalled finding is unused", "GO-2026-0002 2026-12-01 r", "2026-11-24", 2, 0, 1, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allow, err := ParseAllowlist(strings.NewReader(tt.allow))
			if err != nil {
				t.Fatal(err)
			}
			res := Evaluate(scan, allow, day(tt.today))
			got := []int{len(res.Blocking), len(res.Allowed), len(res.Unreached), len(res.Expired), len(res.Unused)}
			want := []int{tt.blocking, tt.allowed, tt.unreached, tt.exp, tt.unused}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("blocking/allowed/unreached/expired/unused = %v, want %v", got, want)
				}
			}
			if res.Failed() != (tt.blocking > 0 || tt.exp > 0) {
				t.Errorf("Failed() = %v", res.Failed())
			}
		})
	}
}

func TestParseAllowlistRejectsMalformedLines(t *testing.T) {
	for _, text := range []string{
		"GO-2026-0001 2026-12-01",                              // no reason
		"CVE-2026-1 2026-12-01 reason",                         // not a Go ID
		"GO-2026-0001 01/12/2026 reason",                       // bad date
		"GO-2026-0001 2026-12-01 a\nGO-2026-0001 2026-12-02 b", // duplicate
	} {
		if _, err := ParseAllowlist(strings.NewReader(text)); err == nil {
			t.Errorf("ParseAllowlist(%q) succeeded, want an error", text)
		}
	}
}

// TestCommittedAllowlistParses keeps the committed allowlist well-formed in
// the offline suite; expiry is checked only by the online gate.
func TestCommittedAllowlistParses(t *testing.T) {
	f, err := os.Open("allowlist.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := ParseAllowlist(f); err != nil {
		t.Fatal(err)
	}
}
