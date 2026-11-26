# Oracle regression cases (T018a2)

Minimised repros of every root cause the investigation oracle run found against
origin/main 73aca2e (the T018a2 task record (internal)). Each directory holds
`migrations/*.sql` (the history, written by the minimiser) and `oracle.json`:
the history settings (prelude, fresh database, generator tags), the recorded
Postgres 17 verdict (per-migration outcome with SQLSTATE, catalog facts) and
`disagreements`, the root causes today's code produces against it.
`TestOracleRegression` checks them without Postgres and re-checks the verdict
when `GRAPHVEIN_TEST_POSTGRES_URL` is set. The cases have no `Cargo.toml`, so
they are not sqlx sets when Graphvein indexes this repository; the test
writes the harness manifest (sqlx 0.8.6, postgres) into its temporary copy.

Ids: `g1-s<seed>`, `g2-s<seed>`, `g3-s<seed>` and `g4-s<seed>` are
generators g1, g2, g3 and g4 at that seed;
`fixture-<set>` comes from `testdata/fixtures/sql-migrations/<set>`.

Columns: `class` is the investigation's root classification (K1, K2, S1); `owner` is
the Revision 1 class the root belongs to (P1-P10, K1, K2, H1-H6; `S1` for a
root in no class, a precision stop), whose unit fixes it; `state` is
`agrees` when today's extraction agrees with the recorded verdict (no
`disagreements`) and `open` when the case still pins a root of a class not
fixed yet. B1a (Revision 1) fixed K1, K2, P1, P2, P3, P10 and the harness
defects H1-H6; `TestOracleRegression` fails on a case without an owner.
Reassigned by B1a: `g1-s1364-S1-N3_reindex` and `g1-s3088-S1-M4_create_unnamed`
carry the investigation's P1 signatures, but their root is a ROLLBACK (P6). Added by
B1a: `g1-s6157-S1-foreign_key` (H4); new roots of classes B1b owns, found by
g2: `g2-s561`, `g2-s3021`, `g2-s5041`, `g2-s7077`, `g2-s8584` (P6),
`g2-s578`, `g2-s3756`, `g2-s4162`, `g2-s6905`, `g2-s7106` (P4), `g2-s7346`
(P9), `g2-s8568` (P7); and `g2-s193-S1-M4_create_expression_p3` (no class:
an S1 stop, see the T018a2 task record (internal)). B1b (Revision 1 and
1.1) fixed P4-P9; every case agrees. Revision 1.1 moved
`g2-s193-S1-M4_create_expression_p3` to P5 (volatility). Added by B1b:
`g1-s9616-S1-M5c_type` (P8: ALTER COLUMN TYPE of a column an unresolved
foreign key uses) and `g2-s1123-S1-table` (P3: CREATE TABLE IF NOT EXISTS
over a generated index name; the investigation's `table` signature, attributed to P6
by g1-s8415 until B1b), and, found by g3: `g3-s117-S1-M5a_order_p9` (P9:
SET NOT NULL and DROP NOT NULL of one column run in pass order) and
`g3-s8736-S1-M6_drop` (P8: DROP TABLE of a table another was created LIKE,
whose copied default uses its sequence). Revision 1.2 extended P1 to the
column references of DML and queries; step B1c fixed it and moved `g3-s6790-S1-N3_update` (a data-changing
statement that names a column the table does not have, 42703) from owner
`S1` to `P1`. Added by B1c: `g3-s1613-S1-N2_commit` and
`g3-s6790-S1-applied`, the minimised repros of the two live signatures of
that root on g3 seeds 1-10000 (`applied|S1|42703|N2.commit` and
`applied|S1|42703|`: a generated item that writes two statements, its
setup CREATE TABLE and the statement it tags, shifts the harness's
statement-to-tag attribution by one), owner `P1`.
Revision 1.3 extended P5 to the values of DML and P1 to the column
references of N1 statements, narrowed E2 (a class 22 or 23 refusal of a
data-changing statement or a query is no longer an assumption hit) and
fixed that attribution (each statement of a generated item gets the item's
tag; the recorded cases keep the tags stored in their `oracle.json`). Unit
B1d found no root on g1-g4 seeds 1-10000 and added no case.
Added by B2 (generator g5): `g5-s80-S1-O1t_trigger_call` (a string
argument of a set function that a regclass parameter reads names a
dropped table, 42P01; owner `P1`, precondition `O1.P1`) and
`g5-s232-S1-N3_insert_empty_v5` (ON CONFLICT DO NOTHING on a table with a
deferrable unique key, 55000; owner `P5`, `N3.P5`).
Every case agrees.

Added by B3 (the 100,000-seed run, g1 seeds above 10000): five roots of
fixed classes that the code missed (contract violations of their rows,
fixed by the class rule): `g1-s16467` (P2), `g1-s20371` (P4), `g1-s27299`
(P8), `g1-s31160` (P1), `g1-s37115` (P1). Two more roots of that run
carry signatures of other cases and are pinned by unit cases instead
(`TestB3Cases`: `g1-s34388`, P2; `g1-s48011`, P8), and one was a harness
defect (H8: LOCK in the implicit block of a multi-statement
no-transaction migration, `g1-s45067`).

Revision 1.4 (2026-11-07) added the independent review's probes as
`review-<probe>` cases (the probe histories of the review of af6a883,
each with no prelude): 31 of them were roots then (signature = the root
the code at af6a883 produced; owners P2, P4 and P5 by the Revision 1.4
rule R1-R4 that fixes them), the other 36 agreed then and pin the forms
the review checked (signature `agreed at af6a883`; `review-e01` and
`review-e02` were E2 then and are judged under H9 now, owner P11). Found by g6 while
the rules were written: `g6-s1525-S1-M5f_generated_name_r2` (P2: a
written name equal to `<table>_check`, the generated name of an unnamed
CHECK over two columns) and `g6-s506-S1-M1_table_options_r3` (P7: `WITH
()` is a syntax error). Found by the 100,000-seed run on a3c9574:
`g1-s52007-S1-N1_create_policy` (P2, precondition `N1.P2`: a policy name
created twice on one table; the names N1 statements create are free only
until the set creates them), `g2-s65990-S1-M4_generated_shape` (P2,
`M4.P2`: an index named like the generated name of an unnamed index the
set created on a table it does not hold) and `g3-s59521-S1-M5f_check`
(P8, `M5a.P8`: ALTER TABLE ONLY ... ADD CHECK on a table with inheritance
children). The same run met a harness defect (H10: the attribution
replay ran every statement inside a savepoint, where a label that ALTER
TYPE ... ADD VALUE added to an enum type created in the same transaction
is refused with 55P04, so the refused statement was not attributed and
the E1 rule for a foreign key to a prelude table never ran;
`g1-s69180`, `g3-s43863`, pinned by `g3-s43863-S1-applied`, owner H10).
`g6-s38420-S1-M5a_add_p1_shape` (P4, `M5a.P1`: ALTER TABLE ... ADD COLUMN
on the index of a written UNIQUE constraint; Postgres accepts only
RENAME, RENAME COLUMN, OWNER TO, SET/RESET options and SET TABLESPACE on
an index) and `g6-s48802-S1-M5c_type` (P8, `M5c.P8`: ALTER COLUMN TYPE of
a column a trigger's UPDATE OF list names; DROP COLUMN without CASCADE
likewise), `g6-s69809-S1-M8_rename` (P1, `M8.P1`: ALTER INDEX of a name
that is only a CHECK constraint's; a CHECK constraint has no index), and
a second harness defect (H11: the comparison matched an unnamed
expression index, whose columns are unknown, to a plain index's catalog
entry first; `g4-s32590`, `g4-s44332`, `g6-s98403`, pinned by
`g4-s44332-S1-index`, owner H11). The run on 1e2b7bd: `g1-s56780-S1-type` (P3, `M5a.P3`: ADD COLUMN IF NOT
EXISTS on a partial table, here a child whose inherited column of that
name has another type, recorded the written type).
Every case agrees.

Revision 1.5 (2026-11-07) added the independent re-check's
class-A probes of its rules U1 and U7-U10, and the class-B probes those
rules cover (a16, b08, b09), as `review15-<probe>` cases (no prelude;
owner U1, U7, U8, U9 or U10, the Revision 1.5 rule that fixes the class;
signature `agreed at Revision 1.5`): each was a confident wrong fact at
5c98c27 and agrees now. Step C2 added those of U3 (b19-b21), U4 (e04,
e05, e10, e11), U5 (a38-a43, a45) and U6 (d01); step C3 adds U2's. Found by
g7 while the rules were written: `g7-s1811-S1-M5f-u_using_index_u1` (P7,
`M8.P7`: `ALTER INDEX rename TO x`, a word after ALTER INDEX name that
starts no alter_table_cmd, is a syntax error). Found by g7:
`g7-s267-S1-R1_sequence_options_u5` (P4, rule U5: `ALTER SEQUENCE ... OWNED BY NONE`
of an identity sequence is refused too, process_owned_by).
Every case agrees.

Revision 1.6 (2026-11-07) added the second re-check's probes of
its rules as `review16-<probe>` cases (no prelude; owner V2, V3, V4, V7,
V9-V13, or H14 for the scoring cases; signature `agreed at Revision
1.6`): 77 that were claimed at 80625b5 or whose refusal the harness hid
(H14-H16), and 10 that pin the boundaries or the new scoring (s04, s05,
s07, s09, s11, y22, y39; y01, y16, y17). Found by g8 while the rules were written:
`g8-s113-S1-N1_object_kinds_v4` (P7, rule V7: ALTER INDEX forms other
than RENAME TO and SET / RESET are not row M8's), `g8-s1038-S1-N2_names_v10`
(V10: `SET NAMES` takes a string constant) and
`g8-s1052-S1-R4_system_column_v11` (V11: an attribute change of a type a
typed table uses needs CASCADE, 2BP01); found by the gate's g8 run:
`g8-s2796-S1-M4_opclass_v9` (P4: a unique index needs btree, the only
built-in method with amcanunique). Step D2 (2026-11-07) adds the probes
of V1, V5, V6 and V8 as `review16-<probe>` cases (owner V1, V5, V6 or V8;
p52 and p56 keep their probe's prelude): 30 that were claimed at 80625b5
or whose refusal the harness hid (r36-r49, y08, y09, y11-y13; p70, p72,
z06, s24; q07, y19; p23, p50-p53, p56-p59) and 2 that pin the boundary
(s21, s23). Found by g8: `g8-s270-S1-R6_compile_v1` (H14:
spi.c _SPI_error_callback writes the SPI call's query as `SQL statement`,
`SQL expression` or `PL/pgSQL assignment`; none is a frame of its own).
Found by the 10,000-seed g8 gate on 07f56a1 (2026-11-07 root fixes):
`g8-s4299-S1-N1_object_kinds_v4` (V4 kinds: relations share pg_class's
namespace, so ALTER INDEX IF EXISTS of a name held as a view does not skip;
Postgres refuses it, 42809); found by that fix's g8 production
`genV4RelKinds`: `g8-s983-S1-R4_relkinds_v4` and
`g8-s1212-S1-R4_relkinds_v4` (V4: ALTER DOMAIN and ALTER TYPE of a type
the pass does not hold; E1 does not cover types other than its own).
Every case agrees.

Revision 1.7 (2026-11-07) adds the third re-check's probes of its rules
as `review17-<probe>` cases (no prelude; owner W1-W7; signature `agreed
at Revision 1.7`): 35 that were claimed at 5f86526 or whose refusal H14
hid (W1: d12-d14, d16-d18, v063, d35, d36, n15-n19, n21, n23, h04; W2:
h01, h13; W3: q14, q42, scored by H17; W4: d01, d02, d04, d05; W5:
n09-n14; W6: w11, w19; W7: x10, m43) and 2 that pin the boundary (d15:
ALTER TRIGGER is row D; d03: COMMENT ON DATABASE of a missing database
is accepted with a warning). Every case agrees.

Revision 1.8 (2026-11-07) adds the fourth re-check's probes of its rules
as `review18-<probe>` cases (owner X1-X4 or H20; signature `agreed at
Revision 1.8`; j03 and j04 have a prelude creating the role and run in a
fresh database): 41 that were claimed at 9303776 or whose refusal E1
attribution hid (X1: a28, b01, c02, c04, c09, c10, c11, c22, c23, c27,
h07, h08, h09, h11, h14, h15, l06, l19, l20, l21, m01, m03, m04, m05, m12;
X2: n13, o05, o19, o20, o25, m13; X3: g04, g05, g06, g08, f18, g11; X4:
j03, j04; H20: j01, j02, which the pass already made may-fail). Every
case agrees.

Revision 1.9 step U-a (2026-11-07) adds the inventory's probes of the
statement-validator rules as `review19-<set>-<probe>` cases (`r1` the
review set, `r3` review3, `r4` review4, `r5` review5; no prelude; owner
the Y rule; signature `agreed at Revision 1.9`; r1-n06, r1-n07, r3-d25,
r5-r23, r5-o02 and r5-o03 run in a fresh database): 92 that were claimed
at d14f0fe (Y01: a21; Y02: a24; Y05: b14; Y06: c11, n07, q03, m50; Y07:
c12; Y08: c10, n06; Y09: d10, d28, a32; Y10: d11, d13, a35; Y11: d12,
d14, m54, m55; Y12: d16-d18; Y14: v001-v003, v005, m48, p084; Y15: d06,
d07, m08, v028; Y16: v023-v027, v085; Y17: p007, p008, p098, p099; Y18:
y16; Y19: f01-f03, d15, d16; Y20: f18, f20, d17; Y21: f09; Y22: g11,
x11, x12; Y25: l02, l04, l09, l11, l15, o03, a41; Y26: m01, m02, m05;
Y30: k01-k03; Y31: d21, d22; Y32: d25; Y35: h02, m15, v043, v044, v046;
Y36: m05, m06; Y37: m46; Y42: i14, r01, r02, r10, r12, r15, r16, r22,
r23; Y43: o02, o03; Y46: u01, a fact case). Every case agrees.

Revision 1.9 step U-b (2026-11-07) adds the inventory's probes of the
state-record rules the same way (`inv` the inventory's own probes;
r3-d26, r3-d27, r3-d28, r3-m29, r3-v050 and r3-v051 run in a fresh
database, because extensions are per database): 33 that were claimed at
d14f0fe (Y03: b01, b03, b05, b18, inv-x01; Y04: b12; Y13: d25, d26,
v072, m47; Y23: k16; Y24: k21; Y27: e01, e09; Y28: r01-r04; Y29: k15,
k16, k17, k18, k20, x01; Y33: d26, v050, v051, m29; Y34: d27, d28; Y38:
q05, c29, c30). r1-b05's verdict records sqlx's post-commit UPDATE
(harness rule H24): 1_a.sql is applied, its UPDATE fails and 2_b.sql is
not run. Every case agrees. The g9 development run found one more root
of Y38's class, `g9-s329-S1-N1_droprole_y38` (GRANT ALL ON t TO r, then
DROP ROLE r), kept under its generator name and fixed by the class rule
(a grant of ALL is a live dependent).

Revision 1.9 step U-c (2026-11-07) adds the inventory's probes of the
dependency rule and the harness families the same way: 33 cases that were
claimed at d14f0fe or hidden by an assumption (Y39: r5 f01, f02, f09,
f12, f15, h01, h05, h06, h08, h10, h20, h24, h25, h26, h29, t05, c06;
Y40: a08, g01, g06, n11; Y41: n07, n08; Y44: v02; Y45: w08; Y47: r4-i07;
Y48: r3-p030, r3-p031; Y49: r3-h14; Y50: r3-p088, r4-n04; Y51: inv-x05,
r2-i20), and the gate root `g8-s8389-S1-columns` (Y52, kept under its
gate name, its verdict re-recorded). Their verdicts are recorded by the
harness of Revision 1.9 (H21-H23: the replay runs as the role
`gv_migrator`, which is not a superuser, so the event-trigger cases
record 42501 from CREATE EVENT TRIGGER). The g7 development run found one
more root of Y48's class, `g7-s147-S1-O1_code_effects_u2` (a called
procedure's constant EXECUTE of DROP INDEX of a key's index, judged since
H21), fixed by the class rule (a statement code runs that the pass does
not analyse where it runs is row D). Two more g9 development roots are kept
under their generator names, minimised by hand:
`g9-s772-S1-M6_ownedseq_y40` (class R1b: a SQL-standard body is parsed
and analysed at CREATE FUNCTION whatever LANGUAGE says, so R1b.P5 applies
to BEGIN ATOMIC and RETURN bodies; before, they were claimed unread) and
`g9-s142-S1-O1_stalecall_y50` (Y49: a called routine's RETURN statement
gets the names rule). The 3000-seed gate found one root of row
O1t once H21 matched `format()` templates whatever their case,
`g5-s2274-S1-O1t_installer` (%I of a text with double quotes names a
relation with quotes in its name; the installer is no longer substituted
then). The first g9 run under H23 found
`g9-s35-S1-N1_roleset_y15` (ALTER ROLE ... SET of a placeholder parameter
is superuser-only as a stored setting), fixed by Y51's class rule. Every
case agrees. H23 also changed
the recorded verdicts of seven earlier cases, re-recorded with their root
causes unchanged (none): `review16-q07-user-schema` (the unqualified
table goes to public, because the "$user" schema is named after the test
role, which no longer runs the migrations), `review17-n12-drop-cast-missing`
(42501: the migrating role does not own the built-in types),
`review18-c10-dropfunc-eventtrigger` (42501 at CREATE EVENT TRIGGER),
`review19-r1-c12-table-pgcatalog` (42501 for schema pg_catalog),
`review19-r1-q05-drop-role-owner` and `review19-r4-c29-droprole-owner`
(42501 at ALTER TABLE ... OWNER TO a role without CREATE on the schema)
and `review19-r3-m47-createrole-in-role-self` (42501: no ADMIN OPTION on
the role being created).

Added by Revision 2.1 (assumption E8, harness rule H27): the 28
clean variants of the open findings' forms (`review21-a1-<probe>`,
`"gate": "strict"`, owner `E8`): Postgres applies every migration and the
pass's facts agree with the catalog, or are partial or unknown where the
forms are not modelled. And the two 10,000-seed roots of 54bfda9 as E8
cases (`"gate": "e8"`, owner `E8`, judged under H27):
`g5-s4095-E8-O1t_trigger_call` and `g9-s5793-E8-O1_stalecall_y50`, the
minimised histories split in two migrations so that the prefix before the
refused migration is judged (0 roots); the refused migration's claim is
recorded as the case's `e8_hit` line. Every case agrees.

| case | class | owner | state | signature | Postgres / fact |
|---|---|---|---|---|---|
| `fixture-pg-opaque-exists-S1-columns` | S1 | P3 | agrees | `columns` | column id is not in the catalog (the investigation) |
| `fixture-pg-rev2-columns-S1-index` | S1 | P3 | agrees | `index` | records index ledger_memo_idx (unique false), catalog has [] (the investigation) |
| `fixture-pg-unreadable-S1-applied` | S1 | P10 | agrees | `applied\|S1\|\|` | applied: true, Postgres set_refused :  (the investigation) |
| `g1-s101-S1-M5k_no_inherit` | S1 | P4 | agrees | `applied\|S1\|42P01\|M5k.no_inherit` | 42P01 relation "t1" is not a parent of relation "t1" |
| `g1-s1011-S1-M5k_alter_constraint` | S1 | P4 | agrees | `applied\|S1\|42704\|M5k.alter_constraint` | 42704 constraint "nosuch_constraint" of relation "t1" does not exist |
| `g1-s102-S1-M7_drop` | S1 | P1 | agrees | `applied\|S1\|42704\|M7.drop` | 42704 index "t1_c2_idx" does not exist |
| `g1-s1070-S1-R6_function_sql_body` | S1 | P5 | agrees | `applied\|S1\|42703\|R6.function_sql_body` | 42703 column "nosuch" does not exist |
| `g1-s1175-S1-N1_comment_column` | S1 | P1 | agrees | `applied\|S1\|42P01\|N1.comment_column` | 42P01 relation "t2" does not exist |
| `g1-s1297-S1-applied` | S1 | P1 | agrees | `applied\|S1\|42704\|` | 42704 index "t1_id_key" does not exist |
| `g1-s130-S1-N1_comment_column` | S1 | P5 | agrees | `applied\|S1\|42703\|N1.comment_column` | 42703 column "nosuch" of relation "t1" does not exist |
| `g1-s1364-S1-N3_reindex` | S1 | P6 | agrees | `applied\|S1\|42P01\|N3.reindex` | 42P01 relation "t3" does not exist |
| `g1-s1372-S1-M5k_enable_rule` | S1 | P4 | agrees | `applied\|S1\|42704\|M5k.enable_rule` | 42704 rule "nosuch_rule" for relation "t1" does not exist |
| `g1-s1458-S1-N3_select` | S1 | P1 | agrees | `applied\|S1\|42P01\|N3.select` | 42P01 relation "t1" does not exist |
| `g1-s1569-S1-applied` | S1 | P5 | agrees | `applied\|S1\|42701\|` | 42701 duplicate column name in statistics definition |
| `g1-s1588-S1-R1_view_columns` | S1 | P5 | agrees | `applied\|S1\|42703\|R1.view_columns` | 42703 column "nosuch" does not exist |
| `g1-s1615-S1-M1_enum_default` | S1 | K2 | agrees | `applied\|S1\|55P04\|M1.enum_default` | 55P04 unsafe use of new value "v3" of enum type e6 |
| `g1-s1674-S1-index` | S1 | H2 | agrees | `index` | records index (c1) (unique true), catalog has [{t1_c1_idx true [r4] false}] (the investigation) |
| `g1-s1776-K1-M4_forward_table` | K1 | K1 | agrees | `applied\|K1\|42P01\|M4.forward_table` | 42P01 relation "t7" does not exist |
| `g1-s1873-S1-N1_create_statistics` | S1 | P1 | agrees | `applied\|S1\|42P01\|N1.create_statistics` | 42P01 relation "t1" does not exist |
| `g1-s188-K1-M5a_forward_table` | K1 | K1 | agrees | `applied\|K1\|42P01\|M5a.forward_table` | 42P01 relation "t2" does not exist |
| `g1-s1896-S1-M5a_add_M5c_type` | S1 | P9 | agrees | `applied\|S1\|42703\|M5a.add` | 42703 column "c2" of relation "t1" does not exist |
| `g1-s1944-S1-M5e_drop_default_M5k_detach` | S1 | P7 | agrees | `applied\|S1\|42601\|M5e.drop_default` | 42601 syntax error at or near "DETACH" |
| `g1-s1980-S1-M1_create_fk` | S1 | P1 | agrees | `applied\|S1\|42P01\|M1.create_fk` | 42P01 relation "t1" does not exist |
| `g1-s1982-S1-M1_create` | S1 | P4 | agrees | `applied\|S1\|42P16\|M1.create` | 42P16 constraints on permanent tables may reference only permanent tables |
| `g1-s2-K1-M1_create` | K1 | K1 | agrees | `applied\|K1\|42P01\|M1.create` | 42P01 relation "t3" does not exist |
| `g1-s201-S1-M5c_type` | S1 | P5 | agrees | `applied\|S1\|42804\|M5c.type` | 42804 column "c1" cannot be cast automatically to type integer |
| `g1-s2069-S1-M5k_alter_column_other` | S1 | P4 | agrees | `applied\|S1\|0A000\|M5k.alter_column_other` | 0A000 column data type bigint does not support compression |
| `g1-s2100-S1-M1_create_fk` | S1 | P4 | agrees | `applied\|S1\|42P16\|M1.create_fk` | 42P16 constraints on permanent tables may reference only permanent tables |
| `g1-s2104-S1-R5_drop_view` | S1 | P4 | agrees | `applied\|S1\|42809\|R5.drop_view` | 42809 "t2" is not a view |
| `g1-s2226-S1-N1_comment_index` | S1 | P1 | agrees | `applied\|S1\|42P01\|N1.comment_index` | 42P01 relation "t1_pkey" does not exist |
| `g1-s2235-S1-R1_create_table_as` | S1 | P1 | agrees | `applied\|S1\|42P01\|R1.create_table_as` | 42P01 relation "t5" does not exist |
| `g1-s2334-K1-M5a_add` | K1 | K1 | agrees | `applied\|K1\|42P01\|M5a.add` | 42P01 relation "t3" does not exist |
| `g1-s24-S1-M4_create_unnamed` | S1 | P7 | agrees | `applied\|S1\|42601\|M4.create_unnamed` | 42601 syntax error at or near "ON" |
| `g1-s2554-S1-M5a_if_not_exists_M5k_no_inherit` | S1 | P4 | agrees | `applied\|S1\|42P01\|M5a.if_not_exists` | 42P01 relation "t1" is not a parent of relation "t1" |
| `g1-s26-S1-M8_rename` | S1 | P2 | agrees | `applied\|S1\|42P07\|M8.rename` | 42P07 relation "t2" already exists |
| `g1-s263-K1-applied` | K1 | K1 | agrees | `applied\|K1\|42P01\|` | 42P01 relation "t4" does not exist |
| `g1-s2839-S1-M5k_inherit` | S1 | P4 | agrees | `applied\|S1\|42804\|M5k.inherit` | 42804 child table is missing column "c3" |
| `g1-s286-S1-M5k_alter_column_other` | S1 | P1 | agrees | `applied\|S1\|42P01\|M5k.alter_column_other` | 42P01 relation "t2" does not exist |
| `g1-s3072-K1-M5f_foreign_key` | K1 | K1 | agrees | `applied\|K1\|42P01\|M5f.foreign_key` | 42P01 relation "t15" does not exist |
| `g1-s3088-S1-M4_create_unnamed` | S1 | P6 | agrees | `applied\|S1\|42P01\|M4.create_unnamed` | 42P01 relation "t4" does not exist |
| `g1-s3211-S1-N1_grant` | S1 | P1 | agrees | `applied\|S1\|42P01\|N1.grant` | 42P01 relation "t1" does not exist |
| `g1-s3268-S1-M1b_inherits` | S1 | P1 | agrees | `applied\|S1\|42P01\|M1b.inherits` | 42P01 relation "t2" does not exist |
| `g1-s3444-S1-M5k_alter_constraint` | S1 | P4 | agrees | `applied\|S1\|42809\|M5k.alter_constraint` | 42809 constraint "t1_pk" of relation "t1" is not a foreign key constraint |
| `g1-s351-K1-M4_generated_shape` | K1 | K1 | agrees | `applied\|K1\|42P01\|M4.generated_shape` | 42P01 relation "t5" does not exist |
| `g1-s357-S1-R5_drop_function` | S1 | P4 | agrees | `applied\|S1\|42809\|R5.drop_function` | 42809 p1() is not a function |
| `g1-s3580-S1-O1_function_call` | S1 | P4 | agrees | `applied\|S1\|42809\|O1.function_call` | 42809 p1() is a procedure |
| `g1-s37-K1-M1_create_fk` | K1 | K1 | agrees | `applied\|K1\|42P01\|M1.create_fk` | 42P01 relation "t2" does not exist |
| `g1-s3747-S1-N1_create_trigger` | S1 | P1 | agrees | `applied\|S1\|42P01\|N1.create_trigger` | 42P01 relation "t1" does not exist |
| `g1-s3797-S1-O5_transaction` | S1 | P6 | agrees | `applied\|S1\|25P01\|O5.transaction` | 25P01 SAVEPOINT can only be used in transaction blocks |
| `g1-s39-S1-O5_transaction` | S1 | P6 | agrees | `applied\|S1\|3B001\|O5.transaction` | 3B001 savepoint "sp1" does not exist |
| `g1-s3904-S1-M5k_not_of_M5k_alter_column_other` | S1 | P1 | agrees | `applied\|S1\|42P01\|M5k.not_of` | 42P01 relation "t2" does not exist |
| `g1-s392-S1-M7_concurrently_tx` | S1 | P6 | agrees | `applied\|S1\|25001\|M7.concurrently_tx` | 25001 DROP INDEX CONCURRENTLY cannot run inside a transaction block |
| `g1-s394-S1-M1_create` | S1 | P1 | agrees | `applied\|S1\|42P01\|M1.create` | 42P01 relation "t1" does not exist |
| `g1-s3946-S1-R3_add_value` | S1 | K2 | agrees | `applied\|S1\|22023\|R3.add_value` | 22023 "nosuch" is not an existing enum label |
| `g1-s4050-S1-R1_view` | S1 | P1 | agrees | `applied\|S1\|42P01\|R1.view` | 42P01 relation "t2" does not exist |
| `g1-s413-S1-M8_rename` | S1 | P1 | agrees | `applied\|S1\|42P01\|M8.rename` | 42P01 relation "t1" does not exist |
| `g1-s4243-S1-M5e_set_default_M5k_detach` | S1 | P7 | agrees | `applied\|S1\|42601\|M5e.set_default` | 42601 syntax error at or near "DETACH" |
| `g1-s425-S1-R6_rename_function` | S1 | P4 | agrees | `applied\|S1\|42809\|R6.rename_function` | 42809 p1() is not a function |
| `g1-s426-S1-N1_lock` | S1 | P6 | agrees | `applied\|S1\|25P01\|N1.lock` | 25P01 LOCK TABLE can only be used in transaction blocks |
| `g1-s429-S1-M4_outside_table_unnamed` | S1 | P7 | agrees | `applied\|S1\|42601\|M4.outside_table_unnamed` | 42601 syntax error at or near "ON" |
| `g1-s436-S1-M1_generated_name_shape` | S1 | P2 | agrees | `applied\|S1\|42P07\|M1.generated_name_shape` | 42P07 relation "t2_pkey" already exists |
| `g1-s4399-S1-M5k_enable_trigger` | S1 | P4 | agrees | `applied\|S1\|42704\|M5k.enable_trigger` | 42704 trigger "nosuch_trigger" for table "t1" does not exist |
| `g1-s4492-S1-M5d_drop_not_null` | S1 | P1 | agrees | `applied\|S1\|42P01\|M5d.drop_not_null` | 42P01 relation "t1" does not exist |
| `g1-s4608-S1-M5k_set_logged` | S1 | P1 | agrees | `applied\|S1\|42P01\|M5k.set_logged` | 42P01 relation "t1" does not exist |
| `g1-s4897-S1-M5c_type` | S1 | P5 | agrees | `applied\|S1\|42846\|M5c.type` | 42846 cannot cast type uuid to integer |
| `g1-s4967-S1-N1_revoke` | S1 | P1 | agrees | `applied\|S1\|42P01\|N1.revoke` | 42P01 relation "t2" does not exist |
| `g1-s5-S1-N3_cluster` | S1 | P8 | agrees | `applied\|S1\|42704\|N3.cluster` | 42704 there is no previously clustered index for table "t1" |
| `g1-s51-S1-N1_security_label` | S1 | P4 | agrees | `applied\|S1\|22023\|N1.security_label` | 22023 no security label providers have been loaded |
| `g1-s515-S1-M4_outside_table` | S1 | P6 | agrees | `applied\|S1\|25001\|M4.outside_table` | 25001 CREATE INDEX CONCURRENTLY cannot run inside a transaction block |
| `g1-s5193-S1-M5k_owner_M5k_detach` | S1 | P7 | agrees | `applied\|S1\|42601\|M5k.owner` | 42601 syntax error at or near "DETACH" |
| `g1-s536-K2-R3_add_value_existing` | K2 | K2 | agrees | `applied\|K2\|42710\|R3.add_value_existing` | 42710 enum label "a" already exists |
| `g1-s5408-S1-M4_create` | S1 | P1 | agrees | `applied\|S1\|42P01\|M4.create` | 42P01 relation "t1" does not exist |
| `g1-s5634-S1-M4_generated_shape` | S1 | P1 | agrees | `applied\|S1\|42P01\|M4.generated_shape` | 42P01 relation "t4" does not exist |
| `g1-s5696-S1-M4_concurrently_notx` | S1 | P1 | agrees | `applied\|S1\|42P01\|M4.concurrently_notx` | 42P01 relation "t2" does not exist |
| `g1-s583-S1-M5k_validate` | S1 | P4 | agrees | `applied\|S1\|42704\|M5k.validate` | 42704 constraint "nosuch_constraint" of relation "t1" does not exist |
| `g1-s6020-K2-R3_missing_type` | K2 | K2 | agrees | `applied\|K2\|42704\|R3.missing_type` | 42704 type "e2" does not exist |
| `g1-s6157-S1-foreign_key` | S1 | H4 | agrees | `foreign_key` | 42P01 relation "t1" does not exist (E1) |
| `g1-s6209-S1-N3_insert` | S1 | P1 | agrees | `applied\|S1\|42P01\|N3.insert` | 42P01 relation "t1" does not exist |
| `g1-s6265-S1-M5k_validate` | S1 | P4 | agrees | `applied\|S1\|42809\|M5k.validate` | 42809 constraint "t1_pk" of relation "t1" is not a foreign key or check constraint |
| `g1-s635-S1-applied` | S1 | P6 | agrees | `applied\|S1\|25P01\|O5.transaction` | 25P01 RELEASE SAVEPOINT can only be used in transaction blocks |
| `g1-s6483-S1-N1_create_policy` | S1 | P1 | agrees | `applied\|S1\|42P01\|N1.create_policy` | 42P01 relation "t1" does not exist |
| `g1-s6498-S1-R4_alter_index_other` | S1 | P1 | agrees | `applied\|S1\|42P01\|R4.alter_index_other` | 42P01 relation "t2_pkey" does not exist |
| `g1-s662-S1-M5k_rename_constraint` | S1 | P4 | agrees | `applied\|S1\|42704\|M5k.rename_constraint` | 42704 constraint "nosuch_constraint" for table "t5" does not exist |
| `g1-s67-S1-N1_create_statistics` | S1 | P5 | agrees | `applied\|S1\|42701\|N1.create_statistics` | 42701 duplicate column name in statistics definition |
| `g1-s6868-S1-M5f_unique_M5k_detach` | S1 | P7 | agrees | `applied\|S1\|42601\|M5f.unique` | 42601 syntax error at or near "DETACH" |
| `g1-s687-S1-M5e_set_default` | S1 | P5 | agrees | `applied\|S1\|42804\|M5e.set_default` | 42804 column "id" is of type bigint but default expression is of type timestamp with time zone |
| `g1-s7-S1-N1_create_statistics` | S1 | P5 | agrees | `applied\|S1\|42703\|N1.create_statistics` | 42703 column "id" does not exist |
| `g1-s7351-K1-M4_name_taken` | K1 | K1 | agrees | `applied\|K1\|42P01\|M4.name_taken` | 42P01 relation "t4" does not exist |
| `g1-s7379-S1-M5k_row_security` | S1 | P1 | agrees | `applied\|S1\|42P01\|M5k.row_security` | 42P01 relation "t1" does not exist |
| `g1-s7433-S1-M5e_set_default_M5b_drop` | S1 | P9 | agrees | `applied\|S1\|42703\|M5e.set_default` | 42703 column "c1" of relation "t9" does not exist |
| `g1-s7589-S1-applied` | S1 | H1 | agrees | `applied\|S1\|42804\|` | 42804 foreign key constraint "t4_c2_fkey" cannot be implemented (E1) |
| `g1-s761-K1-M4_forward_table_unnamed` | K1 | K1 | agrees | `applied\|K1\|42P01\|M4.forward_table_unnamed` | 42P01 relation "t5" does not exist |
| `g1-s762-S1-M4_forward_table_unnamed` | S1 | P7 | agrees | `applied\|S1\|42601\|M4.forward_table_unnamed` | 42601 syntax error at or near "ON" |
| `g1-s7796-S1-M1_create` | S1 | P6 | agrees | `applied\|S1\|42704\|M1.create` | 42704 type "e2" does not exist |
| `g1-s7820-S1-N3_truncate` | S1 | P8 | agrees | `applied\|S1\|0A000\|N3.truncate` | 0A000 cannot truncate a table referenced in a foreign key constraint |
| `g1-s8153-S1-M5k_set_logged` | S1 | P4 | agrees | `applied\|S1\|42P16\|M5k.set_logged` | 42P16 could not change table "t1" to unlogged because it references logged table "t2" |
| `g1-s8178-S1-M5g_generated_M5k_not_of` | S1 | P4 | agrees | `applied\|S1\|42809\|M5g.generated` | 42809 "t2" is not a typed table |
| `g1-s8199-S1-R1_view_columns` | S1 | P1 | agrees | `applied\|S1\|42P01\|R1.view_columns` | 42P01 relation "t1" does not exist |
| `g1-s82-S1-M5k_not_of` | S1 | P4 | agrees | `applied\|S1\|42809\|M5k.not_of` | 42809 "t1" is not a typed table |
| `g1-s8415-S1-table` | S1 | P6 | agrees | `table` | table history.t1 is in the records but not in the catalog (the investigation) |
| `g1-s86-S1-M5k_detach` | S1 | P4 | agrees | `applied\|S1\|42P17\|M5k.detach` | 42P17 table "t2" is not partitioned |
| `g1-s8710-S1-M7_drop` | S1 | P2 | agrees | `applied\|S1\|2BP01\|M7.drop` | 2BP01 cannot drop index t1_pkey because constraint t1_pkey on table ix_4 requires it |
| `g1-s876-S1-M5k_disable_trigger` | S1 | P4 | agrees | `applied\|S1\|42704\|M5k.disable_trigger` | 42704 trigger "nosuch_trigger" for table "t5" does not exist |
| `g1-s8770-S1-N1_lock` | S1 | P1 | agrees | `applied\|S1\|42P01\|N1.lock` | 42P01 relation "t1" does not exist |
| `g1-s92-K1-M1_enum_default` | K1 | K1 | agrees | `applied\|K1\|42P01\|M1.enum_default` | 42P01 relation "t5" does not exist |
| `g1-s9630-S1-M5h_rename_column` | S1 | P1 | agrees | `applied\|S1\|42P01\|M5h.rename_column` | 42P01 relation "t1" does not exist |
| `g1-s9673-S1-M5k_detach_M5k_not_of` | S1 | P7 | agrees | `applied\|S1\|42601\|M5k.detach` | 42601 syntax error at or near "," |
| `g1-s989-S1-M5k_add_identity` | S1 | P4 | agrees | `applied\|S1\|22023\|M5k.add_identity` | 22023 identity column type must be smallint, integer, or bigint |
| `g1-s9891-S1-M4_forward_table` | S1 | P6 | agrees | `applied\|S1\|25001\|M4.forward_table` | 25001 CREATE INDEX CONCURRENTLY cannot run inside a transaction block |
| `g1-s99-S1-M5k_cluster_on` | S1 | P4 | agrees | `applied\|S1\|42704\|M5k.cluster_on` | 42704 index "nosuch_index" for table "t2" does not exist |
| `g2-s193-S1-M4_create_expression_p3` | S1 | P5 | agrees | `applied\|S1\|42P17\|M4.create_expression_p3` | 42P17 functions in index expression must be marked IMMUTABLE |
| `g2-s3021-S1-N1_comment_index_p1_shape` | S1 | P6 | agrees | `applied\|S1\|42P01\|N1.comment_index_p1_shape` | 42P01 relation "t3_c1_key" does not exist |
| `g2-s3756-S1-M5a_add_M5k_not_of` | S1 | P4 | agrees | `applied\|S1\|42809\|M5a.add` | 42809 "t1" is not a typed table |
| `g2-s4162-S1-M5a_add_M5k_alter_constraint` | S1 | P4 | agrees | `applied\|S1\|42704\|M5a.add` | 42704 constraint "nosuch_constraint" of relation "t1" does not exist |
| `g2-s5041-S1-N1_lock_p1_forward` | S1 | P6 | agrees | `applied\|S1\|25P01\|N1.lock_p1_forward` | 25P01 LOCK TABLE can only be used in transaction blocks |
| `g2-s561-S1-N3_analyze` | S1 | P6 | agrees | `applied\|S1\|42P01\|N3.analyze` | 42P01 relation "t3" does not exist |
| `g2-s578-S1-R5_drop_function_p1` | S1 | P4 | agrees | `applied\|S1\|42883\|R5.drop_function_p1` | 42883 could not find a function named "p1" |
| `g2-s6905-S1-M5k_row_security_M5k_cluster_on` | S1 | P4 | agrees | `applied\|S1\|42704\|M5k.row_security` | 42704 index "nosuch_index" for table "t3" does not exist |
| `g2-s7077-S1-M4_generated_shape` | S1 | P6 | agrees | `applied\|S1\|25001\|M4.generated_shape` | 25001 CREATE INDEX CONCURRENTLY cannot run inside a transaction block |
| `g2-s7106-S1-M5c_type_M5k_alter_constraint` | S1 | P4 | agrees | `applied\|S1\|42809\|M5c.type` | 42809 constraint "t1_c4_uq" of relation "t1" is not a foreign key constraint |
| `g2-s7346-S1-M5d_set_not_null_M5b_drop` | S1 | P9 | agrees | `applied\|S1\|42703\|M5d.set_not_null` | 42703 column "id" of relation "t4" does not exist |
| `g2-s8568-S1-M5g_missing_M5k_detach` | S1 | P7 | agrees | `applied\|S1\|42601\|M5g.missing` | 42601 syntax error at or near "DETACH" |
| `g2-s8584-S1-N1_create_trigger` | S1 | P6 | agrees | `applied\|S1\|42P01\|N1.create_trigger` | 42P01 relation "t1" does not exist |
| `g1-s9616-S1-M5c_type` | S1 | P8 | agrees | `applied\|S1\|42804\|M5c.type` | 42804 foreign key constraint "t1_c2_fkey" cannot be implemented |
| `g2-s1123-S1-table` | S1 | P3 | agrees | `table` | table history.t1_pkey is in the records but not in the catalog |
| `g3-s117-S1-M5a_order_p9` | S1 | P9 | agrees | `nullable` | nullable history.t1.d: records nullable true, catalog not null true |
| `g3-s8736-S1-M6_drop` | S1 | P8 | agrees | `applied\|S1\|2BP01\|M6.drop` | 2BP01 cannot drop table t1 because other objects depend on it |
| `g3-s6790-S1-N3_update` | S1 | P1 | agrees | `applied\|S1\|42703\|N3.update` | 42703 column "id" does not exist |
| `g3-s1613-S1-N2_commit` | S1 | P1 | agrees | `applied\|S1\|42703\|N2.commit` | 42703 column "id" does not exist |
| `g3-s6790-S1-applied` | S1 | P1 | agrees | `applied\|S1\|42703\|` | 42703 column "id" does not exist |
| `g5-s80-S1-O1t_trigger_call` | S1 | P1 | agrees | `applied\|S1\|42P01\|O1t.trigger_call` | 42P01 relation "gone4" does not exist |
| `g5-s232-S1-N3_insert_empty_v5` | S1 | P5 | agrees | `applied\|S1\|55000\|N3.insert_empty_v5` | 55000 ON CONFLICT does not support deferrable unique constraints/exclusion constraints as arbiters |
| `g1-s16467-S1-M4_name_taken` | S1 | P2 | agrees | `applied\|S1\|42P07\|M4.name_taken` | 42P07 relation "ix_2" already exists (an index on an outside table holds its name) |
| `g1-s20371-S1-R4_rename_view` | S1 | P4 | agrees | `applied\|S1\|42809\|R4.rename_view` | 42809 "ct1" is not a view (kind not proven after an opaque statement) |
| `g1-s27299-S1-M5a_add` | S1 | P8 | agrees | `applied\|S1\|42P16\|M5a.add` | 42P16 column must be added to child tables too (ALTER TABLE ONLY) |
| `g1-s31160-S1-R4_rename_view` | S1 | P1 | agrees | `applied\|S1\|42P01\|R4.rename_view` | 42P01 relation "v2" does not exist (dropped by DROP TABLE CASCADE) |
| `g1-s37115-S1-M4_name_taken` | S1 | P1 | agrees | `applied\|S1\|42703\|M4.name_taken` | 42703 column "nosuch" does not exist (checked before IF NOT EXISTS) |
| `review-e01-seed-notnull` | S1 | P11 | agrees | `agreed at af6a883` | 23502 column "b" of relation "t" contains null values |
| `review-e02-seed-unique` | S1 | P11 | agrees | `agreed at af6a883` | 23505 could not create unique index "t_a" |
| `review-e03-v5` | S1 | P5 | agrees | `agreed at af6a883` | 42703 column "x" does not exist |
| `review-f01-check-missingcol` | S1 | P5 | agrees | `applied\|S1\|42703\|` | 42703 column "zz" does not exist |
| `review-f02-check-agg` | S1 | P5 | agrees | `applied\|S1\|42803\|` | 42803 aggregate functions are not allowed in check constraints |
| `review-f03-fk-count` | S1 | P4 | agrees | `agreed at af6a883` | 42830 number of referencing and referenced columns for foreign key disagree |
| `review-f04-fk-setnull-cols` | S1 | P4 | agrees | `applied\|S1\|42P10\|` | 42P10 column "y" referenced in ON DELETE SET action must be part of foreign key |
| `review-f05-check-type` | S1 | P5 | agrees | `applied\|S1\|42804\|` | 42804 argument of CHECK must be type boolean, not type text |
| `review-f06-addcheck-missing` | S1 | P5 | agrees | `applied\|S1\|42703\|` | 42703 column "b" does not exist |
| `review-f07-exclude-noop` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-f08-exclude-gist` | S1 | P5 | agrees | `agreed at af6a883` | 42704 data type integer has no default operator class for access method "gist" |
| `review-p01-ident-text` | S1 | P4 | agrees | `applied\|S1\|22023\|` | 22023 identity column type must be smallint, integer, or bigint |
| `review-p02-ident-opt` | S1 | P4 | agrees | `applied\|S1\|22023\|` | 22023 START value (0) cannot be less than MINVALUE (1) |
| `review-p03-do-trigger` | S1 | P5 | agrees | `agreed at af6a883` | 42601 syntax error at or near "b$" |
| `review-p04-evt` | S1 | P5 | agrees | `agreed at af6a883` | 42601 syntax error at or near "b$" |
| `review-p05-rule` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-p06-char` | S1 | P5 | agrees | `agreed at af6a883` | 22001 value too long for type character(1) |
| `review-p07-dupcol` | S1 | P1 | agrees | `agreed at af6a883` | 42701 column "v" specified more than once |
| `review-p08-ondn-target` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-p09-fknull` | S1 | P5 | agrees | `agreed at af6a883` | 23503 insert or update on table "c2" violates foreign key constraint "c2_x_y_fkey" |
| `review-q01-addcol-ident` | S1 | P4 | agrees | `applied\|S1\|22023\|` | 22023 identity column type must be smallint, integer, or bigint |
| `review-q02-nullnotnull` | S1 | P7 | agrees | `agreed at af6a883` | 42601 conflicting NULL/NOT NULL declarations for column "c" of table "t" |
| `review-q03-twodefault` | S1 | P4 | agrees | `applied\|S1\|42601\|` | 42601 multiple default values specified for column "c" of table "t" |
| `review-q04-def-ident` | S1 | P4 | agrees | `applied\|S1\|42601\|` | 42601 both default and identity specified for column "c" of table "t" |
| `review-q05-serial-ident` | S1 | P4 | agrees | `applied\|S1\|42601\|` | 42601 both default and identity specified for column "c" of table "t" |
| `review-q06-uniqdup` | S1 | P4 | agrees | `applied\|S1\|42701\|` | 42701 column "a" appears twice in unique constraint |
| `review-q07-pkdup` | S1 | P4 | agrees | `applied\|S1\|42701\|` | 42701 column "a" appears twice in primary key constraint |
| `review-q08-collate-int` | S1 | P4 | agrees | `applied\|S1\|42804\|` | 42804 collations are not supported by type integer |
| `review-q09-varchar0` | S1 | P5 | agrees | `applied\|S1\|22023\|` | 22023 length for type varchar must be at least 1 |
| `review-q10-numeric0` | S1 | P5 | agrees | `applied\|S1\|22023\|` | 22023 NUMERIC precision 0 must be between 1 and 1000 |
| `review-q11-int11` | S1 | P7 | agrees | `agreed at af6a883` | 42601 syntax error at or near "(" |
| `review-q12-gen-def` | S1 | P4 | agrees | `applied\|S1\|42601\|` | 42601 both default and generation expression specified for column "b" of table "t" |
| `review-q13-twopk` | S1 | P4 | agrees | `applied\|S1\|42P16\|` | 42P16 multiple primary keys for table "t" are not allowed |
| `review-q14-ident-null` | S1 | P7 | agrees | `agreed at af6a883` | 42601 conflicting NULL/NOT NULL declarations for column "a" of table "t" |
| `review-q15-numscale` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-q16-twoident` | S1 | P4 | agrees | `applied\|S1\|42601\|` | 42601 multiple identity specifications for column "c" of table "t" |
| `review-q17-gen-ref-gen` | S1 | P5 | agrees | `agreed at af6a883` | 42P17 cannot use generated column "b" in column generation expression |
| `review-q18-gen-pk` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-q19-collate-bad` | S1 | P4 | agrees | `agreed at af6a883` | 42704 collation "nonexistent_coll" for encoding "UTF8" does not exist |
| `review-q20-check-sub` | S1 | P5 | agrees | `applied\|S1\|0A000\|` | 0A000 cannot use subquery in check constraint |
| `review-q21-def-col` | S1 | P5 | agrees | `agreed at af6a883` | 0A000 cannot use column reference in DEFAULT expression |
| `review-q22-bitvar` | S1 | P5 | agrees | `applied\|S1\|22023\|` | 22023 length for type bit must be at least 1 |
| `review-q23-char0` | S1 | P5 | agrees | `applied\|S1\|22023\|` | 22023 length for type char must be at least 1 |
| `review-q24-ts7` | S1 | P5 | agrees | `type` | accepted |
| `review-r01-match-partial` | S1 | P4 | agrees | `applied\|S1\|0A000\|` | 0A000 MATCH PARTIAL not yet implemented |
| `review-r02-fk-text-char` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-r03-fk-num-int` | S1 | P5 | agrees | `agreed at af6a883` | 42804 foreign key constraint "c_a_fkey" cannot be implemented |
| `review-r04-fk-vc-char` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-r05-conname-dup` | S1 | P2 | agrees | `applied\|S1\|42710\|` | 42710 check constraint "x" already exists |
| `review-r06-conname-fkpk` | S1 | P2 | agrees | `applied\|S1\|42710\|` | 42710 constraint "x" for relation "t" already exists |
| `review-r07-addcheck-dup` | S1 | P2 | agrees | `applied\|S1\|42710\|` | 42710 constraint "x" for relation "t" already exists |
| `review-r08-rename-con` | S1 | P2 | agrees | `agreed at af6a883` | 42710 constraint "x" for relation "t" already exists |
| `review-r09-drop-con-fk` | S1 | P2 | agrees | `agreed at af6a883` | 2BP01 cannot drop constraint pk on table p because other objects depend on it |
| `review-r10-dropcon-notnull` | S1 | P2 | agrees | `agreed at af6a883` | 42704 constraint "nn" of relation "t" does not exist |
| `review-r11-ins-trunc` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-r12-ins-empty-cast` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-r13-fk-int8-int2` | S1 | P5 | agrees | `agreed at af6a883` | accepted |
| `review-r14-pk-dup-name-idx` | S1 | P2 | agrees | `agreed at af6a883` | accepted |
| `review-r15-ins-gen-ident-explicit` | S1 | P5 | agrees | `agreed at af6a883` | 428C9 cannot insert a non-DEFAULT value into column "id" |
| `review-r16-insert-domain` | S1 | P5 | agrees | `agreed at af6a883` | 23514 value for domain d violates check constraint "d_check" |
| `review-r17-ins-omit-domdef` | S1 | P5 | agrees | `agreed at af6a883` | 23502 domain d does not allow null values |
| `review-s01-addfk-dup` | S1 | P2 | agrees | `applied\|S1\|42710\|` | 42710 constraint "x" for relation "t" already exists |
| `review-s02-adduniq-checkname` | S1 | P2 | agrees | `applied\|S1\|42710\|` | 42710 constraint "x" for relation "t" already exists |
| `review-s03-renidx-checkname` | S1 | P2 | agrees | `agreed at af6a883` | 42710 constraint "x" for relation "t" already exists |
| `review-s04-addcol-condup` | S1 | P2 | agrees | `applied\|S1\|42710\|` | 42710 constraint "x" for relation "t" already exists |
| `review-s05-gen-checkname` | S1 | P2 | agrees | `applied\|S1\|42710\|` | 42710 check constraint "t_a_check" already exists |
| `review-s06-two-unnamed` | S1 | P2 | agrees | `agreed at af6a883` | accepted |
| `g6-s1525-S1-M5f_generated_name_r2` | S1 | P2 | agrees | `applied\|S1\|42710\|M5f.generated_name_r2` | 42710 constraint "t2_check" for relation "t2" already exists |
| `g6-s506-S1-M1_table_options_r3` | S1 | P7 | agrees | `applied\|S1\|42601\|M1.table_options_r3` | 42601 syntax error at or near ")" |
| `g1-s52007-S1-N1_create_policy` | S1 | P2 | agrees | `applied\|S1\|42710\|N1.create_policy` | 42710 policy "pol" for table "ext_a" already exists |
| `g2-s65990-S1-M4_generated_shape` | S1 | P2 | agrees | `applied\|S1` | 42P07 relation "ext_a_code_idx" already exists |
| `g3-s59521-S1-M5f_check` | S1 | P8 | agrees | `applied\|S1\|42P16\|M5f.check` | 42P16 constraint must be added to child tables too |
| `g3-s43863-S1-applied` | S1 | H10 | agrees | `applied\|S1\|42804\|` | 42804 foreign key constraint "t4_c1_fkey" cannot be implemented (E1 since H10) |
| `g6-s38420-S1-M5a_add_p1_shape` | S1 | P4 | agrees | `applied\|S1\|42809\|M5a.add_p1_shape` | 42809 ALTER action ADD COLUMN cannot be performed on relation "t2_c3_key" |
| `g6-s48802-S1-M5c_type` | S1 | P8 | agrees | `applied\|S1\|0A000\|M5c.type` | 0A000 cannot alter type of a column used in a trigger definition |
| `g6-s69809-S1-M8_rename` | S1 | P1 | agrees | `applied\|S1\|42P01\|M8.rename` | 42P01 relation "t2_pkey" does not exist |
| `g4-s44332-S1-index` | S1 | H11 | agrees | `index` | accepted; catalog indexes t1_a_idx (a) and t1_coalesce_idx (expression) |
| `g1-s56780-S1-type` | S1 | P3 | agrees | `type` | accepted; t3.c1 stays smallint (the inherited column; the definition is skipped) |
| `review15-a14-reloption-bad` | S1 | U9 | agrees | `agreed at Revision 1.5` | 22023 value 5 out of bounds for option "fillfactor" |
| `review15-a15-reloption-unknown` | S1 | U9 | agrees | `agreed at Revision 1.5` | 22023 unrecognized parameter "no_such_option" |
| `review15-a16-reloption-partitioned` | S1 | U9 | agrees | `agreed at Revision 1.5` | 42809 cannot specify storage parameters for a partitioned table |
| `review15-a29-sysname-create` | S1 | U10 | agrees | `agreed at Revision 1.5` | 42701 column name "xmin" conflicts with a system column name |
| `review15-a30-sysname-add` | S1 | U10 | agrees | `agreed at Revision 1.5` | 42701 column name "cmax" conflicts with a system column name |
| `review15-a31-sysname-rename` | S1 | U10 | agrees | `agreed at Revision 1.5` | 42701 column name "ctid" conflicts with a system column name |
| `review15-a38-seq-minmax` | S1 | U5 | agrees | `agreed at Revision 1.5` | 22023 MINVALUE (10) must be less than MAXVALUE (5) |
| `review15-a39-seq-start0` | S1 | U5 | agrees | `agreed at Revision 1.5` | 22023 START value (0) cannot be less than MINVALUE (1) |
| `review15-a40-seq-inc0` | S1 | U5 | agrees | `agreed at Revision 1.5` | 22023 INCREMENT must not be zero |
| `review15-a41-seq-astext` | S1 | U5 | agrees | `agreed at Revision 1.5` | 22023 sequence type must be smallint, integer, or bigint |
| `review15-a42-seq-ownedby-missing` | S1 | U5 | agrees | `agreed at Revision 1.5` | 42703 column "zz" of relation "t" does not exist |
| `review15-a43-alterseq-bad` | S1 | U5 | agrees | `agreed at Revision 1.5` | 22023 MINVALUE (1) must be less than MAXVALUE (0) |
| `review15-a45-alterseq-ownedby` | S1 | U5 | agrees | `agreed at Revision 1.5` | 42703 column "zz" of relation "t" does not exist |
| `review15-b07-set-unknown-guc` | S1 | U7 | agrees | `agreed at Revision 1.5` | 42704 unrecognized configuration parameter "no_such_setting" |
| `review15-b08-set-bad-value` | S1 | U7 | agrees | `agreed at Revision 1.5` | 22023 invalid value for parameter "work_mem": "abc" |
| `review15-b09-set-with-oids` | S1 | U7 | agrees | `agreed at Revision 1.5` | 0A000 tables declared WITH OIDS are not supported |
| `review15-b19-set-searchpath-session` | S1 | U3 | agrees | `agreed at Revision 1.5` | 42P01 sqlx bookkeeping: relation "_sqlx_migrations" does not exist |
| `review15-b20-set-searchpath-session-next` | S1 | U3 | agrees | `agreed at Revision 1.5` | 42P01 sqlx bookkeeping: relation "_sqlx_migrations" does not exist |
| `review15-b21-set-searchpath-public` | S1 | U3 | agrees | `agreed at Revision 1.5` | accepted |
| `review15-d01-replace-func-rettype` | S1 | U6 | agrees | `agreed at Revision 1.5` | 42P13 cannot change return type of existing function |
| `review15-e04-part-addpk` | S1 | U4 | agrees | `agreed at Revision 1.5` | 0A000 unique constraint on partitioned table must include all partitioning columns |
| `review15-e05-part-uniqueindex` | S1 | U4 | agrees | `agreed at Revision 1.5` | 0A000 unique constraint on partitioned table must include all partitioning columns |
| `review15-e10-part-addcol-unique` | S1 | U4 | agrees | `agreed at Revision 1.5` | 0A000 unique constraint on partitioned table must include all partitioning columns |
| `review15-e11-part-attach-temp` | S1 | U4 | agrees | `agreed at Revision 1.5` | 0A000 unique constraint on partitioned table must include all partitioning columns |
| `review15-f16-col-unknown-type` | S1 | U7 | agrees | `agreed at Revision 1.5` | 42704 type "datetime" does not exist |
| `review15-h11-reserved-colname` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "authorization" |
| `review15-h12-reserved-colname2` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "left" |
| `review15-h13-reserved-tablename` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "collation" |
| `review15-h16-tablename-concurrently` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "concurrently" |
| `review15-h17-reserved-order` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "order" |
| `review15-h18-reserved-user` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "user" |
| `review15-h19-typefunc-addcol` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "natural" |
| `review15-h20-typefunc-rename` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "similar" |
| `review15-h21-typefunc-index` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "inner" |
| `review15-h22-typefunc-type` | S1 | U8 | agrees | `agreed at Revision 1.5` | 42601 syntax error at or near "verbose" |
| `review15-l01-index-json-btree` | S1 | U7 | agrees | `agreed at Revision 1.5` | 42704 data type json has no default operator class for access method "btree" |
| `review15-l12-index-point-btree` | S1 | U7 | agrees | `agreed at Revision 1.5` | 42704 data type point has no default operator class for access method "btree" |
| `review15-l13-unique-xml` | S1 | U7 | agrees | `agreed at Revision 1.5` | 42704 data type xml has no default operator class for access method "btree" |
| `review15-l14-pk-json` | S1 | U7 | agrees | `agreed at Revision 1.5` | 42704 data type json has no default operator class for access method "btree" |
| `review15-s18-pk-using-index` | S1 | U1 | agrees | `agreed at Revision 1.5` | accepted |
| `review15-s21-unique-using-index` | S1 | U1 | agrees | `agreed at Revision 1.5` | accepted |
| `review15-s22-pk-using-index-multi-mig` | S1 | U1 | agrees | `agreed at Revision 1.5` | accepted |
| `review15-s23-using-index-nonunique` | S1 | U1 | agrees | `agreed at Revision 1.5` | 42809 "t_a" is not a unique index |
| `review16-g30-names` | S1 | V10 | agrees | `agreed at Revision 1.6` | 22023 invalid value for parameter "client_encoding": "nosuch_enc" |
| `review16-h01-outside-index-rename` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_idx" does not exist |
| `review16-h05-alter-index-outside-set` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_idx" does not exist |
| `review16-j01-role-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 unrecognized role option "garbage" |
| `review16-j02-grant-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j03-adp-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j07-comment-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j10-trigger-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j11-policy-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j13-enum-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j14-ext-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j15-lock-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j16-notify-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j20-revoke-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j21-alter-role-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j22-create-schema-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j24-comment-col-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j25-alter-func-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j26-drop-func-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j29-grant-role-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j30-collation-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j36-begin-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j37-addvalue-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j42-droptype-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j44-reset-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j45-dropview-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j47-altertype-rename-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j48-dropext-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-j49-commit-junk` | S1 | V7 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "garbage" |
| `review16-k14-index-col-left` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near ")" |
| `review16-k50-index-col-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-k51-index-col-user` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42P17 functions in index expression must be marked IMMUTABLE |
| `review16-k52-index-col-similar` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near ")" |
| `review16-k53-index-col-inner-unique` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near ")" |
| `review16-k57-index-col-verbose-desc` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "DESC" |
| `review16-k58-partition-by-left` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near ")" |
| `review16-k59-index-col-current-user` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42P17 functions in index expression must be marked IMMUTABLE |
| `review16-o01-int4ops-bigint` | S1 | V9 | agrees | `agreed at Revision 1.6` | 42804 operator class "int4_ops" does not accept data type bigint |
| `review16-o05-pattern-ops-int-unique` | S1 | V9 | agrees | `agreed at Revision 1.6` | 42804 operator class "text_pattern_ops" does not accept data type integer |
| `review16-p23-do-serial-seq` | S1 | V8 | agrees | `agreed at Revision 1.6` | 42P07 relation "x_id_seq" already exists |
| `review16-p50-do-serial-dropseq` | S1 | V8 | agrees | `agreed at Revision 1.6` | 2BP01 cannot drop sequence x_id_seq because other objects depend on it |
| `review16-p51-do-serial-createtable-seqname` | S1 | V8 | agrees | `agreed at Revision 1.6` | 42P07 relation "x_id_seq" already exists |
| `review16-p52-do-pk-index-name` | S1 | V8 | agrees | `agreed at Revision 1.6` | 42P07 relation "x_pkey" already exists |
| `review16-p53-do-identity-seq` | S1 | V8 | agrees | `agreed at Revision 1.6` | 42P07 relation "x_id_seq" already exists |
| `review16-p56-do-bigserial-unique-names` | S1 | V8 | agrees | `agreed at Revision 1.6` | 42P07 relation "x_e_key" already exists |
| `review16-p57-do-serial-in-guard` | S1 | V8 | agrees | `agreed at Revision 1.6` | 42P07 relation "x_id_seq" already exists |
| `review16-p58-func-serial` | S1 | V8 | agrees | `agreed at Revision 1.6` | 42P07 relation "x_id_seq" already exists |
| `review16-p59-do-create-table-like-names` | S1 | V8 | agrees | `agreed at Revision 1.6` | 42P07 relation "x_b_key" already exists |
| `review16-p70-do-insert-null-then-setnotnull` | S1 | V5 | agrees | `agreed at Revision 1.6` | 23502 column "a" of relation "t" contains null values |
| `review16-p72-do-insert-dup-then-unique` | S1 | V5 | agrees | `agreed at Revision 1.6` | 23505 could not create unique index "t_a" |
| `review16-q07-user-schema` | S1 | V6 | agrees | `agreed at Revision 1.6` | accepted (the "$user" entry: the unqualified table lands in the schema named like the role) |
| `review16-r36-plpgsql-body-syntax-error` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42601 syntax error at end of input |
| `review16-r40-plpgsql-unknown-decl-type` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42704 type "nosuchtype" does not exist |
| `review16-r41-plpgsql-return-value-void` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42804 RETURN cannot have a parameter in function returning void |
| `review16-r42-trigger-func-args` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42P13 trigger functions cannot have declared arguments |
| `review16-r43-language-missing` | S1 | V2 | agrees | `agreed at Revision 1.6` | 42704 language "plperl" does not exist |
| `review16-r44-plpgsql-unterminated-if` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near ";" |
| `review16-r45-plpgsql-missing-semicolon-trigger` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "NEW" |
| `review16-r46-proc-plpgsql-syntax` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "SELEC" |
| `review16-r47-do-syntax-error` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42804 RETURN cannot have a parameter in function returning void |
| `review16-r48-plpgsql-undeclared-var-assign` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42601 "y" is not a known variable |
| `review16-r49-plpgsql-rowtype-unknown` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch" does not exist |
| `review16-s01-idx-expr-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s02-idx-where-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s03-check-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s04-include-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s05-unique-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s06-gen-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s07-idx-colname-kw` | S1 | V3 | agrees | `agreed at Revision 1.6` | accepted |
| `review16-s08-part-list-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s09-default-user` | S1 | V3 | agrees | `agreed at Revision 1.6` | accepted |
| `review16-s10-idx-opclass-kw` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s11-fk-cols-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s12-exclude-order` | S1 | V3 | agrees | `agreed at Revision 1.6` | 42601 syntax error at or near "order" |
| `review16-s21-top-insert-then-fk` | S1 | V5 | agrees | `agreed at Revision 1.6` | 23503 insert or update on table "c" violates foreign key constraint "c_pid_fkey" |
| `review16-s23-trigger-insert-then-unique` | S1 | V5 | agrees | `agreed at Revision 1.6` | 23505 could not create unique index "l_a" |
| `review16-s24-do-execute-insert` | S1 | V5 | agrees | `agreed at Revision 1.6` | 23502 column "a" of relation "t" contains null values |
| `review16-t08-composite-serial` | S1 | V13 | agrees | `agreed at Revision 1.6` | 42704 type "serial" does not exist |
| `review16-t22-col-regtype-unknown` | S1 | V13 | agrees | `agreed at Revision 1.6` | 42P16 column "a" has pseudo-type unknown |
| `review16-t44-opclass-wrong-type` | S1 | V9 | agrees | `agreed at Revision 1.6` | 42804 operator class "text_ops" does not accept data type integer |
| `review16-w25-set-on-view` | S1 | V12 | agrees | `agreed at Revision 1.6` | 22023 unrecognized parameter "fillfactor" |
| `review16-w26-view-options` | S1 | V12 | agrees | `agreed at Revision 1.6` | 22023 invalid value for boolean option "security_barrier": maybe |
| `review16-w27-matview-with` | S1 | V12 | agrees | `agreed at Revision 1.6` | 22023 value 5 out of bounds for option "fillfactor" |
| `review16-x06-sys-col-matview` | S1 | V11 | agrees | `agreed at Revision 1.6` | 42701 column name "cmax" conflicts with a system column name |
| `review16-x07-sys-col-ctas` | S1 | V11 | agrees | `agreed at Revision 1.6` | 42701 column name "tableoid" conflicts with a system column name |
| `review16-x09-sys-col-partition-of` | S1 | V11 | agrees | `agreed at Revision 1.6` | 42703 column "ctid" does not exist |
| `review16-x10-sys-col-like` | S1 | V11 | agrees | `agreed at Revision 1.6` | 42701 column name "cmin" conflicts with a system column name |
| `review16-x11-sys-col-select-into` | S1 | V11 | agrees | `agreed at Revision 1.6` | 42701 column name "xmin" conflicts with a system column name |
| `review16-x12-sys-col-typed-table` | S1 | V11 | agrees | `agreed at Revision 1.6` | 42701 column name "ctid" conflicts with a system column name |
| `review16-x13-sys-col-alter-type-add-attr` | S1 | V11 | agrees | `agreed at Revision 1.6` | 42701 column name "xmin" conflicts with a system column name |
| `review16-x15-sys-col-inherits` | S1 | V11 | agrees | `agreed at Revision 1.6` | 42701 column name "xmin" conflicts with a system column name |
| `review16-y01-sql-rettype` | S1 | H14 | agrees | `agreed at Revision 1.6` | 42P13 return type mismatch in function declared to return integer |
| `review16-y08-plpgsql-setof-return` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42804 RETURN cannot have a parameter in function returning set |
| `review16-y09-plpgsql-cstring` | S1 | V1 | agrees | `agreed at Revision 1.6` | 0A000 PL/pgSQL functions cannot return type cstring |
| `review16-y10-lang-quoted-upper` | S1 | V2 | agrees | `agreed at Revision 1.6` | 42704 language "PLPGSQL" does not exist |
| `review16-y11-plpgsql-raise-count` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42601 too many parameters specified for RAISE |
| `review16-y12-plpgsql-exc-unknown` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42704 unrecognized exception condition "no_such_condition" |
| `review16-y13-plpgsql-label` | S1 | V1 | agrees | `agreed at Revision 1.6` | 42601 end label "m" differs from block's label "l" |
| `review16-y16-call-commit` | S1 | H14 | agrees | `agreed at Revision 1.6` | 2D000 invalid transaction termination |
| `review16-y17-do-commit` | S1 | H14 | agrees | `agreed at Revision 1.6` | 2D000 invalid transaction termination |
| `review16-y19-q07-qualified` | S1 | V6 | agrees | `agreed at Revision 1.6` | accepted (the "$user" entry: the unqualified table lands in the schema named like the role) |
| `review16-y22-alter-seq-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_seq" does not exist |
| `review16-y23-alter-view-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_v" does not exist |
| `review16-y24-comment-index-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_idx" does not exist |
| `review16-y25-drop-seq-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 sequence "nosuch_seq" does not exist |
| `review16-y27-reindex-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_idx" does not exist |
| `review16-y28-alter-matview-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_mv" does not exist |
| `review16-y31-drop-view-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 view "nosuch_v" does not exist |
| `review16-y32-drop-matview-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 materialized view "nosuch_mv" does not exist |
| `review16-y33-grant-seq-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_seq" does not exist |
| `review16-y34-comment-seq-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_seq" does not exist |
| `review16-y35-comment-view-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_v" does not exist |
| `review16-y37-refresh-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_mv" does not exist |
| `review16-y38-drop-trigger-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_t" does not exist |
| `review16-y39-drop-index-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42704 index "nosuch_idx" does not exist |
| `review16-y42-alter-index-attach` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_a" does not exist |
| `review16-y46-alter-view-col-outside` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_v" does not exist |
| `review16-y50-alter-index-setstat` | S1 | V4 | agrees | `agreed at Revision 1.6` | 42P01 relation "nosuch_idx" does not exist |
| `review16-z06-do-insert-then-fk` | S1 | V5 | agrees | `agreed at Revision 1.6` | 23503 insert or update on table "c" violates foreign key constraint "c_pid_fkey" |
| `review17-d01-alter-database-nosuch` | S1 | W4 | agrees | `agreed at Revision 1.7` | 3D000 database "nosuch_db" does not exist |
| `review17-d02-grant-on-database-nosuch` | S1 | W4 | agrees | `agreed at Revision 1.7` | 3D000 database "nosuch_db" does not exist |
| `review17-d03-comment-database-nosuch` | S1 | W4 | agrees | `agreed at Revision 1.7` | accepted |
| `review17-d04-alter-role-in-db-nosuch` | S1 | W4 | agrees | `agreed at Revision 1.7` | 3D000 database "nosuch_db" does not exist |
| `review17-d05-revoke-database-nosuch` | S1 | W4 | agrees | `agreed at Revision 1.7` | 3D000 database "nosuch_db" does not exist |
| `review17-d12-drop-trigger-missing-settable` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "nosuch" for table "t" does not exist |
| `review17-d13-drop-policy-missing` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 policy "nosuch" for table "t" does not exist |
| `review17-d14-drop-rule-missing` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 rule "nosuch" for relation "t" does not exist |
| `review17-d15-alter-trigger-missing` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "nosuch" for table "t" does not exist |
| `review17-d16-alter-policy-missing` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 policy "nosuch" for table "t" does not exist |
| `review17-d17-comment-policy-missing` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 policy "nosuch" for table "t" does not exist |
| `review17-d18-comment-rule-missing` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 rule "nosuch" for relation "t" does not exist |
| `review17-d35-drop-trigger-of-other-table` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "tr" for table "u" does not exist |
| `review17-d36-comment-trigger-other-table` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "tr" for table "u" does not exist |
| `review17-h01-axum-trigger-helper-typo` | S1 | W2 | agrees | `agreed at Revision 1.7` | 42P01 relation "userz" does not exist |
| `review17-h04-axum-trigger-helper-ok-then-drop-wrongname` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "set_updated_at_users" for table "users" does not exist |
| `review17-h13-regclass-plpgsql-arg-typo` | S1 | W2 | agrees | `agreed at Revision 1.7` | 42P01 relation "userz" does not exist |
| `review17-m43-enum-uescape-label` | S1 | W7 | agrees | `agreed at Revision 1.7` | 42710 enum label "a" already exists |
| `review17-n09-drop-statistics-missing` | S1 | W5 | agrees | `agreed at Revision 1.7` | 42704 statistics object "nosuch" does not exist |
| `review17-n10-drop-publication-missing` | S1 | W5 | agrees | `agreed at Revision 1.7` | 42704 publication "nosuch" does not exist |
| `review17-n11-drop-collation-missing` | S1 | W5 | agrees | `agreed at Revision 1.7` | 42704 collation "nosuch" for encoding "UTF8" does not exist |
| `review17-n12-drop-cast-missing` | S1 | W5 | agrees | `agreed at Revision 1.7` | 2BP01 cannot drop cast from integer to money because it is required by the database system |
| `review17-n13-drop-operator-missing` | S1 | W5 | agrees | `agreed at Revision 1.7` | 42883 operator does not exist: integer === integer |
| `review17-n14-drop-aggregate-missing` | S1 | W5 | agrees | `agreed at Revision 1.7` | 42883 aggregate nosuch(integer) does not exist |
| `review17-n15-drop-trigger-held-other-name-in-mig2` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "tr_b" for table "t" does not exist |
| `review17-n16-drop-trigger-twice` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "tr" for table "t" does not exist |
| `review17-n17-drop-policy-twice` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 policy "p" for table "t" does not exist |
| `review17-n18-drop-trigger-after-rename-table` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "tr" for table "t" does not exist |
| `review17-n19-drop-trigger-after-droptable-recreate` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "tr" for table "t" does not exist |
| `review17-n21-drop-policy-after-renamed` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 policy "p" for table "t" does not exist |
| `review17-n23-cratesio-shape-drop-trigger` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "trigger_crate_set_updated_at" for table "crates" does not exist |
| `review17-q14-cfb-off-sqlfunc-bad-then-call` | S1 | W3 | agrees | `agreed at Revision 1.7` | 42601 syntax error at end of input |
| `review17-q42-cfb-off-sqlfunc-missing-table-call` | S1 | W3 | agrees | `agreed at Revision 1.7` | 42P01 relation "nosuch_tbl" does not exist |
| `review17-v063-comment-trigger-missing` | S1 | W1 | agrees | `agreed at Revision 1.7` | 42704 trigger "nosuch" for table "t" does not exist |
| `review17-w11-code-insert-inh-child` | S1 | W6 | agrees | `agreed at Revision 1.7` | 23502 column "b" of relation "c" contains null values |
| `review17-w19-code-insert-child-partition-parent-notnull` | S1 | W6 | agrees | `agreed at Revision 1.7` | 23502 column "b" of relation "p1" contains null values |
| `review17-x10-enum-escape-label` | S1 | W7 | agrees | `agreed at Revision 1.7` | 42710 enum label "ab" already exists |
| `review18-a28-dropfunc-before-trigger` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function user_history_count() because other objects depend on it |
| `review18-b01-dropfunc-noifexists-trigger` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function f() because other objects depend on it |
| `review18-c02-droptype-func-ret` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type st because other objects depend on it |
| `review18-c04-droptype-composite-attr` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type st because other objects depend on it |
| `review18-c09-dropfunc-trigger-by-code` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function set_updated_at() because other objects depend on it |
| `review18-c10-dropfunc-eventtrigger` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function et() because other objects depend on it |
| `review18-c11-dropfunc-procedure-trigger-replace` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function f() because other objects depend on it |
| `review18-c22-dropfunc-routine-dep-proc` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function g() because other objects depend on it |
| `review18-c23-droptable-atomic-proc-dep` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop table t because other objects depend on it |
| `review18-c27-dropfunc-trigger-diesel` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function diesel_set_updated_at() because other objects depend on it |
| `review18-f18-createtable-ifnotexists-name-is-view` | S1 | X3 | agrees | `agreed at Revision 1.8` | 42809 cannot create index on relation "v" |
| `review18-g04-fk-to-view` | S1 | X3 | agrees | `agreed at Revision 1.8` | 42809 referenced relation "v" is not a table |
| `review18-g05-index-on-type-name` | S1 | X3 | agrees | `agreed at Revision 1.8` | 42809 cannot open relation "ty" |
| `review18-g06-index-on-matview-missingcol` | S1 | X3 | agrees | `agreed at Revision 1.8` | 42703 column "b" does not exist |
| `review18-g08-createtable-ifne-over-matview-index` | S1 | X3 | agrees | `agreed at Revision 1.8` | 42703 column "b" does not exist |
| `review18-g11-createtable-ifne-over-view-cross` | S1 | X3 | agrees | `agreed at Revision 1.8` | 42809 cannot create index on relation "v" |
| `review18-h07-droptype-func-ret-plpgsql` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type status because other objects depend on it |
| `review18-h08-droptype-ifexists-func-ret` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type status because other objects depend on it |
| `review18-h09-droptype-cascade-func-ret-then-call` | S1 | X1 | agrees | `agreed at Revision 1.8` | 42883 function default_status() does not exist |
| `review18-h11-droptype-proc-arg` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type st because other objects depend on it |
| `review18-h14-droptable-composite-attr-rowtype` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop table t because other objects depend on it |
| `review18-h15-droptype-in-array-attr` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type st because other objects depend on it |
| `review18-j01-selectinto-forward-insert` | S1 | H20 | agrees | `agreed at Revision 1.8` | 42P01 relation "foo" does not exist |
| `review18-j02-selectinto-forward-index` | S1 | H20 | agrees | `agreed at Revision 1.8` | 42P01 relation "foo" does not exist |
| `review18-j03-schema-authorization-forward` | S1 | X4 | agrees | `agreed at Revision 1.8` | 3F000 schema "r4auth" does not exist |
| `review18-j04-schema-authorization-dropped` | S1 | X4 | agrees | `agreed at Revision 1.8` | 3F000 schema "r4auth" does not exist |
| `review18-l06-dropfunc-domain-default` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function f() because other objects depend on it |
| `review18-l19-dropfunc-default-after-funcrename` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function g() because other objects depend on it |
| `review18-l20-droptype-col-after-typerename` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type st2 because other objects depend on it |
| `review18-l21-droptrigfunc-after-funcrename` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop function g() because other objects depend on it |
| `review18-m01-enum-swap-forgot-col-noalter` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type status_old because other objects depend on it |
| `review18-m03-enum-swap-ifexists` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type status_old because other objects depend on it |
| `review18-m04-domain-rename-drop` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type email_old because other objects depend on it |
| `review18-m05-rename-type-crossmig-drop` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop type status_old because other objects depend on it |
| `review18-m12-rename-table-rowtype-col-drop` | S1 | X1 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop table t2 because other objects depend on it |
| `review18-m13-rename-collation-drop` | S1 | X2 | agrees | `agreed at Revision 1.8` | 2BP01 cannot drop collation ci2 because other objects depend on it |
| `review18-n13-w5-drop-collation-renamed` | S1 | X2 | agrees | `agreed at Revision 1.8` | 42704 collation "ci" for encoding "UTF8" does not exist |
| `review18-o05-comment-collation-renamed` | S1 | X2 | agrees | `agreed at Revision 1.8` | 42704 collation "ci" for encoding "UTF8" does not exist |
| `review18-o19-alter-collation-renamed-old` | S1 | X2 | agrees | `agreed at Revision 1.8` | 42704 collation "ci" for encoding "UTF8" does not exist |
| `review18-o20-col-collate-renamed` | S1 | X2 | agrees | `agreed at Revision 1.8` | 42704 collation "ci" for encoding "UTF8" does not exist |
| `review18-o25-index-collate-renamed` | S1 | X2 | agrees | `agreed at Revision 1.8` | 42704 collation "ci" for encoding "UTF8" does not exist |
| `g8-s113-S1-N1_object_kinds_v4` | S1 | V7 | agrees | `applied\|S1\|0A000\|N1.object_kinds_v4` | 0A000 cannot alter statistics on non-expression column (ALTER INDEX ... ALTER COLUMN 1 SET STATISTICS: not an M8 form, row D) |
| `g8-s1038-S1-N2_names_v10` | S1 | V10 | agrees | `applied\|S1\|42601\|N2.names_v10` | 42601 syntax error at or near "UTF8" (SET NAMES takes a string constant) |
| `g8-s1052-S1-R4_system_column_v11` | S1 | V11 | agrees | `applied\|S1\|2BP01\|R4.system_column_v11` | 2BP01 cannot alter type because it is the type of a typed table |
| `g8-s4299-S1-N1_object_kinds_v4` | S1 | V4 | agrees | `applied\|S1\|42809\|N1.object_kinds_v4` | 42809 "v23" is not an index (ALTER INDEX IF EXISTS of a name held as a view: IF EXISTS skips only a name pg_class does not hold) |
| `g8-s983-S1-R4_relkinds_v4` | S1 | V4 | agrees | `applied\|S1\|42704\|R4.relkinds_v4` | 42704 type "nosuch9" does not exist (ALTER DOMAIN ... DROP CONSTRAINT IF EXISTS of a type not held) |
| `g8-s1212-S1-R4_relkinds_v4` | S1 | V4 | agrees | `applied\|S1\|42P01\|R4.relkinds_v4` | 42P01 relation "nosuch12" does not exist (ALTER TYPE ... ALTER ATTRIBUTE of a type not held) |
| `g8-s2796-S1-M4_opclass_v9` | S1 | V9 | agrees | `applied\|S1\|0A000\|M4.opclass_v9` | 0A000 access method "gin" does not support unique indexes (only btree has amcanunique; P4) |
| `g8-s270-S1-R6_compile_v1` | S1 | H14 | agrees | `agreed at Revision 1.6` | 22P02 invalid input syntax for type integer: "f" (raised under the SPI context `PL/pgSQL assignment "..."`, an execution frame: H14) |
| `g7-s1811-S1-M5f-u_using_index_u1` | S1 | P7 | agrees | `applied\|S1\|42601\|M5f-u.using_index_u1` | 42601 syntax error at or near "TO" |
| `g7-s267-S1-R1_sequence_options_u5` | S1 | P4 | agrees | `applied\|S1\|0A000\|R1.sequence_options_u5` | 0A000 cannot change ownership of identity sequence |
| `g9-s329-S1-N1_droprole_y38` | S1 | Y38 | agrees | `applied\|S1\|2BP01\|N1.droprole_y38` | 2BP01 role "r1" cannot be dropped because some objects depend on it (GRANT ALL; found by g9 while U-b ran) |
| `review19-inv-x01-ro-only` | S1 | Y03 | agrees | `agreed at Revision 1.9` | 25006 sqlx bookkeeping: cannot execute INSERT in a read-only transaction |
| `review19-r1-a21-altertype-identity-numeric` | S1 | Y01 | agrees | `agreed at Revision 1.9` | 22023 identity column type must be smallint, integer, or bigint |
| `review19-r1-a24-altertype-rowtype` | S1 | Y02 | agrees | `agreed at Revision 1.9` | 0A000 cannot alter table "t" because column "u.x" uses its row type |
| `review19-r1-b01-set-txn-readonly` | S1 | Y03 | agrees | `agreed at Revision 1.9` | 25006 cannot execute CREATE TABLE in a read-only transaction |
| `review19-r1-b03-set-iso-after-query` | S1 | Y03 | agrees | `agreed at Revision 1.9` | 25001 SET TRANSACTION ISOLATION LEVEL must be called before any query |
| `review19-r1-b05-default-readonly` | S1 | Y03 | agrees | `agreed at Revision 1.9` | 1_a.sql applied; sqlx's post-commit UPDATE: 25006 cannot execute UPDATE in a read-only transaction (H24), 2_b.sql not run |
| `review19-r1-b12-set-constraints-notdeferrable` | S1 | Y04 | agrees | `agreed at Revision 1.9` | 42809 constraint "k" is not deferrable |
| `review19-r1-b14-lock-sequence` | S1 | Y05 | agrees | `agreed at Revision 1.9` | 42809 cannot lock relation "s" |
| `review19-r1-b18-txn-snapshot` | S1 | Y03 | agrees | `agreed at Revision 1.9` | 25001 SET TRANSACTION SNAPSHOT must be called before any query |
| `review19-r1-c10-schema-public` | S1 | Y08 | agrees | `agreed at Revision 1.9` | 42P06 schema "public" already exists |
| `review19-r1-c11-schema-pg` | S1 | Y06 | agrees | `agreed at Revision 1.9` | 42939 unacceptable schema name "pg_foo" |
| `review19-r1-c12-table-pgcatalog` | S1 | Y07 | agrees | `agreed at Revision 1.9` | 42501 permission denied to create "pg_catalog.zzt" |
| `review19-r1-d10-trigger-void` | S1 | Y09 | agrees | `agreed at Revision 1.9` | 42P17 function f must return type trigger |
| `review19-r1-d11-trigger-insteadof-table` | S1 | Y10 | agrees | `agreed at Revision 1.9` | 42809 "t" is a table |
| `review19-r1-d12-trigger-truncate-row` | S1 | Y11 | agrees | `agreed at Revision 1.9` | 0A000 TRUNCATE FOR EACH ROW triggers are not supported |
| `review19-r1-d13-trigger-before-view` | S1 | Y10 | agrees | `agreed at Revision 1.9` | 42809 "v" is a view |
| `review19-r1-d14-trigger-transition-before` | S1 | Y11 | agrees | `agreed at Revision 1.9` | 42P17 transition table name can only be specified for an AFTER trigger |
| `review19-r1-d16-policy-insert-using` | S1 | Y12 | agrees | `agreed at Revision 1.9` | 42601 only WITH CHECK expression allowed for INSERT |
| `review19-r1-d17-policy-select-check` | S1 | Y12 | agrees | `agreed at Revision 1.9` | 42601 WITH CHECK cannot be applied to SELECT or DELETE |
| `review19-r1-d18-policy-view` | S1 | Y12 | agrees | `agreed at Revision 1.9` | 42809 "v" is not a table |
| `review19-r1-d25-role-self` | S1 | Y13 | agrees | `agreed at Revision 1.9` | 0LP01 role "rv_self_r" is a member of role "rv_self_r" |
| `review19-r1-d26-role-circular` | S1 | Y13 | agrees | `agreed at Revision 1.9` | 0LP01 role "rv_c2" is a member of role "rv_c1" |
| `review19-r1-d28-trigger-func-proc` | S1 | Y09 | agrees | `agreed at Revision 1.9` | 42883 function p() does not exist |
| `review19-r1-e01-inh-addcol-conflict` | S1 | Y27 | agrees | `agreed at Revision 1.9` | 42804 child table "c" has different type for column "b" |
| `review19-r1-e09-inh-parent-altertype-child-index` | S1 | Y27 | agrees | `agreed at Revision 1.9` | 42701 column "b" of relation "c" already exists |
| `review19-r1-f01-enum-dup-label` | S1 | Y19 | agrees | `agreed at Revision 1.9` | 23505 duplicate key value violates unique constraint "pg_enum_typid_label_index" |
| `review19-r1-f02-enum-long-label` | S1 | Y19 | agrees | `agreed at Revision 1.9` | 42602 invalid enum label "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" |
| `review19-r1-f03-enum-addvalue-long` | S1 | Y19 | agrees | `agreed at Revision 1.9` | 42602 invalid enum label "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" |
| `review19-r1-f09-range-multirange-name` | S1 | Y21 | agrees | `agreed at Revision 1.9` | 42710 type "mymultirange" already exists |
| `review19-r1-f18-composite-dup-attr` | S1 | Y20 | agrees | `agreed at Revision 1.9` | 42701 column "x" specified more than once |
| `review19-r1-f20-domain-constraint-dup` | S1 | Y20 | agrees | `agreed at Revision 1.9` | 42710 constraint "k" for domain "d" already exists |
| `review19-r1-g11-trailing-junk` | S1 | Y22 | agrees | `agreed at Revision 1.9` | 42601 trailing junk after numeric literal at or near "123abc" |
| `review19-r1-k16-values-mixed` | S1 | Y23 | agrees | `agreed at Revision 1.9` | 22P02 invalid input syntax for type integer: "a" |
| `review19-r1-k21-refresh-concurrently` | S1 | Y24 | agrees | `agreed at Revision 1.9` | 55000 cannot refresh materialized view "history.mv" concurrently |
| `review19-r1-l02-index-hash-multi` | S1 | Y25 | agrees | `agreed at Revision 1.9` | 0A000 access method "hash" does not support multicolumn indexes |
| `review19-r1-l04-index-33cols` | S1 | Y25 | agrees | `agreed at Revision 1.9` | 54011 cannot use more than 32 columns in an index |
| `review19-r1-l09-index-brin-include` | S1 | Y25 | agrees | `agreed at Revision 1.9` | 0A000 access method "brin" does not support included columns |
| `review19-r1-l11-index-desc-hash` | S1 | Y25 | agrees | `agreed at Revision 1.9` | 0A000 access method "hash" does not support ASC/DESC options |
| `review19-r1-l15-index-nulls-hash` | S1 | Y25 | agrees | `agreed at Revision 1.9` | 0A000 access method "hash" does not support NULLS FIRST/LAST options |
| `review19-r1-m01-unique-notvalid` | S1 | Y26 | agrees | `agreed at Revision 1.9` | 0A000 UNIQUE constraints cannot be marked NOT VALID |
| `review19-r1-m02-pk-notvalid` | S1 | Y26 | agrees | `agreed at Revision 1.9` | 0A000 PRIMARY KEY constraints cannot be marked NOT VALID |
| `review19-r1-m05-fk-initdeferred-notdeferrable` | S1 | Y26 | agrees | `agreed at Revision 1.9` | 42601 constraint declared INITIALLY DEFERRED must be DEFERRABLE |
| `review19-r1-n06-schema-rename-to-existing` | S1 | Y08 | agrees | `agreed at Revision 1.9` | 42P06 schema "s2" already exists |
| `review19-r1-n07-schema-rename-pg` | S1 | Y06 | agrees | `agreed at Revision 1.9` | 42939 unacceptable schema name "pg_x" |
| `review19-r1-o03-notx-cic-then-fail` | S1 | Y25 | agrees | `agreed at Revision 1.9` | 0A000 access method "hash" does not support multicolumn indexes |
| `review19-r1-q03-create-role-pg` | S1 | Y06 | agrees | `agreed at Revision 1.9` | 42939 role name "pg_rvx" is reserved |
| `review19-r1-q05-drop-role-owner` | S1 | Y38 | agrees | `agreed at Revision 1.9` | 2BP01 role "rv_q5" cannot be dropped because some objects depend on it |
| `review19-r1-r01-like-including-missing` | S1 | Y28 | agrees | `agreed at Revision 1.9` | 42701 column "a" specified more than once |
| `review19-r1-r02-of-type-not-composite` | S1 | Y28 | agrees | `agreed at Revision 1.9` | 42809 type e is not a composite type |
| `review19-r1-r03-inherits-temp` | S1 | Y28 | agrees | `agreed at Revision 1.9` | 42809 cannot inherit from temporary relation "p" |
| `review19-r1-r04-partition-of-nonpartitioned` | S1 | Y28 | agrees | `agreed at Revision 1.9` | 42P17 "p" is not partitioned |
| `review19-r3-d06-alter-role-set-bad` | S1 | Y15 | agrees | `agreed at Revision 1.9` | 22023 invalid value for parameter "work_mem": "lots" |
| `review19-r3-d07-alter-role-set-unknown` | S1 | Y15 | agrees | `agreed at Revision 1.9` | 42704 unrecognized configuration parameter "nosuch_param" |
| `review19-r3-d21-publication-bad-option` | S1 | Y31 | agrees | `agreed at Revision 1.9` | 42601 unrecognized value for publication option "publish": "bogus" |
| `review19-r3-d22-publication-view` | S1 | Y31 | agrees | `agreed at Revision 1.9` | 22023 cannot add relation "v" to publication |
| `review19-r3-d25-schema-element-other-schema` | S1 | Y32 | agrees | `agreed at Revision 1.9` | 42P15 CREATE specifies a schema (other) different from the one being created (s) |
| `review19-r3-d26-alter-ext-update-bad` | S1 | Y33 | agrees | `agreed at Revision 1.9` | 22023 extension "pgcrypto" has no update path from version "1.3" to version "9.9" |
| `review19-r3-d27-drop-ext-with-dependent-default` | S1 | Y34 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop extension pgcrypto because other objects depend on it |
| `review19-r3-d28-drop-ext-uuidossp-default` | S1 | Y34 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop extension uuid-ossp because other objects depend on it |
| `review19-r3-h02-axum-collation-libc-nondet` | S1 | Y35 | agrees | `agreed at Revision 1.9` | 0A000 nondeterministic collations not supported with this provider |
| `review19-r3-k01-pidx-set-fillfactor` | S1 | Y30 | agrees | `agreed at Revision 1.9` | 42809 ALTER action SET cannot be performed on relation "pi" |
| `review19-r3-k02-reindex-pidx` | S1 | Y30 | agrees | `agreed at Revision 1.9` | 25001 REINDEX INDEX cannot run inside a transaction block |
| `review19-r3-k03-reindex-ptable` | S1 | Y30 | agrees | `agreed at Revision 1.9` | 25001 REINDEX TABLE cannot run inside a transaction block |
| `review19-r3-k15-partition-overlap` | S1 | Y29 | agrees | `agreed at Revision 1.9` | 42P17 partition "p2" would overlap partition "p1" |
| `review19-r3-k16-partition-default-twice` | S1 | Y29 | agrees | `agreed at Revision 1.9` | 42P17 partition "pd2" conflicts with existing default partition "pd" |
| `review19-r3-k17-partition-wrong-bound-kind` | S1 | Y29 | agrees | `agreed at Revision 1.9` | 42P16 invalid bound specification for a range partition |
| `review19-r3-k18-partition-empty-range` | S1 | Y29 | agrees | `agreed at Revision 1.9` | 42P17 empty range bound specified for partition "p1" |
| `review19-r3-k20-list-dup-value` | S1 | Y29 | agrees | `agreed at Revision 1.9` | 42P17 partition "p2" would overlap partition "p1" |
| `review19-r3-m05-adp-in-schema-twice` | S1 | Y36 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-m06-adp-for-role-twice` | S1 | Y36 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-m08-alterrole-set-list` | S1 | Y15 | agrees | `agreed at Revision 1.9` | 22023 SET work_mem takes only one argument |
| `review19-r3-m15-collation-provider-twice` | S1 | Y35 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-m29-ext-schema-twice` | S1 | Y33 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-m46-grant-public-grant-option` | S1 | Y37 | agrees | `agreed at Revision 1.9` | 0LP01 grant options can only be granted to roles |
| `review19-r3-m47-createrole-in-role-self` | S1 | Y13 | agrees | `agreed at Revision 1.9` | 0LP01 role "rr_m47" is a member of role "rr_m47" |
| `review19-r3-m48-createrole-password-twice` | S1 | Y14 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-m50-createrole-pg-prefix` | S1 | Y06 | agrees | `agreed at Revision 1.9` | 42939 role name "pg_mine" is reserved |
| `review19-r3-m54-trigger-referencing-twice` | S1 | Y11 | agrees | `agreed at Revision 1.9` | 42P17 NEW TABLE cannot be specified multiple times |
| `review19-r3-m55-trigger-referencing-old-insert` | S1 | Y11 | agrees | `agreed at Revision 1.9` | 42P17 OLD TABLE can only be specified for a DELETE or UPDATE trigger |
| `review19-r3-p007-ret-anyelement-noarg` | S1 | Y17 | agrees | `agreed at Revision 1.9` | 42P13 cannot determine result data type |
| `review19-r3-p008-ret-anyrange-elemarg` | S1 | Y17 | agrees | `agreed at Revision 1.9` | 42P13 cannot determine result data type |
| `review19-r3-p084-windmill-role-conflict-top` | S1 | Y14 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-p098-ret-anycompatible-noarg` | S1 | Y17 | agrees | `agreed at Revision 1.9` | 42P13 cannot determine result data type |
| `review19-r3-p099-out-anyelement-noin` | S1 | Y17 | agrees | `agreed at Revision 1.9` | 42P13 cannot determine result data type |
| `review19-r3-v001-role-login-twice` | S1 | Y14 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-v002-role-connlimit-neg` | S1 | Y14 | agrees | `agreed at Revision 1.9` | 22023 invalid connection limit: -5 |
| `review19-r3-v003-role-validuntil-bad` | S1 | Y14 | agrees | `agreed at Revision 1.9` | 22007 invalid input syntax for type timestamp with time zone: "not a date" |
| `review19-r3-v005-alterrole-conflict` | S1 | Y14 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-v023-alterfunc-cost-neg` | S1 | Y16 | agrees | `agreed at Revision 1.9` | 22023 COST must be positive |
| `review19-r3-v024-alterfunc-rows-nonset` | S1 | Y16 | agrees | `agreed at Revision 1.9` | 22023 ROWS is not applicable when function does not return a set |
| `review19-r3-v025-alterfunc-vol-conflict` | S1 | Y16 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-v026-alterfunc-parallel-bogus` | S1 | Y16 | agrees | `agreed at Revision 1.9` | 42601 parameter "parallel" must be SAFE, RESTRICTED, or UNSAFE |
| `review19-r3-v027-alterproc-strict` | S1 | Y16 | agrees | `agreed at Revision 1.9` | 42P13 invalid attribute in procedure definition |
| `review19-r3-v028-alterfunc-set-bad` | S1 | Y15 | agrees | `agreed at Revision 1.9` | 22023 invalid value for parameter "work_mem": "lots" |
| `review19-r3-v043-collation-provider-bogus` | S1 | Y35 | agrees | `agreed at Revision 1.9` | 42P17 unrecognized collation provider: bogus |
| `review19-r3-v044-collation-deterministic-maybe` | S1 | Y35 | agrees | `agreed at Revision 1.9` | 42601 deterministic requires a Boolean value |
| `review19-r3-v046-collation-no-ctype` | S1 | Y35 | agrees | `agreed at Revision 1.9` | 42P17 parameter "lc_ctype" must be specified |
| `review19-r3-v050-ext-version-bad` | S1 | Y33 | agrees | `agreed at Revision 1.9` | 22023 extension "pgcrypto" has no installation script nor update path for version "9.9" |
| `review19-r3-v051-ext-version-twice` | S1 | Y33 | agrees | `agreed at Revision 1.9` | 42601 conflicting or redundant options |
| `review19-r3-v072-grant-role-self` | S1 | Y13 | agrees | `agreed at Revision 1.9` | 0LP01 role "rr_v072" is a member of role "rr_v072" |
| `review19-r3-v085-alterfunc-rename-exists` | S1 | Y16 | agrees | `agreed at Revision 1.9` | 42723 function g() already exists in schema "history" |
| `review19-r3-x01-partition-bound-type` | S1 | Y29 | agrees | `agreed at Revision 1.9` | 22P02 invalid input syntax for type integer: "x" |
| `review19-r3-x11-default-hex-junk` | S1 | Y22 | agrees | `agreed at Revision 1.9` | 42601 invalid hexadecimal integer at or near "0x" |
| `review19-r3-x12-check-num-dotdot` | S1 | Y22 | agrees | `agreed at Revision 1.9` | 42601 syntax error at or near ".3" |
| `review19-r3-y16-param-text-mod-bad` | S1 | Y18 | agrees | `agreed at Revision 1.9` | 42601 type modifier is not allowed for type "text" |
| `review19-r4-a32-trigger-func-not-trigger` | S1 | Y09 | agrees | `agreed at Revision 1.9` | 42P17 function f must return type trigger |
| `review19-r4-a35-trigger-on-view` | S1 | Y10 | agrees | `agreed at Revision 1.9` | 42809 "v" is a view |
| `review19-r4-a41-hash-multicol` | S1 | Y25 | agrees | `agreed at Revision 1.9` | 0A000 access method "hash" does not support multicolumn indexes |
| `review19-r4-c29-droprole-owner` | S1 | Y38 | agrees | `agreed at Revision 1.9` | 2BP01 role "r4_owner_x" cannot be dropped because some objects depend on it |
| `review19-r4-c30-droprole-grant` | S1 | Y38 | agrees | `agreed at Revision 1.9` | 2BP01 role "r4_grant_x" cannot be dropped because some objects depend on it |
| `review19-r4-d15-enum-dup-label` | S1 | Y19 | agrees | `agreed at Revision 1.9` | 23505 duplicate key value violates unique constraint "pg_enum_typid_label_index" |
| `review19-r4-d16-enum-label-too-long` | S1 | Y19 | agrees | `agreed at Revision 1.9` | 42602 invalid enum label "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" |
| `review19-r4-d17-composite-dup-attr` | S1 | Y20 | agrees | `agreed at Revision 1.9` | 42701 column "name" specified more than once |
| `review19-r5-i14-comment-on-table-view` | S1 | Y42 | agrees | `agreed at Revision 1.9` | 42809 "v" is not a table |
| `review19-r5-o02-x4-authz-twice` | S1 | Y43 | agrees | `agreed at Revision 1.9` | 42P06 schema "r5app2" already exists |
| `review19-r5-o03-x4-named-then-authz` | S1 | Y43 | agrees | `agreed at Revision 1.9` | 42P06 schema "r5app3" already exists |
| `review19-r5-r01-comment-on-table-matview` | S1 | Y42 | agrees | `agreed at Revision 1.9` | 42809 "mv" is not a table |
| `review19-r5-r02-comment-on-table-seq` | S1 | Y42 | agrees | `agreed at Revision 1.9` | 42809 "s" is not a table |
| `review19-r5-r10-comment-on-table-composite` | S1 | Y42 | agrees | `agreed at Revision 1.9` | 42809 "pair" is not a table |
| `review19-r5-r12-comment-on-domain-enum` | S1 | Y42 | agrees | `agreed at Revision 1.9` | 42809 "e" is not a domain |
| `review19-r5-r15-comment-on-table-foreign` | S1 | Y42 | agrees | `agreed at Revision 1.9` | 42809 "v" is not a foreign table |
| `review19-r5-r16-comment-on-table-view-via-ifne` | S1 | Y42 | agrees | `agreed at Revision 1.9` | 42809 "v" is not a table |
| `review19-r5-r22-comment-on-table-renamed-view` | S1 | Y42 | agrees | `agreed at Revision 1.9` | 42809 "v2" is not a table |
| `review19-r5-r23-comment-on-table-view-qualified` | S1 | Y42 | agrees | `agreed at Revision 1.9` | 42809 "v" is not a table |
| `review19-r5-u01-default-null-variants-facts` | S1 | Y46 | agrees | `agreed at Revision 1.9` | every migration applied; the pass records no default for the NULL forms Postgres does not store (a, b, d) and keeps the stored ones (c, e, f) |
| `review19-r5-a08-droptable-seq-used-fn-atomic` | S1 | Y40 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop table a because other objects depend on it
| `review19-r5-c06-trigger-droptable-ifexists-other-then-func` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function touch() because other objects depend on it
| `review19-r5-f01-trigger-same-name-as-func-drop-func` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function set_updated_at() because other objects depend on it
| `review19-r5-f02-trigger-same-name-as-func-drop-func-ifexists` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function set_updated_at() because other objects depend on it
| `review19-r5-f09-domain-default-same-name-func` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function slug() because other objects depend on it
| `review19-r5-f12-event-trigger-same-name` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 42501 permission denied to create event trigger "ddl_log"
| `review19-r5-f15-func-returns-type-same-name-atomic` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop type pair because other objects depend on it
| `review19-r5-g01-atomic-uses-table-seq-dropcol` | S1 | Y40 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop column id of table a because other objects depend on it
| `review19-r5-g06-atomic-uses-identity-seq` | S1 | Y40 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop table a because other objects depend on it
| `review19-r5-h01-trigger-on-table-named-as-func` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function audit() because other objects depend on it
| `review19-r5-h05-trigger-same-name-qualified` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function set_updated_at() because other objects depend on it
| `review19-r5-h06-trigger-same-name-quoted` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function touch() because other objects depend on it
| `review19-r5-h08-lemmy-shape-aggregates-trigger` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function post_aggregates_comment_count() because other objects depend on it
| `review19-r5-h10-constraint-trigger-same-name` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function chk() because other objects depend on it
| `review19-r5-h20-alter-domain-set-default-func` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function d() because other objects depend on it
| `review19-r5-h24-trigger-same-name-on-view-instead` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function v_ins() because other objects depend on it
| `review19-r5-h25-procedure-atomic-same-name-table` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop table cleanup because other objects depend on it
| `review19-r5-h26-function-atomic-same-name-seq` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop sequence next_id because other objects depend on it
| `review19-r5-h29-trigger-same-name-across-migs-replace` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function updated() because other objects depend on it
| `review19-r5-n07-atomic-proc-insert-table-dropcol` | S1 | Y41 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop column name of table items because other objects depend on it
| `review19-r5-n08-atomic-func-col-type-alter` | S1 | Y41 | agrees | `agreed at Revision 1.9` | 0A000 cannot alter type of a column used by a function or procedure
| `review19-r5-n11-sqlbody-identity-seq-of-table-cascade-then` | S1 | Y40 | agrees | `agreed at Revision 1.9` | 42883 function nx() does not exist
| `review19-r5-t05-quoted-type-trigger-func-prisma` | S1 | Y39 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop function "touchUser"() because other objects depend on it
| `review19-r5-v02-selectinto-enum-col-drop-source-then-type` | S1 | Y44 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop type status because other objects depend on it
| `review19-r5-w08-dropfunc-ifexists-renamed-dependent-oldname` | S1 | Y45 | agrees | `agreed at Revision 1.9` | 2BP01 cannot drop type st because other objects depend on it
| `review19-r4-i07-w2-helper-on-view` | S1 | Y47 | agrees | `agreed at Revision 1.9` | 42809 "uv" is a view
| `review19-r4-n04-plpgsql-body-table-dropped` | S1 | Y50 | agrees | `agreed at Revision 1.9` | 42P01 relation "t" does not exist
| `review19-r3-h14-perform-in-do-regclass` | S1 | Y49 | agrees | `agreed at Revision 1.9` | 42P01 relation "userz" does not exist
| `review19-r3-p030-do-execute-createfunc-bad` | S1 | Y48 | agrees | `agreed at Revision 1.9` | 42601 syntax error at end of input
| `review19-r3-p031-do-execute-sqlfunc-bad` | S1 | Y48 | agrees | `agreed at Revision 1.9` | 42601 syntax error at end of input
| `review19-r3-p088-plpgsql-stale-call` | S1 | Y50 | agrees | `agreed at Revision 1.9` | 42703 column "a" does not exist
| `review19-r2-i20-event-trigger` | S1 | Y51 | agrees | `agreed at Revision 1.9` | 42501 permission denied to create event trigger "e"
| `review19-inv-x05-event-trigger` | S1 | Y51 | agrees | `agreed at Revision 1.9` | 42501 permission denied to create event trigger "e"
| `g8-s8389-S1-columns` | S1 | Y52 | agrees | `columns` | DROP TABLE IF EXISTS o2 CASCADE drops column d4.a (type _o1, the array of the renamed row type; the 10,000-seed gate root of Revision 1.8) |
| `g7-s147-S1-O1_code_effects_u2` | S1 | Y48 | agrees | `applied\|S1\|2BP01\|O1.code_effects_u2` | 2BP01 cannot drop index ct1_pkey because constraint ct1_pkey on table ct1 requires it (found by g7 after H21) |
| `g9-s772-S1-M6_ownedseq_y40` | S1 | R1b | agrees | `applied\|S1\|42P01\|M6.ownedseq_y40` | 42P01 relation "other_seq" does not exist (a SQL-standard body's nextval of a missing sequence at CREATE FUNCTION; found by g9 while U-c ran, minimised by hand) |
| `g9-s142-S1-O1_stalecall_y50` | S1 | Y49 | agrees | `applied\|S1\|42P01\|O1.stalecall_y50` | 42P01 relation "t1" does not exist (a called routine's RETURN names a table renamed before the routine was created; found by g9 while U-c ran, minimised by hand) |
| `g5-s2274-S1-O1t_installer` | S1 | O1t | agrees | `applied\|S1\|42P01\|O1t` | 42P01 relation ""t3"" does not exist (a trigger installer's %I of the text '"t3"' names a relation with quotes in its name; judged once H21 read format() templates case-insensitively; found by the U-c 3000-seed gate, minimised by hand) |
| `g9-s35-S1-N1_roleset_y15` | S1 | Y51 | agrees | `applied\|S1\|42501\|N1.roleset_y15` | 42501 permission denied to set parameter "gv9.x" (ALTER ROLE ... SET of a placeholder parameter, superuser-only as a stored setting; found by g9 once H23 replayed as gv_migrator) |
| `g5-s4095-E8-O1t_trigger_call` | S1 | E8 | agrees | `e8-hit R 42P01 O1t.trigger_call` | 42P01 relation "T3" does not exist (an E8 hit; prefix judged, 0 roots) |
| `g9-s5793-E8-O1_stalecall_y50` | S1 | E8 | agrees | `e8-hit R 42703 O1.stalecall_y50` | 42703 column "a" does not exist (an E8 hit; prefix judged, 0 roots) |
| `review21-a1-agg1` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-coll1` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-e1-addfk` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-e1-default` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-e1-dropcascade` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-e1-fk` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-e1-fk2` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-e1-trigger` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-merge1` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-merge2` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-o1t-clean` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-r4-enum-rename` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w1-citext-idx` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w1-hstore` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w1-uuid` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w3-dropowned` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w3-reassign` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w5-add` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w5-alter` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w5-col` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w5-drop` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w5-rename` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w5-typerename` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w6-domain` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w6-domain2` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w6-rename` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-w6-setschema` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
| `review21-a1-y50-clean` | S1 | E8 | agrees | `agreed at Revision 2.1` | every migration applied; 0 roots |
