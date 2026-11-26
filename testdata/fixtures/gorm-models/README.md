# gorm-models fixture

GORM v2 model modules for the GORM front end (T018g5, ADR 0063,
`docs/extract/schema-gorm.md`). Part 1 covers the schema
port; part 2 adds the relation modules; a later part adds the
Revision 2 modules (`rev2*/`, `dialvar/`) and the index golden
`testdata/fixtures/gorm-models.golden.json` (`TestGormIndexedFixture`). Each directory is
its own Go module (`go.mod` requiring `gorm.io/gorm`); nothing here is
built or run by the repository's tests: `internal/extract/models/gorm`
reads the files as sources. No file uses an ignored name, so no inert
names are needed.

| module | dialect | forms |
|---|---|---|
| `pg/` | postgres (gorm 1.31.2) | every GO-T1/T2 type form of the investigation `types` probe, column tags, defaults, permissions, serializer, unique, the `index` probe (GO-I1), primary-key forms (GO-C1), embedding and prefixes (GO-F1, GO-F2), an embedding field with tags, postgres-only forms (index `type`, `uuid`, `text[]`, `jsonb`, `generated`, a raw type outside the list), custom types (a `Valuer`, a `GormDataType`), the 16 naming cases, `TableName` literal, pointer, constant, concatenation with another package's constant, promoted, `TableName(schema.Namer)` and a variable (not claimed), a table name over 63 bytes (not claimed), hashed and truncated index names, a belongs-to foreign key whose type the relation may change |
| `lite/` | sqlite (glebarez, gorm 1.25.12) | sqlite types, `default:null`, `type:` on a time, a descending index, a unique constraint, a `check` tag (row D), an autoincrement field in a composite key, the GO-P1 failures (two autoincrement integers with a sibling model in the same call, a map field, a default that does not parse), a migrate site in a test file (O10) whose model is not proven migrated (GO-M2) |
| `runtime/` | mysql, postgres, sqlite at run time | per-dialect facts (Revision 1, O1), `mysql.New` with `DefaultStringSize`, foreign keys disabled, a string primary key |
| `named/` | sqlite | a literal `NamingStrategy` (`TablePrefix`, `SingularTable`) |
| `nolower/` | postgres | `&schema.NamingStrategy{NoLowerCase: true, IdentifierMaxLength: 30}` |
| `nocfg/` | sqlite | `gorm.Open(d)` without a configuration (Revision 1 addendum 2, P1) |
| `dynamic/` | sqlite | a non-literal `TablePrefix`: names unknown, nothing claimed |
| `v1/` | - | a GORM v1 (`github.com/jinzhu/gorm`) import: GO-U1 marker |
| `rel/` | postgres | the investigation relations, autoadd and createtable probes: belongs-to with an auto-added target, has-one through `foreignKey`, has-many with actions, many2many (`speaker_langs`, and `SpeakerLangTwo` with `joinForeignKey`, `references`, `joinReferences`), a self reference, `references` to a non-key column, a polymorphic relation (row D), `constraint:-`, a belongs-to that yields to the reverse has-many, a join table declared from both sides (open point O8), a relation field of an embedded struct (row D), a relation GORM cannot guess failing its call after a model it keeps (GO-P1 narrowed), `Migrator().CreateTable` with a foreign key to a table not created (GO-P1) |
| `relsplit/` | postgres | children migrated before their has-one and has-many parents, one call each (relations-split probe) |
| `relorder/` | sqlite (glebarez) | a child and its has-one parent in calls whose order is not proven: the child's foreign key unknown |
| `relignore/` | sqlite (glebarez) | `IgnoreRelationshipsWhenMigrating`: no auto-added table, no join table, no foreign key |
| `ctlite/` | sqlite (glebarez) | `Migrator().CreateTable` on sqlite: nothing added, a foreign key to a table that is not created (unresolved) |
| `rev2/` | postgres | Revision 2: the raw types of GO-T2's closed list (B1), raw type texts with more than a type name and a raw `bigserial` (A1), an index `class:FULLTEXT` failing its call (A5), `SetupJoinTable` (A2), a handle through a function's returns and a parameter (A4) |
| `rev2ddl/` | postgres | Revision 2: `Migrator().DropColumn` and `DropIndex`, `Exec` with DDL text (A3), a parameter, a returned handle and a field set from `db.Table(...)` (A4), a type declared inside the function (A6) |
| `rev2alter/` | postgres | Revision 2 addendum: `Exec` with `ALTER TABLE ... DROP COLUMN` (the table keeps only its table and column names), `CREATE UNIQUE INDEX ... ON "taggeds"` (its index lists unknown), a table the text does not name (claimed), `Raw` with non-DDL text; part 2: `CREATE TABLE IF NOT EXISTS public.audits` of a claimed table (it keeps only its table and column names), `CREATE VIEW` (marker only) |
| `rev2create/` | postgres | Revision 2 addendum, part 2: `CREATE TABLE IF NOT EXISTS ?` whose target is not read (every table of the group keeps only its names) |
| `rev2lite/` | sqlite (glebarez) | Revision 2: raw type texts on sqlite (A1), a wrapped function default (claimed) and a bare one failing its call (A5) |
| `rev2old/` | postgres (driver v1.5.4) | Revision 2: a driver outside the verified versions, older than v1.5.6 (A7): the note and a long generated index name unknown |
| `dialvar/` | postgres, sqlite at run time | Revision 2: a `gorm.Dialector` local assigned in a `switch` before one `gorm.Open` |
| `buildsel/` | sqlite | build selection (ADR 0068 known gap, closed): GORM reads the files `golang.Package` selects. A `//go:build ignore` `package main` generator with a migrate site (`gen.go`, no `ghosts` table), a `//go:build ignore` file with a `TableName` (`model/account_ignore.go`, `Account` keeps `accounts`), a `package documentation` file, a `//go:build !cgo` file importing `"C"` with a `TableName` (`model/nocgo.go`, never built: `Ledger` keeps `ledgers`), and a directory with two package clauses (`mixed/`, refused: row D on each file, `mixed.Thing` not read) |

