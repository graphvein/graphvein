# Supabase generated types as a schema source

T024c3 (`docs/contract/claims/nextjs-supabase.md`, rows SB-G; ADR 0077
"T024c3 implementation notes"); package
`internal/extract/models/supabasetypes`, aggregate extractors-49; contract
v1 unchanged (model records of the model kit, ADR 0058, with the open
attributes below).

The Supabase CLI's `supabase gen types typescript` writes a TypeScript
file that postgres-meta renders from the database's catalog
(`dist/server/templates/typescript.js` of the npm package
`@supabase/postgres-meta`, MIT; read at 0.60.0, 0.70.0, 0.75.0, 0.78.0,
0.80.0, 0.83.2, 0.88.0, 0.91.0, 0.93.0, 0.96.0 and 0.97.0; from 0.99.0 the
template moved out of the published package). Projects that manage their
schema in the dashboard often commit only this file. Graphvein reads it as
a schema source labelled "generated types" when its package has no
Supabase migration set, and only compares it with the set otherwise.

## Assumption

- **SB-V6** A generated types file (SB-G1) was generated from the database the client talks to and is current; it is the package's schema source only when the package has no Supabase migration set.

## Records

One model group per types file, `set: "models/supabase_types/<file>"`
(ADR 0058): table nodes `table:<file>#<schema>.<name>` with the kit's
attributes plus `label: "generated types"`, `types_header` (`type` or
`interface`), `package` (the npm package directory, null without one),
`relationships` (`[{foreign_key_name, columns, referenced_relation,
referenced_columns, is_one_to_one, line}]` as written; `is_one_to_one`
null when the version writes none) and `computed_fields`; column nodes
with `tool_type` (the `Row` type), `nullable`, and `type`, `primary_key`,
`unique`, `default`, `foreign_keys` unknown (null), each with its reason.
Claims: `name` and `nullable`, row SB-G1, `verification: mapped`.

Table access (`docs/extract/table-access.md`, rows SB-T1 to SB-T4) uses a
package's generated types as its schema source when the package has no
Supabase migration set: edges carry `assumes: [SB-V6]`; an embedded
resource never resolves through generated types (the relationships are not
resolved), so SB-T3 records it as a partial unknown.

## Claim contract

| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| SB-G1 | a scanned `.ts` file whose declarations are exactly postgres-meta's `typescript` template output (versions 0.70.0 to 0.97.0, the generator the Supabase CLI's `gen types typescript` runs), compared token by token (whitespace, comments, `;` and `,` separators and quote style ignored): first `export type Json =` with the generator's union; then `export type Database = {` or (up to 0.77) `export interface Database {`, an optional `__InternalSupabase` member, per schema exactly `Tables`, `Views`, `Functions`, `Enums`, `CompositeTypes` in that order (`[_ in never]: never` when empty); each table `Row`, `Insert`, `Update` (the same columns in the same order, every `Update` property optional, `Row` properties never optional; `Row` may end with computed fields) and `Relationships`; then only the generator's helpers (`PublicSchema`, `DefaultSchema`, `DatabaseWithoutInternals`, `Tables`, `TablesInsert`, `TablesUpdate`, `Enums`, `CompositeTypes`, `Constants`) | a schema source `tool: supabase_types`, labelled "generated types", `dialect: postgres`, every fact `verification: mapped`: tables `<schema>.<name>` from `Tables` keys (`column_set` complete; `constraint_set` partial: the generator states no key, unique, index or check); columns from `Insert`, with `tool_type` the `Row` type as written and `nullable` from a top-level `\| null` (`unknown` and `any` alone: unknown); a computed field listed in `computed_fields`, not a column; `Relationships` listed as written (`relationships`), not resolved to foreign keys (the generator names no referenced schema, and some versions leave out keys to other schemas); `Views` read for their shape, no records (as views of migration sets) | any other shape: not a schema source (diagnostic `supabase_types_unread`, info, naming the line) | SB-V6 |
| SB-G2 | a package (nearest `package.json`) with a Supabase migration set and a generated types file | the migration set is the schema source; the types file is not read as one: it is compared with the set (tables; on complete migration tables also column names, nullability and relationships), each difference a `supabase_types_disagreement` diagnostic (info), one `supabase_types_compared` summary; platform schemas (SB-V2) not compared | - | - |
| D | other generated forms (`supabase gen types` for other languages), a `Database` type in a file that does not start with `export type Json` (hand-written, Kysely) | nothing | always | - |

`TestSupabaseTypesContract` keeps this table, the task's table and
`Contract` 1:1 (ids, order, assumptions, forms, branch functions);
`TestSupabaseTypesTags` checks that the fixture
`testdata/fixtures/supabase-types` reaches every row and form;
`TestSupabaseTypesIndexedFixture` checks its golden.

## Oracle

`scripts/oracle/nextjs_supabase/supabase_types.cjs` reads each types file
with the TypeScript 6.0.3 checker (`Database[schema]["Tables"]`, each
table's `Row`, `Insert` and `Relationships` property types) and prints the
tables, columns, `null` in the `Row` type's union, computed fields and
relationships. `TestSupabaseTypesOracle` (opt-in with
`GRAPHVEIN_TS_ORACLE_DIR`, as the TypeScript structure oracle) compares
them with what Graphvein reads, on the fixture and on the corpus files
named in `GRAPHVEIN_SUPABASE_TYPES_FILES`.

## Known gaps

- Relationships are not foreign keys: the generator writes the referenced
  relation without its schema, and from some version on leaves out keys to
  other schemas.
- The TypeScript type of a column is not mapped back to a Postgres type
  (several Postgres types share one TypeScript type).
- SB-G2 compares names, nullability and relationships, not types.
- Views are not recorded (migration sets record none either).
