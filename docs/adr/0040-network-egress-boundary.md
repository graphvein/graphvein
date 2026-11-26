# ADR 0040 - Network egress boundary

**Status:** Accepted (delegated review, 2026-11-03)

## Context

ARCHITECTURE.md ("Network egress") and AGENTS.md say that only
`internal/egress` may open network connections and that an offline run opens
no connections except explicitly configured infrastructure. Until T016b this
was a convention: `egress.ConfigurePostgres` installed a plain `net.Dialer`
and the default resolver on the pgx configuration, and nothing checked that
other code stayed away from `net`. The next M2 tasks add the first code that
listens (the HTTP server on 127.0.0.1, T016c) and dials beyond the database
(the "try it" proxy, live schema introspection), and M3 adds LLM providers.
The boundary has to be a tested property before they arrive.

## Options considered

- **Status quo: ConfigurePostgres plus code review.** No new code, but the
  rule depends on reviewers noticing every `net.Dial`, `http.Get`,
  `sql.Open("pgx", ...)` or default-transport use, in first-party code and in
  dependencies. Rejected: the point of the tool is not to rely on that.
- **OS-level enforcement (network namespaces, firewall rules, seccomp).**
  Catches everything, including subprocesses and dependencies, but needs root
  or is Linux-only, and cannot run in the Windows and macOS CI jobs or on the
  developer's Windows machine. Rejected for now; it remains possible as an
  extra CI job later.
- **An allow-list dialer in one package, plus static and runtime tests.**
  Every lookup, dial and listen goes through `egress.Policy`; a static scan of
  first-party code forbids the other paths; a reviewed list bounds which
  dependencies could dial on their own; a runtime test proves an offline run
  makes no attempt. Selected.
- **Scan implementation: go/packages with type information, or go/parser
  selectors.** Type information would see method values and shadowing
  correctly, but needs `golang.org/x/tools` (a new dependency) and loads one
  build configuration at a time. go/parser needs no dependency and parses
  every file regardless of build tags and GOOS suffixes, so platform-specific
  files cannot hide a dial. Selected: go/parser; a local identifier that
  shadows an import name may cause a false positive, fixed by renaming.

## Decision

`internal/egress` provides:

- `Mode` (`offline`, the default, and `llm`) and `ParseMode`;
- `Destination{Host, Port, Purpose}` with purposes `database` (allowed in
  every mode: explicitly configured infrastructure) and `llm` (allowed only
  in `llm` mode); later tasks add try-it and introspection purposes;
- `Policy` built by `NewPolicy(mode, destinations...)`, with
  - `LookupHost`: IP literals are returned without a resolver call and
    without an event (not a network lookup; the following dial is gated and
    reported); a name is resolved only if it equals (case-insensitively,
    trailing dot ignored) an allowed destination host, using the package's
    own `net.Resolver`; the answers become dialable for that destination's
    port only; resolver errors are returned unwrapped so the Postgres error
    text is unchanged;
  - `DialContext`: `unix` only to the exact socket file
    `<dir>/.s.PGSQL.<port>` of an allowed socket directory (what pgx dials,
    also for cancel requests); `tcp*` to
    an IP literal only if it is a configured literal or a remembered answer
    for that port (IPv4-mapped IPv6 compared unmapped, IPv6 zones compared
    exactly); `tcp*` to a name only
    if the name and port are a destination, then through `LookupHost` and
    each address in order with a 10 s dial timeout; every other network is
    blocked;
  - `HTTPClient`: an `http.Client` whose transport dials through the policy,
    ignores proxy environment variables (a proxy would be an unlisted
    destination) and keeps HTTP/2 available; redirects to unlisted hosts fail
    closed;
- `ListenLoopback`: TCP listeners only on loopback IP literals; `localhost`
  and empty hosts are rejected because a name could bind elsewhere;
- `OpenPostgres(url, runtimeParams)`: the only use of the pgx driver. The
  destinations are the host and port of the parsed configuration and of every
  fallback (multiple hosts, PG* variables, `sslmode=prefer`), never the raw
  string. `DialFunc` and `LookupFunc` are set last. Parse errors keep the
  existing message `invalid Postgres configuration` and never echo the URL.
  It replaces `ConfigurePostgres`;
- `ErrBlocked`, wrapped by every refusal, and `Observe`, a process-wide
  observer of every lookup, dial and listen for tests and future audits.

Configuration gains the optional root key `egress` with the single key
`mode` (`offline` or `llm`, default `offline`), strictly decoded like the
other sections. No command consumes it yet; `llm` adds no destinations until
providers exist.

Three tests enforce the boundary:

- R1 (`internal/egress/boundary_test.go`): every first-party `.go` file
  outside `internal/egress` is parsed; dial/listen/resolve identifiers of
  `net`, dialing `crypto/tls` functions, default clients/transports and
  `ListenAndServe*` of `net/http`, networking packages without a safe use
  (`httputil`, `httptest`, `textproto`, `rpc/...`, `smtp`, `log/syslog`,
  `x/net`, `plugin`), `syscall`/`x/sys`, `os/exec` and `os.StartProcess` /
  `syscall.ForkExec|StartProcess|Exec` outside reviewed allow-lists, pgx
  imports, `sql.Open` with any driver but the literal `"sqlite"`,
  `sql.OpenDB`, dot imports of network packages and `import "C"` outside the
  ADR 0002 package are reported with file and line. Test files are exempt
  except for pgx, `sql.Open*`, dot imports and cgo.
- R2 (`internal/egress/deps_test.go`): `go list -deps` of `cmd/graphvein` for
  linux, darwin and windows; the non-standard packages importing `net`,
  `net/http`, `crypto/tls`, `x/net` or `os/exec` must equal a reviewed list
  with a reason per entry. The test runs with `GOPROXY=off` and never
  downloads; CI runs `go mod download` first, and a missing module fails in
  CI and skips locally.
- R3 (`cmd/graphvein/egress_test.go`): an offline `index`, `status`, `query`,
  `index` run produces no egress event, no default-resolver DNS traffic and
  no default-HTTP-transport request. The backend test (R4) shows that
  Postgres connections do go through egress.

## Consequences

- Every later network consumer (server, try-it proxy, introspection, LLM
  providers) must use `Policy.DialContext`, `Policy.HTTPClient` or
  `ListenLoopback`; R1 fails otherwise.
- A new dependency that imports networking or subprocess packages fails R2
  until it is reviewed.
- `cmd/graphvein` links `net/http` through `HTTPClient` before any caller
  exists, so the binary grows.
- Adding a subprocess or syscall use needs an allow-list entry with a reason.

## Known gaps

- Subprocesses (the user's external indexer, ADR 0010; `git rev-parse`) are
  outside in-process control; R1 only forces each new one to be reviewed.
- On macOS and Windows the OS resolver may serve lookups; egress gates the
  request to resolve by name, not the resolver's packets.
- Platform certificate verification on Windows and macOS may fetch AIA, CRL
  or OCSP data itself (reachable only with Postgres `sslmode=verify-*`
  without `sslrootcert`).
- A dependency that builds its own `net.Dialer` to an IP literal is invisible
  at runtime; R2 is the only control.
- R2 watches imports of networking packages only. A dependency that opens
  sockets through raw system calls is not seen: modernc.org/libc implements
  `connect` and `getaddrinfo` through syscalls (listed only for `os/exec`;
  unreachable from the SQLite driver, but not proven by the test).
- The R3 resolver tripwire sees only Go-resolver DNS traffic outside egress.
  Names answered from the hosts file (for example `localhost`) and raw dials
  to IP literals never reach it; first-party code is covered by R1 and
  dependencies by R2 only.
- `DialContext` gives each address of a resolved name a full 10 s dial
  timeout instead of splitting one deadline as `net.Dialer` does. Only the
  HTTP path dials names (pgx dials looked-up literals) and it has no caller
  yet.
- The name-based scan cannot see method values, interface calls or
  reflection that reach a dial; it sees only the selectors written in source.
- `llm` mode has no destinations until providers are configured (M3).
- Serving beyond loopback is deferred to the server task, which needs its own
  configuration key and an amendment of this ADR.

## Review notes (delegated review, 2026-11-03)

- Delegated review in place of developer review. Results:
  - The investigation: outbound inventory found Postgres as the only first-party
    connection path (pgx dials only through `DialFunc`/`LookupFunc`,
    including fallbacks, multi-host and cancel requests); no stop.
  - Review 1 (independent): no blocker; 2 major and 6 minor findings, fixed
    in Revision 1: Postgres DNS error text restored byte for byte (pinned by
    a test through pgx); R2 always runs with `GOPROXY=off` and CI gets a
    `go mod download` step; R1 extended (net/textproto, net/rpc/...,
    log/syslog, net/http/httptest in non-test code, os.StartProcess,
    syscall.ForkExec/StartProcess/Exec, `import "C"` outside the ADR 0002
    package); unix-socket destinations allow exactly `<dir>/.s.PGSQL.<port>`;
    ListenLoopback names 127.0.0.1 / [::1] in its error; observers called
    outside the lock; zones matched exactly. Raw-syscall sockets in
    dependencies, hosts-file answers and literal dials in R3, and the
    per-address dial timeout are known gaps above.
  - Behaviour: index, status, query, callers, ingest and `--emit` output
    unchanged on the go fixture; binary +1.2% (net/http linked).
  - CI green on Linux and Windows, postgres job with only the four helper
    skips and `TestBackendEgressRouting/postgres` passing.
