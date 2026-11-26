# ADR 0051 - Schema claim contract and Postgres oracle

**Status:** Accepted (delegated review, 2026-11-09)

Numbering: the task (T018a2) asked for the next free number; 0048-0050 are
taken and 0052 is taken by T016j's re-index ADR, so this is 0051.

## Context

T018a (ADR 0047) modelled sqlx migration sets with PostgreSQL 17 semantics
and took nine reviews. Three causes (developer discussion of
2026-11-04): the task said "model Postgres DDL" without a closed list of
what the pass claims, so every reviewer found one more form; correctness
was judged by reasoning although a real Postgres 17 was available in CI;
and coverage was never measured, so over-conservative rules crept in
unseen. The convergence rule that ended T018a left known wrong facts and
coverage gaps (ADR 0047, Known gaps): K1 a foreign key to a table the set
creates later (`applied: true` where one migration must fail), K2 `ALTER
TYPE ... ADD VALUE` of an existing label or a missing type, C2 widening a
referenced key, C3 DROP CONSTRAINT of a written name forgetting every key,
C4 `-- no-transaction` not read, C5 generated-name shapes on partial
tables.

T018a2's investigation built a differential oracle and measured today's code: 3
fixture root causes and 96 repros besides K1 and K2. Revision 1 brought
each root into a class rule (P1-P10), and steps B1a-B3 (2026-11-04 to 2026-11-07)
implemented them; three later precision stops became Revisions 1.1-1.3
(volatility, column references of DML, values of DML).

## Options considered

1. **Keep case review.** Reviewers read the code against Postgres sources
   and report wrong facts. This is what T018a did; it does not converge
   because the space of forms is open and each finding is a new case.
2. **Contract only.** A finite table of the forms the pass models, their
   claims and refusal conditions, everything else to a default branch.
   Review becomes row by row, but whether a row is right is still judged
   by reasoning.
3. **Contract and oracle.** The table of option 2, plus a harness that
   applies fixture, real and generated histories to a real Postgres 17 and
   compares the catalog with the records; zero disagreements is the
   precision gate, and coverage is measured on real histories.

## Decision

Option 3.

- **Claim contract.** `internal/extract/schema/contract.go` lists the rows
  (kinds M modelled, R recorded name, N no effect, O opaque, F refused in
  sqlx's transaction, T transaction control, Q set reading, D default)
  and the precondition ids `<row>.<class>` (P1 names, P2 shared
  namespaces, P3 conditional forms, P4 untracked objects and options, P5
  columns, expressions and values, P6 transaction structure, P7 syntax, P8
  dependents, P9 subcommand order, P10 set reading, P11 known rows
  (Revision 1.4), K2 enum labels).
  `docs/extract/schema.md` "Claim contract" holds the same tables with the
  refusal conditions. A fact or success claim needs every precondition
  proven by the records the pass holds; anything that depends on an
  unknown is may-fail (`applied: null`), and every form no row covers is
  `D`, which claims nothing. Every analysis carries its row
  (`analysis.row`); every precondition diagnostic cites its id.
  `TestContractIDs`, `TestContractRows` and `TestContractDocument` keep
  code, Go table and document 1:1 and check that every row is exercised by
  a fixture or regression case and by the generators; `TestContractFacts`
  (Revision 1.4 R6) checks every fact each statement adds or changes
  against the fact kinds its rows may claim, and that every precondition
  is met by at least one case.
- **Revision 1.4 class rules** (after the independent review): R1 every
  DDL expression (CHECK, EXCLUDE, generated columns, index expressions,
  domain checks) is applied only when the closed typer proves it; R2 one
  constraint namespace per table across every kind, with generated names
  computed as Postgres computes them (makeObjectName) where it can; R3 an
  allowlist of column-definition clause combinations, key column lists,
  foreign-key options and table options; R4 type modifiers checked against
  each typmodin, time precisions recorded as stored; R5 rows the set's own
  DML wrote are known rows, so statements that check them may fail
  (class P11), and the harness judges their refusals (H9).
- **Revision 1.5 class rules, step C1** (after the independent re-check,
  2026-11-07): harness rule H12 attributes a refusal to E1 only by a
  closed list of message templates (a relation, `DROP TABLE`'s table,
  schema, role, function or operator outside the set, a column of an
  outside table, a type or operator class of the contrib E1 tables);
  every other refusal with those codes is judged. U1 sends every ALTER
  TABLE subcommand without an M5k word to row D (it may fail, and the
  table's indexes stop being facts) and adds row `M5f-u` for `ADD
  [CONSTRAINT n] PRIMARY KEY | UNIQUE USING INDEX i` alone. U7 proves
  types, collations, default operator classes and settings from tables
  generated from REL_17_6 (`pgtables.go`, `TestGeneratePgTables`: kwlist.h,
  pg_opclass.dat, pg_collation.dat, guc_tables.c, reloptions.c and the
  contrib scripts); U8 checks every unquoted keyword the readers take as
  a name against its gram.y nonterminal (ColId, type_function_name,
  ColLabel); U9 checks storage parameters against reloptions.c; U10 holds
  the six system column names in every table's column namespace. E1 is
  amended: its extensions are contrib's, and indexes, types, operator
  classes, collations and settings are not E1 unless those tables list
  them. Generator g7 (stream 0x7018a27) adds their valid and refused
  variants. Steps C2 (U3-U6, H13) and C3 (U2, P12) follow.
- **Revision 1.5 class rules, step C2** (statement semantics). U3 (class
  P13, rows O3 and Q1) follows the session's search_path through the run
  (one connection for every migration, sqlx 0.8.6): SET [SESSION | LOCAL],
  SET SCHEMA, RESET, RESET ALL and the whole-statement `SELECT
  [pg_catalog.]set_config('search_path', '<constant>', bool)`, scoped to
  sqlx's transaction, a no-transaction string's implicit block, or no
  effect outside a block; any other change, SET ROLE over a path holding
  `"$user"`, and a migration with transaction control make the path
  unknown. Entries are known when they are pg_catalog, E4's value or a
  schema the set creates (`public` is never known: the set's CREATE
  SCHEMA IF NOT EXISTS public may be E4's own schema). `Q1.P13`: sqlx's
  unqualified INSERT INTO _sqlx_migrations (and, for later migrations,
  its UPDATE after the commit) must resolve to the default schema under
  the path in force. `O3.P13`: an unqualified CREATE lands in the first
  existing entry, and the record carries that schema; an unknown
  placement may fail and keeps `schema: null` with a search_path reason.
  References after a change keep Revision 8's R2 rule (uncertain names).
  Harness H13 compares such a record only with the one catalog relation
  of its name. U4 (P4) applies the partition-key rule to every unique
  index on a partitioned table (keys, ADD COLUMN constraints, CREATE
  UNIQUE INDEX, partitions that are themselves partitioned, and a
  sub-partitioned PARTITION OF child, `M1b.P4`). U5 (P4) computes
  sequence.c init_params for CREATE SEQUENCE (`R1.P4`) and ALTER
  SEQUENCE (`N1.P4`) over parameters the pass read (a serial or identity
  column's defaults), with OWNED BY per process_owned_by. U6 (P3,
  `R6.P3`) checks CREATE OR REPLACE FUNCTION/PROCEDURE over a held
  routine of the same identity per ProcedureCreate.
- **Revision 1.5 class rule, step C3** (code effects). U2 (class P12)
  gives every O1 statement a DDL inventory read from the bodies it may
  run (the DO body, the set-defined routines it names, and for data
  statements, EXECUTE and statements that run index expressions or view
  queries also the trigger functions and every routine the set names
  outside its definition; closed over the routines the bodies call).
  PL/pgSQL bodies are split into statements with their block, IF, CASE,
  loop and exception headers removed; LANGUAGE sql bodies are statement
  lists; DDL heads and the one statement of `EXECUTE '<constant>'` are
  read by the statement analyser. Any other EXECUTE, a SET, RESET or
  set_config, transaction control, a statement not read, DROP ...
  CASCADE, another language or a routine created by code makes the
  inventory unbounded: the migration may fail from the statement on
  (`O1.P12`; the code may have changed search_path, so sqlx's bookkeeping
  INSERT may fail). A bounded inventory makes the names it may create
  possibly held (`M1.P12`, `M4.P12`, `M5a.P12`, `M8.P12`, `R1.P12`,
  `R6.P12`, `N1.P12` by kind), the objects it may drop, rename or move
  no longer held, and every table it names partial with dependents the
  pass does not see (`M1b.P12`, `M5a.P12`, `M6.P12`, `M7.P12`,
  `R3.P12`, `R5.P12`). E3 covers only the code finishing; E1 says that a
  function outside the set or in pg_catalog changes no set object.
- **Revision 1.6 class rules, step D1** (after the second independent
  re-check). Harness H14-H16 first: E3 only for a refusal whose
  innermost context frame is code running (a PL/pgSQL execution frame,
  an SQL function statement, inlining or startup, an extension script;
  never the compile or validator frame, never 2D000); E1's relation
  templates only for a relation the refused statement names in a table
  position; E2 only for rows a COPY or the prelude may have put into a
  checked table. They exposed no root outside the revision's classes.
  V7 (class P7) holds every row other than D to a closed list of gram.y
  productions read whole (`wholestmt.go`, with a syntax-only expression
  reader for the a_expr positions it needs, `exprsyntax.go`): COMMENT,
  GRANT/REVOKE, ALTER DEFAULT PRIVILEGES, roles, CREATE TRIGGER /
  POLICY / COLLATION / STATISTICS / PUBLICATION / OPERATOR / CAST /
  AGGREGATE, LOCK, NOTIFY, RESET, DISCARD, transaction control, the
  extension statements, CREATE SCHEMA with its elements, enums, the R4
  and R5 forms, ALTER FUNCTION, DO, CALL, EXECUTE, CLUSTER, REINDEX,
  REFRESH, TABLE, COPY, the M1b forms and the M5k subcommands; anything
  else is row D, and `TestContractRows` checks that every row's sample
  statement with one extra word reaches D. V3 applies U8's keyword
  categories to index and partition elements, operator-class positions,
  EXCLUDE elements and expression column references, and treats a bare
  SQL value function as that STABLE function (refused where an IMMUTABLE
  expression is needed). V4 (P1) makes statements needing an index,
  sequence, view, materialized view, or a trigger, policy, rule or
  constraint of an outside table, may fail unless the pass holds the
  object. Same-rule completions: V2 (only sql and plpgsql are proven
  languages), V9 (a written operator class must accept the column
  type), V10 (SET NAMES as client_encoding), V11 (system column names in
  LIKE / INHERITS / OF / ADD ATTRIBUTE / CREATE TABLE AS / SELECT INTO /
  materialized views), V12 (view and materialized view storage
  parameters) and V13 (serial only in column definitions, pseudo-types in
  no column, attribute or domain base). Generated tables of
  `pgtables16.go` (pseudo-types, encoding names, PL/pgSQL condition
  names and keywords); generator g8 (stream 0x7018a28) with appendable
  `productions8`. Step D2 adds V1 (P14), V5, V6 and V8.
- **Revision 1.6 class rules.** V1 (class P14, `plpgsql.go`,
  `rev16d2.go`): a PL/pgSQL CREATE FUNCTION / PROCEDURE (with
  check_function_bodies on or unknown) and every PL/pgSQL DO block are
  applied only when the pass proves the body compiles, which it does for
  a closed provable subset of pl_gram.y (one block, declarations,
  assignments to variables, parameters and NEW/OLD.col of a trigger
  function, RETURN by make_return_stmt, IF, NULL, RAISE, EXECUTE,
  PERFORM, INSERT / UPDATE / DELETE of the expression reader's subset,
  statements V7's readers read whole in a strict form, exception
  handlers), with Postgres's compile-time checks as refusals and every
  other body may fail (`R6.P14`, `O1.P14`). check_function_bodies is
  followed with U3's scopes; off, the body is compiled at the first call
  that an O1 inventory may reach (`O1.P14`, pg_catalog declared types
  only). The validators' pseudo-type rules, every signature type
  (`M2.P1`) and the function option list (`R6.P4`) apply at CREATE
  whatever the setting. The syntax-only expression reader was tightened
  for V1 (scan.l operator splitting, the nonassociative comparison, IS
  and IN / LIKE / BETWEEN levels, numbers without trailing junk, string
  forms, constant type modifiers). V5 (P11): the inventory lists the
  bodies' data statements and their targets, which may hold rows the set
  wrote; a rule whose action writes makes every table one that may. V6
  (P13): the "$user" entry of E4's path is unknown once a statement may
  create a schema (the current user's name is unknown), until every such
  schema is dropped. V8 (P12): the names Postgres generates with a table
  code creates are possibly held. Harness H14 skips every form of the SPI
  query context (`SQL expression`, `PL/pgSQL assignment`, as `SQL
  statement`), and the harness resets role-level settings after every
  history (E4: a history's `ALTER ROLE CURRENT_USER SET search_path` is
  cluster-wide). Class fixes found while writing the rules (pre-existing
  roots): the lexer refuses the escapes scan.l refuses (P7), and the
  routine signature and option rules above (M2.P1, P4). No row was added
  or removed.
- **Revision 1.7 class rules (2026-11-07, after the third re-check).**
  W1 (P1, `objectkinds.go`): trigger, policy and rule names live in a
  per-table namespace; DROP / COMMENT ON TRIGGER, POLICY or RULE and
  ALTER POLICY on a table the set holds are applied only when the pass
  holds the name there (its per-table record exact; a name code created
  is only possibly held; no rule is ever held, CREATE RULE being row D),
  which also corrects `N1.P1`'s text (D1). W2 (P1, `rev17.go`): a string
  literal bound to a reg* parameter of a set-defined routine is proven
  only as a regclass naming a relation the pass holds under a certain
  name (regclassin runs when the call is analysed). W3 (P14): a LANGUAGE
  sql AS body created with check_function_bodies off or unknown is parsed
  at its first call (`O1.P14`). W4 (P1): databases are not E1 objects and
  the database name is unknown, so ALTER DATABASE, GRANT / REVOKE ON
  DATABASE and ALTER ROLE ... IN DATABASE may fail. W5 (P1): DROP
  STATISTICS, PUBLICATION, COLLATION, OPERATOR, AGGREGATE and CAST of a
  name not held may fail. W6 (P11): the rows a statement checks include
  those of the table's inheritance children and partitions. W7 (R3.K2):
  enum labels are decoded string values; an undecoded form makes the
  labels unknown. Harness: H17 (startup and inlining frames of a SQL
  function possibly created with check_function_bodies off are judged,
  not E3), H18 (roles a history creates are dropped after it), H19 (the
  catalog is read with pg_catalog-qualified names; a history whose facts
  cannot be read fails the test). Decided, not a rule: validator errors
  raised by statements code runs stay E3's (D4). No row or precondition
  id was added; eleven precondition texts are extended.
- **Revision 1.7 gaps** (coverage only): a trigger cloned onto a
  partition from its partitioned parent is not held by the record (its
  DROP and COMMENT may fail); DROP STATISTICS, OPERATOR, AGGREGATE and
  CAST are never proven (the record keeps no signatures or statistics
  objects); reg* arguments other than regclass are never proven, and a
  regclass argument naming an index or a composite type is not proven;
  W6 also counts a child's rows for statements that do not recurse to
  inheritance children (CREATE UNIQUE INDEX, ADD UNIQUE on an
  inheritance parent); H17 judges the startup frames of every routine
  created after any mention of check_function_bodies; H19 still resolves
  operators in the catalog query through the session's search_path.
- **Revision 1.8 class rules (2026-11-07, after the fourth re-check).**
  X1 (P8, `rev18.go`): a DROP without CASCADE of a function, procedure,
  routine, aggregate, type, domain, table, view, materialized view,
  sequence, foreign table, collation or schema is applied only when the
  pass's dependency record proves the object has no dependent: the record
  keeps, for every statement that can create an object with a pg_depend
  dependency, the words it writes (strings included) less its own target
  and owning table, and for a routine or DO body the words of the DDL it
  may run (every word when the body is not read or is dynamic); an entry
  dies when its object is dropped by a statement the pass applied; the
  object's names follow ALTER ... RENAME TO (identity), its array and
  multirange names included. A live entry naming the object makes the
  drop may-fail (`R5.P8`, new; `M6.P8`). After DROP ... CASCADE every
  object such an entry created is gone (no longer held; its later CREATE
  may fail). X2: ALTER COLLATION ... RENAME TO is tracked (the old name is
  no longer held). X3 (V4 kinds): a REFERENCES or CREATE INDEX target held
  as a view, sequence, composite type or index is refused (may fail when
  the kind is uncertain), an index on a materialized view may fail.
  X4: CREATE SCHEMA AUTHORIZATION r without a name creates schema r; with
  CURRENT_USER and the like the created name is unknown. Harness H20: E1
  attribution recognises SELECT INTO targets, CREATE SCHEMA AUTHORIZATION
  and the other creating forms it missed. One precondition id was added
  (`R5.P8`); five precondition texts are extended.
- **Revision 1.8 gaps** (coverage only): the dependency record is a
  superset by words, so a word equal to the object's name in an unrelated
  position (a column named like a type, a CREATE TABLE AS or LIKE source,
  an ALTER SEQUENCE OWNED BY) makes the drop may-fail; an entry of a
  function with overloads of different argument text is never removed by
  a drop; an entry created by code stays live; CASCADE makes every table
  an entry names partial even when Postgres drops only a column or a
  constraint; an index on a materialized view is never proven; after
  CREATE SCHEMA AUTHORIZATION CURRENT_USER every schema the pass does not
  hold may fail; a statement that writes a reg* type and a numeric
  constant (`16384::regclass` names an object by OID) counts as naming
  every object.
- **Revision 1.9 (2026-11-07, after the fifth re-check and the backlog
  inventory; specified, not yet implemented).** *Merge bar (developer
  decision, 2026-11-08):* zero known confident wrong facts. Any probe,
  case, history or seed where the extractor claims something Postgres 17
  contradicts reopens the task, however exotic the form; the earlier
  split (class A reopens, class B "exotic wrong fact" is backlog) is
  abolished. Coverage loss stays acceptable and is recorded. *Decisions:*
  D-public, a new assumption E5 (the database has `public` and
  `information_schema`; `CREATE SCHEMA public` is refused; including
  `information_schema` is the revision's reading, flagged for review);
  D-superuser, no superuser assumption: a new assumption E6 states the
  privileges a non-superuser migrating role is assumed to hold, and every
  superuser-only statement (CREATE EVENT TRIGGER, SUPERUSER roles,
  LEAKPROOF and SUPPORT functions, untrusted languages and extensions,
  PGC_SUSET parameters, base types, operator classes, FOR ALL TABLES
  publications and the rest of a generated table) is may-fail;
  D-dependency, the dependency record of Revision 1.8 loses its word
  exceptions ("less its own target", "less the owning table", entries
  ended by a written-name drop): every word occurrence is kept with its
  position, a DROP without CASCADE is may-fail while any live occurrence
  names the object (owned sequences, copied column types and
  SQL-standard bodies' column use included), exact only where the pass
  proves no dependent (the created-name position, the ON-table position
  for a drop of that table, a REFERENCES target the foreign-key record
  decides), and an entry ends only when the object of its identity is
  removed. *Rules:* Y01-Y52, one per inventory family (B01-B38, R5-A1,
  R5-B1..B7, H1-H5) and the gate root G1 (`g8-s8389`: DROP ... CASCADE
  removes columns typed by the dropped type, and a table whose partition
  key uses one), in three units: U-a statement validators, U-b state
  records, U-c the dependency rule with the harness families.
  *Harness:* H21 (E3 covers code logic and text the set does not fix,
  never names, kinds or syntax of a statement the set wrote, constant
  EXECUTE included), H22 (assumption hits on claimed migrations are
  listed and must be granted by their assumption's text), H23 (the
  replay runs as a non-superuser role per E6; superuser-only refusals
  are judged), H24 (sqlx's post-commit UPDATE is replayed). Seventeen
  precondition ids are added and about thirty texts extended; no row is
  added or removed. Four proposed fixes were corrected against live
  Postgres before being written (SET TRANSACTION ISOLATION refuses only
  a different level; LOCK refuses materialized views and foreign
  tables; a NULL default is dropped only for a bare NULL of the column's
  own unmodified non-domain type; the dependency rule keeps every
  occurrence). A new generator g9 carries one production per rule.
- **Revision 1.9 gaps** (coverage only, once implemented): the
  dependency record stays a superset by words outside its three proven
  positions; a copied column whose type is unknown makes every DROP TYPE
  / DOMAIN may-fail while its entry lives; a SQL-standard body naming a
  table makes every DROP COLUMN / ALTER COLUMN TYPE of it may-fail;
  superuser-only forms, untrusted contrib extensions and PGC_SUSET
  parameters are never claimed; SET CONSTRAINTS, VALID UNTIL other than
  ISO literals, partition bounds of other key types or non-constant
  bounds, extension versions other than the default, role grants that
  could close a cycle through outside roles, REFRESH CONCURRENTLY with
  unread indexes and SET TRANSACTION SNAPSHOT stay may-fail; `COPY ...
  FROM STDIN` in a migration is still not replayed and what the pass
  claims for it is unchecked.
- **Revision 2.0 (2026-11-07, after the sixth re-check; specified, not
  implemented; the after-v1 plan since Revision 2.1).** *Context:* the six independent re-checks found 10,
  5, 4, 2, 8 and 10 new wrong-fact families. The sixth found extension
  members, ADMIN granted back to its grantor, bookkeeping lost through
  DROP OWNED and REVOKE, whitespace in boolean settings, composite
  ALTER TYPE attributes, ALTER DOMAIN, a permanent foreign key to a
  temporary table, an unknown tablespace, 31 claims hidden by E6, and a
  harness gap after REASSIGN OWNED. Each family is a form whose row
  claims success while Postgres can refuse for a reason no precondition
  names. Probing samples that space and does not close it.

  *Decision (D-audit, developer, 2026-11-09):* an error-site audit.
  Every `ereport`/`elog(ERROR)` on the execution path of every claimed
  form in REL_17_6 (about 1,350 user-facing sites, 80 privilege checks
  and 380 internal elog, with callees) is mapped to exactly one of four
  dispositions: a modelled precondition, may-fail, a genuine
  environment assumption, or unreachable with its guard.
  - `TestGenerateErrorSites` generates the site list per form key
    (about 260 keys) from REL_17_6.
  - `TestErrorSiteAudit` checks that the reviewed TSV audit tables
    cover it exactly once. `TestAuditFormKeys` checks that every
    claimed statement has an audited form key.
  - When the revision closes, a claim gate sends every unaudited form
    to row D.
  - Forms with too large an error list are demoted: composite ALTER TYPE
    attributes, ALTER DOMAIN, CREATE OPERATOR, CAST, AGGREGATE,
    PUBLICATION and COLLATION, SECURITY LABEL, REASSIGN and DROP OWNED,
    and MERGE (2 real-history statements).
  - E1 is restated as an exact list: outside roles, outside
    non-built-in non-member objects, contrib extensions the set never
    creates or drops (members exist but are not changeable), plpgsql,
    and server files.
  - E6 no longer grants ownership of `information_schema`, built-in
    objects, extension members or plpgsql.
  - A new precondition class P15 (privileges and ownership) refuses
    statements that need those grants.
  - E7 (runtime resources) is proposed for the developer's decision.
  - Harness H25 cleans up roles after REASSIGN OWNED. H26 derives
    assumption scoring from the audit's assumption rows, replacing
    H23's blanket 42501.

  *Consequences:* the work runs in steps U0-U11 plus Z, ordered by risk. It accepts coverage loss from the
  demotions and from f rows. A later re-check finding at an audited
  `a` or `x` site is an audit error, and one at a missing site is a
  generator gap. Neither is a new family to patch.

  *Known gaps of the method:* the call graph is name-based (missed
  pointer calls are controlled by declared edges, a live node-tag
  check and the seventh re-check), and code errors stay E3.
- **Revision 2.1 (2026-11-09; implemented).**
  *Decision (D-E8, developer, 2026-11-09):* T018a2 wraps up under a new
  environment assumption E8 instead of the error-site audit, which moves
  after v1 (VISION "After v1") with the 100,000-seed run and further
  re-check rounds. **E8:** the committed migration history applies
  cleanly, in order, as it did in the project's own environments.
  Migration verdicts and the resulting schema rest on E8: the schema is
  shown as the migrations define it, the way code is shown without
  claiming it compiles. A refusal the pass proves stays reported
  (`applied: false` and its diagnostic: the migration fails on a fresh
  database); a statement whose effects are not modelled stays unknown;
  a refusal Postgres raises for a migration the pass claims is a missed
  refusal prediction (an E8 hit), not a wrong fact. Wrong facts, to
  which the merge bar still applies, are a wrong effect (table, column,
  type, key, default, index or dependency) for a history that applies
  cleanly, and a false refusal. E7 is subsumed by E8 in v1; the
  Revision 2.0 texts of E1 and E6, E7 and class P15 stay with Revision
  2.0. `contractAssumptions` names E8 on every row that claims a fact
  or `applied: true` (all rows but D and F1), on no precondition.
  - *Classification of the open findings:* the sixth re-check families
    S6-W1..S6-W9, the two 10,000-seed roots on 54bfda9 (an O1t trigger
    installer on a quoted outside name, a Y50 call after its column was
    renamed away) and the 785 claimed assumption hits are all missed
    refusals (R); S6-W10 is a harness defect. 28 clean variants of their
    forms, replayed live, gave 0 roots. No wrong effect and no false
    refusal is known, so no class rule is added.
  - *Demotions:* Revision 2.0's demotion list was re-judged (a form stays
    demoted only if its effects are not modelled); none applies, since
    each form either changes no fact or leaves the tables that use its
    object partial.
  - *Oracle gate (H27):* the facts of every history or history prefix
    Postgres applies, plus no false refusal. A history Postgres refuses
    at migration k is judged on its prefix before k (the pass re-run on
    the prefix); the pass's claim on k is an E8 hit. Roots are a wrong
    fact in a compared history or prefix, an `applied: false` on an
    applied migration, and a history the harness could not judge. The
    fixtures and regression cases keep the strict comparison, so every
    refusal prediction already built stays pinned. H25 (role cleanup) is
    brought forward from Revision 2.0; H28 serialises the harness's role
    setup; H29 (found by the gate: a history that created in the oracle
    database's public schema reached the prefix replay) drops what the
    migrating role still owns after a schema-mode history.
  - *Suite time:* the default suite runs seeds 1-200 per generator (1-1000
    passed go test's 10-minute package timeout under load); gate runs
    name their range.
  - *Gates (v1 bar):* the full check; fixtures, regression cases and the
    11 real histories at 0 roots; g1-g9 seeds 1-3000 then 1-10000 at 0
    roots under H27; corpora equal to main except counted schema
    changes; self-index; Langflow timing, store size and schema API
    latency; no private corpus, no 100,000-seed run, no seventh re-check.
- **Environment assumptions E1-E4** (E5, E6 from Revision 1.9; E8 from
  Revision 2.1) are part of every `applied: true` claim: E1 objects the set never creates exist (also server files COPY
  names, and functions outside `pg_catalog` with a fitting signature); E2
  existing rows and server-file rows are valid (not the values the set's
  own DML writes, Revision 1.3, nor the rows it leaves, Revision 1.4); E3 code the set runs does not raise; E4
  default session settings; E8 the committed history applies cleanly, in
  order, as it did in the project's own environments (Revision 2.1). The harness classifies refusals by SQLSTATE
  and context (task inventory A0, amended by H1-H9) and lists assumption
  hits separately.
- **Oracle harness** (test files only, `egress.OpenPostgres`): a fresh
  schema or database per history, the E1 prelude, sqlx 0.8.6 semantics
  (order, one simple query per migration, `-- no-transaction` when the
  manifest allows only versions that read it, stop at the first refusal),
  catalog comparison of known facts (facts of `applied: null` migrations
  are conditional and not compared), root-cause classification, a
  minimiser and recorded repros under `testdata/oracle/`. Generators g1-g6
  (a grammar change gets a new id). `go test` with
  `GRAPHVEIN_TEST_POSTGRES_URL` runs the fixtures, the regression cases and
  seeds 1-1000 of each generator (Revision 2.1: 1-200); longer runs use
  `GRAPHVEIN_ORACLE_SEEDS` and `GRAPHVEIN_ORACLE_HISTORIES`.
- **Metrics and gates.** Per history: tables complete, migrations applied,
  facts known. Precision: 0 disagreements on fixtures, the 11 real
  histories, g1-g6 seeds 1-10000, every regression case, and one
  100,000-seed run. Coverage: every real history at or above the
  baseline except listed precision corrections. Revision 2.1 (v1 bar):
  0 roots under H27 on the fixtures, every regression case, the 11 real
  histories and g1-g9 seeds 1-10000; the 100,000-seed run is after v1.
- **Review protocol.** Findings are classified as contract violation
  (fix and repro), out-of-contract form (coverage backlog, not a
  revision) or harness defect (fix and rerun); a round never reopens the
  whole task, and a form a class rule covers but the code misses is a
  violation of that class's row.
- **Coverage recovered inside the contract.** K1 and K2 as classes (P1,
  K2); C2 verified foreign-key type pairs; C3 exact constraint names (row
  M5g-1); C4 the sqlx version requirement decides whether the marker is
  read; C5 a closed list of partial reasons that create no unnamed
  object; V5/V6 empty tables and fresh sequences, ON CONFLICT DO NOTHING
  arbiters, MATCH SIMPLE NULL foreign keys and O1t trigger installers for
  DML and axum's trigger helper.

## Consequences

- A review checks rows, the default branch, the harness and the gates;
  new forms are coverage backlog. The oracle found every root that the
  revisions after the investigation fixed (the investigation's signatures and three later
  precision stops), none of which T018a's reasoning rounds had found.
- The pass is stricter where Postgres semantics depend on unknowns.
  Precision corrections lowered some coverage (K1/K2, P3 for
  supabase-auth, P1 columns and P5 values of DML for cratesio, harbor,
  lemmy, temporal and zero2prod); B2 and B3 recovered most of it: the
  aggregate facts-known share of the real histories went from 8.5%
  (the investigation) through 2.3% (after Revision 1.3) to 9.7% (B2); temporal,
  zero2prod, axum and axum-ddd are fully known, vaultwarden's first
  migration applies.
- Coverage costs that remain: the first possible failure of most real
  histories is a Revision 8 unknown fact (ADD COLUMN or ALTER TYPE on a
  partial table); DML values are proven only for the V1-V6 allowlist.
- The fast subset adds about 3-4 minutes to the postgres job at g1-g5
  seeds 1-1000 (this container: 97 s for seeds 1-500 of each generator
  with the fixtures and regression cases; to be measured in CI).
  Revision 2.1: the fast subset is seeds 1-200 of g1-g9 (1-1000 of g1-g9
  passed go test's 10-minute package timeout under load); the
  `internal/extract/schema` package took 148 s under `go test ./...` in
  this container with another unit's work running (budget: 4 minutes in
  the postgres job).
- The contract table is a maintenance cost: every new row or precondition
  needs the Go table, the document and a case.
- Extractor output version unchanged (extractors-28): the schema pass runs
  at link time on every index run and writes no cached payload, so a store
  gets the new records on its next index.

## Known gaps

- **Refusal prediction is not exhaustive (E8, Revision 2.1).** The pass
  may claim a migration that Postgres refuses on a fresh database. Known
  families: the sixth re-check's S6-W1..S6-W9 and the 10,000-seed roots
  `g5-s4095` (O1t) and `g9-s5793` (Y50). Under E8 they are E8 hits,
  listed by the gate, not wrong facts. The error-site audit that would
  close them (Revision 2.0) is after v1.
- **Harness assumptions**: E1-E4 are assumptions, not checks; E1 grants
  extension and outside objects (K3: objects `CREATE EXTENSION` creates are
  not known); rehosted real histories (diesel, golang-migrate) are judged
  under sqlx semantics; H7 re-runs a class 22/23 refusal of ALTER TABLE or
  CREATE INDEX with the tables emptied to tell row-dependent refusals from
  predictions, but only for the attributed statement.
- **CHECK constraints are not evaluated**: a row into a table with a CHECK
  is not proven (V5); CHECK expressions are typed (Revision 1.4 R1) but a
  CHECK form outside the typer's closed grammar (ANY, SIMILAR TO, ESCAPE,
  variadic functions such as num_nonnulls) is may-fail.
- **Known rows are not evaluated** (Revision 1.4 R5): SET NOT NULL, a
  validated constraint, ALTER COLUMN TYPE or a unique or expression index
  on a table holding rows the set wrote is may-fail even when the rows
  satisfy it. Identity sequence options, STORAGE, COMPRESSION, storage
  parameters and other table access methods are not proven (R3).
- **Coarse serial proof**: V6 is blocked by any `nextval`/`setval` or ALTER
  SEQUENCE text anywhere in the set (lemmy).
- **`now()` and other function values** in DML are not proven (cratesio's
  first possible failure); **`INSERT ... SELECT`** is not proven
  (vaultwarden's next possible failure).
- **Generated constraint names** stay inexact (Revision 7); RENAME,
  VALIDATE and ALTER CONSTRAINT of an exact name still make the table
  partial; a primary-key drop is not claimed in a set with a view, rule or
  SQL-standard function body (functional dependency).
- **C4** knows the 0.8 series only (A1 verified sqlx up to 0.8.6); F1
  statements other than CONCURRENTLY stay may-fail in a no-transaction
  migration.
- **Defaults** are compared by presence only.
- **CREATE OR REPLACE FUNCTION** (Revision 1.5 U6) proves identities
  only over pg_catalog argument types; a routine with another type, or
  one a statement the pass did not read may have changed, may fail when
  replaced. A plain CREATE FUNCTION over a held identity (42723) is not
  checked.
- **Revision 1.5 C2 gaps**: an unqualified reference after a search_path
  change is not resolved through the path (Revision 8's uncertain names
  apply), so statements on tables of a set schema under that path may
  fail; sqlx's post-commit UPDATE is not replayed by the harness; a
  sequence's last value is unknown once the set calls nextval or setval
  or writes rows into a serial table, so narrowing bounds may fail; an
  OWNED BY makes the sequence's existence unproven afterwards (the pass
  does not follow the dependency).
- **Revision 1.5 C3 gaps** (code effects): names are compared by their
  last part (a body's search_path, including a routine's SET clause, is
  not followed), and every word of a body's DDL counts as a name that may
  have dependents, so later statements may fail more often than needed;
  a routine created by code is never read (its calls are unbounded); a
  guarded creation is not special-cased; a data statement's inventory
  includes every trigger function and every routine the set names
  elsewhere, not only those its target's triggers and defaults run; an
  ALTER COLUMN TYPE that re-checks a CHECK constraint calling a set
  function is not an O1 statement (the function is not named in it);
  event triggers keep row O5's treatment.
- **Revision 1.6 D2 gaps** (coverage only): the provable subset leaves
  out loops, CASE statements, nested blocks, cursors, GET DIAGNOSTICS,
  RETURN NEXT / QUERY, CONSTANT / NOT NULL / %TYPE / %ROWTYPE / record
  declarations, SELECT ... INTO and RETURNING in embedded SQL, embedded
  CREATE TABLE / INDEX / ALTER TABLE other than RENAME, CREATE POLICY /
  TRIGGER / SCHEMA inside bodies, and every expression outside the
  syntax-only reader (typed literals, AT TIME ZONE, window functions,
  subscripts, JOIN); a call compiles bodies with set-declared types as may
  fail; a schema created with AUTHORIZATION CURRENT_USER makes "$user"
  unknown rather than known; function SET clauses with list values or
  units, SUPPORT, TRANSFORM and WINDOW are not proven.
- **Revision 1.6 D1 gaps**: the whole-statement readers model less than
  gram.y in places, each a coverage loss only: CREATE SCHEMA IF NOT
  EXISTS with elements (refused anyway), transaction modes and AND CHAIN,
  COPY's old option forms, WHERE and query form, REINDEX with an option
  list, the base-type form of CREATE TYPE, OPERATOR CLASS / FAMILY,
  ALTER EXTENSION other than UPDATE, ADD GENERATED AS IDENTITY with
  sequence options, column lists of PARTITION OF and OF; the syntax-only
  expression reader is V1's closed subset (typed literals such as
  `interval '1 day'` and AT TIME ZONE are outside it), used only where no
  typer reads the expression. Names held for V4 are compared by their
  last part, as `holds` compares them. The harness's E1 table positions
  skip DELETE ... USING and FROM-list aliases after a comma (judged,
  never hidden).
- **Revision 1.5 C1 gaps**: a domain's base type, a set-created range or
  base type and an expression whose type the typer does not prove have no
  proven default operator class (may fail); a written operator class is
  not proven (the element is an expression to the readers); collations
  the set creates are not proven; the per-hook value tables of settings
  hold only `timezone` (UTC, GMT) and `application_name`; values with a
  unit suffix and list values are not proven; `toast.` storage parameters
  and storage parameters of a contrib access method are not proven.
- **The weekly long oracle job** (100,000 seeds and every real history at
  its pin, task B8) is not yet in CI; the 100,000-seed run is after v1
  (Revision 2.1).

## Review notes (2026-11-09, delegated review, 2026-11-09)

- **Results.** Oracle gates under H27 (T018a2 summary, "Revision 2.1
  A2"): fixtures, all 669 regression cases (strict and E8) and the
  11 real histories at 0 roots; g1-g9 seeds 1-10000 at 0 roots (789
  E8 hits, none a root). Corpora: ten corpora identical to main except
  14 axum schema records where row O1t reads the trigger installer (no
  schema fact changed); self-index differs only in
  the `sql-migrations` fixtures whose golden T018a2 changes. Timing
  (Langflow): fresh -0.9%, warm no-op +2.1% (gate +10%). Store size
  identical (935,981,056 bytes). Schema API latency at or below main
  except axum's table maximum (+0.5 ms). CI: run 42612952489 on 4433534
  (main plus b84be6d, identical code) green on Windows and the postgres
  job (0 FAIL, expected skips only); the earlier run 42610351067 failed
  only in the web UI check, fixed by b84be6d.
- **Revisions** (task file the T018a2 task record (internal)):
  - Revision 1 (2026-11-04, after the investigation stop): root causes as class
    rules, T018b1 backlog, implementation steps.
  - Revision 1.1 (after the B1a stop): P5 extended to volatility
    (IMMUTABLE index, generated and partition expressions).
  - Revision 1.2 (after the B1b stop): P1 extended to column references in
    DML and queries.
  - Revision 1.3 (after B1c): P5 extended to DML values (N3.P5, the V1-V6
    allowlist).
  - Revision 1.4 (after the independent review): class rules R1-R5
    (CHECK typing, constraint namespace, identity, column and key
    conflicts, rows the set wrote).
  - Revision 1.5 (after the first re-check): steps C1-C3 (types and
    operator classes, statement semantics, code effects).
  - Revision 1.6 (after the second re-check): D1 whole-statement readers,
    D2 provable PL/pgSQL subset.
  - Revision 1.7 (after the third re-check): W rules (per-table trigger,
    policy and rule namespaces, reg* literals bound to set routines).
  - Revision 1.8 (after the fourth re-check): X rules (the dependency
    record behind DROP without CASCADE).
  - Revision 1.9 (after the fifth re-check and the backlog inventory):
    rules Y01-Y52, E5 and E6, the 51 inventoried families.
  - Revision 2.0 (after the sixth re-check): the error-site audit; not
    implemented, kept unchanged as the after-v1 plan (only H25 brought
    forward).
  - Revision 2.1 (2026-11-09): wrap-up under E8, harness rules H27-H29,
    default generated seeds 1-200.
- **Coverage per real history** (tables complete, migrations applied,
  facts known; equal to Revision 1.9): atuin 5/5, 22/22, 171/171; axum
  5/5, 4/4, 146/146; axum-ddd 7/7, 5/5, 163/163; cratesio 0/41, 8/292,
  0/980; harbor 0/61, 0/39, 0/1771; lemmy 0/96, 1/346, 0/2404;
  supabase-auth 0/27, 1/76, 0/1247; temporal 38/38, 25/25, 967/967;
  vaultwarden 4/29, 7/48, 53/959; windmill 0/185, 2/702, 0/7193;
  zero2prod 6/6, 13/13, 109/109.
- **Assumptions:** E1-E6 and E8 (`docs/extract/schema.md`, "Environment
  assumptions"); E7 (runtime resources) is subsumed by E8 for v1 and
  stays with Revision 2.0's texts.
- **Known gaps:** as listed under "Known gaps" above; chiefly, refusal
  prediction is not exhaustive (S6-W1..S6-W9, `g5-s4095`, `g9-s5793` are
  E8 hits, not wrong facts), E1-E4 are assumptions rather than checks,
  and the coverage gaps of Revisions 1.5-1.9.
- **After v1:** the Revision 2.0 error-site audit, the 100,000-seed run
  (and the weekly long oracle job), further re-check rounds, claiming
  facts after a refusal the pass proves, and the superuser-only forms
  (Y51).
- **Developer decisions (2026-11-09) relevant to T018a2:** wrap up under
  E8 instead of Revision 2.0; the v1 verification bar (claim contract,
  golden fixtures, corpus checks, one independent review, an oracle only
  where cheap; no 100,000-seed runs, re-check rounds or exhaustive audits
  in v1; nothing built is dropped); E7 subsumed by E8 for v1; fixtures and
  regression cases keep the strict comparison (the E8 gate applies to
  generated and real histories and `"gate": "e8"` cases only); facts
  after a proven refusal stay unclaimed.
- **Follow-up:** the `schema-table-partial` server golden
  (`internal/server/testdata`) now shows a complete table (pg-drops
  `accounts`, after row M5g-1); rename it to match. The partial-table UI
  tests use `schema-table-partial-notes` since b84be6d.
