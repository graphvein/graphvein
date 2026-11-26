# typeorm-syntax fixture

Own code (no third-party text) for T018g6: the version 5 rows of
the cached TypeScript model payload (`docs/lang/typescript-models.md`,
ADR 0064). Together with `drizzle-syntax` and `sequelize-syntax` it covers
every row of the payload table; the TypeORM claims themselves are tested
by the `typeorm-models` fixture of the later units. The files are syntax
samples and do not form a working application.

| file | rows |
|---|---|
| `src/entity/Post.ts` | G10; S4 (class decorators `@Entity`, `@Index`, `@Unique`, `@Check` as calls, a class exported through `export default Post`); M-property (optional, definite, readonly, declare, with and without an initializer and an annotation, member decorators with arrows, option objects, enum members and `() => "<sql>"` defaults), M-method (a listener, a getter and a setter), M-unknown (a computed name, an index signature, the constructor), statics left to S4; T-keyword, T-literal, T-union, T-intersection, T-reference (`Promise`, `Array`, `Relation`, a dotted name), T-array, T-tuple, T-function, T-wrap, T-unknown (an object type literal) |
| `src/entity/Author.ts` | G10; S4 (an abstract base with an abstract property, `extends BaseEntity`; an entity with `@Entity({...})`); NestJS `@ApiProperty`, `@ApiHideProperty` and `class-transformer` `@Exclude` member decorators; a string relation target |
| `src/constants/status.ts` | G12 (no ORM import): S8 (an exported enum with auto-increment, numeric and negative members, a `const enum` with a string key, an enum with unread initializers); SD (`declare enum`, a const, an array const and a default export: a G12 payload holds only enums) |
| `src/barrel/Tag.ts` | G11: `@Entity()` imported from a project barrel |
| `src/orm.ts` | GD: a re-export from `typeorm` is not an import |
| `src/data-source.ts` | G10; S1 (an `entities` array with a spread, a conditional options object, option objects with type annotations and `as`, globs through `path.join`, `join`, a template with `__dirname`, `new SnakeNamingStrategy()`, `new DataSource(getDataSource())`); S6 with B-if (`if`/`else if`/`else`), B-return and E-conditional (seerr's options selector); S2 |
| `src/config/database.config.ts` | G10 (a type import); S1 (a string token const, an options object, `registerAs(token, () => options)`) |
| `src/app.module.ts` | G10; S4 (`@Module({imports: [TypeOrmModule.forRootAsync({...}), TypeOrmModule.forFeature([...], "default")]})` with a block-arrow `useFactory` holding a `try` statement (B-try) and nest-admin's async block `dataSourceFactory` with `await` (E-async, E-await; T018g6 Revision 3 addendum 2)) |
| `src/feature.module.ts` | G10 through an `@nestjs/typeorm` import only (T018g6); S4 (`@Module({imports: [TypeOrmModule.forFeature([...])]})`) |

`payload.golden.json` is the expected cached payload per file (`null`
outside the gate); the test removes it from its temporary copy before
indexing. `../typeorm-syntax.golden.json` is the graph golden (TypeScript
symbol records only: the payload adds no record). No file here needs an
inert name.
