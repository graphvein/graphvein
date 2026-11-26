package typescript

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// shapeType renders a declared type compactly, close to TypeScript syntax,
// so tests and the census can compare it with the source annotation. A
// reference whose arguments are not kept prints its argument count.
func shapeType(t *ModelType) string {
	if t == nil {
		return ""
	}
	join := func(sep string) string {
		var parts []string
		for i := range t.Types {
			parts = append(parts, shapeType(&t.Types[i]))
		}
		return strings.Join(parts, sep)
	}
	switch t.Kind {
	case TypeKeyword:
		return t.Name
	case TypeReference:
		switch {
		case len(t.Types) > 0:
			return t.Name + "<" + join(", ") + ">"
		case t.Arguments > 0:
			return t.Name + "<" + strconv.Itoa(t.Arguments) + ">"
		}
		return t.Name
	case TypeUnion:
		return join(" | ")
	case TypeIntersection:
		return join(" & ")
	case TypeArray:
		inner := shapeType(&t.Types[0])
		if k := t.Types[0].Kind; k == TypeUnion || k == TypeIntersection || k == TypeFunction {
			inner = "(" + inner + ")"
		}
		return inner + "[]"
	case TypeLiteral:
		if t.Literal == "string" {
			return "'" + t.Value + "'"
		}
		return t.Value
	case TypeTuple:
		return "[" + join(", ") + "]"
	case TypeFunction:
		return "function"
	}
	return "?"
}

// shapeMember renders a class member as "name: type @d1 @d2" with its
// modifier flags.
func shapeMember(m ModelClassMember) string {
	var b strings.Builder
	if m.Declare {
		b.WriteString("declare ")
	}
	if m.Abstract {
		b.WriteString("abstract ")
	}
	if m.Readonly {
		b.WriteString("readonly ")
	}
	switch m.Kind {
	case MemberProperty:
		b.WriteString(m.Name)
	case MemberGet, MemberSet:
		b.WriteString(m.Kind + " " + m.Name + "()")
	case MemberMethod:
		b.WriteString(m.Name + "()")
	default:
		b.WriteString("?member")
	}
	if m.Optional {
		b.WriteString("?")
	}
	if m.Definite {
		b.WriteString("!")
	}
	if m.Type != nil {
		b.WriteString(": " + shapeType(m.Type))
	}
	if m.HasInitializer {
		b.WriteString(" = ...")
	}
	for _, d := range m.Decorators {
		b.WriteString(" @" + shape(d))
	}
	return b.String()
}

// classOf returns the payload's class statement with the given name.
func classOf(t *testing.T, s *ModelSyntax, name string) ModelStatement {
	t.Helper()
	for _, st := range s.Statements {
		if st.Kind == StatementClass && st.Name == name {
			return st
		}
	}
	t.Fatalf("no class %s in %+v", name, s)
	return ModelStatement{}
}

func shapeList(list []ModelExpr) []string {
	out := []string{}
	for _, e := range list {
		out = append(out, shape(e))
	}
	return out
}

func shapeMembers(list []ModelClassMember) []string {
	out := []string{}
	for _, m := range list {
		out = append(out, shapeMember(m))
	}
	return out
}

// TestModelPayloadTypeORMEntity checks every decorator form of the T018g6
// claim contract (rows TO-E1 to TO-R4, the refusal and D forms included)
// on class and member decorators, with the declared types TypeORM's design
// types depend on.
func TestModelPayloadTypeORMEntity(t *testing.T) {
	source := `import { Entity, Column, PrimaryColumn, PrimaryGeneratedColumn, CreateDateColumn, UpdateDateColumn, DeleteDateColumn, VersionColumn, Index, Unique, Check, Exclusion, ManyToOne, OneToMany, OneToOne, ManyToMany, JoinColumn, JoinTable, RelationId, RelationCount, VirtualColumn, AfterLoad, BeforeInsert, Tree, TreeChildren, TreeParent, Generated, ObjectIdColumn, ViewEntity, ViewColumn, TableInheritance, ChildEntity, BaseEntity, Relation } from "typeorm";
import { ApiProperty } from "@nestjs/swagger";
import { Exclude } from "class-transformer";
import { DbAwareColumn } from "../utils/DbColumnHelper";

@Entity("media", { schema: "app", synchronize: false })
@Index(["tmdbId", "mediaType"], { unique: true, where: "x > 0" })
@Index("IDX_named", ["a", "b"], { unique: true })
@Index((m: Media) => [m.a])
@Unique("UQ_x", ["tmdbId", "user"])
@Unique(["a"])
@Check("CHK_rating", "rating >= 0")
@Check(` + "`rating >= 0`" + `)
@Exclusion("USING gist (room WITH =)")
@Tree("materialized-path")
@TableInheritance({ column: { type: "varchar", name: "type" } })
export class Media extends Base {
  @PrimaryGeneratedColumn() id: number;
  @PrimaryGeneratedColumn("uuid") uuid: string;
  @PrimaryGeneratedColumn("identity", { generatedIdentity: "ALWAYS" }) ident: number;
  @PrimaryColumn("varchar", { length: 255 }) key: string;
  @Column({ primary: true, generated: "increment" }) legacy: number;
  @Column() title: string;
  @Column("text") body: string;
  @Column({ type: "int", nullable: true, default: MediaStatus.UNKNOWN, unique: true, name: "media_status" }) status: MediaStatus | null;
  @Column({ type: "enum", enum: Shade, enumName: "shade_enum", default: Shade.Light }) shade: Shade;
  @Column({ type: "simple-enum", enum: ["a", "b", 3] }) choice: "a" | "b" | 3;
  @Column({ type: "decimal", precision: 6, scale: 2, default: -1.5 }) ratio: number;
  @Column({ default: () => "CURRENT_TIMESTAMP" }) at: Date;
  @Column({ type: "varchar", array: true, default: ["x", "y"] }) tags: string[];
  @Column({ type: "jsonb", default: { a: 1 }, transformer: { from: (v) => v, to: (v) => v }, select: false, comment: "c" }) meta: Record<string, unknown>;
  @Column({ type: resolveDbType("datetime"), length: LEN }) computed: Date;
  @Column(() => Embedded) embedded: Embedded;
  @CreateDateColumn() createdAt: Date;
  @UpdateDateColumn({ default: () => "CURRENT_TIMESTAMP", precision: 3 }) updatedAt: Date;
  @DeleteDateColumn() deletedAt?: Date;
  @VersionColumn() version: number;
  @Index() @Column() indexed: boolean;
  @Index("IDX_p", { unique: true, synchronize: false }) @Column() named: bigint;
  @Index({ spatial: true }) @Column({ type: "geometry", spatialFeatureType: "Point", srid: 4326 }) point: GeoJSON | null;
  @Check("x > 0") @Column() checked: number;
  @ManyToOne(() => User, (user) => user.media, { onDelete: "CASCADE", nullable: false, eager: true, createForeignKeyConstraints: false })
  @JoinColumn({ name: "user_id", referencedColumnName: "id", foreignKeyConstraintName: "FK_x" })
  user: Relation<User>;
  @ManyToOne(() => Season) @JoinColumn([{ name: "a" }, { name: "b" }]) season: Promise<Season>;
  @ManyToOne("Other") other: Other;
  @OneToOne(() => Blocklist, (b) => b.media) @JoinColumn() blocklist: Blocklist;
  @OneToOne(() => Settings, (s) => s.media) settings: Settings;
  @OneToMany(() => Request, (r) => r.media, { cascade: true }) requests: Request[];
  @ManyToMany(() => Tag, (t) => t.media, { onDelete: "CASCADE" })
  @JoinTable({ name: "media_tags", joinColumn: { name: "media_id", referencedColumnName: "id" }, inverseJoinColumn: { name: "tag_id" } })
  tags2: Tag[];
  @ManyToMany(() => Group) @JoinTable() groups: Array<Group>;
  @ManyToMany(() => Label, (l) => l.media) labels: Label[];
  @RelationId((m: Media) => m.user) userId: number;
  @RelationCount((m: Media) => m.requests) requestCount: number;
  @VirtualColumn({ query: (alias) => ` + "`SELECT 1`" + ` }) total: number;
  @Generated("uuid") @Column() generated: string;
  @ObjectIdColumn() _id: ObjectId;
  @TreeChildren({ cascade: true }) children: Media[];
  @TreeParent({ onDelete: "SET NULL" }) parent: Media;
  @ApiProperty({ description: "d" }) @Exclude() @Column() documented: string;
  @DbAwareColumn({ type: "datetime", default: () => "CURRENT_TIMESTAMP" }) wrapped: Date;
  @AfterLoad() load() {}
  @BeforeInsert() async insert(): Promise<void> {}
}

@ViewEntity({ expression: "SELECT 1" })
export class View { @ViewColumn() a: string; }

@ChildEntity("child")
class Child extends Media {}

export class Plain extends BaseEntity {}
`
	_, s := parseModels(t, "server/entity/Media.ts", source)
	if s == nil || !s.Valid() {
		t.Fatalf("payload %+v", s)
	}
	media := classOf(t, s, "Media")
	wantClass := []string{
		`Entity("media", {schema: "app", synchronize: false})`,
		`Index(["tmdbId", "mediaType"], {unique: true, where: "x > 0"})`,
		`Index("IDX_named", ["a", "b"], {unique: true})`,
		`Index((m) => [m.a])`,
		`Unique("UQ_x", ["tmdbId", "user"])`,
		`Unique(["a"])`,
		`Check("CHK_rating", "rating >= 0")`,
		"Check(`rating >= 0`)",
		`Exclusion("USING gist (room WITH =)")`,
		`Tree("materialized-path")`,
		`TableInheritance({column: {type: "varchar", name: "type"}})`,
	}
	if got := shapeList(media.DecoratorList); !reflect.DeepEqual(got, wantClass) {
		t.Errorf("class decorators\n got %q\nwant %q", got, wantClass)
	}
	if media.Extends == nil || shape(*media.Extends) != "Base" || !media.Exported {
		t.Errorf("class header %+v", media)
	}
	wantMembers := []string{
		"id: number @PrimaryGeneratedColumn()",
		`uuid: string @PrimaryGeneratedColumn("uuid")`,
		`ident: number @PrimaryGeneratedColumn("identity", {generatedIdentity: "ALWAYS"})`,
		`key: string @PrimaryColumn("varchar", {length: 255})`,
		`legacy: number @Column({primary: true, generated: "increment"})`,
		"title: string @Column()",
		`body: string @Column("text")`,
		`status: MediaStatus | null @Column({type: "int", nullable: true, default: MediaStatus.UNKNOWN, unique: true, name: "media_status"})`,
		`shade: Shade @Column({type: "enum", enum: Shade, enumName: "shade_enum", default: Shade.Light})`,
		`choice: 'a' | 'b' | 3 @Column({type: "simple-enum", enum: ["a", "b", 3]})`,
		`ratio: number @Column({type: "decimal", precision: 6, scale: 2, default: -1.5})`,
		`at: Date @Column({default: () => "CURRENT_TIMESTAMP"})`,
		`tags: string[] @Column({type: "varchar", array: true, default: ["x", "y"]})`,
		`meta: Record<2> @Column({type: "jsonb", default: {a: 1}, transformer: {from: (v) => v, to: (v) => v}, select: false, comment: "c"})`,
		`computed: Date @Column({type: resolveDbType("datetime"), length: LEN})`,
		"embedded: Embedded @Column(() => Embedded)",
		"createdAt: Date @CreateDateColumn()",
		`updatedAt: Date @UpdateDateColumn({default: () => "CURRENT_TIMESTAMP", precision: 3})`,
		"deletedAt?: Date @DeleteDateColumn()",
		"version: number @VersionColumn()",
		"indexed: boolean @Index() @Column()",
		`named: bigint @Index("IDX_p", {unique: true, synchronize: false}) @Column()`,
		`point: GeoJSON | null @Index({spatial: true}) @Column({type: "geometry", spatialFeatureType: "Point", srid: 4326})`,
		`checked: number @Check("x > 0") @Column()`,
		`user: Relation<1> @ManyToOne(() => User, (user) => user.media, {onDelete: "CASCADE", nullable: false, eager: true, createForeignKeyConstraints: false}) @JoinColumn({name: "user_id", referencedColumnName: "id", foreignKeyConstraintName: "FK_x"})`,
		`season: Promise<Season> @ManyToOne(() => Season) @JoinColumn([{name: "a"}, {name: "b"}])`,
		`other: Other @ManyToOne("Other")`,
		"blocklist: Blocklist @OneToOne(() => Blocklist, (b) => b.media) @JoinColumn()",
		"settings: Settings @OneToOne(() => Settings, (s) => s.media)",
		"requests: Request[] @OneToMany(() => Request, (r) => r.media, {cascade: true})",
		`tags2: Tag[] @ManyToMany(() => Tag, (t) => t.media, {onDelete: "CASCADE"}) @JoinTable({name: "media_tags", joinColumn: {name: "media_id", referencedColumnName: "id"}, inverseJoinColumn: {name: "tag_id"}})`,
		"groups: Array<Group> @ManyToMany(() => Group) @JoinTable()",
		"labels: Label[] @ManyToMany(() => Label, (l) => l.media)",
		"userId: number @RelationId((m) => m.user)",
		"requestCount: number @RelationCount((m) => m.requests)",
		"total: number @VirtualColumn({query: (alias) => `SELECT 1`})",
		`generated: string @Generated("uuid") @Column()`,
		"_id: ObjectId @ObjectIdColumn()",
		"children: Media[] @TreeChildren({cascade: true})",
		`parent: Media @TreeParent({onDelete: "SET NULL"})`,
		`documented: string @ApiProperty({description: "d"}) @Exclude() @Column()`,
		`wrapped: Date @DbAwareColumn({type: "datetime", default: () => "CURRENT_TIMESTAMP"})`,
		"load() @AfterLoad()",
		"insert() @BeforeInsert()",
	}
	if got := shapeMembers(media.Members); !reflect.DeepEqual(got, wantMembers) {
		for i := range got {
			if i < len(wantMembers) && got[i] != wantMembers[i] {
				t.Errorf("member %d\n got %s\nwant %s", i, got[i], wantMembers[i])
			}
		}
		if len(got) != len(wantMembers) {
			t.Errorf("members %d, want %d", len(got), len(wantMembers))
		}
	}
	// The count keeps its version 2 meaning: class plus member decorators.
	count := len(media.DecoratorList)
	for _, m := range media.Members {
		count += len(m.Decorators)
	}
	if media.Decorators != count {
		t.Errorf("decorator count %d, lists hold %d", media.Decorators, count)
	}
	if v := classOf(t, s, "View"); shapeList(v.DecoratorList)[0] != `ViewEntity({expression: "SELECT 1"})` || shapeMembers(v.Members)[0] != "a: string @ViewColumn()" {
		t.Errorf("view entity %+v", v)
	}
	if c := classOf(t, s, "Child"); shapeList(c.DecoratorList)[0] != `ChildEntity("child")` || c.Exported || shape(*c.Extends) != "Media" {
		t.Errorf("child entity %+v", c)
	}
	if p := classOf(t, s, "Plain"); len(p.DecoratorList) != 0 || len(p.Members) != 0 || shape(*p.Extends) != "BaseEntity" {
		t.Errorf("plain class %+v", p)
	}
}

// TestModelPayloadTypeORMMembers checks the member forms: modifiers,
// initializers, methods and accessors, static members (kept in Statics
// only), the constructor and computed, private and string names, and
// decorators written before export and on an abstract base.
func TestModelPayloadTypeORMMembers(t *testing.T) {
	source := `import { Column, Entity } from "typeorm";
@Entity()
export default abstract class Base {
  @Column() declare readonly a: string;
  abstract b?: number;
  c!: boolean;
  d = 1;
  "quoted key": string;
  42: number;
  #secret: string;
  @Column() ["computed"]: number;
  [key: string]: unknown;
  static table = "t";
  static make() {}
  constructor(@Inject() private readonly repo: Repo) {}
  get e(): number { return 1; }
  set e(v: number) {}
  abstract f(): void;
}
@Entity() class Second {}
export { Second };
`
	_, s := parseModels(t, "base.ts", source)
	base := classOf(t, s, "Base")
	want := []string{
		"declare readonly a: string @Column()",
		"abstract b?: number",
		"c!: boolean",
		"d = ...",
		"quoted key: string",
		"42: number",
		"?member",
		"?member @Column()",
		"?member",
		"?member",
		"get e()",
		"set e()",
		"f()",
	}
	if got := shapeMembers(base.Members); !reflect.DeepEqual(got, want) {
		t.Errorf("members\n got %q\nwant %q", got, want)
	}
	if !base.Default || !base.Abstract || len(base.Statics) != 2 || shapeList(base.DecoratorList)[0] != "Entity()" {
		t.Errorf("base %+v", base)
	}
	if second := classOf(t, s, "Second"); second.Exported || shapeList(second.DecoratorList)[0] != "Entity()" {
		t.Errorf("second %+v", second)
	}
}

// TestModelPayloadTypeORMTypes checks every declared-type row on the
// annotation forms the corpora use (design types depend on
// them) and the depth limit.
func TestModelPayloadTypeORMTypes(t *testing.T) {
	cases := map[string]string{
		"string":                    "string",
		"number | null":             "number | null",
		"string | null | undefined": "string | null | undefined",
		"'sms' | 'email'":           "'sms' | 'email'",
		"1 | -2 | true | false":     "1 | -2 | true | false",
		"Date":                      "Date",
		"geo.Point":                 "geo.Point",
		"Promise<Media>":            "Promise<Media>",
		"Promise<User | null>":      "Promise<User | null>",
		"Array<string>":             "Array<string>",
		"Relation<UserEntity>":      "Relation<1>",
		"Partial<A, B>":             "Partial<2>",
		"User[]":                    "User[]",
		"(string | null)[]":         "(string | null)[]",
		"number[][]":                "number[][]",
		"A & B":                     "A & B",
		"[number, string]":          "[number, string]",
		"(a: number) => void":       "function",
		"((string))":                "string",
		"bigint | any | unknown | object | never | void | symbol": "bigint | any | unknown | object | never | void | symbol",
		"typeof x":            "?",
		"keyof T":             "?",
		"{ a: number }":       "?",
		"readonly string[]":   "?",
		"T extends U ? X : Y": "?",
		"A['b']":              "?",
		"`a${string}`":        "?",
		"[a?: number]":        "[?]",
		"Array<Array<Array<Array<Array<Array<Array<Array<Array<string>>>>>>>>>": "Array<Array<Array<Array<Array<Array<Array<Array<Array<?>>>>>>>>>",
	}
	for annotation, want := range cases {
		_, s := parseModels(t, "a.ts", "import { Column } from \"typeorm\";\nclass T { a: "+annotation+"; }\n")
		if s == nil || !s.Valid() {
			t.Fatalf("%s: payload %+v", annotation, s)
		}
		m := classOf(t, s, "T").Members
		if len(m) != 1 || m[0].Type == nil {
			t.Fatalf("%s: members %+v", annotation, m)
		}
		if got := shapeType(m[0].Type); got != want {
			t.Errorf("%s: got %s, want %s", annotation, got, want)
		}
	}
	_, s := parseModels(t, "a.ts", "import { Column } from \"typeorm\";\nclass T { a; b = 1; }\n")
	for _, m := range classOf(t, s, "T").Members {
		if m.Type != nil {
			t.Errorf("unannotated member %+v has a type", m)
		}
	}
}

// TestModelPayloadTypeORMEnums checks row S8 (auto-increment, string,
// negative and computed members, const enums) and row G12 (an enum module's
// payload holds only its enums).
func TestModelPayloadTypeORMEnums(t *testing.T) {
	source := `export enum MediaStatus {
  UNKNOWN = 1,
  PENDING,
  PROCESSING,
  AVAILABLE = 5,
  DELETED = -1,
}
export const enum Shade { Light = "light", 'Dark Mode' = "dark" }
enum Computed { A = Shade.Light, B = 1 << 2, C, D = "a" + "b", E = +3 }
declare enum Ambient { X }
export const DEFAULT = MediaStatus.UNKNOWN;
export function f() { return 1; }
export default MediaStatus;
`
	f, root, s := parseModelsRoot(t, "constants/media.ts", source)
	if gate := ModelGate("constants/media.ts", root, f); gate != "G12" {
		t.Fatalf("gate %q", gate)
	}
	if s == nil || !s.Valid() || len(s.Declarations) != 0 || s.Default != nil {
		t.Fatalf("enum module payload holds more than enums: %+v", s)
	}
	var got []string
	for _, st := range s.Statements {
		got = append(got, shapeStatement(st)+map[bool]string{true: " exported", false: ""}[st.Exported]+map[bool]string{true: " const", false: ""}[st.Const])
	}
	want := []string{
		"enum MediaStatus {UNKNOWN = 1, PENDING, PROCESSING, AVAILABLE = 5, DELETED = -1} exported",
		`enum Shade {Light = "light", Dark Mode = "dark"} exported const`,
		"enum Computed {A = ?, B = ?, C, D = ?, E = +3}",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("enums\n got %q\nwant %q", got, want)
	}
	if s.Statements[0].EnumMembers[1].Span.StartLine != 3 || s.Statements[0].Span.StartLine != 1 {
		t.Errorf("spans %+v", s.Statements[0])
	}
	// In a file gated by another row, enums are captured beside everything else.
	_, s = parseModels(t, "e.ts", "import { Column } from \"typeorm\";\nexport enum E { A }\nexport const x = f();\n")
	if len(s.Statements) != 1 || s.Statements[0].Kind != StatementEnum || len(s.Declarations) != 1 {
		t.Errorf("enum in a G10 file %+v", s)
	}
}

// TestModelPayloadTypeORMOptions checks the data source forms of the
// contract's discovery and rows TO-K1 to TO-K8: seerr's options selector
// (if/else-if/else with conditional returns), option objects with type
// annotations, the entities array, globs, spreads of local objects
// (TO-K8), an imported options object (TO-K7), SnakeNamingStrategy
// (TO-K6), NestJS forRoot/forRootAsync/forFeature (TO-K3, TO-K4) and a
// registerAs namespace with a string token const (TO-K5).
func TestModelPayloadTypeORMOptions(t *testing.T) {
	seerr := `import path from "path";
import { DataSource, DataSourceOptions } from "typeorm";
import { SnakeNamingStrategy } from "typeorm-naming-strategies";
import Media from "@server/entity/Media";
import { User } from "@server/entity/User";

const entities = [Media, User];

const testConfig: DataSourceOptions = { type: "sqlite", database: ":memory:", entities, synchronize: true };
const prodConfig: DataSourceOptions = { type: "sqlite", entities, migrations: ["server/migration/sqlite/**/*.ts", path.join(__dirname, "x/*.ts")] };
const pgConfig: DataSourceOptions = { type: "postgres", entities, namingStrategy: new SnakeNamingStrategy() };
const info = url ? { url } : { host, ...(user && { username: user }) };
const spread = { type: "postgres", ...info, entities: [path.resolve(__dirname, "src/**", "*.entity.ts"), ` + "`${__dirname}/a/*.{ts,js}`" + `] } as DataSourceOptions;

function getDataSource(): DataSourceOptions {
  if (process.env.NODE_ENV === "test") {
    return testConfig;
  } else if (process.env.NODE_ENV === "production") {
    return isPgsql ? pgConfig : prodConfig;
  } else {
    return isPgsql ? pgConfig : testConfig;
  }
}

const dataSource = new DataSource(getDataSource());
export default dataSource;
`
	_, s := parseModels(t, "server/datasource.ts", seerr)
	if s == nil || !s.Valid() {
		t.Fatalf("payload %+v", s)
	}
	decls := map[string]string{}
	for _, d := range s.Declarations {
		decls[d.Name] = shape(d.Value)
	}
	wantDecls := map[string]string{
		"entities":   "[Media, User]",
		"testConfig": `{type: "sqlite", database: ":memory:", entities, synchronize: true}`,
		"prodConfig": `{type: "sqlite", entities, migrations: ["server/migration/sqlite/**/*.ts", path.join(__dirname, "x/*.ts")]}`,
		"pgConfig":   `{type: "postgres", entities, namingStrategy: new SnakeNamingStrategy()}`,
		"info":       "(url ? {url} : {host, ...(user && {username: user})})",
		"spread":     `{type: "postgres", ...info, entities: [path.resolve(__dirname, "src/**", "*.entity.ts"), ` + "`${__dirname}/a/*.{ts,js}`" + `]}`,
		"dataSource": "new DataSource(getDataSource())",
	}
	if !reflect.DeepEqual(decls, wantDecls) {
		t.Errorf("declarations\n got %q\nwant %q", decls, wantDecls)
	}
	var fn *ModelStatement
	for i := range s.Statements {
		if s.Statements[i].Kind == StatementFunction && s.Statements[i].Name == "getDataSource" {
			fn = &s.Statements[i]
		}
	}
	if fn == nil {
		t.Fatal("no getDataSource")
	}
	wantBody := `if (process.env.NODE_ENV === "test") {return testConfig} else {if (process.env.NODE_ENV === "production") {return (isPgsql ? pgConfig : prodConfig)} else {return (isPgsql ? pgConfig : testConfig)}}`
	if got := shapeStatements(fn.Value.Statements); got != wantBody {
		t.Errorf("selector\n got %s\nwant %s", got, wantBody)
	}
	if s.Default == nil || shape(s.Default.Value) != "dataSource" {
		t.Errorf("default %+v", s.Default)
	}

	nest := `import { Module } from "@nestjs/common";
import { ConfigService } from "@nestjs/config";
import { TypeOrmModule } from "@nestjs/typeorm";
import { DataSource } from "typeorm";
import ormConfig from "./config/typeOrmConfig";

@Module({
  imports: [
    TypeOrmModule.forRoot(ormConfig),
    TypeOrmModule.forRoot({ type: "better-sqlite3", database: "x.db", autoLoadEntities: true }, "second"),
    TypeOrmModule.forRootAsync({
      imports: [ConfigModule],
      inject: [ConfigService],
      useFactory: (configService: ConfigService) => {
        let logging = configService.get("DB_LOGGING");
        try {
          logging = JSON.parse(logging);
        }
        catch {
          // ignore
        }
        return { ...configService.get<IDatabaseConfig>("database"), autoLoadEntities: true, logging };
      },
      dataSourceFactory: async (options) => {
        const dataSource = await new DataSource(options).initialize();
        return dataSource;
      },
    }),
    TypeOrmModule.forRootAsync({ useClass: TypeOrmConfigService }),
    TypeOrmModule.forFeature([UserEntity, RoleEntity], "second"),
  ],
})
export class AppModule {}
`
	_, s = parseModels(t, "src/app.module.ts", nest)
	app := classOf(t, s, "AppModule")
	wantModule := `Module({imports: [TypeOrmModule.forRoot(ormConfig), TypeOrmModule.forRoot({type: "better-sqlite3", database: "x.db", autoLoadEntities: true}, "second"), TypeOrmModule.forRootAsync({imports: [ConfigModule], inject: [ConfigService], useFactory: (configService) => {return ?}, dataSourceFactory: async (options) => {return ?}}), TypeOrmModule.forRootAsync({useClass: TypeOrmConfigService}), TypeOrmModule.forFeature([UserEntity, RoleEntity], "second")]})`
	if got := shapeList(app.DecoratorList); len(got) != 1 || got[0] != wantModule {
		t.Errorf("module decorator\n got %q\nwant %s", got, wantModule)
	}
	factory := app.DecoratorList[0].Args[0].Entries[0].Value.Elements[2].Args[0].Entries[2].Value
	if got := shapeStatements(factory.Statements); got != `let logging = configService.get("DB_LOGGING"); try {logging = JSON.parse(logging)} catch {}; return {...configService.get("database"), autoLoadEntities: true, logging}` {
		t.Errorf("factory body %s", got)
	}
	dsf := app.DecoratorList[0].Args[0].Entries[0].Value.Elements[2].Args[0].Entries[3].Value
	if got := shapeStatements(dsf.Statements); !dsf.Async || got != `const dataSource = await new DataSource(options).initialize(); return dataSource` {
		t.Errorf("dataSourceFactory async=%v body %s", dsf.Async, got)
	}
	// Rows E-async, E-await and B-try are kept only in the TypeORM gate
	// rows: a Sequelize file's payload keeps those forms unknown.
	_, sq := parseModels(t, "models/x.ts", "import { Model } from \"sequelize\";\nf(async (o) => o);\nf(() => { try { g(); } catch {} });\nf(function () { return await g(); });\n")
	if got := shapeStatements(sq.Statements); got != `f(?); f(() => {return ?}); f(function () {return ?})` {
		t.Errorf("sequelize file %s", got)
	}

	config := `import { registerAs } from "@nestjs/config";
import { DataSource, DataSourceOptions } from "typeorm";
const dataSourceOptions: DataSourceOptions = { type: "mysql", entities: ["dist/modules/**/*.entity{.ts,.js}"] };
export const dbRegToken = "database";
export const DatabaseConfig = registerAs(dbRegToken, (): DataSourceOptions => dataSourceOptions);
export const Other = registerAs("other", () => ({ type: "postgres" }));
const ormConfig: TypeOrmModuleOptions = { type: "better-sqlite3", migrations: isDev ? ["a/*.ts"] : [], autoLoadEntities: true };
export default ormConfig;
`
	_, s = parseModels(t, "src/config/database.config.ts", config)
	decls = map[string]string{}
	for _, d := range s.Declarations {
		decls[d.Name] = shape(d.Value)
	}
	wantDecls = map[string]string{
		"dataSourceOptions": `{type: "mysql", entities: ["dist/modules/**/*.entity{.ts,.js}"]}`,
		"dbRegToken":        `"database"`,
		"DatabaseConfig":    "registerAs(dbRegToken, () => dataSourceOptions)",
		"Other":             `registerAs("other", () => {type: "postgres"})`,
		"ormConfig":         `{type: "better-sqlite3", migrations: (isDev ? ["a/*.ts"] : []), autoLoadEntities: true}`,
	}
	if !reflect.DeepEqual(decls, wantDecls) {
		t.Errorf("config declarations\n got %q\nwant %q", decls, wantDecls)
	}
}

// TestModelGateTypeORM checks rows G10-G12 and their order after G1-G9.
func TestModelGateTypeORM(t *testing.T) {
	cases := []struct {
		file, source, gate string
	}{
		{"a.ts", "import { Entity } from \"typeorm\";\n", "G10"},
		{"a.ts", "import type { DataSourceOptions } from \"typeorm\";\n", "G10"},
		{"a.ts", "import { x } from \"typeorm/util/StringUtils\";\n", "G10"},
		{"a.ts", "export { Column } from \"typeorm\";\n", ""},
		{"a.ts", "import { TypeOrmModule } from \"@nestjs/typeorm\";\n", "G10"},
		{"a.ts", "import type { TypeOrmModuleOptions } from \"@nestjs/typeorm/dist/interfaces\";\n", "G10"},
		{"a.ts", "import { x } from \"@nestjs/typeorm-extra\";\n", ""},
		{"ormconfig.ts", "export default { type: \"sqlite\" };\n", "G10"},
		{"pkg/ormconfig.js", "module.exports = { type: \"sqlite\" };\n", "G10"},
		{"ormconfig.json.ts", "export default {};\n", ""},
		{"a.ts", "import { SnakeNamingStrategy } from \"typeorm-naming-strategies\";\n", ""},
		{"a.ts", "// import { Entity } from \"typeorm\";\n", ""},
		{"a.js", "const { Entity } = require(\"typeorm\");\n", ""},
		{"a.ts", "import { Entity } from \"./orm\";\n@Entity()\nexport class A {}\n", "G11"},
		{"a.ts", "@ViewEntity({ expression: \"x\" }) class A {}\n", "G11"},
		{"a.ts", "@ChildEntity export default class extends B {}\n", "G11"},
		{"a.ts", "@Entity() export abstract class A {}\n", "G11"},
		{"a.ts", "@orm.Entity() class A {}\n", ""},
		{"a.ts", "@Entities() class A {}\n", ""},
		{"a.ts", "function f() { @Entity() class A {} }\n", ""},
		{"a.ts", "export enum E { A }\n", "G12"},
		{"a.ts", "const enum E { A }\n", "G12"},
		{"a.ts", "declare enum E { A }\n", ""},
		{"a.ts", "function f() { enum E { A } }\n", ""},
		{"a.ts", "import { Model } from \"sequelize\";\nexport enum E { A }\n", "G4"},
		{"a.ts", "import { pgTable } from \"drizzle-orm/pg-core\";\nimport { Entity } from \"typeorm\";\n", "G1"},
		{"a.ts", "import { Entity } from \"typeorm\";\nexport enum E { A }\n", "G10"},
	}
	for _, c := range cases {
		f, root, s := parseModelsRoot(t, c.file, c.source)
		if got := ModelGate(c.file, root, f); got != c.gate {
			t.Errorf("%s %q: gate %q, want %q", c.file, c.source, got, c.gate)
		}
		if (s != nil) != (c.gate != "") {
			t.Errorf("%s %q: payload %v", c.file, c.source, s)
		}
	}
}

// TestModelPayloadValidV5 breaks each version 5 field and expects Valid to
// reject it.
func TestModelPayloadValidV5(t *testing.T) {
	_, s := parseModels(t, "a.ts", `import { Column, Entity } from "typeorm";
@Entity("x")
class T { @Column() a: Promise<string | null>; m() {} }
export enum E { A = 1, B }
function f() { if (a) return b ? c : d; else return e; }
`)
	if !s.Valid() {
		t.Fatal("fresh payload invalid")
	}
	broken := []func(*ModelSyntax){
		func(s *ModelSyntax) { s.Version = 4 },
		func(s *ModelSyntax) { s.Statements[0].DecoratorList[0].Kind = "" },
		func(s *ModelSyntax) { s.Statements[0].Members[0].Kind = "field" },
		func(s *ModelSyntax) { s.Statements[0].Members[0].Name = "" },
		func(s *ModelSyntax) { s.Statements[0].Members[0].Decorators[0].Callee = nil },
		func(s *ModelSyntax) { s.Statements[0].Members[0].Type.Kind = "maybe" },
		func(s *ModelSyntax) { s.Statements[0].Members[0].Type.Types = nil },
		func(s *ModelSyntax) { s.Statements[0].Members[0].Type.Types[0].Types[0].Kind = "" },
		func(s *ModelSyntax) {
			s.Statements[0].Members[0].Type.Types[0].Types = s.Statements[0].Members[0].Type.Types[0].Types[:1]
		},
		func(s *ModelSyntax) { s.Statements[0].Members[0].Type.Types[0].Types[0].Name = "str" },
		func(s *ModelSyntax) { s.Statements[0].Members[1].Type = &ModelType{Kind: TypeKeyword, Name: "string"} },
		func(s *ModelSyntax) { s.Statements[1].Name = "" },
		func(s *ModelSyntax) { s.Statements[1].EnumMembers = nil },
		func(s *ModelSyntax) { s.Statements[1].EnumMembers[0].Value.Kind = ModelIdentifier },
		func(s *ModelSyntax) { s.Statements[1].Kind = StatementReturn },
		func(s *ModelSyntax) { s.Statements[2].Value.Statements[0].Else[0].Kind = "loop" },
		func(s *ModelSyntax) { s.Statements[2].Value.Statements[0].Body[0].Value.Test = nil },
		func(s *ModelSyntax) { s.Statements[2].Value.Statements[0].Body[0].Value.Else.Kind = "" },
	}
	for i, breakIt := range broken {
		copy := roundTrip(t, s)
		breakIt(copy)
		if copy.Valid() {
			t.Errorf("broken payload %d is valid", i)
		}
	}
}

// roundTrip copies a payload through JSON, as the cache stores it.
func roundTrip(t *testing.T, s *ModelSyntax) *ModelSyntax {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var out ModelSyntax
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}
