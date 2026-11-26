package schema

// The claim contract (T018a2, ADR 0047 follow-up): the finite list of
// statement forms whose PostgreSQL 17 semantics the pass models, with the
// facts each may claim, and the precondition ids of Revision 1 (P1-P10,
// K2). Every form no row covers goes to the default branch D. This table is
// the skeleton the code is held to: every refusal or possible-failure
// diagnostic that a precondition produces names its id (precondition), and
// TestContractIDs checks that each id the code names is listed here. The
// authoritative wording of each row is the task's contract table
// (the T018a2 task record (internal), "Claim contract" with
// Revision 1); B3 moves it into docs/extract/schema.md and checks the two
// against each other.

// contractRow is one row of the claim contract.
type contractRow struct {
	id string
	// kind is M (modelled), R (recorded name), N (no effect), O (opaque),
	// F (failing), T (transaction control), Q (set reading) or D.
	kind  string
	form  string
	claim string
}

// contractRows lists every row id in the contract's order. The draft row
// M5k (other alter_table_cmd words) is not listed: Revision 1 split it into
// M5k-a and M5k-b, which together cover every word.
var contractRows = []contractRow{
	{"M1", "M", "CREATE [UNLOGGED] TABLE [IF NOT EXISTS] n (cols, table constraints)", "table, columns, keys, foreign keys, CHECK presence"},
	{"M1b", "M", "CREATE TABLE n (LIKE ...) / OF t / PARTITION OF p / INHERITS (...)", "partial node with the name only"},
	{"M2", "M", "column type (Typename grammar)", "written type; alias-normalised type for comparisons"},
	{"M3", "M", "REFERENCES t [(c)] / FOREIGN KEY (...) REFERENCES in M1, M5f", "foreign_key edge to a proven key"},
	{"M4", "M", "CREATE [UNIQUE] INDEX [CONCURRENTLY] [IF NOT EXISTS] [n] ON [ONLY] t ...", "db_index, column unique"},
	{"M5a", "M", "ALTER TABLE ... ADD [COLUMN] [IF NOT EXISTS] coldef", "new column"},
	{"M5b", "M", "ALTER TABLE ... DROP [COLUMN] [IF EXISTS] c [CASCADE|RESTRICT]", "column removed with its dependents"},
	{"M5c", "M", "ALTER TABLE ... ALTER [COLUMN] c [SET DATA] TYPE t [COLLATE] [USING]", "new type"},
	{"M5d", "M", "ALTER TABLE ... ALTER [COLUMN] c SET / DROP NOT NULL", "nullable"},
	{"M5e", "M", "ALTER TABLE ... ALTER [COLUMN] c SET DEFAULT e / DROP DEFAULT", "default"},
	{"M5f", "M", "ALTER TABLE ... ADD [CONSTRAINT n] PRIMARY KEY / UNIQUE / FOREIGN KEY / CHECK / EXCLUDE", "key, foreign key, CHECK presence"},
	{"M5f-u", "M", "ALTER TABLE [ONLY] t ADD [CONSTRAINT n] PRIMARY KEY | UNIQUE USING INDEX i [DEFERRABLE ...] as the statement's only subcommand", "the constraint over the index's key columns; the index renamed to n; primary-key columns NOT NULL (Revision 1.5 U1)"},
	{"M5g", "M", "ALTER TABLE ... DROP CONSTRAINT [IF EXISTS] n [CASCADE|RESTRICT]", "every key of each table the name may mean is forgotten (Revision 7)"},
	{"M5g-1", "M", "DROP CONSTRAINT of a name the set wrote on this table, tracked exactly", "that constraint removed"},
	{"M5h", "M", "ALTER TABLE ... RENAME [COLUMN] a TO b", "column name"},
	{"M5i", "M", "ALTER TABLE ... RENAME TO n", "name"},
	{"M5j", "M", "ALTER TABLE ... SET SCHEMA s", "schema"},
	{"M5k-a", "M", "ALTER TABLE words whose success depends only on the table and E1", "table partial; SET LOGGED/UNLOGGED records persistence"},
	{"M5k-b", "M", "ALTER TABLE words that depend on an object or option", "table partial"},
	{"M6", "M", "DROP TABLE [IF EXISTS] names [CASCADE|RESTRICT]", "tables removed"},
	{"M7", "M", "DROP INDEX [CONCURRENTLY] [IF EXISTS] names [CASCADE]", "index removed"},
	{"M8", "M", "ALTER INDEX [IF EXISTS] n RENAME TO m", "index name"},
	{"M9", "M", "ALTER SCHEMA s RENAME TO t", "schema of tables (partial, name move)"},
	{"R1", "R", "CREATE [OR REPLACE] [TEMP] VIEW / MATERIALIZED VIEW / SEQUENCE / TYPE / DOMAIN, CREATE TABLE AS, SELECT INTO, CREATE SCHEMA", "namespace record"},
	{"R1b", "R", "the query of a view, CREATE TABLE AS or SELECT INTO, and a LANGUAGE sql body", "as R1"},
	{"R2", "R", "CREATE TYPE n AS ENUM ('a', ...)", "the enum's labels, in order"},
	{"R3", "R", "ALTER TYPE n ADD VALUE [IF NOT EXISTS] 'v' [BEFORE|AFTER 'w'] / RENAME VALUE", "label added or renamed"},
	{"R4", "R", "ALTER TYPE/DOMAIN (other), ALTER VIEW/SEQUENCE/MATERIALIZED VIEW ... RENAME, ALTER COLLATION", "tables that use it become partial"},
	{"R5", "R", "DROP <other kind> [IF EXISTS] names [CASCADE|RESTRICT], DROP SCHEMA", "dependents partial with CASCADE"},
	{"R6", "R", "CREATE FUNCTION/PROCEDURE, ALTER FUNCTION/PROCEDURE/ROUTINE ... [RENAME TO g]", "the set's function names"},
	{"N1", "N", "COMMENT, GRANT, REVOKE, ..., CREATE TRIGGER/POLICY/STATISTICS/..., ALTER SEQUENCE/POLICY/ROLE/...", "nothing changes; CREATE TRIGGER and CREATE POLICY record the name on their table, the other named CREATE forms in the set's record of names"},
	{"N2", "N", "SET/RESET/DISCARD other than O3/O4", "nothing changes"},
	{"N3", "N", "DML, SELECT/VALUES/WITH/TABLE, ANALYZE/CLUSTER/REINDEX/REFRESH", "nothing changes"},
	{"O1", "O", "DO, CALL, EXECUTE; a call of a set-defined function; DML in a set that defines functions", "opaque count"},
	{"O1t", "O", "SELECT f('<relation>') of a set-defined trigger installer", "as N1 CREATE TRIGGER"},
	{"O2", "O", "CREATE/ALTER/DROP EXTENSION", "opaque count"},
	{"O3", "O", "search_path, role and session-authorization statements, DISCARD ALL", "the session's search_path when every change to it is read (U3); otherwise later names uncertain"},
	{"O4", "O", "lexing settings", "later migrations may fail"},
	{"O5", "O", "CREATE EVENT TRIGGER, DROP OWNED, DROP ... CASCADE of untracked kinds", "opaque; tables partial"},
	{"F1", "F", "VACUUM, REINDEX ... CONCURRENTLY, DROP DATABASE, DROP TABLESPACE, ...", "migration may fail"},
	{"T1", "T", "BEGIN/START, COMMIT/END, ROLLBACK/ABORT, SAVEPOINT, RELEASE, ROLLBACK TO, PREPARE TRANSACTION, LOCK", "nothing changes, or the P6 effect"},
	{"Q1", "Q", "the set's members as sqlx reads them", "applied of every migration"},
	{"D", "D", "every other form", "tables it may name partial; the migration may fail"},
}

// unnamedFreeWords is the C5 table (T018a2 B6 as amended, rows M1, M4,
// M5f): the ALTER TABLE words, by their M5k subform (parse.go m5kWord),
// that make a table partial but cannot create an unnamed object on it
// (tablecmds.c REL_17_6: none of them adds a constraint, an index or a
// sequence; ATExecAddIdentity, ATExecAttachPartition, ATExecAddInherit and
// the subforms the pass does not classify are not on the list). A name
// shaped like a generated name meets only tables where something else may
// have created unnamed objects (unnamedUnknown).
var unnamedFreeWords = map[string]bool{
	"owner":                  true, // OWNER TO
	"row_security":           true, // ENABLE/DISABLE/FORCE/NO FORCE ROW LEVEL SECURITY
	"trigger_all":            true, // ENABLE/DISABLE TRIGGER ALL/USER
	"enable_trigger":         true, // ENABLE/DISABLE [ALWAYS|REPLICA] TRIGGER n
	"enable_rule":            true, // ENABLE/DISABLE [ALWAYS|REPLICA] RULE n
	"set_logged":             true, // SET LOGGED
	"set_unlogged":           true, // SET UNLOGGED
	"set_without_cluster":    true, // SET WITHOUT CLUSTER
	"set_without_oids":       true, // SET WITHOUT OIDS
	"storage_parameters":     true, // SET (...) / RESET (...)
	"replica_identity":       true, // REPLICA IDENTITY FULL/DEFAULT/NOTHING
	"replica_identity_index": true, // REPLICA IDENTITY USING INDEX i
	"cluster_on":             true, // CLUSTER ON i
	"validate":               true, // VALIDATE CONSTRAINT n
	"alter_constraint":       true, // ALTER CONSTRAINT n [NOT] DEFERRABLE ...
	"no_inherit":             true, // NO INHERIT p
	"of":                     true, // OF t / NOT OF
}

// contractPrecondition is one Revision 1 precondition: <row>.<class>.
type contractPrecondition struct {
	id   string
	text string
}

// contractPreconditions lists the precondition ids: P1, P2, P3, P10 and
// K2 from step B1a, P4-P9 from step B1b, P11 from Revision 1.4, P12 and
// P13 from Revision 1.5.
var contractPreconditions = []contractPrecondition{
	// P1 names: an object a statement needs is proven only when the pass
	// holds it; a name the set creates but does not hold is not E1.
	{"M1.P1", "CREATE TABLE: column types, defaults, checks and parents the set creates must be held"},
	{"M1b.P1", "CREATE TABLE LIKE/OF/PARTITION OF/INHERITS: the source the set creates must be held; Revision 1.6 V11: a WITH OPTIONS column list of PARTITION OF or OF is not read (row D, M1b.P7)"},
	{"M3.P1", "REFERENCES: a referenced table the set creates must be held; Revision 1.8 X3: a referenced name the pass holds as a view, materialized view, sequence, composite type or index is refused (42809), may fail when its kind is not certain, and a table that may hold another relation kind (CREATE TABLE IF NOT EXISTS) is not proven"},
	{"M4.P1", "CREATE INDEX: the table and the functions the elements call must be held; Revision 1.8 X3: a table the pass holds as a view, sequence, composite type, foreign table or index is refused (42809), may fail when its kind is not certain; an index on a materialized view (its columns not proven) or on a table that may hold another relation kind may fail"},
	{"M5a.P1", "ALTER TABLE (every M5 row): the table, and the types and expression objects the set creates, must be held; on a table the pass does not hold that the set changed earlier, the statement is not proven; on a name that is an index only RENAME, RENAME COLUMN, OWNER TO, SET/RESET (...) and SET TABLESPACE are accepted, other actions are refused (42809) or not proven (Revision 1.4); Revision 1.6 V4 (kinds, 2026-11-07): on a name held as a view, materialized view, sequence or foreign table, also with IF EXISTS, an action whose ATSimplePermissions mask excludes that kind is refused (42809) and only RENAME, SET SCHEMA, OWNER TO and (on a sequence) SET LOGGED/UNLOGGED are accepted without further proof; on a name held only as a type or domain the statement (without IF EXISTS) may fail"},
	{"M6.P1", "DROP TABLE: each name the set creates must be held (without IF EXISTS)"},
	{"M7.P1", "DROP INDEX: a set name or a generated-name shape of a set table must be held (without IF EXISTS); E1 does not cover indexes, so any other name is not proven (Revision 1.5 H12)"},
	{"M5f-u.P1", "USING INDEX: the index held under a certain name on the table, with its columns, method, predicate, expressions, ordering, NULLS NOT DISTINCT and validity known (Revision 1.5 U1)"},
	{"M2.P1", "a written type exists: a pg_catalog type of pg_type.dat or a gram.y SQL type name, an array of one, a type the set holds, or an E1 type of contrib; COLLATE names default, C, POSIX, ucs_basic, pg_c_utf8 or a collation the set holds; anything else may fail (Revision 1.5 U7); Revision 1.6 V13: serial and its aliases are a type only in a column definition of CREATE TABLE or ADD COLUMN, anywhere else (composite attributes, domains, ALTER COLUMN TYPE, routine signatures) refused (42704); the built-in collations ucs_basic and pg_c_utf8 are proven under E4's UTF8 database only; Revision 1.6 D2: every type of a routine signature (parameters, RETURNS [SETOF], RETURNS TABLE), a pg_catalog pseudo-type there included"},
	{"N2.P1", "SET/RESET: the name is a parameter of guc_tables.c (unknown one-part names refused) or a two-part placeholder whose prefix no module reserves (Revision 1.5 U7)"},
	{"M8.P1", "ALTER INDEX: a set name or a generated-name shape of a set table must be held (without IF EXISTS); Revision 1.6 V4: every ALTER INDEX form of a name the pass does not hold as an index may fail (E1 does not cover indexes); Revision 1.6 V4 (kinds, 2026-11-07): a name held as a relation of another kind is refused also with IF EXISTS (42809; RENAME accepts any relation but a composite type), a name held only as a type or domain may fail without IF EXISTS, and SET LOGGED, UNLOGGED, WITHOUT CLUSTER, WITHOUT OIDS and ACCESS METHOD are refused on an index and, with IF EXISTS, may fail on a name not held"},
	{"M9.P1", "ALTER SCHEMA: a schema the set creates must be held"},
	{"M9.P2", "ALTER SCHEMA s RENAME TO n: refused (42P06) when n is a schema the set holds, public or information_schema under E5 unless the set dropped it, pg_catalog or pg_toast; refused (42939) when n starts with pg_; may fail when code or CREATE SCHEMA AUTHORIZATION of an unknown role may have created n (Revision 1.9 Y06, Y08)"},
	{"R5.P1", "DROP SCHEMA: a schema the set creates must be held (without IF EXISTS); Revision 1.6 V4: DROP SEQUENCE, VIEW, MATERIALIZED VIEW, TYPE and DOMAIN of a name the pass does not hold (without IF EXISTS) may fail; Revision 1.7 W5: DROP COLLATION of a name the N1.P2 record does not hold (or dropped since) and DROP AGGREGATE (without IF EXISTS) may fail; Revision 1.8 X2: after ALTER COLLATION a RENAME TO b the pass holds b and not a"},
	{"N3.P1", "DML and queries (Revision 1.2): every column reference proven on a relation whose columns are known; INSERT targets and VALUES widths, SET targets, COPY and ANALYZE column lists; anything the reader does not prove may fail; Revision 1.6 V4: REFRESH MATERIALIZED VIEW, REINDEX INDEX and CLUSTER ... USING of a name not held as that kind may fail"},
	{"N1.P1", "token rule for N1, N3, R1, R1b, R4, R5, R6 and O1 statements: an identifier equal to a name the set creates must be held; Revision 1.6 V4: COMMENT ON INDEX, SEQUENCE, VIEW or MATERIALIZED VIEW of a name not held, COMMENT ON CONSTRAINT and GRANT/REVOKE ON SEQUENCE of a name not held may fail; Revision 1.7 W1 (with the D1 doc fix): DROP TRIGGER, POLICY or RULE (without IF EXISTS), ALTER POLICY and COMMENT ON TRIGGER, POLICY or RULE may fail on a table outside the set always, and on a table the set holds unless the pass holds the name there (the table resolves certainly with its facts known and its record of names of the kind is exact: a name created by code is only possibly held; the pass holds no rule); W4: ALTER DATABASE, GRANT/REVOKE ON DATABASE and ALTER ROLE ... IN DATABASE may fail (E1 does not list databases, the database name is unknown; COMMENT ON DATABASE never refuses); W5: DROP PUBLICATION of a name the N1.P2 record does not hold, and DROP STATISTICS, OPERATOR and CAST (without IF EXISTS), may fail; Revision 1.8 X2: a collation name renamed away is no longer held; X4: CREATE SCHEMA AUTHORIZATION r without a schema name creates schema r, and after one with CURRENT_USER, SESSION_USER or CURRENT_ROLE (a schema named after the unknown role) a statement naming a schema the pass does not hold (other than public, information_schema and pg_ names) and CREATE SCHEMA without IF NOT EXISTS may fail"},
	{"R3.P1", "ALTER TYPE: a type the set creates must be held; Revision 1.6 V4 (kinds, 2026-11-07): ADD VALUE and RENAME VALUE of a table's row type are refused, and of a name the pass does not hold as a type may fail (E1 does not cover enums)"},
	{"R4.P1", "ALTER VIEW/SEQUENCE/MATERIALIZED VIEW/TYPE of a relation the set created: its existence and kind must still be proven (no opaque statement or CASCADE since); Revision 1.6 V4: ALTER VIEW, ALTER MATERIALIZED VIEW and ALTER SEQUENCE of a name the pass does not hold as that kind (without IF EXISTS) may fail; Revision 1.6 V4 (kinds, 2026-11-07): ALTER DOMAIN of a name held as another kind, ALTER TYPE ... ADD/DROP/ALTER/RENAME ATTRIBUTE of a name held as anything but a composite type, and other ALTER TYPE forms of a relation's row type are refused (a sequence or index: may fail); ALTER VIEW / MATERIALIZED VIEW ... RENAME [COLUMN] and ALTER TYPE ... RENAME ATTRIBUTE of a held name may fail (columns not tracked); ALTER DOMAIN ... DROP CONSTRAINT (without IF EXISTS), VALIDATE CONSTRAINT and RENAME CONSTRAINT may fail (E1 does not cover constraints); ALTER TYPE (other than ADD / RENAME VALUE) and ALTER DOMAIN of a name the pass does not hold as a type may fail (E1 does not cover types other than its own)"},
	// P2 shared namespaces and renames.
	{"M1.P2", "CREATE TABLE: index-backed constraint names need a free pg_class name; constraint names are unique per table across every kind, and a written name that may be the generated name of an unnamed constraint of the statement may fail (Revision 1.4 R2); a column named ctid, xmin, cmin, xmax, cmax or tableoid is refused (Revision 1.5 U10); Revision 1.9: a table created in pg_catalog or pg_toast refused (42501, Y07); a statement that creates a pg_type entry named like a multirange type the set holds refused (42710, Y21)"},
	{"M5a.P2", "ADD COLUMN: as M1.P2, and the column's constraint names must not be held on the table (Revision 1.4 R2); a system column name is refused, also under IF NOT EXISTS (Revision 1.5 U10)"},
	{"M5h.P2", "RENAME COLUMN to a system column name is refused (Revision 1.5 U10); Revision 1.9 Y27: RENAME COLUMN a TO b without ONLY refused (42701) when an inheritance child holds b, may fail when a child's columns are not held"},
	{"M5f-u.P2", "USING INDEX: the constraint name free among the table's constraints and, when it differs from the index name, free in the pg_class namespace (Revision 1.5 U1)"},
	{"M5f.P2", "ADD CONSTRAINT: an index-backed constraint name needs a free pg_class name; a name held by any constraint of the table is refused, one the table may hold may fail (Revision 1.4 R2)"},
	{"M8.P2", "ALTER INDEX RENAME TO: the new name needs a free pg_class name; generated names keep their creation-time form; the index of a constraint takes a name no other constraint of its table holds (Revision 1.4 R2)"},
	{"O1.P1", "a string argument that spells a relation name (a reg* parameter may read it) follows P1 (B2, g5-s80); Revision 1.7 W2: a string literal bound (by position, by name, or as a VARIADIC element) to a reg* parameter of a set-defined routine, or to a parameter whose type the pass does not know or that is outside pg_catalog, is proven only as a regclass argument naming a table, view, materialized view or sequence the pass holds under a certain name (search_path known); otherwise the statement may fail; Revision 1.9 Y49: the static SELECT, PERFORM, INSERT, UPDATE, DELETE, VALUES and WITH statements of a DO body and of a set PL/pgSQL routine the statement may call are checked where they run with the top-level rules for names (N1.P1), columns (N3.P1) and reg* arguments (W2), and a failure there makes the running statement may fail; Y50: a call of a set PL/pgSQL routine (also a data statement that may fire a set trigger) may fail when, since the routine's creation or last CREATE OR REPLACE, a statement the pass applied dropped, renamed or changed the type of an object or column a word of its body names"},
	// P2 per-table trigger names (B2).
	{"N1.P2", "CREATE TRIGGER (and O1t) and CREATE POLICY: the name must be free on the table; exact on a table whose facts are known, not proven on another table the set holds, the set's own record of names on a table it does not hold; CREATE ROLE/USER/GROUP, PUBLICATION, COLLATION, AGGREGATE, OPERATOR, CAST: a name the set created earlier and no read DROP or RENAME freed is refused (roles, publications) or not proven (the others, and every kind an unread RENAME may have used) (Revision 1.4); Revision 1.9 Y06: CREATE ROLE, USER or GROUP of a pg_ name, and ALTER ROLE ... RENAME from or to a pg_ name, refused (42939); Revision 1.9 Y13: the membership edges the set creates (GRANT r TO m, CREATE ROLE ... IN ROLE, ROLE, USER, ADMIN, ALTER GROUP ... ADD USER) and removes (REVOKE, ALTER GROUP ... DROP USER, DROP ROLE), with the migrating role (E6) a member of every role the set creates: making m a member of r is refused (0LP01) when r = m or the edges make r a member of m; may fail when r's memberships and m's members both include a role outside the set, after GRANTED BY, or when code may change memberships; Revision 1.9 (E6): while SET ROLE puts a role the set created in force, every statement but SET, RESET, SHOW, DISCARD and transaction control may fail (the role's privileges are not the migrating role's)"},
	// P3 conditional forms.
	{"M1.P3", "CREATE TABLE IF NOT EXISTS whose skip depends on an unknown writes no known facts"},
	{"M4.P2", "CREATE INDEX on a table the pass does not hold that the set changed earlier (an index created on it, ALTER TABLE): its names and columns are not proven (Revision 1.4); Revision 1.9 Y07: an index on a relation in pg_catalog or pg_toast refused (42501)"},
	{"M5a.P3", "ADD COLUMN IF NOT EXISTS on a table whose facts are not known writes no known facts, and its constraints and expressions are not proven (Revision 1.4)"},
	{"M4.P3", "CREATE INDEX IF NOT EXISTS whose skip depends on an unknown writes no index"},
	{"M7.P3", "DROP INDEX IF EXISTS under an uncertain qualification: the index it may mean is no longer a fact"},
	// P4 untracked objects, kinds and options (B1b).
	{"M1.P4", "CREATE TEMP TABLE with a foreign key: the referenced table's persistence is not proven; Revision 1.4 R3: a column definition is applied only in the allowlisted clause combinations (one DEFAULT, serial or identity; identity on smallint, integer or bigint without options; no generation expression with a default or identity; one PRIMARY KEY and COLLATE; COLLATE on a collatable type; no STORAGE or COMPRESSION), key columns written once, no table options other than TABLESPACE and WITH storage parameters proven by reloptions.c for a heap table (refused on a partitioned one), a key's WITH (...) proven for btree (Revision 1.5 U9), ON COMMIT refused, PARTITION BY RANGE or LIST over plain btree columns that every key includes (Revision 1.5 U4: as every unique index on a partitioned table)"},
	{"M1b.P4", "CREATE TABLE ... PARTITION OF p ... PARTITION BY: the new partition receives p's unique indexes and keys, each of which must include its partition key (RANGE or LIST over plain columns); p's unique indexes not known: not proven (Revision 1.5 U4); Revision 1.9 Y28: PARTITION OF a table the pass holds under a certain name as not partitioned refused (42P17), may fail when the name or its partitioning is not certain"},
	{"M1b.P5", "Partition bounds (Revision 1.9 Y29; parse_utilcmd.c transformPartitionBound, partbounds.c check_new_partition_bound): for a partitioned table the set created, the bounds of its partitions created by PARTITION OF or ATTACH PARTITION, until DETACH or the partition's drop; a bound of the wrong kind for the strategy, DEFAULT on a hash-partitioned table, a hash remainder not below its modulus, FROM or TO with more than one value for a one-column key (42P16), a second DEFAULT, an empty range, a list value or a range another partition holds, a hash modulus that is not a factor or multiple of the others or a hash remainder another holds (42P17), a NULL range bound (42601) and a string without a digit for an integer key (22P02) refused, for one-column keys of integer types, text and varchar (lists), date (ISO literals) and boolean with constant bounds (MINVALUE, MAXVALUE); other key types, multi-column and expression keys, values not proven, a table whose partitioning is not held, partitions code may have created, and a new partition while the default partition may hold rows the set wrote may fail"},
	{"M4.P4", "CREATE INDEX WITH (...) and ALTER INDEX SET/RESET (...): storage parameters proven by reloptions.c for the access method; unknown names, repeats and values out of bounds refused, fractions for integer options and other forms not proven (Revision 1.5 U9); CREATE UNIQUE INDEX on a partitioned table (or one with partitioned partitions): btree, every partition key column a bare key column (not INCLUDE), else refused; a column with an operator class or COLLATE, or a table whose partitioning is not known, not proven (Revision 1.5 U4); Revision 1.6 V9: a written operator class of pg_opclass.dat for the method must accept the column type (equal to opcintype, binary-coercible, or polymorphically matched), a proven pg_catalog type it does not accept refused (42804), an E1 class as its extension declares, otherwise may fail; a UNIQUE index of a method other than btree refused (0A000, amcanunique); Revision 1.9 Y25: the access method's capabilities (amcaps.go, generated from the REL_17_6 handlers): UNIQUE, more than one key column, INCLUDE and ASC, DESC or NULLS FIRST / LAST where the method lacks them (0A000), more than 32 key and INCLUDE columns (54011), in CREATE INDEX and in EXCLUDE (which also needs amgettuple); an access method outside the core six may fail"},
	{"M5f-u.P4", "USING INDEX: a unique, non-partial btree index over plain columns with default ordering, not built CONCURRENTLY, no constraint attributes, not NULLS NOT DISTINCT under PRIMARY KEY, no second primary key, the table not partitioned; a property the records prove false is refused, one they do not prove may fail (Revision 1.5 U1)"},
	{"M5a.P4", "ADD COLUMN: the column definition as M1.P4 (Revision 1.4 R3); a PRIMARY KEY or UNIQUE column on a partitioned table as M4.P4's unique indexes (Revision 1.5 U4)"},
	{"M5f.P4", "ADD CONSTRAINT: key columns written once (Revision 1.4 R3); PRIMARY KEY and UNIQUE on a partitioned table as M4.P4's unique indexes, EXCLUDE on one not proven (Revision 1.5 U4)"},
	{"M3.P4", "REFERENCES: a permanent table cannot reference an unlogged one; Revision 1.4 R3: MATCH PARTIAL and an ON UPDATE column list are refused, an ON DELETE SET column list names key columns only, a generated key column takes no action that writes it"},
	{"M5k-a.P4", "SET LOGGED/UNLOGGED: foreign-key persistence pairs must be proven; Revision 1.9 Y30: ALTER INDEX SET (...) or RESET (...) of a partitioned index refused (42809)"},
	{"M5k-b.P4", "ALTER TABLE words that depend on an object or option the pass does not track may fail; identity needs an integer type; Revision 1.9 Y51: ENABLE or DISABLE [REPLICA | ALWAYS] TRIGGER ALL, or of a system trigger, may fail (a table's internal triggers need a superuser); E6: OWNER TO a role the set created may fail"},
	{"N2.P4", "SET/RESET: the parameter's context is user; superuser contexts may fail; postmaster, sighup, internal, backend and superuser-backend are refused (Revision 1.5 U7); Revision 1.9 Y04: SET CONSTRAINTS names {DEFERRED | IMMEDIATE} (trigger.c AfterTriggerSetState): a name that is not a ColId or a statement that is not ALL or a name list followed by the mode refused (42601); an unqualified name held with its deferrability read on set tables whose constraint names are exact (C3), with E4's search_path, no temporary table and no domain, constraint trigger or code that may hold the name: DEFERRED refused (42809) when one held constraint of the name is not deferrable (FOREIGN KEY, UNIQUE, PRIMARY KEY or EXCLUDE without DEFERRABLE, any CHECK), claimed otherwise, IMMEDIATE claimed; any other name may fail; SET CONSTRAINTS ALL claimed; Revision 1.9 Y51: ALTER ROLE ... SET or RESET, a routine's SET clause and ALTER FUNCTION ... SET of a two-part placeholder parameter (a stored setting checks it as superuser-only), and SET SESSION AUTHORIZATION of a role other than the session's, may fail"},
	{"N1.P4", "SECURITY LABEL needs a loaded label provider (not granted by E1-E4); ALTER SEQUENCE options as R1.P4 over the sequence's current parameters (a sequence created with options the pass read, or a serial or identity column's sequence; otherwise only the checks that need no current value), bounds that narrow over an unknown last value not proven, OWNED BY of an identity sequence refused (Revision 1.5 U5); Revision 1.9: CREATE TRIGGER (and O1t): a set routine the pass read with one definition that is a procedure or takes arguments (42883) or does not return trigger (42P17) refused, several definitions or an unread header may fail (Y09; T024c1b: public.f names the set's unqualified f created under E4's search_path); the relation kind as CreateTriggerFiringOn (table and partitioned table BEFORE or AFTER; view INSTEAD OF FOR EACH ROW or BEFORE/AFTER FOR EACH STATEMENT, no TRUNCATE, no transition table; foreign table BEFORE or AFTER, no constraint trigger, no transition table; any other kind refused; 42809, 0A000), a kind not certain may fail (Y10); TRUNCATE FOR EACH ROW, INSTEAD OF without FOR EACH ROW or with WHEN or a column list, ROW variables, and transition tables on a constraint trigger, not AFTER, with TRUNCATE, more than one event, a column list, NEW without INSERT or UPDATE, OLD without DELETE or UPDATE, twice, or under one name, or FOR EACH ROW on a partitioned table, partition or inheritance child, refused (Y11); CREATE and ALTER POLICY: INSERT with USING, SELECT or DELETE with WITH CHECK (42601; ALTER by the command the pass recorded), a relation other than a table or partitioned table (42809) refused (Y12); CREATE and ALTER PUBLICATION: an unknown or repeated option or publish value (42601), FOR, ADD or SET TABLE of a relation other than a table or partitioned table or of an unlogged table (22023) refused, a possibly temporary table may fail (Y31); CREATE COLLATION: an unknown or repeated attribute, LOCALE with LC_COLLATE or LC_CTYPE, FROM with another option (42601), an unknown provider, a deterministic value that is no boolean, a missing locale (builtin, icu) or lc_collate or lc_ctype (libc), a nondeterministic collation or rules with a provider other than icu, refused (Y35); GRANT ... TO PUBLIC WITH GRANT OPTION, also in ALTER DEFAULT PRIVILEGES, refused (0LP01, Y37); COMMENT ON TABLE, VIEW, MATERIALIZED VIEW, SEQUENCE, FOREIGN TABLE, INDEX, TYPE or DOMAIN of a name the pass holds as a kind the word does not take (TYPE: a sequence or index) refused (42809, 42704), followed through renames and CREATE TABLE IF NOT EXISTS, a kind not certain may fail (Y42); Revision 1.9 Y51 (D-superuser, E6; superuserForms): CREATE ROLE, USER or GROUP with SUPERUSER, ALTER ROLE with SUPERUSER or NOSUPERUSER, ALTER ROLE ALL ... SET, CREATE CAST ... WITHOUT FUNCTION, CREATE LANGUAGE, CREATE OPERATOR CLASS or FAMILY, ALTER OPERATOR FAMILY, CREATE TEXT SEARCH PARSER or TEMPLATE, CREATE ACCESS METHOD, CREATE or ALTER FOREIGN DATA WRAPPER, CREATE PUBLICATION FOR ALL TABLES or TABLES IN SCHEMA and ALTER PUBLICATION ADD or SET TABLES IN SCHEMA may fail (only a superuser may run them); E6: CREATE CAST may fail unless the set created its source or target type or it is an E1 type (the migrating role must own one), and ALTER ... OWNER TO a role the set created may fail (the new owner needs CREATE on the schema)"},
	{"R1.P4", "CREATE SEQUENCE options: each written once (42601), signed integer constants in int8 range, AS smallint, integer or bigint, and the parameters sequence.c init_params computes (INCREMENT not 0, MINVALUE < MAXVALUE within the type's range, START and RESTART within the bounds, CACHE >= 1; 22023); OWNED BY NONE or a column of a table held under a certain name in the sequence's schema (55000, 42703); other values not proven (Revision 1.5 U5); Revision 1.9 Y51: CREATE TYPE of a base or shell type (CREATE TYPE name, alone or with a parameter list) may fail (only a superuser may create one)"},
	{"R4.P4", "ALTER TYPE ... SET (...) may fail: it changes a base type's properties, which only a superuser may do (Revision 1.9 Y51; typecmds.c 4493); E6: ALTER TYPE or DOMAIN ... OWNER TO a role the set created may fail"},
	{"R5.P4", "DROP <kind> of an object held as another kind is refused; Revision 1.6 V4 (kinds, 2026-11-07): DROP INDEX [IF EXISTS] of a name held as another relation kind is refused (42809)"},
	{"R6.P4", "functions and procedures: the kind a statement names must be the kind the set holds; Revision 1.6 D2: the options of CREATE FUNCTION/PROCEDURE at most once each except SET (42601), volatility, STRICT, LEAKPROOF, COST, ROWS and PARALLEL refused for a procedure (42P13), COST and ROWS positive constants, ROWS only for a set-returning function (22023), PARALLEL SAFE, RESTRICTED or UNSAFE, a SET clause proven as N2.P5 proves SET; TRANSFORM, WINDOW, SUPPORT and options not read may fail; Revision 1.9 Y16: the actions of ALTER FUNCTION, PROCEDURE and ROUTINE as the CREATE options (an option twice, function attributes on a procedure, COST and ROWS, PARALLEL), against the one held definition when the pass read it; ROWS, and a ROUTINE's function attributes, on a routine not read may fail; Revision 1.9 Y51: a function or procedure with LEAKPROOF or SUPPORT, or in a language other than plpgsql and sql (c, internal, the untrusted procedural languages), may fail (only a superuser may create it); E6: ALTER FUNCTION, PROCEDURE, ROUTINE or AGGREGATE ... OWNER TO a role the set created may fail"},
	{"R6.P5", "CREATE FUNCTION and PROCEDURE: a polymorphic result or OUT parameter that no input determines refused (42P13; check_valid_polymorphic_signature: anyrange and anymultirange need an anyrange or anymultirange input, the other anyelement-family types any of that family, anycompatiblerange and anycompatiblemultirange an input of those two, the other anycompatible-family types any of that family, internal an internal input; Y17); a type modifier on an argument, OUT, RETURNS or RETURNS TABLE type as M2.P5 (42601 when the type takes none; Y18); the SET values of ALTER FUNCTION, PROCEDURE and ROUTINE as N2.P1 and N2.P5 (Y15) (Revision 1.9)"},
	{"R6.P3", "CREATE OR REPLACE FUNCTION/PROCEDURE over a held routine of the same identity (schema, name, input types of pg_catalog): kind (42809), return type and SETOF, OUT/INOUT/TABLE columns, named input parameters, and at least its defaults kept (42P13), else refused; an identity the pass cannot decide (types outside pg_catalog, a schema not proven, a routine changed by a statement not read) not proven (Revision 1.5 U6)"},
	{"R6.P2", "ALTER FUNCTION, PROCEDURE or ROUTINE ... RENAME TO g: refused (42723) when the set holds g with the same argument types in the same schema; may fail when the argument types or the held routines are not proven (Revision 1.9 Y16)"},
	// P5 columns and expressions (B1b, with Revision 1.1 volatility).
	{"M1.P5", "CREATE TABLE: DEFAULT result types assignable to the column; generation expressions IMMUTABLE and typed; CHECK expressions proven boolean over held columns with proven functions and operators, EXCLUDE only btree WITH = or gist over ranges (Revision 1.4 R1); Revision 1.6 V3: a generation expression, partition key or EXCLUDE element holding a SQL value function written as a keyword refused (42P17); Revision 1.9 Y46: a DEFAULT that is NULL, (NULL), or NULL cast (::, CAST) any number of times to unknown or to the column's type, with or without COLLATE, on a column whose type is not a domain and has no modifier, is stored as no default (atthasdef false)"},
	{"M2.P5", "column types: modifiers that the type's typmodin accepts; a time precision above 6 is recorded as 6; other modifiers are not proven (Revision 1.4 R4)"},
	{"M4.P5", "CREATE INDEX: expression elements and the predicate IMMUTABLE and typed (Revision 1.1); Revision 1.6 V3: an element or the predicate holding a SQL value function written as a keyword (user, current_user, ...: STABLE) refused (42P17)"},
	{"M5a.P5", "ADD COLUMN: as M1.P5; Revision 1.9 Y46: the NULL defaults of M1.P5"},
	{"M5f.P5", "ADD CONSTRAINT: CHECK and EXCLUDE as M1.P5 (Revision 1.4 R1)"},
	{"M5f.P7", "constraint attributes in CREATE TABLE and ALTER TABLE ADD: NOT DEFERRABLE with INITIALLY DEFERRED and conflicting attribute words refused (42601); DEFERRABLE or INITIALLY DEFERRED on CHECK, NOT VALID on UNIQUE, PRIMARY KEY or EXCLUDE, NO INHERIT on any but CHECK refused (0A000, processCASbits); a column constraint's DEFERRABLE, NOT DEFERRABLE and INITIALLY words only after PRIMARY KEY, UNIQUE or REFERENCES, once each (42601); NOT VALID on a column constraint, and NO INHERIT after one other than CHECK, refused (42601) (Revision 1.9 Y26)"},
	{"M5c.P5", "ALTER COLUMN TYPE: an assignment cast without USING, a proven USING result, the default re-coerced; Revision 1.9 Y01: the new type of an identity column must be smallint, integer or bigint after alias normalisation (22023; a domain over them too)"},
	{"M5e.P5", "SET DEFAULT: the result type assignable to the column; Revision 1.9 Y46: SET DEFAULT of a NULL form M1.P5 lists removes the default"},
	{"N1.P5", "COMMENT ON COLUMN, CREATE TRIGGER ... UPDATE OF and GRANT/REVOKE (cols): the columns held on a table whose facts are known; CREATE STATISTICS, a trigger's WHEN and a policy's USING/WITH CHECK (other than TRUE or FALSE) not proven (Revision 1.3); Revision 1.9: CREATE and ALTER ROLE VALID UNTIL proven only for 'infinity', '-infinity' and YYYY-MM-DD[ HH:MM[:SS[.frac]]][zone] with each field in range, otherwise may fail (Y14); ALTER ROLE [IN DATABASE] ... SET and RESET as N2.P1 and N2.P5 (Y15)"},
	{"N3.P5", "DML values (Revision 1.3): every written value, default, condition and RETURNING item in the V1-V4 allowlist (literals by the B2 grammars), and no constraint, trigger or per-row expression the write may meet whose outcome depends on rows or values the pass does not prove; B2: V5 a single row into a table proven empty meets its keys, ON CONFLICT DO NOTHING arbiters (no deferrable key), a MATCH SIMPLE foreign key with a NULL column; V6 a fresh sequence's first value; Revision 1.9 Y23: a VALUES list of two or more rows that is a query (a VALUES statement, a parenthesised VALUES, CREATE TABLE or VIEW ... AS VALUES; not INSERT's VALUES, whose rows are coerced to the target columns one by one): each column's common type is resolved as select_common_type does from constants (integers as int4, int8 or numeric by size, decimals as numeric, TRUE and FALSE as boolean, a :: cast to int2, int4, int8, numeric, text or boolean; untyped strings and NULL alone are text): types of different categories refused (42804), an untyped string the common type's input does not prove refused or may fail (22P02), rows of different lengths refused (42601), an entry that is not such a constant may fail"},
	{"N3.P4", "REFRESH MATERIALIZED VIEW CONCURRENTLY (Revision 1.9 Y24; matview.c RefreshMatViewByOid): with WITH NO DATA refused (42601); of a materialized view the set created WITH NO DATA or last refreshed WITH NO DATA refused (0A000); of one whose index record is exact (no index of a generated name dropped, no IF NOT EXISTS skip, no code that may create or drop an index) with no unique btree index without a predicate on plain columns refused (55000); claimed when it is populated and holds such an index; a materialized view the pass does not hold, or an inexact record, may fail"},
	{"N3.P6", "REINDEX INDEX of a partitioned index and REINDEX TABLE of a partitioned table inside a transaction block refused (25001); a table whose kind is not proven may fail (Revision 1.9 Y30)"},
	{"R1.P5", "CREATE DOMAIN: the base type's modifiers as M2.P5, one DEFAULT assignable to the base type, not NULL with NOT NULL, COLLATE on a collatable base type, each CHECK proven boolean over VALUE; other clauses not proven; CREATE TYPE AS (...): attribute type modifiers and collations as for columns (Revision 1.4 R1, R3, R4); Revision 1.6 V12: CREATE VIEW WITH (...) checked against the view options of reloptions.c, CREATE MATERIALIZED VIEW WITH (...) against the heap ones; a check_option or WITH CHECK OPTION may fail (the pass does not prove a view automatically updatable); Revision 1.9 Y20: a composite type's attribute named twice (42701) and a domain's constraint name written twice (42710) refused"},
	{"M5k-a.P5", "ALTER TABLE SET/RESET (...): storage parameters proven by reloptions.c for a table whose kind is known (heap; any parameter on a partitioned table refused); RESET of any name without a value accepted (Revision 1.5 U9); Revision 1.6 V12: on a relation held as a view or materialized view, against the view or heap options"},
	{"N2.P5", "SET: the value proven for the parameter's type (bool words, integer and real constants within bounds, enum values; a parameter with a check hook only by its closed table; T024c1b: search_path any value, check_search_path checks only the list syntax, which flatten_set_variable_args's quoting makes valid), a certain violation refused (Revision 1.5 U7); Revision 1.6 V10: SET NAMES and SET client_encoding: DEFAULT and the UTF8 names of encnames.c (compared as clean_encoding_name) proven, an unknown name refused (22023), any other encoding may fail and makes later migrations O4"},
	{"N2.P6", "Transaction modes of sqlx's transaction (Revision 1.9 Y03; variable.c check_transaction_read_only, check_transaction_isolation, check_transaction_deferrable; pquery.c PlannedStmtRequiresSnapshot; utility.c PreventCommandIfReadOnly): SET TRANSACTION ISOLATION LEVEL (and transaction_isolation) to a level other than the current one refused (25001) after a statement that took a snapshot (every statement but transaction control, LOCK, SET, RESET, SHOW, SET CONSTRAINTS, FETCH, MOVE, LISTEN, NOTIFY, UNLISTEN, CHECKPOINT) or inside a savepoint; [NOT] DEFERRABLE refused after a snapshot or inside a savepoint; READ WRITE in a read-only transaction refused after a snapshot or inside a savepoint; SET TRANSACTION SNAPSHOT refused after a snapshot (25001) or under READ COMMITTED (0A000), otherwise may fail; in a read-only transaction DDL, DML and every other statement PreventCommandIfReadOnly guards refused (25006), a query, CALL, DO, EXECUTE, EXPLAIN, DECLARE or COPY may fail; in a no-transaction migration these statements may fail"},
	{"N2.P13", "Session transaction defaults (Revision 1.9 Y03): SET (not SET LOCAL) of default_transaction_read_only, default_transaction_isolation and default_transaction_deferrable, SET SESSION CHARACTERISTICS AS TRANSACTION, their RESET and RESET ALL (E4's values) set the modes every later transaction starts with, from the next transaction on (after a COMMIT in the string, and every later migration); a value not read makes them unknown, and a statement Y03 judges then may fail; in a no-transaction migration these statements may fail"},
	{"R1b.P5", "the query of a view, CREATE TABLE AS or SELECT INTO, and a LANGUAGE sql body, are not proven; a LANGUAGE sql AS body only while check_function_bodies is on or unknown (Revision 1.6 V1: off, the validator does not parse it); T024c1c: a LANGUAGE sql function's AS body that the validator parse-analyses at creation (fmgr_sql_validator) is proven in one form: named parameters and a result of pg_catalog base types, options LANGUAGE, volatility, STRICT, LEAKPROOF, SECURITY and PARALLEL only, one SELECT of one output expression of the RETURNS type over at most one table held with known columns under a certain name ([AS] alias) with an optional WHERE; a name is a column of its level's table before a parameter (an outer level's column is not proven); EXISTS of the same form, one integer subscript of a parenthesised array, pg_catalog functions the set does not overload and the platform functions of SB-M8; never after an opaque statement, an operator, cast, operator class or family, access method or aggregate of the set, or a statement naming row_security"},
	// P6 transaction structure (B1b).
	{"T1.P6", "transaction control: the outcome in each block state (the P6 table in transactions.go)"},
	{"T1.P4", "LOCK [TABLE]: a name the pass holds as a materialized view, foreign table, sequence, index or composite type is refused (42809); only a table, a partitioned table and a view (with its base relations, whatever their kinds) are locked; a kind not certain may fail (Revision 1.9 Y05)"},
	{"M4.P6", "CREATE INDEX CONCURRENTLY is refused in any block; outside one (C4) it is refused on a partitioned table and not proven on a table the pass does not hold"},
	{"M7.P6", "DROP INDEX CONCURRENTLY is refused in any block; outside one (C4) it drops one index without CASCADE, is refused on a partitioned table and is not proven for an index the pass does not hold"},
	{"F1.P6", "statements PreventInTransactionBlock refuses in any block"},
	// P7 syntax (B1b).
	{"M4.P7", "CREATE INDEX IF NOT EXISTS needs a name; a keyword its name position does not accept (Revision 1.5 U8); Revision 1.6 V3: index elements, operator classes and expression column references follow the keyword categories of index_elem, opt_qualified_name and columnref"},
	{"M1.P7", "CREATE TABLE (also M1b): an unquoted keyword in a name position whose gram.y nonterminal (ColId, type_function_name, ColLabel) does not accept its kwlist.h category is a syntax error (Revision 1.5 U8); Revision 1.6 V3: partition keys, EXCLUDE elements, CHECK and generation expressions likewise (part_elem, ExclusionConstraintElem, columnref); Revision 1.9 Y26: constraint attributes of CREATE TABLE as M5f.P7"},
	{"M5a.P7", "ALTER TABLE (every M5 row): a keyword a name position does not accept, and a storage parameter list item that is not name [= value] (Revision 1.5 U8, U9); Revision 1.6 V3: CHECK, generation and EXCLUDE expressions of ADD likewise"},
	{"M8.P7", "ALTER INDEX: a keyword a name position does not accept (Revision 1.5 U8); Revision 1.6 V7: ALTER INDEX forms other than RENAME TO and SET / RESET (...) are row D"},
	{"R2.P7", "CREATE TYPE: a keyword a name position does not accept (Revision 1.5 U8); the whole statement read as CREATE TYPE n AS ENUM ( [Sconst [, ...]] ), otherwise row D (Revision 1.6 V7)"},
	{"R2.P5", "CREATE TYPE AS ENUM: a label written twice (23505) or longer than 63 bytes (42602) refused; a label the pass does not decode may fail (Revision 1.9 Y19)"},
	{"R6.P7", "CREATE/ALTER FUNCTION/PROCEDURE: an unqualified routine name is a type_function_name (no col_name or reserved keyword) (Revision 1.5 U8); ALTER FUNCTION/PROCEDURE/ROUTINE read whole as RENAME TO, OWNER TO, SET SCHEMA, [NO] DEPENDS ON EXTENSION or alterfunc_opt_list [RESTRICT], otherwise row D (Revision 1.6 V7)"},
	{"N1.P7", "N1 statements: a keyword a name position does not accept (Revision 1.5 U8); the whole statement read by one of the N1 productions of Revision 1.6 V7 (COMMENT, GRANT/REVOKE, ALTER DEFAULT PRIVILEGES, roles, CREATE TRIGGER/POLICY/COLLATION/STATISTICS/PUBLICATION/OPERATOR/CAST/AGGREGATE, NOTIFY/LISTEN/UNLISTEN, CHECKPOINT, SECURITY LABEL, REASSIGN OWNED, DROP TRIGGER/POLICY/RULE, ALTER POLICY/TRIGGER/GROUP/DATABASE/SEQUENCE, other DROP kinds), otherwise row D; Revision 1.9: CREATE and ALTER ROLE (USER, GROUP) options given twice or conflicting (42601) and CONNECTION LIMIT below -1 (22023) refused (Y14); ALTER DEFAULT PRIVILEGES with IN SCHEMA or FOR ROLE given twice refused (42601, Y36)"},
	{"N1.P8", "DROP ROLE, USER or GROUP of a role the set created (Revision 1.9 Y38; user.c DropRole, checkSharedDependencies): refused (2BP01) while the set gave it a live shared dependent: a table grant until REVOKE of the same privileges (REVOKE ALL included) or the table's drop, ownership by OWNER TO (a set table's until it is dropped or owned by another role), CREATE SCHEMA ... AUTHORIZATION, policies TO it until DROP POLICY or the table's drop, ALTER DEFAULT PRIVILEGES, any other grant, and any statement while SET ROLE it was in force; DROP OWNED BY ends them, REASSIGN OWNED BY the owned ones; refused (55006) while it is the current role; may fail after SET SESSION AUTHORIZATION, code that may create, own or grant, or a statement naming it that the pass does not read; memberships, settings and comments are not dependents"},
	{"M5k-b.P7", "RENAME, SET SCHEMA and ATTACH/DETACH PARTITION are whole statements, not list items; the subcommand read whole by its alter_table_cmd or partition_cmd production, otherwise row D (Revision 1.6 V7)"},
	{"N3.P7", "SELECT INTO: no DISTINCT or target after the target; INTO is no alias; no INTO in data-changing statements; a keyword a name position of a column list does not accept (Revision 1.5 U8); CLUSTER, REINDEX, REFRESH MATERIALIZED VIEW, TABLE and COPY (name, columns, direction, file or STDIN/STDOUT/PROGRAM, the option list) read whole, otherwise row D (Revision 1.6 V7)"},
	{"R1.P7", "no SELECT INTO in a view's or CREATE TABLE AS's query; a keyword a name position does not accept (Revision 1.5 U8); CREATE SCHEMA read whole (name, AUTHORIZATION, elements by their own rows' readers) and the shell, composite and range forms of CREATE TYPE, otherwise row D (Revision 1.6 V7)"},
	// V7 whole-statement syntax (Revision 1.6).
	{"M1b.P7", "CREATE TABLE ... LIKE / OF / PARTITION OF / INHERITS read whole (elements, bound, OptPartitionSpec, USING, WITH, ON COMMIT, TABLESPACE in gram.y's order), otherwise row D (Revision 1.6 V7)"},
	{"M5e.P7", "SET DEFAULT on a table the pass does not hold: the expression read whole by the syntax-only expression reader, otherwise may fail (Revision 1.6 V7)"},
	{"M5k-a.P7", "the subcommand read whole by its alter_table_cmd production, otherwise row D (Revision 1.6 V7)"},
	{"M9.P7", "ALTER SCHEMA s RENAME TO t / OWNER TO r read whole, otherwise row D (Revision 1.6 V7)"},
	{"R3.P7", "ALTER TYPE n ADD VALUE [IF NOT EXISTS] Sconst [{BEFORE | AFTER} Sconst] and RENAME VALUE Sconst TO Sconst read whole, otherwise row D (Revision 1.6 V7)"},
	{"R4.P7", "ALTER TYPE (RENAME TO, RENAME ATTRIBUTE, OWNER TO, SET SCHEMA, ADD/DROP/ALTER ATTRIBUTE), ALTER DOMAIN (AlterDomainStmt, RENAME, OWNER, SET SCHEMA), ALTER VIEW / MATERIALIZED VIEW (RENAME, RENAME COLUMN, SET SCHEMA, OWNER TO, [NO] DEPENDS ON EXTENSION, SET/RESET (...), ALTER COLUMN SET/DROP DEFAULT), ALTER SEQUENCE RENAME TO and ALTER COLLATION read whole, otherwise row D (Revision 1.6 V7)"},
	{"R5.P7", "DROP {TYPE | DOMAIN | VIEW | MATERIALIZED VIEW | SEQUENCE | COLLATION | ...} [IF EXISTS] names [CASCADE | RESTRICT], DROP {FUNCTION | PROCEDURE | ROUTINE | AGGREGATE | OPERATOR} with argument types, DROP CAST read whole, otherwise row D (Revision 1.6 V7)"},
	{"N2.P7", "RESET {name | ALL | TIME ZONE | TRANSACTION ISOLATION LEVEL | SESSION AUTHORIZATION} and DISCARD {PLANS | SEQUENCES | TEMP} read whole; SET NAMES takes a string constant or DEFAULT (gram.y opt_encoding), otherwise row D (Revision 1.6 V7)"},
	{"T1.P7", "BEGIN [WORK | TRANSACTION], START TRANSACTION, COMMIT / END / ROLLBACK / ABORT [WORK | TRANSACTION] [AND NO CHAIN], SAVEPOINT, RELEASE [SAVEPOINT], ROLLBACK TO [SAVEPOINT], PREPARE TRANSACTION, COMMIT/ROLLBACK PREPARED and LOCK read whole; transaction modes and AND CHAIN are row D (Revision 1.6 V7)"},
	{"O1.P7", "DO [LANGUAGE l] body, CALL f(args read by the syntax-only expression reader) and EXECUTE name [(args)] read whole, otherwise row D (Revision 1.6 V7)"},
	{"O2.P7", "CREATE EXTENSION [IF NOT EXISTS] name [WITH] [SCHEMA s] [VERSION v] [CASCADE], DROP EXTENSION [IF EXISTS] names [CASCADE | RESTRICT], ALTER EXTENSION name UPDATE [TO v] read whole; other ALTER EXTENSION forms are row D (Revision 1.6 V7); Revision 1.9 Y33: CREATE EXTENSION with SCHEMA, VERSION or CASCADE given twice refused (42601)"},
	{"O2.P4", "Extension versions (Revision 1.9 Y33; extension.c CreateExtension, find_install_path, ExecAlterExtensionStmt; the generated table e1ExtensionVersions of the REL_17_6 contrib control files and scripts): CREATE EXTENSION ... VERSION v claimed only for the control file's default version, refused (22023) when no install script reaches v through update scripts, otherwise may fail, and may fail for an extension outside the table; ALTER EXTENSION x UPDATE [TO v] claimed when v is the version the set installed (a notice) or the default version reached from it, refused (22023) when no update path leads from the installed version to v, otherwise may fail; CREATE EXTENSION of an extension the set installed (or plpgsql, which initdb installs) without IF NOT EXISTS refused (42710), with it skipped; Revision 1.9 Y51: CREATE EXTENSION of an extension whose REL_17_6 control file is not trusted = true (with CASCADE, or that requires one), and ALTER EXTENSION ... UPDATE of one, may fail (only a superuser may install it)"},
	{"O5.P4", "CREATE EVENT TRIGGER and ALTER EVENT TRIGGER may fail: only a superuser may run them, and E6 grants no superuser (Revision 1.9 Y51; event_trigger.c 137)"},
	{"O2.P8", "DROP EXTENSION [IF EXISTS] names without CASCADE (Revision 1.9 Y34; dependency.c reportDependentObjects, 2BP01; the generated table e1ExtensionMembers of the names each REL_17_6 contrib extension's scripts create): refused while another extension the set installed requires it (control file requires) or a live entry of the dependency record (X1) names a member; may fail for an extension whose scripts create operators, or one outside the table, while any live entry exists, and while code may have created objects"},
	{"O3.P7", "DISCARD ALL and ALTER ROLE ... SET read whole, otherwise row D (Revision 1.6 V7)"},
	{"O5.P7", "CREATE EVENT TRIGGER, DROP OWNED BY and DROP ... CASCADE read whole, otherwise row D (Revision 1.6 V7)"},
	// Revision 1.6 D1 (V2, V11, V12, V13).
	{"R6.P1", "CREATE FUNCTION/PROCEDURE: the language must exist; sql and plpgsql (an identifier folded, or a string constant spelled exactly so) are proven, every other language may fail (Revision 1.6 V2)"},
	{"R6.P14", "CREATE FUNCTION/PROCEDURE ... LANGUAGE plpgsql: the validator refuses a pseudo-type result other than trigger, event_trigger, record, void and the polymorphic types and a pseudo-type argument other than record and the polymorphic types (0A000), LANGUAGE sql a pseudo-type result other than record, void and the polymorphic types and a pseudo-type input argument other than the polymorphic types (42P13), a function without RETURNS and OUT parameters is refused (42P13); with check_function_bodies on or unknown the body is compiled and applied only in the provable subset (one block [<<l>>] [DECLARE name type [:= expr];...] BEGIN ... [EXCEPTION WHEN cond [OR cond] THEN ...] END [l] [;]; assignments to declared variables, parameters and NEW/OLD.col of a trigger function, RETURN per make_return_stmt, IF/ELSIF/ELSE, NULL, RAISE [level] 'fmt' [, expr], EXECUTE expr, PERFORM query, INSERT/UPDATE/DELETE of the expression reader's subset, statements the V7 readers read completely, T024c1b: SELECT with at most one INTO [STRICT] at parenthesis depth 0 whose targets are parameters or declared variables (several only of a pg_catalog type that is not composite) and the rest a query of the syntax-only reader; declared names may be unreserved SQL keywords; expressions of the syntax-only reader, whose FROM items are relations or, T024c1b, function calls); a certain compile error inside it (end label, trailing text, duplicate declaration, unknown variable, NEW.x outside a trigger function, RETURN against the result, RAISE parameter count, unknown condition, bad SQLSTATE, serial declared type, a trigger function with parameters) refused, anything else may fail (Revision 1.6 V1)"},
	{"O1.P14", "DO: the LANGUAGE clause as R6.P1 (Revision 1.6 V2); a PL/pgSQL DO body is compiled before it runs and is applied only in the provable subset of R6.P14 (the routine kind of a DO block: RETURN without an expression), otherwise may fail; an O1 statement whose inventory (O1.P12) may call a set routine whose PL/pgSQL body is compiled at that call (check_function_bodies off or unknown at its creation, or a declared type outside pg_catalog) may fail unless that body is in the subset with pg_catalog types (Revision 1.6 V1); Revision 1.7 W3: likewise a set LANGUAGE sql routine whose AS body was created with check_function_bodies off or unknown is parsed at that call and may fail unless R1b.P5's constant-body reader proves it"},
	{"R4.P5", "ALTER VIEW / MATERIALIZED VIEW SET / RESET (...): checked against the view options (security_barrier, security_invoker, check_option) or the heap ones; a check_option may fail (Revision 1.6 V12)"},
	{"M2.P4", "a pg_type.dat pseudo-type (typtype p) as a column or composite attribute type refused (42P16), as a domain base refused (42804) (Revision 1.6 V13)"},
	{"M1b.P2", "CREATE TABLE ... LIKE / INHERITS: an explicit column named like a system column refused (42701); CREATE TABLE ... OF a composite type with such an attribute refused (Revision 1.6 V11); Revision 1.9 Y28 (and Y27's merge): LIKE of a held table whose columns are known: a column another LIKE source or a column definition also has refused (42701), LIKE of a source whose columns are not held may fail when the table has other columns, LIKE with INHERITS may fail; OF a name the pass holds as a type other than a composite type (an enum, domain or base type, or a table's row type) refused (42809); INHERITS of a temporary table from a permanent one, of a partitioned table, a partition or a relation that is not a table refused (42809), of the same parent twice refused (42P07), and inherited columns whose types or collations differ between parents or from the column definition refused (42804)"},
	{"R1.P2", "CREATE TABLE AS, SELECT INTO and CREATE MATERIALIZED VIEW: an output column name (alias, column list, or the name of a plain column reference, function call or cast) equal to a system column name refused (42701) (Revision 1.6 V11); Revision 1.9: CREATE SCHEMA of a pg_ name, also the role-named schema of AUTHORIZATION, refused (42939, Y06); a relation (table, view, materialized view, sequence, composite type, foreign table, index) created in pg_catalog or pg_toast refused (42501), any other object created in pg_catalog may fail (E6, Y07); CREATE SCHEMA, by name or by AUTHORIZATION, of a schema the set holds, or of public or information_schema under E5 unless the set dropped it, refused (42P06) and skipped with IF NOT EXISTS, and may fail when code may have created it (Y08, Y43); CREATE TYPE AS RANGE whose multirange type name (multirange_type_name, or makeMultirangeTypeName's) a pg_type entry the pass holds already takes refused (42710, Y21); a CREATE SCHEMA element qualified by another schema refused (42P15, Y32)"},
	{"R4.P2", "ALTER TYPE ... ADD ATTRIBUTE of a system column name on a type a typed table uses: refused with CASCADE (42701), may fail otherwise; any attribute change without CASCADE on a type a typed table uses refused (2BP01) (Revision 1.6 V11)"},
	// P8 dependents (B1b).
	{"M5c.P8", "ALTER COLUMN TYPE: indexes, keys, CHECK constraints and expression indexes that use the column; ALTER COLUMN TYPE and DROP COLUMN without CASCADE: a trigger the set created that depends on the column (UPDATE OF, WHEN other than OLD IS DISTINCT FROM NEW; Revision 1.4); Revision 1.9 Y02: any ALTER COLUMN TYPE of a table is refused (0A000) while a column of a table the pass holds has a written type that reaches the table's row type, under any name the table has had, through arrays, domains, composite attributes or a range subtype; it may fail while a live dependency entry of a statement that creates columns whose types the pass does not hold (LIKE, OF, INHERITS, CREATE TABLE AS, SELECT INTO, a materialized view) names the table; Revision 1.9 Y40: DROP COLUMN c and ALTER COLUMN c DROP IDENTITY without CASCADE may fail while a live dependency entry names a sequence c owns; Y41: DROP COLUMN and ALTER COLUMN TYPE of table t without CASCADE may fail while a live entry of a SQL-standard body (BEGIN ATOMIC, RETURN expr) names t, and no fact of the statement is claimed; with CASCADE, DROP COLUMN makes such routines, and the objects whose entries name the column's sequence, no longer held"},
	{"M5a.P8", "ALTER TABLE ONLY ... ADD COLUMN, ALTER COLUMN TYPE, ADD CHECK (without NO INHERIT) or RENAME COLUMN is refused while the table has inheritance children (the last two: Revision 1.4); Revision 1.9 Y27: the columns of a CREATE TABLE ... INHERITS child are recorded (its parents' columns merged with its own) and follow the parent's ADD and RENAME COLUMN: ALTER TABLE p ADD COLUMN c t without ONLY refused (42804) when a child or grandchild holds c with another type or collation (the same merges), may fail when a child's columns are not held or the column has constraints, NOT NULL, identity or generation; ALTER COLUMN TYPE without ONLY on a table with inheritance children may fail; on a recorded child, RENAME, DROP or ALTER COLUMN TYPE of an inherited column refused (42P16)"},
	{"M6.P8", "DROP TABLE without CASCADE: a table created LIKE it may use its sequences; Revision 1.8 X1 as replaced by Revision 1.9 Y39: and any dependent of the table the dependency record (R5.P8) names, an occurrence at the ON position of an object of the table and a REFERENCES target the foreign-key record decides excepted; Y40: the sequences its columns own are its names; Y52: DROP ... CASCADE also drops the columns of other tables whose type is its row type (R5.P8)"},
	{"R5.P8", "Revision 1.9 Y39 (replaces Revision 1.8 X1's record): DROP FUNCTION, PROCEDURE, ROUTINE, AGGREGATE, TYPE, DOMAIN, VIEW, MATERIALIZED VIEW, SEQUENCE, FOREIGN TABLE, COLLATION, SCHEMA or EXTENSION (Y34; and DROP TABLE, M6.P8) without CASCADE, with or without IF EXISTS, may fail while a live entry of the pass's dependency record has an occurrence that names the object under any name it has had (ALTER ... RENAME TO; its array and multirange names): every statement that can create an object with a pg_depend dependency adds an entry with the identity of its object and every word it writes (strings included; a routine's or DO body's DDL, or the whole body when it is not read or dynamic; a reg* type with a numeric constant names every object), no word removed; three positions are proven no dependency: the entry's own created name, the ON-table of an object of a table when a relation of that name is dropped, and a REFERENCES target the foreign-key record decides (a held table with known facts); an entry ends only when the object of its identity is removed: DROP of it (a trigger or policy by name and table; a routine without an argument list only when it has one held identity), DROP of its table, CREATE OR REPLACE of the same identity; Y45: a DROP that removes nothing ends none, and a rename moves the identity to the new name; Y40: the sequences a table's columns own (serial, identity, OWNED BY; the generated name clipped as ChooseRelationName does, with any numeric suffix) are names of the table; Y44: a statement that copies columns (CREATE TABLE AS, SELECT INTO, LIKE, INHERITS, PARTITION OF, OF, ALTER TABLE ... INHERIT, and views and materialized views) names the copied columns' types as the pass holds them, or every type when it does not, so DROP TYPE and DROP DOMAIN may fail while it is live; DROP SCHEMA also while the search_path may resolve to it; after DROP ... CASCADE the objects such entries created are no longer held and their CREATE or use may fail, and their entries stay live; Y52: DROP ... CASCADE of a table, view, type or domain drops every held table's column whose type is a dropped type, its array, or a domain or range over it (under any name it has had) with M5b's CASCADE effects, the table partial when the column's type or the dropped type is not exact; a partition-key column takes its table (M6 CASCADE), and the statement may fail when that is not exact"},
	{"N3.P8", "TRUNCATE: no known foreign key from a table not truncated (without CASCADE); CLUSTER: a proven clusterable index"},
	// P11 known rows (Revision 1.4 R5): rows the set's own statements
	// wrote are not E2's; a statement that checks or rewrites them is not
	// proven.
	{"M4.P11", "CREATE UNIQUE INDEX or an index with expressions or a predicate on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively)"},
	{"M5a.P11", "ADD COLUMN with NOT NULL (no default), a key, CHECK, REFERENCES or a generation expression on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively)"},
	{"M5c.P11", "ALTER COLUMN TYPE on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively)"},
	{"M5d.P11", "SET NOT NULL on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively)"},
	{"M5f-u.P11", "USING INDEX PRIMARY KEY over a nullable column on a table holding rows the set wrote (Revision 1.5 U1) (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively)"},
	{"M5f.P11", "ADD CONSTRAINT other than NOT VALID (keys, CHECK, EXCLUDE, FOREIGN KEY) on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively)"},
	// C3 exact constraint names (B3, row M5g-1).
	{"M5g-1.P1", "DROP CONSTRAINT of a name the table does not hold, on a table whose constraint names are exact, is refused without IF EXISTS"},
	{"M5g-1.P8", "DROP CONSTRAINT of a key a foreign key uses is refused without CASCADE; with CASCADE the foreign key is dropped when the key was its only candidate"},
	// P9 subcommand order (B1b).
	{"M5a.P9", "ALTER TABLE subcommands that name one column run in the P9 pass order, or may fail"},
	// P12 code effects (Revision 1.5 U2, codeeffects.go).
	{"O1.P12", "every O1 statement has a DDL inventory read from the bodies it may run (the DO body; the set-defined routines it names; for DML, EXECUTE and statements that run index expressions or view queries also every trigger function and every routine the set names elsewhere; closed over the routines the bodies call); unbounded (EXECUTE of anything but one string constant, SET, RESET or set_config, transaction control, a statement not read, DROP ... CASCADE, another language, a routine created by code) the migration may fail; the tables it names are partial (Revision 1.5 U2); Revision 1.6 V5: the inventory also lists the data statements of the bodies (INSERT, UPDATE, MERGE, COPY ... FROM, WITH) with their targets resolved as U3 resolves names (a target not resolved, one with triggers, a set rule that writes, or an unbounded inventory: every table), which from the statement on may hold rows the set wrote (P11); V8: for every relation the inventory may create, every name Postgres may generate with it is possibly held (<table>_<suffix> in pg_class, after the 63-byte truncation; the array type _<table> in pg_type), and DROP of such a name without CASCADE may fail; Revision 1.9 Y48: a CREATE FUNCTION or PROCEDURE the code runs (from a constant EXECUTE or as a static statement) is checked as at top level (R6.P14's compilation, R1b.P5's SQL body); a refusal or possible failure there makes the running statement may fail"},
	{"M1.P12", "CREATE TABLE of a name code may have created or left behind may fail; with IF NOT EXISTS its facts are unknown (Revision 1.5 U2)"},
	{"M1b.P12", "CREATE TABLE ... PARTITION OF, LIKE, OF or INHERITS of a source code names (partitions, columns or objects the pass does not see) may fail (Revision 1.5 U2)"},
	{"M4.P12", "CREATE INDEX of a name code may have created or left behind may fail (Revision 1.5 U2)"},
	{"M5a.P12", "ALTER TABLE (every M5 row): a constraint or new name code may have created, a name code may have dropped, and DROP COLUMN or DROP CONSTRAINT without CASCADE, ALTER COLUMN TYPE or RENAME on a table code names (dependents the pass does not see) may fail (Revision 1.5 U2)"},
	{"M6.P12", "DROP TABLE without CASCADE of a table code names (dependents the pass does not see) may fail (Revision 1.5 U2)"},
	{"M7.P12", "DROP INDEX without CASCADE of an index code names, or of an index of a table code names, may fail (Revision 1.5 U2)"},
	{"M8.P12", "ALTER INDEX RENAME TO a name code may have created or left behind may fail (Revision 1.5 U2)"},
	{"R1.P12", "CREATE VIEW/SEQUENCE/TYPE/DOMAIN/SCHEMA (also R2) of a name code may have created or left behind may fail; with IF NOT EXISTS or OR REPLACE its record is uncertain (Revision 1.5 U2)"},
	{"R3.P12", "ALTER TYPE ADD VALUE or RENAME VALUE of a type code names (labels the pass does not see) may fail (Revision 1.5 U2)"},
	{"R5.P12", "DROP <other kind> or DROP SCHEMA without CASCADE of an object code names, or of a schema code may have created objects in, may fail (Revision 1.5 U2)"},
	{"R6.P12", "CREATE FUNCTION/PROCEDURE without OR REPLACE of a routine code may have created, and DROP of a routine code names, may fail; a routine code creates, replaces, alters or drops has unknown definitions (R6.P3) (Revision 1.5 U2)"},
	{"N1.P12", "CREATE TRIGGER, POLICY, ROLE, PUBLICATION, COLLATION, AGGREGATE, OPERATOR or CAST of a kind code may have created a name of may fail; a kind code drops or renames leaves that kind's names unknown (N1.P2, M2.P1 collations) (Revision 1.5 U2)"},
	// P13 session state (Revision 1.5 U3).
	{"Q1.P13", "sqlx's unqualified INSERT INTO _sqlx_migrations at the end of each migration (and its UPDATE after a commit, for later migrations): applied only when the path in force reaches the default schema with only pg_catalog or set schemas before it; a path no entry of which can hold it refused (42P01); an unknown path or entry, a set relation named _sqlx_migrations, or a statement that may drop or rename public or _sqlx_migrations not proven; Revision 1.6 V6: the \"$user\" entry counts as any set schema; Revision 1.9 Y03: the INSERT runs in the transaction in force at the end of the string (a new one after the string committed sqlx's) and is refused (25006) when that transaction is read-only; the UPDATE after the commit runs in a new transaction with the session's defaults, and a committed default_transaction_read_only on (SET or SET SESSION CHARACTERISTICS) refuses it, which stops the run: every later migration is not applied"},
	{"O3.P13", "an unqualified CREATE under a search_path the pass reads lands in its first existing entry (the record carries that schema); no existing entry refused (3F000), pg_catalog first refused (42501); an unknown path or an unknown entry before the deciding one not proven, and the record keeps schema null with a search_path reason; Revision 1.6 V6: the \"$user\" entry of E4's path is unknown from the first statement that may create a schema (CREATE SCHEMA, AUTHORIZATION, ALTER SCHEMA RENAME, code) while such a schema may exist"},
	// P10 set reading.
	{"Q1.P10", "a member the pass could not read: every migration applied null; a member not UTF-8: the set is refused; a manifest the dialect depends on could not be read: no records"},
	{"Q1.P7", "a numeric literal scan.l REL_17_6 refuses anywhere in a migration (an integer, numeric or real followed by an identifier character, an exponent sign without digits, 0x, 0o or 0b without digits) or two numeric constants written together (1.2.3) refuses the whole migration (42601): Postgres parses the whole string before it runs any statement (Revision 1.9 Y22)"},
	// K2 enums (B2 as amended).
	{"R3.K2", "ADD VALUE: an existing label (without IF NOT EXISTS) or a missing BEFORE/AFTER label is refused; RENAME VALUE of a missing label or to an existing one is refused; unknown labels may fail; a label added in this transaction to a type that existed before it is unsafe to use before the commit (55P04); Revision 1.7 W7: labels are the decoded string values, and a label written in a form the pass does not decode (octal, hexadecimal or Unicode escapes, U&'...') makes the type's labels unknown (and may be any unsafe label); Revision 1.9 Y19: ADD VALUE and RENAME VALUE of a label longer than 63 bytes refused (42602)"},
}

// contractAssumptions names, for every row whose claim and every
// precondition whose verdict depends on an environment assumption (E1-E6
// and E8, docs/extract/schema.md "Environment assumptions"), the assumptions it
// rests on: a later task shows them next to the facts. It is metadata only
// (T018a2 Revision 1.9, developer requirement of 2026-11-08): no claim or
// verdict reads it. A row or precondition that is not listed rests on
// none (its verdict follows from the set's text and the pass's records).
// TestContractAssumptions checks the ids, and TestContractDocument that
// the tables of docs/extract/schema.md list the same.
var contractAssumptions = map[string][]string{
	"M1":        {"E1", "E4", "E5", "E6", "E8"},
	"M1b":       {"E1", "E4", "E5", "E6", "E8"},
	"M2":        {"E1", "E4", "E5", "E6", "E8"},
	"M3":        {"E1", "E4", "E5", "E6", "E8"},
	"M4":        {"E1", "E2", "E4", "E5", "E6", "E8"},
	"M5a":       {"E1", "E2", "E4", "E5", "E6", "E8"},
	"M5b":       {"E1", "E4", "E5", "E6", "E8"},
	"M5c":       {"E1", "E2", "E4", "E5", "E6", "E8"},
	"M5d":       {"E1", "E2", "E4", "E5", "E6", "E8"},
	"M5e":       {"E1", "E4", "E5", "E6", "E8"},
	"M5f":       {"E1", "E2", "E4", "E5", "E6", "E8"},
	"M5f-u":     {"E1", "E2", "E4", "E5", "E6", "E8"},
	"M5g":       {"E1", "E4", "E5", "E6", "E8"},
	"M5g-1":     {"E1", "E4", "E5", "E6", "E8"},
	"M5h":       {"E1", "E4", "E5", "E6", "E8"},
	"M5i":       {"E1", "E4", "E5", "E6", "E8"},
	"M5j":       {"E1", "E4", "E5", "E6", "E8"},
	"M5k-a":     {"E1", "E4", "E5", "E6", "E8"},
	"M5k-b":     {"E1", "E4", "E5", "E6", "E8"},
	"M6":        {"E1", "E4", "E5", "E6", "E8"},
	"M7":        {"E1", "E4", "E5", "E6", "E8"},
	"M8":        {"E1", "E4", "E5", "E6", "E8"},
	"M9":        {"E1", "E4", "E5", "E6", "E8"},
	"R1":        {"E1", "E4", "E5", "E6", "E8"},
	"R1b":       {"E1", "E3", "E4", "E5", "E6", "E8"},
	"R2":        {"E1", "E4", "E5", "E6", "E8"},
	"R3":        {"E1", "E4", "E5", "E6", "E8"},
	"R4":        {"E1", "E4", "E5", "E6", "E8"},
	"R5":        {"E1", "E4", "E5", "E6", "E8"},
	"R6":        {"E1", "E4", "E5", "E6", "E8"},
	"N1":        {"E1", "E4", "E5", "E6", "E8"},
	"N2":        {"E4", "E6", "E8"},
	"N3":        {"E1", "E2", "E4", "E6", "E8"},
	"O1":        {"E1", "E3", "E4", "E6", "E8"},
	"O1t":       {"E1", "E3", "E4", "E6", "E8"},
	"O2":        {"E1", "E3", "E6", "E8"},
	"O3":        {"E4", "E8"},
	"O4":        {"E4", "E8"},
	"O5":        {"E3", "E6", "E8"},
	"T1":        {"E8"},
	"Q1":        {"E4", "E5", "E8"},
	"M1.P1":     {"E1"},
	"M1b.P1":    {"E1"},
	"M3.P1":     {"E1"},
	"M4.P1":     {"E1"},
	"M5a.P1":    {"E1", "E4", "E6"},
	"M6.P1":     {"E1"},
	"M7.P1":     {"E1"},
	"M5f-u.P1":  {"E1"},
	"M2.P1":     {"E1", "E4"},
	"N2.P1":     {"E1"},
	"M8.P1":     {"E1", "E4"},
	"M9.P1":     {"E1"},
	"M9.P2":     {"E3", "E5"},
	"R5.P1":     {"E1"},
	"N3.P1":     {"E1"},
	"N1.P1":     {"E1", "E3", "E4", "E5"},
	"R3.P1":     {"E1", "E4"},
	"R4.P1":     {"E1", "E4", "E6"},
	"O1.P1":     {"E1", "E3", "E4"},
	"N1.P2":     {"E1", "E3", "E6"},
	"M4.P2":     {"E1"},
	"M1b.P5":    {"E2", "E3"},
	"M4.P4":     {"E1"},
	"M5f-u.P4":  {"E3"},
	"M5k-b.P4":  {"E6"},
	"N2.P4":     {"E3", "E4", "E6"},
	"N1.P4":     {"E1", "E4", "E5", "E6"},
	"R1.P4":     {"E6"},
	"R4.P4":     {"E3", "E6"},
	"R5.P4":     {"E4"},
	"R6.P4":     {"E6"},
	"R6.P3":     {"E1"},
	"N3.P5":     {"E3"},
	"N3.P4":     {"E1", "E3"},
	"N2.P5":     {"E4"},
	"N2.P6":     {"E3", "E4"},
	"N2.P13":    {"E4"},
	"R1b.P5":    {"E3", "E4"},
	"M4.P6":     {"E1"},
	"M7.P6":     {"E1"},
	"R6.P7":     {"E1", "E6"},
	"N1.P7":     {"E6"},
	"N1.P8":     {"E1", "E3", "E4", "E6"},
	"R1.P7":     {"E6"},
	"M5e.P7":    {"E1"},
	"M9.P7":     {"E6"},
	"R4.P7":     {"E1", "E6"},
	"N2.P7":     {"E4"},
	"O1.P7":     {"E3"},
	"O2.P7":     {"E1"},
	"O2.P4":     {"E1", "E6"},
	"O5.P4":     {"E6"},
	"O2.P8":     {"E1", "E3"},
	"R6.P1":     {"E1"},
	"R6.P14":    {"E1", "E3", "E4"},
	"O1.P14":    {"E1", "E3", "E4"},
	"R1.P2":     {"E3", "E5", "E6"},
	"M5c.P8":    {"E1", "E3"},
	"M5a.P8":    {"E1", "E3", "E6"},
	"M6.P8":     {"E1", "E3", "E6"},
	"R5.P8":     {"E1", "E3", "E4", "E6"},
	"N3.P8":     {"E1", "E3"},
	"M4.P11":    {"E2", "E3"},
	"M5a.P11":   {"E2", "E3"},
	"M5c.P11":   {"E2", "E3"},
	"M5d.P11":   {"E2", "E3"},
	"M5f-u.P11": {"E2", "E3"},
	"M5f.P11":   {"E2", "E3"},
	"M5g-1.P1":  {"E1"},
	"M5g-1.P8":  {"E1", "E3"},
	"O1.P12":    {"E1", "E2", "E3"},
	"M1.P12":    {"E1", "E3"},
	"M1b.P12":   {"E1", "E3"},
	"M4.P12":    {"E1", "E3"},
	"M5a.P12":   {"E1", "E3"},
	"M6.P12":    {"E1", "E3"},
	"M7.P12":    {"E1", "E3"},
	"M8.P12":    {"E1", "E3"},
	"R1.P12":    {"E1", "E3"},
	"R3.P12":    {"E1", "E3"},
	"R5.P12":    {"E1", "E3"},
	"R6.P12":    {"E1", "E3"},
	"N1.P12":    {"E1", "E3"},
	"Q1.P13":    {"E4", "E5"},
	"O3.P13":    {"E3", "E4"},
}

// precondition returns the id as diagnostics cite it. TestContractIDs
// checks every id the code passes here against contractPreconditions.
func precondition(id string) string {
	return " [contract " + id + "]"
}

// Rows of analyses (B7): analyze gives every statement the contract row it
// was produced under (analysis.row), statementRows adds the rows of its
// parts (column types, references, ALTER TABLE subcommands, queries), and
// the state adds the rows it decides (M5g-1, O1t, Q1). Each modelled form
// maps to exactly one row; a form no row covers is D. TestContractRows
// checks the ids against contractRows and that every row is exercised.

// noEffectN3Heads are the statement heads of row N3.
var noEffectN3Heads = map[string]bool{
	"insert": true, "update": true, "delete": true, "copy": true, "truncate": true, "merge": true,
	"select": true, "values": true, "with": true, "table": true, "(": true,
	"analyze": true, "cluster": true, "reindex": true, "refresh": true,
}

// opaqueRows maps the opaque kinds of analyze to their rows.
var opaqueRows = map[string]string{
	"do": "O1", "call": "O1", "execute": "O1", "function_call": "O1", "dml": "O1", "runs_code": "O1",
	"extension":      "O2",
	"search_path":    "O3",
	"lexing_setting": "O4",
	"event_trigger":  "O5", "drop_owned": "O5", "drop_cascade": "O5",
	"transaction": "T1",
}

// rowOf returns the contract row of one analysed statement.
func rowOf(toks []token, a analysis) string {
	head := ""
	if len(toks) > 0 && (toks[0].kind == tokIdent || toks[0].kind == tokPunct) {
		head = toks[0].value
	}
	second := ""
	if len(toks) > 1 && toks[1].kind == tokIdent {
		second = toks[1].value
	}
	kind, _, _ := txStatement(toks, nil)
	switch {
	case a.class == classError:
		return "D"
	case kind != txOther && kind != txPrevent:
		return "T1"
	case a.create != nil && a.create.unmodeled != "":
		return "M1b"
	case a.create != nil:
		return "M1"
	case a.index != nil:
		return "M4"
	case a.alter != nil && len(a.alter.actions) > 0:
		return actionRow(a.alter.actions[0])
	case a.drop != nil && a.drop.kind == "table":
		return "M6"
	case a.drop != nil && a.drop.kind == "index":
		return "M7"
	case a.drop != nil && a.drop.kind == "schema":
		return "R5"
	case a.alterIndex != nil:
		return "M8"
	case a.schemaRename != nil:
		return "M9"
	case a.enum != nil:
		return "R3"
	case a.class == classOpaque:
		if r, ok := opaqueRows[a.opaqueKind]; ok {
			return r
		}
		return "D"
	case a.mayFail != "":
		return "F1"
	case len(a.relations) == 1 && (a.relations[0].labels != nil || a.relations[0].labelsUndecoded):
		return "R2"
	case len(a.relations) > 0, head == "create" && second == "schema":
		return "R1"
	case a.dropOthers != nil:
		return "R5"
	case a.function != nil, head == "alter" && (second == "function" || second == "procedure" || second == "routine"):
		return "R6"
	case a.renamed != nil && (a.renamed.newName != "" || second == "type" || second == "domain"), len(a.dependents) > 0 && head == "alter":
		return "R4"
	case a.class == classNoEffect && noEffectN3Heads[head]:
		return "N3"
	case a.class == classNoEffect && (head == "set" || head == "reset" || head == "discard"):
		return "N2"
	case a.class == classNoEffect:
		return "N1"
	case a.query == "SELECT INTO":
		return "R1"
	}
	return "D"
}

// actionRow returns the contract row of one ALTER TABLE subcommand.
func actionRow(a alterAction) string {
	switch a.kind {
	case "add_column":
		return "M5a"
	case "drop_column":
		return "M5b"
	case "alter_type":
		return "M5c"
	case "set_not_null", "drop_not_null":
		return "M5d"
	case "set_default", "drop_default":
		return "M5e"
	case "add_constraint":
		return "M5f"
	case "add_using_index":
		return "M5f-u"
	case "drop_constraint":
		return "M5g"
	case "rename_column":
		return "M5h"
	case "rename_table":
		return "M5i"
	case "set_schema":
		return "M5j"
	case "unsupported":
		switch {
		case a.word == "":
			return "D"
		case a.dependsOn == "":
			return "M5k-a"
		}
		return "M5k-b"
	}
	return "D"
}

// statementRows returns the row of a statement and the rows of its parts:
// every ALTER TABLE subcommand, M2 for the column types and M3 for the
// references a CREATE TABLE or ADD writes, and R1b for a query or SQL body
// Postgres analyses.
func statementRows(toks []token, a analysis) []string {
	rows := []string{a.row}
	add := func(r string) {
		for _, x := range rows {
			if x == r {
				return
			}
		}
		rows = append(rows, r)
	}
	var cols []columnDef
	var refs int
	if a.create != nil {
		cols = a.create.columns
		refs = len(a.create.constraints.foreignKeys)
	}
	if a.alter != nil {
		for _, x := range a.alter.actions {
			add(actionRow(x))
			if x.column != nil {
				cols = append(cols, *x.column)
			}
			refs += len(x.constraints.foreignKeys)
		}
	}
	for _, c := range cols {
		add("M2")
		refs += len(c.references)
	}
	if refs > 0 {
		add("M3")
	}
	if a.query != "" || a.row == "R6" && len(toks) > 0 && containsKw(&cursor{toks: toks}, "sql") {
		add("R1b")
	}
	if a.runsFunction != "" {
		// A modelled statement that runs a set-defined function is also
		// an O1 statement (Revision 1.5 U2: its inventory).
		add("O1")
	}
	return rows
}
