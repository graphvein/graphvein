package memlimit

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
)

const gib = int64(1) << 30

func TestDecide(t *testing.T) {
	cases := []struct {
		name       string
		env        bool
		cgroup     int64
		cgroupOK   bool
		physical   int64
		physicalOK bool
		want       Decision
	}{
		{"env set wins over everything", true, 7 * gib, true, 16 * gib, true, Decision{Source: SourceEnv}},
		{"env set with nothing readable", true, 0, false, 0, false, Decision{Source: SourceEnv}},
		{"cgroup below physical", false, 7 * gib, true, 16 * gib, true, Decision{Limit: 7 * gib / 10 * 7, Source: SourceCgroup, Available: 7 * gib}},
		{"cgroup equal to physical", false, 16 * gib, true, 16 * gib, true, Decision{Limit: 16 * gib / 10 * 7, Source: SourceCgroup, Available: 16 * gib}},
		{"cgroup above physical", false, 64 * gib, true, 16 * gib, true, Decision{Limit: 16 * gib / 10 * 7, Source: SourcePhysical, Available: 16 * gib}},
		{"cgroup only", false, 2 * gib, true, 0, false, Decision{Limit: 2 * gib / 10 * 7, Source: SourceCgroup, Available: 2 * gib}},
		{"physical only", false, 0, false, 8 * gib, true, Decision{Limit: 8 * gib / 10 * 7, Source: SourcePhysical, Available: 8 * gib}},
		{"cgroup unlimited value", false, 9223372036854771712, true, 8 * gib, true, Decision{Limit: 8 * gib / 10 * 7, Source: SourcePhysical, Available: 8 * gib}},
		{"cgroup zero", false, 0, true, 8 * gib, true, Decision{Limit: 8 * gib / 10 * 7, Source: SourcePhysical, Available: 8 * gib}},
		{"negative physical", false, 0, false, -1, true, Decision{Source: SourceNone}},
		{"nothing readable", false, 0, false, 0, false, Decision{Source: SourceNone}},
		{"largest valid value does not overflow", false, unlimitedAbove - 1, true, 0, false, Decision{Limit: (unlimitedAbove - 1) / 10 * 7, Source: SourceCgroup, Available: unlimitedAbove - 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decide(c.env, c.cgroup, c.cgroupOK, c.physical, c.physicalOK)
			if got != c.want {
				t.Fatalf("Decide = %+v, want %+v", got, c.want)
			}
			if got.Limit < 0 || got.Limit > got.Available {
				t.Fatalf("limit %d outside 0..%d", got.Limit, got.Available)
			}
		})
	}
}

// mountinfoV1 and mountinfoV2 are trimmed from real hosts: a cgroup v1
// container (with a cgroup2 mount carrying no memory controller) and a
// cgroup v2 host.
const mountinfoV1 = `36 23 0:28 / /sys/fs/cgroup rw,relatime - tmpfs tmpfs rw,mode=755
37 36 0:29 / /sys/fs/cgroup/cpu rw,relatime - cgroup cgroup rw,cpu
40 36 0:32 / /sys/fs/cgroup/memory rw,relatime - cgroup cgroup rw,memory
46 36 0:38 / /sys/fs/cgroup/unified rw,relatime - cgroup2 cgroup2 rw
`

const mountinfoV2 = `24 31 0:22 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw
35 24 0:30 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:9 - cgroup2 cgroup2 rw,nsdelegate,memory_recursiveprot
`

func TestCgroupLimitFS(t *testing.T) {
	file := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	cases := []struct {
		name  string
		files fstest.MapFS
		want  int64
		ok    bool
	}{
		{
			name: "v2 own cgroup",
			files: fstest.MapFS{
				"proc/self/cgroup":                              file("0::/user.slice/run.scope\n"),
				"proc/self/mountinfo":                           file(mountinfoV2),
				"sys/fs/cgroup/user.slice/run.scope/memory.max": file("7516192768\n"),
				"sys/fs/cgroup/user.slice/memory.max":           file("max\n"),
			},
			want: 7516192768, ok: true,
		},
		{
			name: "v2 ancestor limit is smaller",
			files: fstest.MapFS{
				"proc/self/cgroup":                              file("0::/user.slice/run.scope\n"),
				"proc/self/mountinfo":                           file(mountinfoV2),
				"sys/fs/cgroup/user.slice/run.scope/memory.max": file("max\n"),
				"sys/fs/cgroup/user.slice/memory.max":           file("4294967296\n"),
			},
			want: 4 * gib, ok: true,
		},
		{
			name: "v2 max everywhere",
			files: fstest.MapFS{
				"proc/self/cgroup":           file("0::/a\n"),
				"proc/self/mountinfo":        file(mountinfoV2),
				"sys/fs/cgroup/a/memory.max": file("max\n"),
			},
		},
		{
			name: "v2 root cgroup (namespace) has no memory.max",
			files: fstest.MapFS{
				"proc/self/cgroup":    file("0::/\n"),
				"proc/self/mountinfo": file(mountinfoV2),
			},
		},
		{
			name: "v1 own cgroup, unlimited parent, hybrid cgroup2 without memory",
			files: fstest.MapFS{
				"proc/self/cgroup":    file("4:memory:/process_api/x/bash\n1:cpu:/\n0::/\n"),
				"proc/self/mountinfo": file(mountinfoV1),
				"sys/fs/cgroup/memory/process_api/x/bash/memory.limit_in_bytes": file("7516192768\n"),
				"sys/fs/cgroup/memory/process_api/x/memory.limit_in_bytes":      file("9223372036854771712\n"),
				"sys/fs/cgroup/memory/memory.limit_in_bytes":                    file("9223372036854771712\n"),
			},
			want: 7 * gib, ok: true,
		},
		{
			name: "v1 unlimited",
			files: fstest.MapFS{
				"proc/self/cgroup":                           file("4:memory:/\n"),
				"proc/self/mountinfo":                        file(mountinfoV1),
				"sys/fs/cgroup/memory/memory.limit_in_bytes": file("9223372036854771712\n"),
			},
		},
		{
			name: "v1 mount root strips the cgroup path prefix",
			files: fstest.MapFS{
				"proc/self/cgroup":                           file("9:memory,hugetlb:/docker/abc\n"),
				"proc/self/mountinfo":                        file("40 36 0:32 /docker/abc /sys/fs/cgroup/memory ro - cgroup cgroup rw,memory,hugetlb\n"),
				"sys/fs/cgroup/memory/memory.limit_in_bytes": file("2147483648\n"),
			},
			want: 2 * gib, ok: true,
		},
		{
			name: "cgroup path outside the mount root is not read",
			files: fstest.MapFS{
				"proc/self/cgroup":                           file("9:memory:/other\n"),
				"proc/self/mountinfo":                        file("40 36 0:32 /docker/abc /sys/fs/cgroup/memory ro - cgroup cgroup rw,memory\n"),
				"sys/fs/cgroup/memory/memory.limit_in_bytes": file("2147483648\n"),
			},
		},
		{
			name: "escaped mount point",
			files: fstest.MapFS{
				"proc/self/cgroup":    file("0::/a\n"),
				"proc/self/mountinfo": file("35 24 0:30 / /my\\040cg rw - cgroup2 cgroup2 rw\n"),
				"my cg/a/memory.max":  file("1073741824\n"),
			},
			want: gib, ok: true,
		},
		{
			name: "v1 and v2 both limited: the smaller",
			files: fstest.MapFS{
				"proc/self/cgroup":           file("4:memory:/m\n0::/u\n"),
				"proc/self/mountinfo":        file("40 36 0:32 / /v1 rw - cgroup cgroup rw,memory\n46 36 0:38 / /v2 rw - cgroup2 cgroup2 rw\n"),
				"v1/m/memory.limit_in_bytes": file("3221225472\n"),
				"v2/u/memory.max":            file("2147483648\n"),
			},
			want: 2 * gib, ok: true,
		},
		{
			name: "garbage limit file",
			files: fstest.MapFS{
				"proc/self/cgroup":           file("0::/a\n"),
				"proc/self/mountinfo":        file(mountinfoV2),
				"sys/fs/cgroup/a/memory.max": file("lots\n"),
			},
		},
		{
			name: "unreadable mountinfo",
			files: fstest.MapFS{
				"proc/self/cgroup": file("0::/a\n"),
			},
		},
		{
			name:  "no proc at all",
			files: fstest.MapFS{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := CgroupLimitFS(c.files)
			if got != c.want || ok != c.ok {
				t.Fatalf("CgroupLimitFS = %d, %v; want %d, %v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestMemTotal(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"MemTotal:       16473316 kB\nMemFree:  1 kB\n", 16473316 * 1024, true},
		{"MemFree:  1 kB\nMemTotal: 1024 kB\n", 1024 * 1024, true},
		{"MemTotal: 12 MB\n", 0, false},
		{"MemTotal: x kB\n", 0, false},
		{"MemTotal: 0 kB\n", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := MemTotal([]byte(c.in))
		if got != c.want || ok != c.ok {
			t.Errorf("MemTotal(%q) = %d, %v; want %d, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

type fakeSystem struct {
	cgroup, physical     int64
	cgroupOK, physicalOK bool
}

func (f fakeSystem) CgroupLimit() (int64, bool)    { return f.cgroup, f.cgroupOK }
func (f fakeSystem) PhysicalMemory() (int64, bool) { return f.physical, f.physicalOK }

func TestApply(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		sys     fakeSystem
		wantSet int64
		source  string
	}{
		{"unset env, cgroup", "", fakeSystem{cgroup: 7 * gib, cgroupOK: true, physical: 16 * gib, physicalOK: true}, 7 * gib / 10 * 7, SourceCgroup},
		{"GOMEMLIMIT=off", "off", fakeSystem{cgroup: 7 * gib, cgroupOK: true}, 0, SourceEnv},
		{"GOMEMLIMIT=5GiB", "5GiB", fakeSystem{physical: 16 * gib, physicalOK: true}, 0, SourceEnv},
		{"nothing readable", "", fakeSystem{}, 0, SourceNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var set int64
			var log bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))
			getenv := func(k string) string {
				if k == "GOMEMLIMIT" {
					return c.env
				}
				return ""
			}
			d := apply(logger, c.sys, getenv, func(n int64) int64 { set = n; return 0 })
			if set != c.wantSet || d.Source != c.source {
				t.Fatalf("set %d source %q, want %d %q", set, d.Source, c.wantSet, c.source)
			}
			if !strings.Contains(log.String(), "level=DEBUG") || !strings.Contains(log.String(), "source=") {
				t.Fatalf("debug log missing: %q", log.String())
			}
		})
	}
	// At the default Info level nothing is printed.
	var quiet bytes.Buffer
	apply(slog.New(slog.NewTextHandler(&quiet, nil)), fakeSystem{physical: gib, physicalOK: true}, func(string) string { return "" }, func(int64) int64 { return 0 })
	if quiet.Len() != 0 {
		t.Fatalf("normal output not empty: %q", quiet.String())
	}
}

// TestDefaultSystem only checks the platform reader does not panic and
// returns sane values where it reads anything.
func TestDefaultSystem(t *testing.T) {
	sys := Default()
	c, cok := sys.CgroupLimit()
	p, pok := sys.PhysicalMemory()
	t.Logf("cgroup %d %v, physical %d %v: %+v", c, cok, p, pok, Decide(false, c, cok, p, pok))
	if n, ok := sys.CgroupLimit(); ok && !valid(n) {
		t.Fatalf("cgroup limit %d reported valid", n)
	}
	if n, ok := sys.PhysicalMemory(); ok && !valid(n) {
		t.Fatalf("physical memory %d reported valid", n)
	}
}
