package schema

import (
	"strconv"
	"strings"
)

// C4 (T018a2 B5 as amended by Revision 1): whether the set's sqlx reads
// `-- no-transaction`. sqlx reads the marker from 0.8.0 (CHANGELOG 0.8.0,
// #3181; source.rs 127 at 0.8.6), and A1 verified it for the 0.8 series
// only, so the pass knows the answer when the Cargo requirement can only
// resolve to 0.8 versions (the marker is read) or only to versions before
// 0.8.0 (it is not). Any other requirement, a pre-release, an entry without
// a version (git or path), or two entries that disagree leave it unknown,
// and a marked migration is then followed both ways (transactions.go).

// markerReading is what the pass knows about the marker.
type markerReading uint8

const (
	// markerUnknown: the requirement may resolve to versions on both sides.
	markerUnknown markerReading = iota
	// markerRead: every version the requirement allows reads the marker.
	markerRead
	// markerIgnored: no version the requirement allows reads it.
	markerIgnored
)

// semver is a version without pre-release or build parts.
type semver [3]int

func (a semver) less(b semver) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// noUpper stands for an interval without an upper bound.
var noUpper = semver{1 << 30, 0, 0}

// requirementReading classifies one Cargo version requirement (Cargo
// reference, "Specifying dependencies": caret, tilde, wildcard,
// comparison and multiple requirements separated by commas; a bare
// version is a caret requirement).
func requirementReading(req string) markerReading {
	lo, hi, ok := requirementRange(req)
	switch {
	case !ok || !lo.less(hi):
		return markerUnknown
	case !lo.less(semver{0, 8, 0}) && !(semver{0, 9, 0}).less(hi):
		return markerRead
	case !(semver{0, 8, 0}).less(hi):
		return markerIgnored
	}
	return markerUnknown
}

// requirementRange returns the half-open interval [lo, hi) of versions a
// requirement allows; ok is false when it cannot be read exactly.
func requirementRange(req string) (lo, hi semver, ok bool) {
	lo, hi = semver{}, noUpper
	parts := strings.Split(req, ",")
	for _, p := range parts {
		l, h, ok := comparatorRange(strings.TrimSpace(p))
		if !ok {
			return lo, hi, false
		}
		if lo.less(l) {
			lo = l
		}
		if h.less(hi) {
			hi = h
		}
	}
	return lo, hi, true
}

// comparatorRange reads one comparator.
func comparatorRange(c string) (lo, hi semver, ok bool) {
	op := ""
	for _, o := range []string{">=", "<=", "^", "~", "=", ">", "<"} {
		if strings.HasPrefix(c, o) {
			op, c = o, strings.TrimSpace(c[len(o):])
			break
		}
	}
	if c == "*" && op == "" {
		return semver{}, noUpper, true
	}
	fields := strings.Split(c, ".")
	if len(fields) == 0 || len(fields) > 3 {
		return lo, hi, false
	}
	var v semver
	n := 0
	wild := false
	for i, f := range fields {
		if f == "*" || f == "x" || f == "X" {
			if op != "" {
				return lo, hi, false
			}
			wild = true
			continue
		}
		if wild {
			return lo, hi, false
		}
		x, err := strconv.Atoi(f)
		if err != nil || x < 0 || strconv.Itoa(x) != f {
			// Pre-releases, build metadata and other text are not read.
			return lo, hi, false
		}
		v[i] = x
		n = i + 1
	}
	// next is the first version after every version that matches the n
	// written parts exactly.
	next := func() semver {
		switch n {
		case 1:
			return semver{v[0] + 1, 0, 0}
		case 2:
			return semver{v[0], v[1] + 1, 0}
		}
		return semver{v[0], v[1], v[2] + 1}
	}
	switch {
	case n == 0:
		return lo, hi, false
	case wild:
		return v, next(), true
	case op == "" || op == "^":
		switch {
		case v[0] > 0 || n == 1:
			return v, semver{v[0] + 1, 0, 0}, true
		case v[1] > 0 || n == 2:
			return v, semver{0, v[1] + 1, 0}, true
		}
		return v, semver{0, 0, v[2] + 1}, true
	case op == "~":
		if n == 1 {
			return v, semver{v[0] + 1, 0, 0}, true
		}
		return v, semver{v[0], v[1] + 1, 0}, true
	case op == "=":
		return v, next(), true
	case op == ">=":
		return v, noUpper, true
	case op == ">":
		return next(), noUpper, true
	case op == "<":
		return semver{}, v, true
	case op == "<=":
		return semver{}, next(), true
	}
	return lo, hi, false
}

// entryVersion returns the version requirement of a dependency entry, or
// "" when it has none (git or path dependencies, or a value that is not a
// string).
func entryVersion(entry any) string {
	switch e := entry.(type) {
	case string:
		return e
	case tomlTable:
		if v, ok := e["version"].(string); ok {
			return v
		}
	}
	return ""
}

// combineReadings merges the readings of several sqlx entries: they must
// agree.
func combineReadings(readings []markerReading) markerReading {
	if len(readings) == 0 {
		return markerUnknown
	}
	for _, r := range readings[1:] {
		if r != readings[0] {
			return markerUnknown
		}
	}
	return readings[0]
}
