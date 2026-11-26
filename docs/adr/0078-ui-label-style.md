# ADR 0078 - UI label style: Plain / Technical, first-run banner and ui.labels

**Status:** Accepted (delegated review, 2026-11-18)

## Context

T025 (the T025 task record (internal)) made plain wording the web UI's
default: rule and row ids (I4, C3), assumption ids (V1), tiers with
confidence numbers (`inferred 0.9 exact`) and raw kinds (`client_call`)
moved into tooltips, `data-*` attributes and accessible names. Newcomers
read the UI faster; people who already know the ids and numbers read them
slower, and every lookup became a hover. The developer asked for a
technical option, and for a first-run choice that experienced developers
will not miss (2026-11-18, T025b).

## Options considered and decisions

**Where the choice lives.**

- A. Per browser, stored like the editor choice (ADR 0050). Chosen: it is
  a reading preference of one person, like the editor.
- B. Server-side per user. Rejected: the server has no users.

**How it is offered on first launch.**

- A. A small dismissible notice next to the sidebar setting. First built,
  then rejected in review: easy to miss for the people who want it most.
- B. A modal dialog. Rejected: it blocks the page on first launch, and
  the UI has no other modal on load.
- C. A one-time, non-modal banner across the top of the content. Chosen:
  "How should Graphvein label facts?" with two equal buttons, each with a
  one-line example ("Plain", e.g. Exact path match; "Technical", e.g.
  inferred 0.9 exact, I4), and × that keeps the current style (plain).

**A repository default.**

- A. None. Rejected: power users would answer the banner in every
  browser and on every server address (localStorage is per origin).
- B. `graphvein.yaml` `ui: {labels: plain|technical}`, served by a new
  `GET /api/v0/ui` (`{api_version, labels, error}`). Chosen. A new
  endpoint rather than a member of `GET /api/v0/ide`, whose body is the
  editor preset list.

**How the two styles are kept in step.** One table,
`web/src/lib/labels.ts`, keyed by the rows of the T025 table (plus the
T025b additions, rows 20-21); components read these strings only through
it, and a test checks every row has both variants.

## Decision

- Precedence: (1) the browser's stored choice (`graphvein.labels.v1`,
  `plain` | `technical`; other values ignored); (2) `graphvein.yaml`
  `ui.labels`, read from `GET /api/v0/ui` on every request (no index
  read, no consistency check); (3) plain.
- The setting is "Labels: Plain / Technical" in the sidebar footer under
  "Editor for links"; its tooltip names the source of the applied style.
- The banner shows while the browser has no choice, the banner was not
  closed (`graphvein.labels-banner.v1` = `dismissed`) and the repository
  sets no `ui.labels`, and only after `GET /api/v0/ui` answered (a failed
  request counts as no default). Choosing hides it for good; × records a
  dismissal only, so a later `ui.labels` still applies.
- Tooltips stay in both styles: plain keeps the technical detail,
  technical the plain words. A route's full path and an edge form without
  a checked word read the same in both.
- Every storage access is wrapped; blocked storage reads as no choice and
  a choice then lasts for the page ("Not saved").

## Consequences

- One more config section (`ui`, strict like the others: an invalid value
  fails the whole file, for `index` too) and one more read-only endpoint
  with the usual security headers and Host and origin checks.
- New UI wording that reworded an id or number goes through the table, or
  the two styles drift; the table test catches a T025 row without both
  variants, not a new string that bypasses the table.
- `docs/cli/serve.md` ("Labels", `GET /api/v0/ui`) and `docs/config.md`
  ("UI") describe the behaviour.

## Known gaps

- The icon rail (collapsed sidebar: graph views and windows below 600 px)
  has no Labels control; the expanded sidebar and the banner reach it.
- With browser storage blocked the banner shows on every load (nothing
  can record the dismissal).
- `ui.labels` is per repository, not per person: a team that commits it
  chooses for everyone who has made no browser choice.

## Review notes (2026-11-18)

- Results: gate CI run 44326776885 on a0ed4ae (Linux only; web and server change, not Windows-sensitive): postgres raw log 0 FAIL, 176 `/postgres` PASS, skips only the env/corpus-gated opt-ins. Local full check 0 FAIL, 175 `/postgres` PASS; check-web ok (72 files, 1169+ tests).
- Revisions: the first-run notice became a one-time non-modal banner with both choices and examples (developer concern that experienced users could miss a small notice); `ui.labels` added; Revision 1 added rows 20-21 (Unknowns line) and this ADR.
- Screenshot checked: banner at 1440 px correct; no horizontal overflow at 900 px.
- Known gaps as listed above (icon rail has no control; blocked storage shows the banner on every load; `ui.labels` is per repository, not per person). After v1: none.

## Revision (T042): depth defaults with the same precedence

Status of this revision: Accepted (delegated review, 2026-11-25; the
decisions above keep their status).

- Context: the Impact tab always started at depth 3 and the architecture
  chain at 6, and the maximum rose to 10 (ADR 0056 and ADR 0079, revisions
  T042). People who always want a deeper walk had to pick it again on every
  item.
- Options: (a) a per-browser memory only; (b) a repository default only;
  (c) both, in the order this ADR uses for labels. Chosen: (c), for the
  same reasons as labels: a reading preference of one person first, then
  what a team commits, then built-in.
- Decision: precedence (1) the browser's last chosen depth
  (`graphvein.impact-depth.v1`, `graphvein.chain-depth.v1`, an integer 1
  to 10; other values ignored); (2) `graphvein.yaml` `ui.impact_depth` /
  `ui.chain_depth` (integers 1 to 10, strict like `ui.labels`), served by
  `GET /api/v0/ui` as `impact_depth` and `chain_depth`; (3) 3 for impact,
  6 for the chain. A depth in the hash (a shared link) applies to its page
  and is not stored. Choosing a depth stores it and writes it to the hash.
  The `ui` keys are one table in `internal/config` (`UIKeys`), which the
  decoder reads, so the Settings page lists each key from one row.
- While `GET /api/v0/ui` is pending and neither the hash nor the browser
  decides, the walk waits, so it is requested once.
- Consequences: one more pair of `ui` keys and two more fields on
  `GET /api/v0/ui`; every storage access is wrapped (blocked storage keeps
  a choice for the page).
- Known gaps: as with `ui.labels`, the repository default is per
  repository, not per person; the depth memory is per browser origin.

## Revision 1 (T041: plain words plus the number)

**Status:** Accepted (delegated review, 2026-11-25)

### Context

Reader feedback on the launch build (developer decision 2026-11-19): most
readers took "exact 0.9" and "fact 1.0" as a sign of honesty, and only one
found the UI dense, mostly because of ids and raw kinds. The plain style
had moved the confidence into tooltips with the ids. Separately, the
Changes section printed confidences with two fixed decimals (0.90 for the
0.9 the API section shows; a stored 0.875 would read 0.88), so the same
value read differently in two places.

### Options considered

- A. Keep the number in the tooltip (status quo). Rejected: it hides the
  part readers valued, and the two number formats stay.
- B. Plain style shows the rule name and the number, ids and raw kinds
  stay in tooltips. Chosen.
- C. Make technical the default. Rejected: ids and raw kinds are what the
  one dense-UI reader objected to.

### Decision

- Plain labels that carry a stored confidence show it after a middle dot,
  and nothing else: "Exact path match · 0.9", "Path suffix match · 0.6",
  "Fact · 1.0", provenance tags "Inferred · 0.9" (label table rows 1-3,
  5-8, 10 and 22). The visible text stays short (about 24 characters;
  the longest rule chip is the Flow legend chip "Exact path match · 0.9",
  22; its second rank, 0.7, is in its tooltip).
  To keep it short the project link (row 22) shows "Inferred · 0.7"; the
  repository badge beside it and its tooltip say it crosses repositories.
  The rule's meaning, the stored fields, the kind and assumptions stay in
  the tooltip.
- One number format: the stored value in its shortest exact form, 1 as
  "1.0" and every other value as stored (0.85 stays 0.85, 0.875 is never
  rounded). One function (`confidenceNumber`, `web/src/lib/labels.ts`)
  prints every confidence the web UI shows; a source test fails when a
  component formats one another way.
- What the number is: the Flow legend strip carries "a number is a fixed
  rank per match rule, not a probability", and the tooltip of each plain
  label with a number carries the sentence "The number is a fixed rank per
  match rule, not a probability." (`RANK_NOTE`), the Flow legend chips
  included (after the tier's explanation, before the line count). One
  exception: the Explorer and schema provenance chip (row 59) carries the
  per-kind sentence instead (`SHAPE_NOTE`: "The number is fixed per kind
  (1.0 for anything read from code; a mapped item takes its link's rank),
  not a probability."), because its number is fixed per kind (claim row
  SM-11), not per match rule. The OpenAPI "Documented by" chip has its own
  tooltip text, which also ends in `RANK_NOTE`.
- The technical style is unchanged apart from the number format (the
  Changes provenance tag reads "inferred 0.9", no longer "inferred 0.90").
- One chip: every visible provenance or confidence label, in both styles,
  renders through the one shared chip (`web/src/components/precision/ProvChip.tsx`,
  the Changes tag's look: a fact on a solid green tint, an inferred link
  in a dashed amber border, anything else neutral), and a source test
  refuses a component that builds such a label itself; only labels drawn
  on the Flow and architecture canvases, the Flow legend's filter chips
  and the Labels setting's examples keep their own form.

### Consequences

- Plain Flow edge labels are longer, so the gaps between columns grow
  (about 80 px on the route fixture) and cards narrow a step earlier on
  a tight canvas.
- MCP is unchanged: its JSON keeps numbers, and it prints no confidence as
  text in a format of its own.

### Known gaps

- Closed by the consistency follow-up below: rows 35 and 43 and the
  server's whole-number tier text.

### Consistency follow-up (developer rule: one "rule · number" pattern everywhere)

- Every plain chip or label that names a provenance or a rule with a
  stored confidence reads "rule · number": the architecture link tier
  (row 35, "Fact · 1.0", "Inferred · 0.9") and chain edge (row 43, after
  the server's wording), the Flow table chip (row 54, "Reads · 1.0, may
  write · 0.9") and table line (row 55, "may write · 0.9"), the Explorer
  and schema provenance chip (row 59, "Read in handler · 1.0"; its tooltip
  says the number is fixed per kind, claim row SM-11), an inferred edge
  with no match field (row 4, "Inferred · 0.9", the OpenAPI "Documented
  by" chip), the endpoint header's fact badge and the route side of a
  request difference ("Fact · 1.0": the API sends no number there, and
  every fact is 1.0 by the indexer contract: contract v1 defines
  confidence as exactly 1 for a fact, and claim row SM-11 gives 1 to every
  kind read from code). The same rule is the source of the client-built
  fact provenances in the schema and Explorer views. These "Fact · 1.0"
  chips are derived from that rule, not read from the response; if the
  contract ever allows a fact below 1, they must read the stored value.
- The Flow legend chip shows the first rank only ("Exact path match ·
  0.9", 22 characters); its tooltip names the second (0.7 when the call's
  origin is assumed, not proven).
- Where the API sends no number (a move hint, a possible table use, a path
  inferred only by its rule), the word stands alone; all three are
  inferred, so no fact chip lacks its number.
- The server writes a link tier's confidence in the same format, and so
  does every human-readable CLI output (one helper,
  `contract.ConfidenceText`): "inferred 1.0", "confidence=1.0", never
  "1". JSON numbers are unchanged.
- Remaining gap: the Flow filter chip names (Fact, Exact, Suffix in the
  chips' accessible names) are filter categories with line counts, not
  chips about one edge, and keep their words.

## Review notes (delegated review, 2026-11-25, T041)

Revision 1 accepted after the final CI gate of 2026-11-25 (CI runs 45627357238, 45632313364 and 45638139948 (the last green on Linux, Postgres and Windows; postgres log 0 FAIL, 291 /postgres PASS)): plain label = rule name plus the number in one format (contract.ConfidenceText / confidenceNumber, enforced by test), details and the rank sentence in tooltips, one shared provenance chip for every provenance label (enforcing source test; Flow and map canvas line labels and legend toggles remain the recorded gap). One independent review, fixed.
