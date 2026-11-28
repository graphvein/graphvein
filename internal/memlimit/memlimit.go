// Package memlimit sets graphvein's default soft memory limit (ADR 0094).
//
// Go's garbage collector lets the heap grow to about twice the live heap
// before it collects (GOGC=100) and reads neither the cgroup limit nor
// the machine's memory by itself. An index run whose retained facts are
// half the memory available is therefore killed at the GC's headroom,
// with about half of its memory garbage (T079: t3code without `.repos/`,
// 3.7 GB live, killed under a 7 GiB cap). A soft limit below the
// available memory makes the GC collect harder as the heap nears it, so
// such a run finishes. The limit only steers the GC: a live heap above it
// still grows, so a repository too large for the machine is still killed.
package memlimit

import (
	"bufio"
	"bytes"
	"io/fs"
	"log/slog"
	"path"
	"runtime/debug"
	"strconv"
	"strings"
)

// Fraction of the available memory used as the soft limit, as
// FractionNum/FractionDen. 70% leaves room for what the limit does not
// govern or the cgroup also charges: the tree-sitter guest memory and
// compiled code mapped outside the Go heap, page cache charged to the
// cgroup, and other processes when the bound is physical memory. T079
// measured a 5 GiB limit (71%) under a 7 GiB cap finishing at 5.2 GB
// anonymous RSS.
const (
	FractionNum = 7
	FractionDen = 10
)

// unlimitedAbove is the value at or above which a cgroup limit means "no
// limit": cgroup v1 writes 2^63-1 rounded down to a page, and no machine
// graphvein runs on has 4 EiB.
const unlimitedAbove = int64(1) << 62

// Source names where a Decision's limit came from.
const (
	// SourceEnv: GOMEMLIMIT is set, so the runtime has already applied it
	// (or "off") and graphvein changes nothing.
	SourceEnv = "GOMEMLIMIT"
	// SourceCgroup: the memory limit of the process's cgroup.
	SourceCgroup = "cgroup"
	// SourcePhysical: the machine's physical memory.
	SourcePhysical = "physical memory"
	// SourceNone: nothing could be read; no limit is set.
	SourceNone = "none"
)

// System reads the two memory bounds of the platform. Each returns false
// when the bound does not exist or cannot be read.
type System interface {
	// CgroupLimit is the smallest memory limit on the process's cgroup
	// and its ancestors, in bytes.
	CgroupLimit() (int64, bool)
	// PhysicalMemory is the machine's total physical memory, in bytes.
	PhysicalMemory() (int64, bool)
}

// Decision is the chosen soft limit. Limit is 0 when none is set.
type Decision struct {
	Limit     int64
	Source    string
	Available int64
}

// Decide chooses the soft limit. envSet reports a non-empty GOMEMLIMIT
// (the runtime ignores an empty one). Otherwise the bound is the smaller
// of the cgroup limit and physical memory, whichever can be read, and the
// limit is FractionNum/FractionDen of it. A bound of zero or less, or at
// or above unlimitedAbove, counts as unreadable.
func Decide(envSet bool, cgroup int64, cgroupOK bool, physical int64, physicalOK bool) Decision {
	if envSet {
		return Decision{Source: SourceEnv}
	}
	cgroupOK = cgroupOK && valid(cgroup)
	physicalOK = physicalOK && valid(physical)
	var d Decision
	switch {
	case cgroupOK && (!physicalOK || cgroup <= physical):
		d = Decision{Source: SourceCgroup, Available: cgroup}
	case physicalOK:
		d = Decision{Source: SourcePhysical, Available: physical}
	default:
		return Decision{Source: SourceNone}
	}
	// Divide first: Available*FractionNum could overflow near 2^62.
	d.Limit = d.Available / FractionDen * FractionNum
	return d
}

func valid(n int64) bool { return n > 0 && n < unlimitedAbove }

// Apply sets the default soft limit for this process unless GOMEMLIMIT
// is set, and logs the decision at debug level. It never fails: a bound
// that cannot be read means no limit.
func Apply(logger *slog.Logger, sys System, getenv func(string) string) Decision {
	return apply(logger, sys, getenv, debug.SetMemoryLimit)
}

func apply(logger *slog.Logger, sys System, getenv func(string) string, set func(int64) int64) Decision {
	cgroup, cgroupOK := sys.CgroupLimit()
	physical, physicalOK := sys.PhysicalMemory()
	d := Decide(getenv("GOMEMLIMIT") != "", cgroup, cgroupOK, physical, physicalOK)
	if d.Limit > 0 {
		set(d.Limit)
	}
	logger.Debug("soft memory limit", "limit_bytes", d.Limit, "source", d.Source, "available_bytes", d.Available)
	return d
}

// Default is the System of the running platform.
func Default() System { return platform{} }

// CgroupLimitFS reads the process's cgroup memory limit from fsys, a file
// system rooted at "/" (os.DirFS("/") on Linux; a fixture in tests). It
// reads /proc/self/cgroup and /proc/self/mountinfo, finds the cgroup v2
// hierarchy and the cgroup v1 memory hierarchy, and returns the smallest
// limit set on the process's cgroup or any ancestor up to the mount
// (memory.max, memory.limit_in_bytes). "max", unlimited and unreadable
// values are skipped.
func CgroupLimitFS(fsys fs.FS) (int64, bool) {
	cgroups, err := fs.ReadFile(fsys, "proc/self/cgroup")
	if err != nil {
		return 0, false
	}
	mountinfo, err := fs.ReadFile(fsys, "proc/self/mountinfo")
	if err != nil {
		return 0, false
	}
	var v1, v2 string
	have1, have2 := false, false
	for _, line := range strings.Split(string(cgroups), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "0" && parts[1] == "" {
			v2, have2 = parts[2], true
			continue
		}
		for _, c := range strings.Split(parts[1], ",") {
			if c == "memory" {
				v1, have1 = parts[2], true
			}
		}
	}
	best, found := int64(0), false
	consider := func(n int64, ok bool) {
		if ok && valid(n) && (!found || n < best) {
			best, found = n, true
		}
	}
	for _, m := range parseMountinfo(mountinfo) {
		switch {
		case have2 && m.fstype == "cgroup2":
			consider(walk(fsys, m, v2, "memory.max"))
		case have1 && m.fstype == "cgroup" && hasOption(m.superOptions, "memory"):
			consider(walk(fsys, m, v1, "memory.limit_in_bytes"))
		}
	}
	return best, found
}

type mount struct {
	root, point, fstype, superOptions string
}

// parseMountinfo reads the fields of /proc/self/mountinfo it needs: the
// mount's root within its file system (4th field), its mount point (5th)
// and, after the "-" separator, the file system type and super options.
func parseMountinfo(b []byte) []mount {
	var out []mount
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		sep := -1
		for i := 6; i < len(f); i++ {
			if f[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+3 >= len(f) {
			continue
		}
		out = append(out, mount{root: unescape(f[3]), point: unescape(f[4]), fstype: f[sep+1], superOptions: f[sep+3]})
	}
	return out
}

// unescape decodes the octal escapes (\040 for a space) mountinfo uses.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hasOption(options, want string) bool {
	for _, o := range strings.Split(options, ",") {
		if o == want {
			return true
		}
	}
	return false
}

// walk reads file in the process's cgroup directory under mount m and in
// each ancestor up to the mount point, and returns the smallest limit.
// A cgroup path outside the mount's root (another cgroup namespace) is
// not readable.
func walk(fsys fs.FS, m mount, cgroupPath, file string) (int64, bool) {
	rel := cgroupPath
	if m.root != "/" {
		if rel != m.root && !strings.HasPrefix(rel, m.root+"/") {
			return 0, false
		}
		rel = strings.TrimPrefix(rel, m.root)
	}
	top := strings.TrimPrefix(path.Clean(m.point), "/")
	dir := path.Join(top, strings.TrimPrefix(path.Clean("/"+rel), "/"))
	best, found := int64(0), false
	for {
		if n, ok := readLimit(fsys, path.Join(dir, file)); ok && (!found || n < best) {
			best, found = n, true
		}
		if dir == top || dir == "." || dir == "" {
			break
		}
		dir = path.Dir(dir)
	}
	return best, found
}

func readLimit(fsys fs.FS, name string) (int64, bool) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || !valid(n) {
		return 0, false
	}
	return n, true
}

// MemTotal reads MemTotal from the contents of /proc/meminfo, in bytes.
func MemTotal(meminfo []byte) (int64, bool) {
	for _, line := range strings.Split(string(meminfo), "\n") {
		rest, ok := strings.CutPrefix(line, "MemTotal:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) != 2 || f[1] != "kB" {
			return 0, false
		}
		kb, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil || kb <= 0 || kb >= unlimitedAbove/1024 {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}
