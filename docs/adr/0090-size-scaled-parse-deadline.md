# ADR 0090 - Size-scaled parse deadline

**Status:** Accepted (delegated review, 2026-11-25)

## Context

Every file is parsed and its tree detached under a per-file deadline
(ADR 0006; `--timeout`, 5 s by default, also used by runs the server
starts). A file past it gets the `parse_failed` diagnostic ("context
deadline exceeded") and no facts; other files go on.

owncast commits its Next.js export (ADR 0085, row GB-1). Every fresh
owncast run with four workers failed the same four minified chunks of
`static/web/_next/static/chunks/` on the deadline: 722,537, 493,151,
423,179 and 342,626 bytes. They are not pathological. Parsing and
detaching is linear in the bytes (tree-sitter's parse in WASM, then one
call into the WASM runtime per node property while the tree is copied
out): measured on a 4-CPU machine with the deadline lifted,

| File (bytes) | Alone | Four parsed at once |
|---|---|---|
| 722,537 | 5.0-5.5 s (6.9-7.6 µs/byte) | 8.1-8.2 s (11.3 µs/byte) |
| 493,151 | 3.7-3.9 s (7.5-7.9 µs/byte) | 7.0-7.1 s (14.3-14.5 µs/byte) |
| 423,179 | 2.8-2.9 s (6.6-6.8 µs/byte) | 5.9-6.0 s (14.0-14.2 µs/byte) |
| 342,626 | 2.8-3.0 s (8.2-8.7 µs/byte) | 5.9-6.0 s (17.3-17.5 µs/byte) |

So with four workers busy every one of them crosses 5 s, and the largest
crosses it even alone. The outcome is lost coverage (the files' symbols,
`calls` edges and generated-output client calls) and a result that
depends on machine load below the old 5 s. Walking the tree with a cursor
instead of indexed children gave identical output but was 2.2 times
slower overall, so the cost is not a traversal mistake.

## Options considered

1. **Skip committed bundles** (do not parse GB-1/2/3 minified files).
   Loses facts ADR 0085 keeps on purpose (file node, symbols, `calls`
   edges, the `generated` client calls), and only for the layouts listed;
   a large hand-written or vendored file would still time out. Rejected
   (coverage first).
2. **A higher fixed deadline for every file** (for example 30 s). Gives a
   pathological small file (a grammar's worst case, a deeply nested
   expression) the same 30 s before it is given up, six times today's
   bound on a run's stall per such file, without any evidence that small
   files need it. Rejected.
3. **A deadline that grows with the file's size, with a cap** (chosen).
   Linear work gets linear time; small files keep today's deadline.

## Decision

The per-file deadline is

    deadline(size) = base × clamp(size / 128 KiB, 1, 12)

where `base` is `--timeout` (5 s by default, `parse.Options.Timeout`).
`parse.FileTimeout` computes it and the parser applies it to every file;
`--timeout` keeps its meaning as the base (a zero or negative base still
disables the deadline).

- **Rate.** With the default base that is 5 s per 128 KiB, about
  38 µs per byte: 2.2 times the slowest rate measured with four workers
  busy (17.5 µs/byte) and 5 times the idle rate (7-8 µs/byte). owncast's
  chunks get 13.1-27.6 s against 5.9-8.2 s needed under full load.
- **Floor.** Files up to 128 KiB keep the base exactly: at the measured
  rates they need at most about 2.3 s under load, so a small file that
  still hits 5 s is pathological, and it is given up as soon as before.
- **Cap.** 12 bases (60 s by default), reached at 1.5 MiB. The cap bounds
  the worst case: no file holds a worker longer than 12 bases, whatever
  its content, so k pathological files cost a run with W workers at most
  about k/W × 12 bases of wall time (a pathological file of at most
  128 KiB, as before, at most one base). The default `--max-file-size`
  (2 MiB) needs at most 36.7 s at the worst measured rate, under the cap
  with a margin of 1.6; a larger `--max-file-size` can reach the cap.

## Consequences

- owncast's four chunks are indexed (measured in the T044 task record
  (internal)): their facts are added, and their client calls carry ADR
  0085's `generated` status, so no route link comes from them.
- Determinism: a file that still times out keeps today's `parse_failed`
  diagnostic and is retried by the next run (failures are not cached). An
  outcome that depends on machine load remains possible for a file whose
  need under the load at hand exceeds its deadline: with one parse per
  CPU, only above the cap (a file over about 1.5 MiB needing more than
  12 bases at about 17.5 µs/byte, that is over about 3.4 MiB, more than
  the default `--max-file-size`); when other work oversubscribes the CPUs,
  any file needing more than about 38 µs/byte, more than twice the
  slowest rate measured with all workers busy (see Known gaps); and, as
  before, a small file needing more than the base.
- No extractor version bump: cached syntax is unchanged; only which
  files finish parsing changes, and failed parses were never cached.
- A pathological file over 128 KiB is given up later than before (at most
  the cap).

## Known gaps

- The rate is measured on one 4-CPU Linux machine with the JavaScript
  grammar; other grammars and slower machines (CI's Windows runner) were
  not measured separately. The margin is the protection.
- The deadline scales with bytes, not with the tree's node count, which
  is what the detach step pays for; a file with many more nodes per byte
  than minified JavaScript gets proportionally less margin.
- More work than CPUs slows every parse; the deadline knows neither the
  worker count nor other processes. Observed: in one owncast run while
  other processes kept the 4-CPU machine at a load of about 10-12, a
  191,898-byte chunk (deadline 7.3 s) still failed on the deadline, so
  below the cap the outcome stays load-dependent once the machine is
  oversubscribed about threefold; with at most one parse per CPU it was
  not seen.

## Review notes (delegated review, 2026-11-25, T044)

Accepted after gate 1 (CI run 45612588023: changes and postgres green; postgres log 0 FAIL, 254 `/postgres` PASS). Measured on owncast (interleaved fresh `index --emit`, main vs branch, warm-up discarded): main 99.5-103.2 s with 4 parse-deadline failures per run, branch 116.2-125.5 s with none; outputs of all clean rounds identical record for record. Facts gained, only in the four generated `_next` chunks: +4,564 nodes and +7,902 edges; the 4 client calls there carry the ADR 0085 generated-output label and no route link, so no new confident wrong link. The independent review found that the newly parsed bundles exposed anonymous-scope call binding errors; that class was fixed by T046 (ADR 0011 revisions), which merged in the same gate. Known gap as recorded above: below the cap the outcome stays load-dependent when the machine is oversubscribed about threefold.
