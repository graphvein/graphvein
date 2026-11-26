# typeorm-models fixture

Own code (no third-party text) for T018g6: the
TypeORM claim pass over entities, columns, types and defaults (TOB3) and
relations, junction tables, indexes, constraints and generated names
(TOB4) (`docs/extract/schema-typeorm.md`, ADR 0064), indexed through the
indexer into `../typeorm-models.golden.json`; the oracle runs
on it. The files
are entity and data source modules as TypeORM reads them; no package is
installed, and the lockfiles name only the versions the contract reads.

No file here is an ignore file or sits under an ignored path, so the
fixture needs no inert names (AGENTS.md).

`TestTypeORMFixtureRows`, `TestTypeORMFixtureTags`
(`internal/extract/models/typeorm/fixture_test.go`: every row with a
branch and every listed form reached) and `TestTypeORMFixtureRelations`
(`relations_test.go`) check the cases per row;
`TestTypeORMIndexedFixture` (`indexed_test.go`) the golden output.

| package | typeorm | files | rows |
|---|---|---|---|
| `app/` | 0.3.31, `pg` and `sqlite3` | `src/data-source.ts` | TO-K1 selector with a run-time dialect set (sqlite or postgres: per-dialect claims, ADR 0065) |
| | | `src/pg-data-source.ts` | a postgres-only data source (investigation probe 2 forms) |
| | | `src/entity/Base.ts` | TO-E2 (an abstract base `extends BaseEntity`, its members first), TO-C1 increment key, TO-C3 create and update dates |
| | | `src/entity/Account.ts` | probe 1 forms: TO-K2 names, TO-C2 explicit types with length, precision and scale, design types, `simple-array`, `simple-json`, TO-C4 defaults (number, boolean, string, `() => "<sql>"`, `CURRENT_TIMESTAMP`, an enum member, a project call), `unique`, `nullable`, no-fact options; TO-C5 `simple-enum` over a string enum; TO-C3 delete date and version; TO-E1 listener; TO-E3 `@ApiProperty` |
| | | `src/entity/Profile.ts` | TO-K2 literal name, TO-C1 composite primary key, more TO-C2 types (`smallint`, `bigint`, `real`, `boolean`, `numeric(p)`, `character(n)`) |
| | | `src/entity/Note.ts` | TO-C1 uuid key; TO-R1 owning relations (one to Account, one self-reference) and a TO-R3 inverse |
| | | `src/entity/Member.ts` | TOB4 (no migrations: TO-X4 generated names): class and property `@Index` (fields, a name, `unique`, a partial `where`, on a relation), `@Unique` (a relation property, a name), `@Check` (with and without a name); TO-R1 a reused explicit column (its `number \| null` design type no failure), a composite join column array (0.3.x array order), `nullable: false`, `onUpdate`, `createForeignKeyConstraints: false`, a lazy `Promise<T>`; TO-R2 a one-to-one owner with a named join column and foreign key name; TO-R4 junctions with default names (inverse NO ACTION), named columns referencing a named column (A30) and a self-relation (`_1`/`_2`) |
| | | `src/entity/Tag.ts` | TO-R3 inverse sides (`@ManyToMany` without `@JoinTable`, `@OneToOne` without `@JoinColumn`) |
| | | `src/entity/Odd.ts` | TOB4 refusals: the function form of `@Index`, `synchronize: false`, `fulltext`, an unread option (D), a deferrable `@Unique` (D), a property-level `@Check` (D), a string relation target, a junction with `joinColumns` |
| | | `src/entity/Archive.ts` | TO-E1 `synchronize: false` (mapped, shown assumption) |
| | | `src/entity/Tagged.ts`, `src/utils/DbColumnHelper.ts` | TO-C6 (a `DbAwareColumn`-style wrapper), TO-P2 (a project call as the type, a wrapper that renames) |
| | | `src/entity/Holder.ts` | D (`@Tree`, `@TreeChildren`, `@TreeParent`, an embedded column) |
| | | `src/entity/Typed.ts` | postgres forms: TO-C1 identity, TO-C5 native enums (numeric labels, `enumName`), arrays, `jsonb`, `timestamptz`, `uuid`, a rewritten datetime default (unknown); Revision 4: a number and a boolean string on boolean columns (stored `true`), `gen_random_uuid()`, string defaults on a timestamp (not claimed) |
| | | `src/entity/Audited.ts` | an entity `schema` (TO-K2, TO-V7) |
| | | `src/constants/enums.ts` | S8 enums for TO-C4 and TO-C5 |
| `failures/` | 0.3.31 | `src/f1.ts` to `src/f6.ts`, `src/entity/*.ts` | TO-P1 (1) to (6), one data source each; `f2.ts` selects postgres or sqlite and only the postgres candidate fails |
| | | `src/f10.ts` to `src/f14.ts`, `src/f13s.ts` | Revision 4: TO-P1 (10) `'CURRENT_TIMESTAMP'` on a postgres timestamp (`PgKeyword`), (11) `'uuid_generate_v4()'` on a postgres uuid (`PgUuidText`), (12) `'CURRENT_TIMESTAMP'` on a MySQL datetime (`MyDatetime`, mapped), (13) a length on int (postgres and sqlite, `IntLength`) and on text (postgres, `TextLength`), (14) a precision on int and on text (postgres, `IntPrecision`, `TextPrecision`) |
| `v1/` | 1.1.3, `better-sqlite3` | `src/data-source.ts`, `src/entity/Item.ts` | 1.x rules: a falsy date-column default (TO-C3, open point O19) |
| | | `src/entity/Shelf.ts`, `src/entity/Book.ts`, `src/entity/Reader.ts` | 1.x TOB4 rules: a composite join column array sorted by the target's primary key (A34), the inverse junction foreign key CASCADE (A33), a partial index and a check whose generated names hash with standard SHA-1 (A35), sqlite's simple-enum CHECK (TO-C5) |
| | | `src/legacy-data-source.ts` | TO-P1 (7): `type: "sqlite"` under 1.x |
| | | `src/count-data-source.ts`, `src/entity/Counted.ts` | TO-P1 (8): `@RelationCount` under 1.x |
| | | `src/index-type-data-source.ts`, `src/entity/Indexed.ts` | TO-P1 (9): an `@Index` `type` option on better-sqlite3 under 1.x |
| `snake/` | 0.3.31, `typeorm-naming-strategies` 4.1.0 | `src/data-source.ts`, `src/entity/SnakeUser.ts`, `src/migrations/*.ts` | TO-K6 names, the snake enum type name, snake join column and junction names; a `migrations` option (TO-U1), so no generated name is claimed (TO-X4) |
| | | `src/custom-data-source.ts` | D: a custom naming strategy |
| `nest/` | 0.3.22, `@nestjs/typeorm` 11.0.0, `mysql2` | `src/app.module.ts`, `src/config/database.config.ts`, `src/user/*.ts` | TO-K3 `forRootAsync` (a `try` in the factory, an async block `dataSourceFactory`), TO-K5 `registerAs`, TO-K4 `forFeature` with `autoLoadEntities`, TO-E3 `@ApiProperty` and `@Exclude`, mapped MySQL types and defaults; `role.entity.ts`: a MySQL unique index name (IDX_), a junction, a `@Check` TypeORM does not create on MySQL; `user.entity.ts` (Revision 4 A3): MySQL's own index for a foreign key no declared index leads with (named as the foreign key), and one a declared `@Index` covers |
| `forms/` | 0.3.31, `@nestjs/typeorm` 12.0.3 | `src/options.ts`, `src/data-source.ts`, `src/app.module.ts`, `src/entity/thing.entity.ts` | TO-K7 imported options (`new DataSource(X)`, `TypeOrmModule.forRoot(X)`), TO-K8 a scope-neutral spread, TO-K1 a glob through `join` imported by name, TO-K3 a literal `forRoot` with the 12.x name argument; one entity reached by three groups (model `<Class>~<root>`) |
| `ormcfg/` | 0.3.31 | `ormconfig.json`, `src/entity/Widget.ts` | TO-K1 ormconfig (no data source in the package) with a cwd-relative glob; Revision 4 on sqlite: a number and a boolean string on boolean columns (stored as written), `datetime('now')`, a function default the oracle fixture does not list (`mapped`), a string default on a datetime (not claimed) |
| `multiroot/` | 0.3.31, `@nestjs/typeorm` 11.0.0, `mysql2` | `src/a/*.ts`, `src/b/*.ts`, `src/c/*.ts`, `src/entity/*.ts` | Revision 4 A4 (TO-K4): two `forRoot` roots with `autoLoadEntities` in one package, each reaching only the `forFeature` calls of the files it imports (root B through a re-export); root C (`name: "lazy"`) whose `forFeature` file no root imports (a lazily loaded module): its table list may be incomplete |
| `legacy/` | 0.2.45 | `src/*.ts` | D: another major (marker) |
