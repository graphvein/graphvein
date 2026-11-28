# ADR 0094 - Default soft memory limit

**Status:** Proposed

## Context

Go's garbage collector, at its default `GOGC=100`, lets the heap grow to
about twice the live heap before it collects, and it reads neither the
cgroup memory limit nor the machine's memory by itself. An index run
holds every file's facts until linking (ARCHITECTURE, step 3), so its
live heap grows through parsing.

T079 measured t3code (8,839 files after excluding its vendored
`.repos/`) under a 7 GiB cgroup cap: the live heap grew smoothly to
3.7 GB, the GC's goal reached 6.9 GB, and with the parser memory on top
the process passed 7 GiB and was killed while about half of its memory
was garbage. The same run with `GOMEMLIMIT=5GiB` finished at 5.2 GB
anonymous RSS. Nothing in the run was wrong except the GC's headroom.

## Options considered

1. **No default** (status quo): users who hit the kill must know to set
   `GOMEMLIMIT`. Rejected: the failure is a kill with no message, and the
   remedy is a runtime detail.
2. **A fixed default** (for example 4 GiB): too low on a large machine
   (needless GC work on big repositories) and too high on a small one or
   in a small container (the kill stays). Rejected.
3. **A fraction of the memory actually available** (chosen): the
   process's cgroup limit where there is one, else physical memory, the
   smaller of the two when both exist.

## Decision

`internal/memlimit` runs at the start of every graphvein command
(`cmd/graphvein` `main`), since index, serve (re-index, workspace add),
the web UI with no arguments and `changes` all index, and a command with
a small heap never reaches the limit:

- `GOMEMLIMIT` set and non-empty (any value, including `off`): do
  nothing; the runtime has applied it. An empty value is unset, as the
  runtime treats it.
- Otherwise the bound is the smallest of: the cgroup limit (cgroup v2
  `memory.max`, cgroup v1 `memory.limit_in_bytes`, read for the
  process's cgroup from `/proc/self/cgroup` and `/proc/self/mountinfo`,
  and for each ancestor up to the hierarchy's mount, since a parent's
  limit binds a child too; `max`, values of 2^62 or more and unreadable
  files count as no limit) and physical memory (Linux `/proc/meminfo`
  `MemTotal`; macOS `hw.memsize` through `golang.org/x/sys/unix`;
  Windows `GlobalMemoryStatusEx` through `golang.org/x/sys/windows`; no
  cgo). Other systems read nothing.
- The soft limit is 70% of that bound (`debug.SetMemoryLimit`). If
  nothing can be read, no limit is set.
- The decision (limit, source, bound) is logged with `slog` at debug
  level; normal output is unchanged. The CLI has no `--verbose` flag.

70% follows T079's measurement (a 5 GiB limit, 71% of a 7 GiB cap,
peaked at 5.2 GB anonymous RSS) and leaves room for memory the limit
does not govern or the cgroup also charges: the tree-sitter guest
memory and compiled code outside the Go heap, the per-call goroutines
wazero starts while parsing (T079: up to about 0.5 GB), page cache
charged to a cgroup v1 limit, and other processes on the machine when
the bound is physical memory.

The decision function (`memlimit.Decide`) is pure and table-tested; the
cgroup reader takes a file system rooted at `/`, so tests run on fixture
trees on every platform, and the platform readers sit behind the
`memlimit.System` interface.

## Consequences

- A repository whose live heap is clearly below the limit finishes where
  the default GC was killed. No fact changes: the limit only changes when
  the GC runs.
- GC CPU rises as the heap nears the limit (T079: 17% at the end of the
  t3code run), so such a run is slower than an uncapped one on a machine
  with memory to spare would be. Where memory is ample the limit is never
  reached and costs nothing.
- A live heap above the limit still grows: the runtime caps GC CPU at
  about half and lets the heap exceed a soft limit. A repository too
  large for the machine is still killed by the operating system; a
  run-level memory budget that fails with a message (T079 option 3) is
  the honest failure and is separate work.
- `GOMEMLIMIT` remains the override, and `GOMEMLIMIT=off` restores the
  default GC behaviour.
- Two new files import `golang.org/x/sys` (darwin, windows), listed in
  the network boundary test's syscall allow-list. `golang.org/x/sys` was
  already a dependency.

## Known gaps

- Windows job object memory limits and macOS process limits are not
  read; physical memory is the bound there.
- Memory other processes use is not subtracted: on a busy machine whose
  bound is physical memory, 70% may still be more than is free.
- cgroup v1's `memory.memsw.limit_in_bytes` and v2's `memory.high` are not
  read; `memory.high` throttles rather than kills, so it is not a hard
  bound.
- A cgroup path outside the hierarchy's mounted root (a process moved
  into another cgroup namespace's tree) is not read for that hierarchy.
- The fraction is not configurable other than by setting `GOMEMLIMIT`.
