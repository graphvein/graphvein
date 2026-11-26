package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Entry is one reviewed allowlist line: a vulnerability the project has
// decided to accept until Expires. Every entry expires so that an accepted
// finding is looked at again instead of being forgotten.
type Entry struct {
	ID      string
	Expires time.Time
	Reason  string
	Line    int
}

// idPattern matches Go vulnerability database IDs (GO-YYYY-NNNN), the IDs
// govulncheck reports.
var idPattern = regexp.MustCompile(`^GO-[0-9]{4}-[0-9]{4,}$`)

// ParseAllowlist reads the allowlist file format: one entry per line,
// "ID EXPIRY REASON" separated by whitespace, EXPIRY as YYYY-MM-DD (the last
// day the entry is valid, UTC). Blank lines and lines starting with # are
// ignored. A malformed line is an error, so a typo never widens the list.
func ParseAllowlist(r io.Reader) ([]Entry, error) {
	var entries []Entry
	seen := map[string]int{}
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return nil, fmt.Errorf("line %d: want \"ID EXPIRY REASON\", got %q", n, line)
		}
		id := fields[0]
		if !idPattern.MatchString(id) {
			return nil, fmt.Errorf("line %d: %q is not a Go vulnerability ID (GO-YYYY-NNNN)", n, id)
		}
		expires, err := time.Parse(time.DateOnly, fields[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: expiry %q is not YYYY-MM-DD: %w", n, fields[1], err)
		}
		if prev, ok := seen[id]; ok {
			return nil, fmt.Errorf("line %d: %s is already listed on line %d", n, id, prev)
		}
		seen[id] = n
		entries = append(entries, Entry{
			ID:      id,
			Expires: expires,
			Reason:  strings.Join(fields[2:], " "),
			Line:    n,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading allowlist: %w", err)
	}
	return entries, nil
}

// Finding is one vulnerability govulncheck reported, reduced to what the
// gate and its report need.
type Finding struct {
	ID      string
	Summary string
	Module  string
	Version string
	Fixed   string
	// Symbol is true when govulncheck found a vulnerable symbol in the
	// binary. Only symbol-level findings fail the gate: package- and
	// module-level ones mean the vulnerable code is not linked in.
	Symbol bool
}

// Scan is the parsed govulncheck JSON stream.
type Scan struct {
	DB             string
	DBLastModified string
	Findings       []Finding
}

// The govulncheck JSON protocol (v1.0.0) is a stream of objects that each
// hold exactly one of these keys; only the ones read here are modelled.
type message struct {
	Config *struct {
		DB             string `json:"db"`
		DBLastModified string `json:"db_last_modified"`
	} `json:"config"`
	OSV *struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	} `json:"osv"`
	Finding *struct {
		OSV          string `json:"osv"`
		FixedVersion string `json:"fixed_version"`
		Trace        []struct {
			Module   string `json:"module"`
			Version  string `json:"version"`
			Function string `json:"function"`
		} `json:"trace"`
	} `json:"finding"`
}

// ReadScan parses `govulncheck -format json` output. govulncheck emits one
// finding per trace, so findings are merged per vulnerability ID; an ID is
// symbol-level when any of its traces names a function.
func ReadScan(r io.Reader) (Scan, error) {
	var scan Scan
	summaries := map[string]string{}
	byID := map[string]*Finding{}
	dec := json.NewDecoder(r)
	for {
		var m message
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Scan{}, fmt.Errorf("decoding govulncheck output: %w", err)
		}
		switch {
		case m.Config != nil:
			scan.DB = m.Config.DB
			scan.DBLastModified = m.Config.DBLastModified
		case m.OSV != nil:
			summaries[m.OSV.ID] = m.OSV.Summary
		case m.Finding != nil:
			f := byID[m.Finding.OSV]
			if f == nil {
				f = &Finding{ID: m.Finding.OSV, Fixed: m.Finding.FixedVersion}
				byID[f.ID] = f
			}
			if len(m.Finding.Trace) > 0 {
				top := m.Finding.Trace[0]
				f.Module, f.Version = top.Module, top.Version
				if top.Function != "" {
					f.Symbol = true
				}
			}
		}
	}
	for _, f := range byID {
		f.Summary = summaries[f.ID]
		scan.Findings = append(scan.Findings, *f)
	}
	sort.Slice(scan.Findings, func(i, j int) bool { return scan.Findings[i].ID < scan.Findings[j].ID })
	return scan, nil
}

// Result is the gate's verdict.
type Result struct {
	Blocking  []Finding // symbol-level findings not covered by a valid entry
	Allowed   []Finding // symbol-level findings covered by a valid entry
	Unreached []Finding // package- or module-level findings (reported only)
	Expired   []Entry   // entries past their expiry date (always fail)
	Unused    []Entry   // valid entries that match no symbol-level finding
}

// Failed reports whether the gate fails.
func (r Result) Failed() bool { return len(r.Blocking) > 0 || len(r.Expired) > 0 }

// Evaluate applies the allowlist to a scan. An entry is valid through its
// expiry day (UTC); from the day after, it no longer covers anything and
// fails the gate itself, so it has to be reviewed or removed.
func Evaluate(scan Scan, allow []Entry, today time.Time) Result {
	var res Result
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	valid := map[string]bool{}
	for _, e := range allow {
		if day.After(e.Expires) {
			res.Expired = append(res.Expired, e)
			continue
		}
		valid[e.ID] = true
	}
	used := map[string]bool{}
	for _, f := range scan.Findings {
		switch {
		case !f.Symbol:
			res.Unreached = append(res.Unreached, f)
		case valid[f.ID]:
			used[f.ID] = true
			res.Allowed = append(res.Allowed, f)
		default:
			res.Blocking = append(res.Blocking, f)
		}
	}
	for _, e := range allow {
		if valid[e.ID] && !used[e.ID] {
			res.Unused = append(res.Unused, e)
		}
	}
	return res
}

// Report writes the human-readable verdict.
func Report(w io.Writer, scan Scan, res Result) {
	fmt.Fprintf(w, "vulncheck: database %s (last modified %s)\n", scan.DB, scan.DBLastModified)
	line := func(prefix string, f Finding) {
		version, fixed := f.Version, f.Fixed
		if moduleName(f.Module) == "stdlib" {
			// The JSON protocol writes Go versions in semver form (v1.27.1).
			version, fixed = goVersion(version), goVersion(fixed)
		}
		fmt.Fprintf(w, "  %s %s %s@%s, fixed in %s: %s\n", prefix, f.ID, moduleName(f.Module), version, orNone(fixed), f.Summary)
	}
	for _, f := range res.Blocking {
		line("FAIL", f)
	}
	for _, e := range res.Expired {
		fmt.Fprintf(w, "  FAIL allowlist line %d: %s expired on %s (%s)\n", e.Line, e.ID, e.Expires.Format(time.DateOnly), e.Reason)
	}
	for _, f := range res.Allowed {
		line("allowed", f)
	}
	for _, f := range res.Unreached {
		line("not linked", f)
	}
	for _, e := range res.Unused {
		fmt.Fprintf(w, "  note allowlist line %d: %s matches no finding; remove it\n", e.Line, e.ID)
	}
	fmt.Fprintf(w, "vulncheck: %d blocking, %d expired allowlist entries, %d allowed, %d not linked into the binary\n",
		len(res.Blocking), len(res.Expired), len(res.Allowed), len(res.Unreached))
}

func moduleName(m string) string {
	if m == "stdlib" || m == "" {
		return "stdlib"
	}
	return m
}

func goVersion(v string) string {
	if strings.HasPrefix(v, "v") {
		return "go" + v[1:]
	}
	return v
}

func orNone(s string) string {
	if s == "" {
		return "(no fix)"
	}
	return s
}
