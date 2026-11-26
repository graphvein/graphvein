# ADR 0065 - Model kit: per-dialect facts, type families and tool-side types

**Status:** Accepted (delegated review, 2026-11-15)

## Context

ADR 0058 gave every model column one value per fact, one claim per known
fact and one `verification` per claim, under a group that states one
dialect or none. The developer's decisions of 2026-11-15 (Revision 1 of
T018g4 Diesel, T018g5 GORM and T018g6 TypeORM) need three things that shape
cannot hold:

- GORM (gotify: mysql, postgres, sqlite) and TypeORM (seerr: postgres,
  sqlite) choose the dialect at run time. The SQL type, the default text
  and the primary-key/autoincrement rendering differ per dialect; one value
  would be wrong on the other dialects. Each dialect's fact needs its own
  claim and `verification` (`oracle` for the dialects the oracle compared,
  `mapped` for MySQL renderings).
- Diesel's `schema.rs` drops type modifiers (`Numeric`, `Timestamptz`,
  ...). The bare type (`numeric`) would claim an unmodified column; the
  type must be a family whose modifiers are unknown.
- Diesel's own SQL type (`Nullable<Int4>`) is worth showing but is not a
  database fact (O5).

The record shape reaches the Schema API, which serves stored attributes
verbatim; contract v1 `attributes` are open maps (`docs/contract/v1.md`),
so the indexer contract is not changed.

## Options considered

1. **Where per-dialect values live.**
   - A. One column attribute `by_dialect: {<dialect>: {<fact>: value}}`,
     the flat fact null, claims in the one `claims` list with a `dialect`
     key (chosen: every reader of a flat fact, the web view included, sees
     unknown instead of one dialect's value; `/sets`' claim stripping, the
     stored `verification_counts` and `/table`'s claim links keep working
     unchanged).
   - B. One column record per dialect (rejected: column identities and
     `contains` edges would multiply; dialect-independent facts would be
     repeated and could disagree).
   - C. Suffixed attributes (`type@mysql`) (rejected: open-ended keys every
     reader must parse; no place for a missing dialect).
   - D. Per-dialect groups (`models/gorm/.@mysql`) (rejected: one source
     table would appear three times; foreign keys and overlaps triple).
2. **Type family.**
   - A. A separate fact `type_family: {name, unknown: [modifiers]}` with
     `type` and the listed modifiers null (chosen: `type` keeps its meaning
     "the full type"; a reader that knows only `type` sees unknown; the
     modifiers' null is explained by the family).
   - B. `type` holds the family name plus a flag (rejected: every existing
     reader would show the bare family as the type).
   - C. `type` becomes an object (rejected: changes the JSON type of an
     existing attribute for every front end).
3. **Autoincrement rendering.**
   - A. A new claim fact `autoincrement` (boolean: the database generates
     the value), per dialect where it differs; the rendering itself
     (`bigserial`, `AUTO_INCREMENT`) is the type or the dialect's keyword
     (chosen: one meaning on every dialect).
   - B. A free text of the rendering (rejected: not comparable across
     dialects, no claim vocabulary).
4. **Tool-side type.** A `tool_type` string outside `ClaimFacts` (chosen:
   never claimed, so never verified; the view labels it as the tool's) vs
   a claimed fact (rejected: it is not a database fact, O5).
5. **Compatibility.** Every new attribute is written only when a front
   end sets it and the claim's `dialect` is `omitempty` (chosen: existing
   records and goldens stay byte-identical, no extractor version bump) vs
   always-present nulls (rejected: every model golden and the stored
   records of every index would change for no fact).

## Decision

Options 1A, 2A, 3A, 4 (`tool_type`) and 5 (omit when unset).

- `Group.Dialects`: the run-time dialects; tables write `dialects`
  (sorted) and no single `dialect`. A group stating both is a front-end
  bug: `dialect` is not written, with a diagnostic.
- `Column.ByDialect` / `Column.DialectClaims`: per-dialect values and
  claims for `DialectFacts` (`type`, `type_family`, `length`, `timezone`,
  `precision`, `scale`, `autoincrement`, `server_default`;
  `server_default_text` kept beside `server_default`). The core writes
  `by_dialect` with one entry per group dialect listing the same facts
  (null where unknown), writes each covered flat fact null, keeps a type
  and its family (and a server default and its text) together, and emits
  one claim per dialect whose value is known, at the fact's place in
  `ClaimFacts` order, in dialect order.
- `type_family` and `autoincrement` join `ClaimFacts`. A family is valid
  only as `{name, unknown}` with a non-empty name and distinct modifiers
  among `length`, `precision`, `scale`; the core writes `type` and the
  listed modifiers null.
- In a group with run-time dialects, every dialect-dependent fact is
  stated per dialect (T018g0b Revision 1, A3): a flat value of a
  `DialectFacts` fact (`type`, `type_family`, the modifiers,
  `autoincrement`, `server_default` and its text) is written unknown with
  `schema_model_internal_error`, even when no dialect states that fact. A
  value every dialect agrees on is stated on each dialect (one claim each,
  with its own verification); the view joins agreeing values into one "…
  on a, b, c" line. A modifier a per-dialect family lists unknown counts as
  stated per dialect (A4), so a flat value beside it is written unknown
  with a diagnostic in any group.
- Misuse (a dialect outside the group's, a fact outside `DialectFacts`,
  per-dialect facts without group dialects, a type beside a family, a
  listed modifier with a value, a family of another shape, a stated flat
  value beside per-dialect values, a non-string `tool_type`) writes the
  facts concerned unknown with `schema_model_internal_error`, never a
  guess; a per-dialect claim without a known verification is refused like
  any claim (ADR 0058 Revision 2).
- Helpers: `SetDialectFact`, `SetDialectTypeFamily`, `SetTypeFamily`,
  `SetToolType`, `TypeFamily`. `modeltest.CheckClaims` checks claims per
  (fact, dialect) and runs `CheckDialectFacts` and `CheckTypeFamilies`.
- View (Revision 1): a per-dialect line is marked verified only when every
  known value on it (each dialect of the line, each fact with a non-null
  value in that dialect's entry) has an `oracle` claim for that fact and
  dialect; a mapped claim, a missing claim or an unrecorded verification
  makes it "unverified". A per-dialect default follows the server's model
  default rule per dialect: `server_default` false with a flat ORM-side
  default (Python or client) shows that default, false with `default`
  "absent" shows "none" only on a complete table, anything else unknown.
- API: `/sets` model groups add `dialects` (omitted unless every table
  states the same list; differing or missing lists add `dialects` to
  `conflicts`); `/table` adds `dialects` beside `dialect`; both omitted
  otherwise. Claims pass through with `dialect`; links and `editor=` as
  before. `api_version` stays 0.

## Consequences

- GORM and TypeORM can claim types and default texts per dialect with
  their own verification; Diesel can claim `numeric (precision unknown)`
  and show its own type. They rebase over this change.
- No current record changes: SQLModel, Prisma, Drizzle and Sequelize
  goldens are byte-identical, so no extractor version bump here. The first
  front end that writes the new fields bumps the aggregate version with its
  own change.
- A per-dialect server default makes the column's `default_state`
  `unknown` (the flat `server_default` is null); the view reads
  `by_dialect` for the per-dialect answer.
- `verification_counts` counts each per-dialect claim, so one mapped MySQL
  claim marks the table unverified, as intended (verification per dialect).
- The web view must read `by_dialect`, `type_family` and
  `tool_type`; until it does, such columns show their type as unknown,
  which is under-claiming, not wrong.

- (Revision 1) A front end of a run-time-dialect group must repeat a
  dialect-independent-looking value (`text` everywhere) once per dialect;
  that costs one claim per dialect but never shows one dialect's
  verification for the others.

## Known gaps

- Table-level facts (keys, constraint names, index lists) stay single;
  a dialect-dependent constraint name is still unknown, not per dialect.
- Nullability and foreign keys cannot be per dialect.
- `autoincrement` says whether, not how; the rendering keyword is implied
  by the dialect and type.
- `/sets` serves `by_dialect` values (small) with every column; the claims
  stay on `/table`.

## Review notes (delegated review, 2026-11-15)

- **Steps:** K1 (kit records, per-dialect claims, type families, tool type,
  API), K2 (Schema view), KF (review fixes, Revision 1), KM (column chip
  and joined-line wording), plus a test fix for a pre-existing
  `TestServeCommand` race (the job records its end just before the run
  releases the repository lock; the test now waits for both).
- **Independent review (KR):** four class-A findings, all fixed: a joined
  per-dialect line shown oracle-verified when one dialect's claim was
  missing (A1); a per-dialect default shown "none" while a flat ORM-side or
  unknown default existed (A2); flat dialect facts accepted and claimed in
  a runtime-dialect group (A3: now refused, written unknown with
  `schema_model_internal_error`); a per-dialect type family next to a
  claimed flat modifier (A4). Compatibility clean: no existing golden
  changed by the kit, every new field omitempty, schema endpoints keep
  `editor=`, contract_version stays 1 (open attribute maps). Wording:
  "Type / annotation" header, per-dialect "unverified" tag naming the
  unverified dialects, "Verified on <dialects>" tooltip.
- **Results:** full check exit 0 (157-158 `/postgres` PASS), check-web ok;
  CI run 43725489535 (`ci/s30-t018g0b-2`): Windows and postgres green,
  postgres raw log 0 FAIL, only env-gated skips. Screenshots read at 1280
  (light); no page overflow at 900-1920.
- **Coverage:** no front end emits the new fields yet; Diesel (type
  families, tool type), GORM and TypeORM (per-dialect facts) adopt them in
  their own tasks. The extractor version is unchanged.
- **Known gaps:** per-dialect table-level facts (constraint names) and
  per-dialect nullability are not modelled; a runtime-dialect front end
  must state every dialect fact per dialect, or the fact reads unknown.
