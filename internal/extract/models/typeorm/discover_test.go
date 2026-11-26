package typeorm

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// lock is a package-lock.json (lockfile 3) locking typeorm, typescript and
// the NestJS packages at the given versions.
func lock(typeorm, nest, naming string) string {
	s := `{"lockfileVersion": 3, "packages": {"node_modules/typeorm": {"version": "` + typeorm + `"}, "node_modules/typescript": {"version": "5.4.5"}`
	if nest != "" {
		s += `, "node_modules/@nestjs/typeorm": {"version": "` + nest + `"}`
	}
	if naming != "" {
		s += `, "node_modules/typeorm-naming-strategies": {"version": "` + naming + `"}`
	}
	return s + "}}"
}

const manifest03 = `{"dependencies": {"typeorm": "^0.3.31", "pg": "8.0.0", "@nestjs/typeorm": "^11.0.0", "@nestjs/config": "^4.0.0", "typeorm-naming-strategies": "^4.1.0"}, "devDependencies": {"typescript": "5.4.5"}}`

const tsconfigOn = `{"compilerOptions": {"experimentalDecorators": true, "emitDecoratorMetadata": true, "strict": true}}`

// entityFile is an entity module.
func entityFile(class string) string {
	return "import { Entity, PrimaryGeneratedColumn } from 'typeorm';\n@Entity()\nexport class " + class + " {\n  @PrimaryGeneratedColumn() id: number;\n}\n"
}

// pkg03 returns a TypeORM 0.3 package with entities User and Post, plus
// the extra files.
func pkg03(extra map[string]string) map[string]string {
	src := map[string]string{
		"package.json":         manifest03,
		"package-lock.json":    lock("0.3.31", "11.0.0", "4.1.0"),
		"tsconfig.json":        tsconfigOn,
		"src/entity/User.ts":   entityFile("User"),
		"src/entity/Post.ts":   entityFile("Post"),
		"src/entity/README.md": "x",
	}
	for k, v := range extra {
		src[k] = v
	}
	return src
}

func discover(t *testing.T, src map[string]string) (*Package, []string, map[string]int) {
	t.Helper()
	tags := map[string]int{}
	in := input(t, src)
	in.Tag = func(row string) { tags[row]++ }
	pkgs, diags := Discover(in)
	if len(pkgs) != 1 {
		t.Fatalf("%d packages", len(pkgs))
	}
	return pkgs[0], diagLines(diags), tags
}

func entityNames(g *Group) []string {
	var out []string
	for _, e := range g.Entities {
		out = append(out, e.Name)
	}
	return out
}

func hasLine(lines []string, parts ...string) bool {
	for _, l := range lines {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(l, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// TestDiscoverSelector covers the TO-K1 candidate rule on seerr's shape:
// a selector function with if/else-if/else returns of const option
// objects and conditionals, the dialect set, the shared entities array of
// class references, the migrations option and the pins.
func TestDiscoverSelector(t *testing.T) {
	p, diags, tags := discover(t, pkg03(map[string]string{
		"src/datasource.ts": `import { DataSource, DataSourceOptions } from 'typeorm';
import { User } from './entity/User';
import { Post } from './entity/Post';
const isPg = process.env.DB_TYPE === 'postgres';
const entities = [User, Post];
const testConfig: DataSourceOptions = { type: 'sqlite', database: ':memory:', entities, synchronize: true };
const prodConfig: DataSourceOptions = { type: 'sqlite', database: 'db.sqlite3', entities, migrations: ['src/migration/sqlite/**/*.ts'], migrationsRun: true };
const pgConfig: DataSourceOptions = { type: 'postgres', entities, migrations: ['dist/migration/postgres/*.js'] };
function getDataSource(): DataSourceOptions {
  const env = process.env.NODE_ENV;
  if (env === 'test') return testConfig;
  else if (env === 'production') return isPg ? pgConfig : prodConfig;
  else {
    return isPg ? pgConfig : testConfig;
  }
}
export const dataSource = new DataSource(getDataSource());
`,
		"src/migration/sqlite/1-init.ts": "export class Init1 {}\n",
	}))
	if !reflect.DeepEqual(p.Pins, map[string]any{"typeorm": "0.3.31", "typescript": "5.4.5"}) || p.Line != Line03 || !reflect.DeepEqual(p.Drivers, []string{"pg"}) {
		t.Errorf("package %+v", p)
	}
	if len(p.Groups) != 1 {
		t.Fatalf("%d groups", len(p.Groups))
	}
	g := p.Groups[0]
	if g.Root != "src/datasource.ts" || g.SetKey() != "models/typeorm/src/datasource.ts" || g.Binding != "dataSource" || g.Selector != "src/datasource.ts:9" || g.EntitiesAt != "src/datasource.ts:5" {
		t.Errorf("group %+v", g)
	}
	if !g.Claimable() || !reflect.DeepEqual(g.Types, []string{"sqlite", "postgres", "sqlite", "postgres", "sqlite"}) || !reflect.DeepEqual(g.Dialects, []string{"postgres", "sqlite"}) || g.Dialect != "" {
		t.Errorf("types %v dialects %v why %q refused %q", g.Types, g.Dialects, g.OptionsWhy, g.Refused)
	}
	if !reflect.DeepEqual(g.Verification, map[string]string{"postgres": "oracle", "sqlite": "oracle"}) || g.Naming != "default" {
		t.Errorf("verification %v naming %s", g.Verification, g.Naming)
	}
	if !reflect.DeepEqual(entityNames(g), []string{"User", "Post"}) || len(g.Incomplete) != 0 {
		t.Errorf("entities %v incomplete %v", entityNames(g), g.Incomplete)
	}
	// Candidates in return order: testConfig, pgConfig, prodConfig,
	// pgConfig, testConfig.
	if !g.HasMigrations || len(g.Migrations) != 2 || g.Migrations[1].Dir != "src/migration/sqlite" || g.Migrations[1].Files != 1 || g.Migrations[0].Dir != "dist/migration/postgres" || g.Migrations[0].Files != 0 {
		t.Errorf("migrations %+v", g.Migrations)
	}
	if !reflect.DeepEqual(g.Synchronize, []string{"true", "", "", "", "true"}) || g.MigrationsRun[2] != "true" {
		t.Errorf("synchronize %v migrationsRun %v", g.Synchronize, g.MigrationsRun)
	}
	if !hasLine(diags, codeMigrations, migrationsMarker+"src/migration/sqlite") || hasLine(diags, codeNotProven) || hasLine(diags, "not claimed") {
		t.Errorf("diagnostics %v", diags)
	}
	for _, row := range []string{"TO-K1/package dependency", "TO-K1/lockfile pins", "TO-K1/`new DataSource(<options>)`", "TO-K1/candidate rule", "TO-K1/class references", "TO-K1/the dialect set", "TO-K1/tsconfig decorator flags", "TO-U1"} {
		if tags[row] == 0 {
			t.Errorf("row %s not tagged", row)
		}
	}
}

// TestDiscoverOptionsNotRead covers the forms TO-K1, TO-K7 and TO-K8
// leave unread: unequal scope options, a let, a mutated object, a spread
// that can supply a scope option, a non-literal type, an unread
// selector statement, an import from another package, an async options
// function.
func TestDiscoverOptionsNotRead(t *testing.T) {
	cases := []struct{ name, source, why string }{
		{"unequal entities", `import { DataSource } from 'typeorm';
import { User } from './entity/User';
const a = { type: 'sqlite', entities: [User] };
const b = { type: 'postgres', entities: ['src/entity/*.ts'] };
export const ds = new DataSource(process.env.X ? a : b);
`, "the scope option entities differs"},
		{"let", `import { DataSource } from 'typeorm';
let options = { type: 'sqlite' };
export const ds = new DataSource(options);
`, "declared with let"},
		{"mutated", `import { DataSource } from 'typeorm';
const options = { type: 'sqlite', entities: [] };
options.entities = ['x/*.ts'];
export const ds = new DataSource(options);
`, "is assigned into"},
		{"scope spread", `import { DataSource } from 'typeorm';
const base = process.env.X ? { url: 'a' } : { url: 'b', entityPrefix: 'p_' };
export const ds = new DataSource({ type: 'postgres', ...base });
`, "scope option entityPrefix"},
		{"call spread", `import { DataSource } from 'typeorm';
export const ds = new DataSource({ type: 'postgres', ...load() });
`, "not an identifier"},
		{"type", `import { DataSource } from 'typeorm';
export const ds = new DataSource({ type: process.env.DB as any });
`, "type option"},
		{"statement", `import { DataSource } from 'typeorm';
const a = { type: 'sqlite' };
function pick() { for (const x of []) { return x; } return a; }
export const ds = new DataSource(pick());
`, "is not read (it could return"},
		{"computed", `import { DataSource } from 'typeorm';
export const ds = new DataSource({ type: 'sqlite', [key]: 1 });
`, "computed key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, diags, _ := discover(t, pkg03(map[string]string{"src/datasource.ts": c.source}))
			if len(p.Groups) != 1 {
				t.Fatalf("%d groups", len(p.Groups))
			}
			g := p.Groups[0]
			if g.Claimable() || !strings.Contains(g.OptionsWhy, c.why) {
				t.Errorf("why %q, want %q", g.OptionsWhy, c.why)
			}
			if !hasLine(diags, codeNotProven, "are not read", "may be incomplete") || !hasLine(diags, "entity class User is not claimed") {
				t.Errorf("diagnostics %v", diags)
			}
		})
	}
}

// TestDiscoverImportedOptions covers TO-K7 (options imported from a
// module of the package, default and named), TO-K8 (aqualink's
// scope-neutral spread of a conditional with &&-guarded spreads), TO-K6
// (SnakeNamingStrategy), the glob subset with `join` imported by name and
// the O18 `.js` sibling.
func TestDiscoverImportedOptions(t *testing.T) {
	p, diags, tags := discover(t, pkg03(map[string]string{
		"src/config/orm.ts": `import { SnakeNamingStrategy } from 'typeorm-naming-strategies';
import { join } from 'path';
import { DataSourceOptions } from 'typeorm';
const url = process.env.DATABASE_URL;
const info = url
  ? { url }
  : { host: 'localhost', ...(process.env.PGUSER && { username: process.env.PGUSER }) };
export const options: DataSourceOptions = {
  type: 'postgres',
  ...info,
  namingStrategy: new SnakeNamingStrategy(),
  entities: [join(__dirname, '../entity', '*.ts'), join(__dirname, '../entity', '*.js')],
};
export default options;
`,
		"src/a.ts": `import { DataSource } from 'typeorm';
import options from './config/orm';
export const a = new DataSource(options);
`,
		"src/b.ts": `import { DataSource } from 'typeorm';
import { options } from './config/orm';
export default new DataSource(options);
`,
	}))
	if len(p.Groups) != 2 {
		t.Fatalf("%d groups", len(p.Groups))
	}
	for _, g := range p.Groups {
		if !g.Claimable() || g.Options[0].From != "imported" || g.Options[0].File != "src/config/orm.ts" || g.Naming != "snake" || g.Dialect != "postgres" {
			t.Errorf("group %s: why %q refused %q from %s naming %s", g.Root, g.OptionsWhy, g.Refused, g.Options[0].From, g.Naming)
		}
		names := entityNames(g)
		sort.Strings(names)
		if !reflect.DeepEqual(names, []string{"Post", "User"}) || len(g.Incomplete) != 0 {
			t.Errorf("group %s: entities %v incomplete %v", g.Root, names, g.Incomplete)
		}
	}
	if p.Groups[1].Binding != "" || p.Groups[1].Root != "src/b.ts" {
		t.Errorf("default export group %+v", p.Groups[1])
	}
	for _, row := range []string{"TO-K7", "TO-K7/`new DataSource(X)`", "TO-K8", "TO-K6", "TO-K1/the glob subset", "TO-K1/`join(...)`/`resolve(...)` imported by name"} {
		if tags[row] == 0 {
			t.Errorf("row %s not tagged", row)
		}
	}
	if hasLine(diags, codeNotProven) || hasLine(diags, codeUnread) {
		t.Errorf("diagnostics %v", diags)
	}
}

// TestDiscoverNaming covers the namingStrategy values TO-K6 does not read
// (row D) and the naming-strategy pin (TO-V10).
func TestDiscoverNaming(t *testing.T) {
	cases := []struct{ name, imports, value, naming, why string }{
		{"arguments", "import { SnakeNamingStrategy } from 'typeorm-naming-strategies';", "new SnakeNamingStrategy('x')", "4.1.0", "with arguments"},
		{"re-export", "import { SnakeNamingStrategy } from './naming';", "new SnakeNamingStrategy()", "4.1.0", "not SnakeNamingStrategy imported by name"},
		{"custom", "import { MyStrategy } from './naming';", "new MyStrategy()", "4.1.0", "not SnakeNamingStrategy imported by name"},
		{"variable", "const strategy = 1;", "strategy", "4.1.0", "it is not `new SnakeNamingStrategy()`"},
		{"pin", "import { SnakeNamingStrategy } from 'typeorm-naming-strategies';", "new SnakeNamingStrategy()", "3.0.0", "not proven 4.x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := pkg03(map[string]string{
				"src/naming.ts": "export { SnakeNamingStrategy } from 'typeorm-naming-strategies';\nexport class MyStrategy {}\n",
				"src/ds.ts":     "import { DataSource } from 'typeorm';\n" + c.imports + "\nexport const ds = new DataSource({ type: 'postgres', entities: [], namingStrategy: " + c.value + " });\n",
			})
			src["package-lock.json"] = lock("0.3.31", "11.0.0", c.naming)
			p, diags, tags := discover(t, src)
			g := p.Groups[0]
			if g.OptionsWhy != "" || !strings.Contains(g.Refused, c.why) || g.Claimable() || tags["D"] == 0 {
				t.Errorf("why %q refused %q, want %q", g.OptionsWhy, g.Refused, c.why)
			}
			if !hasLine(diags, codeUnread, "is not read", "(row D)") {
				t.Errorf("diagnostics %v", diags)
			}
		})
	}
}

// TestDiscoverVersionsAndDialects covers the major lines (0.3.x and 1.x
// read, another major refused), 1.x's removed sqlite type (TO-P1 (7)), a
// dialect outside the read set (row D), the SQLite family (Revision 1
// O8) and the mapped dialects.
func TestDiscoverVersionsAndDialects(t *testing.T) {
	ds := func(types ...string) string {
		var objs []string
		for _, ty := range types {
			objs = append(objs, "{ type: '"+ty+"', entities: [] }")
		}
		e := objs[0]
		if len(objs) == 2 {
			e = "process.env.X ? " + objs[0] + " : " + objs[1]
		}
		return "import { DataSource } from 'typeorm';\nexport const ds = new DataSource(" + e + ");\n"
	}
	t.Run("another major", func(t *testing.T) {
		src := pkg03(map[string]string{"src/ds.ts": ds("postgres")})
		src["package-lock.json"] = lock("0.2.45", "", "")
		p, diags, _ := discover(t, src)
		if p.Line != "" || !strings.Contains(p.Refused, "0.3.x and 1.x only") || len(p.Groups) != 0 || !hasLine(diags, codeNotProven, "typeorm 0.2.45") {
			t.Errorf("package line %q refused %q groups %d %v", p.Line, p.Refused, len(p.Groups), diags)
		}
	})
	t.Run("1.x sqlite", func(t *testing.T) {
		src := pkg03(map[string]string{"src/ds.ts": ds("sqlite", "postgres")})
		src["package-lock.json"] = lock("1.1.3", "", "")
		p, diags, tags := discover(t, src)
		g := p.Groups[0]
		if p.Line != Line1 || g.Failures["sqlite"] == "" || len(g.Failures) != 1 || !g.Claimable() || tags["TO-P1/(7)"] == 0 {
			t.Errorf("line %s failures %v why %q refused %q", p.Line, g.Failures, g.OptionsWhy, g.Refused)
		}
		if !hasLine(diags, codeFailure, "TO-P1 (7)", "claimed on sqlite") {
			t.Errorf("diagnostics %v", diags)
		}
	})
	t.Run("0.3 sqlite family", func(t *testing.T) {
		p, _, _ := discover(t, pkg03(map[string]string{"src/ds.ts": ds("better-sqlite3", "sqljs")}))
		g := p.Groups[0]
		if !reflect.DeepEqual(g.Dialects, []string{"sqlite"}) || g.Dialect != "sqlite" || g.Verification["sqlite"] != "mapped" || len(g.Failures) != 0 {
			t.Errorf("dialects %v %v failures %v", g.Dialects, g.Verification, g.Failures)
		}
	})
	t.Run("mapped", func(t *testing.T) {
		p, _, _ := discover(t, pkg03(map[string]string{"src/ds.ts": ds("mysql", "mssql")}))
		g := p.Groups[0]
		if !reflect.DeepEqual(g.Verification, map[string]string{"mysql": "mapped", "mssql": "mapped"}) || !g.Claimable() {
			t.Errorf("verification %v", g.Verification)
		}
	})
	t.Run("other dialect", func(t *testing.T) {
		p, diags, _ := discover(t, pkg03(map[string]string{"src/ds.ts": ds("mongodb")}))
		g := p.Groups[0]
		if !strings.Contains(g.Refused, "mongodb") || g.Claimable() || !hasLine(diags, codeUnread, "(row D)") {
			t.Errorf("refused %q %v", g.Refused, diags)
		}
	})
}

// TestDiscoverEntities covers the entities elements: class references,
// the glob subset (a cwd-relative string, `${__dirname}`, `__dirname +`,
// path.resolve through a namespace import, one level of braces), the
// elements that make the list incomplete (a build-output glob, glob syntax
// outside the subset, a spread, an EntitySchema, an unresolved
// identifier), the entity forms of row D and the unreached marker.
func TestDiscoverEntities(t *testing.T) {
	p, diags, _ := discover(t, pkg03(map[string]string{
		"src/entity/Tag.ts":     "import { Entity } from 'typeorm';\n@Entity('tags')\nclass Tag {}\nexport default Tag;\n",
		"src/entity/Hidden.ts":  "import { Entity } from 'typeorm';\n@Entity()\nclass Hidden {}\n",
		"src/entity/View.ts":    "import { ViewEntity } from 'typeorm';\n@ViewEntity({ expression: 'select 1' })\nexport class View {}\n",
		"src/entity/Other.ts":   "import { Entity } from './fake';\n@Entity()\nexport class Other {}\n",
		"src/entity/fake.ts":    "export function Entity() { return () => {}; }\n",
		"src/entity/types.d.ts": "export declare class Typed {}\n",
		"src/extra/Comment.ts":  entityFile("Comment"),
		"src/extra/Like.ts":     entityFile("Like"),
		"src/unreached/Lost.ts": entityFile("Lost"),
		"src/legacy/Old.ts":     entityFile("Old"),
		"src/ds.ts": `import * as path from 'path';
import { DataSource, EntitySchema } from 'typeorm';
import { User } from './entity/User';
const more = [];
export const ds = new DataSource({
  type: 'postgres',
  entities: [
    User,
    'src/entity/*.ts',
    ` + "`${__dirname}/extra/{Comment,Like}.ts`" + `,
    __dirname + '/extra/Comment.ts',
    path.resolve(__dirname, 'entity', 'Tag.ts'),
    'dist/entity/*.js',
    'src/legacy/[a-z]*.ts',
    ...more,
    new EntitySchema({ name: 'x' }),
    Missing,
  ],
});
`,
	}))
	g := p.Groups[0]
	names := entityNames(g)
	sort.Strings(names)
	if want := []string{"Comment", "Like", "Post", "Tag", "User"}; !reflect.DeepEqual(names, want) {
		t.Errorf("entities %v, want %v", names, want)
	}
	wantIncomplete := []string{"matches no source file", "outside the subset", "is not read (it is not a class reference", "is not read (it is not a class reference", "Missing is not read", "not typeorm's"}
	if len(g.Incomplete) != len(wantIncomplete) {
		t.Errorf("incomplete %q", g.Incomplete)
	}
	for _, w := range wantIncomplete {
		if !hasLine(g.Incomplete, w) {
			t.Errorf("no incompleteness reason %q in %q", w, g.Incomplete)
		}
	}
	for _, want := range [][]string{
		{"@ViewEntity class View is not read (row D)"},
		{"class Other has an @Entity decorator that does not resolve to typeorm's"},
		{"entity class Lost is not claimed: not in the data source's entities"},
		{"entity class Hidden is not claimed"},
		{"entity class Old is not claimed"},
		{"may be incomplete"},
	} {
		if !hasLine(diags, want...) {
			t.Errorf("no diagnostic %q in %v", want, diags)
		}
	}
}

// TestDiscoverNest covers TO-K3, TO-K4 and TO-K5: forRoot with a literal
// and a 12.x name argument, forRootAsync with a ConfigService factory read
// through registerAs (spread and returned), the forFeature lists per data
// source name under autoLoadEntities, the 11.x replacement of a non-array
// entities option, forFeature calls in test files and in functions, and
// the forms left unread (useClass, an unknown token, two registerAs of one
// token, a custom dataSourceFactory, forRoot() without options).
func TestDiscoverNest(t *testing.T) {
	base := map[string]string{
		"src/config/db.ts": `import { registerAs } from '@nestjs/config';
import { DataSourceOptions } from 'typeorm';
const dbOptions: DataSourceOptions = { type: 'postgres', host: 'x' };
export const dbToken = 'database';
export const DbConfig = registerAs(dbToken, (): DataSourceOptions => dbOptions);
export const Twice = registerAs('twice', () => ({ type: 'postgres' }));
export const Twice2 = registerAs('twice', () => ({ type: 'mysql' }));
`,
		"src/user.module.ts": `import { Module } from '@nestjs/common';
import { TypeOrmModule } from '@nestjs/typeorm';
import { User } from './entity/User';
@Module({ imports: [TypeOrmModule.forFeature([User])] })
export class UserModule {}
`,
		"src/post.module.ts": `import { Module } from '@nestjs/common';
import { TypeOrmModule } from '@nestjs/typeorm';
import { Post } from './entity/Post';
@Module({ imports: [TypeOrmModule.forFeature([Post], 'second')] })
export class PostModule {}
`,
		"src/user.module.spec.ts": `import { TypeOrmModule } from '@nestjs/typeorm';
import { Post } from './entity/Post';
const m = TypeOrmModule.forFeature([Post]);
`,
	}
	t.Run("roots", func(t *testing.T) {
		src := pkg03(base)
		src["src/app.module.ts"] = `import { Module } from '@nestjs/common';
import { UserModule } from './user.module';
import { PostModule } from './post.module';
import { ConfigService } from '@nestjs/config';
import { TypeOrmModule } from '@nestjs/typeorm';
import { DataSource } from 'typeorm';
@Module({
  imports: [
    TypeOrmModule.forRootAsync({
      inject: [ConfigService],
      useFactory: (config: ConfigService) => {
        const logging = config.get('DB_LOGGING');
        return { ...config.get('database'), autoLoadEntities: true, logging };
      },
      dataSourceFactory: (options) => new DataSource(options).initialize(),
    }),
    TypeOrmModule.forRoot({ type: 'sqlite', database: 'x.db', name: 'second', autoLoadEntities: true, entities: { a: 1 } }),
  ],
})
export class AppModule {}
`
		p, diags, tags := discover(t, src)
		if len(p.Groups) != 2 || len(p.Features) != 2 {
			t.Fatalf("%d groups, %d features", len(p.Groups), len(p.Features))
		}
		a, b := p.Groups[0], p.Groups[1]
		if a.Form != "forRootAsync" || !a.Claimable() || a.Name != "default" || a.Dialect != "postgres" || !a.AutoLoad || a.Root != "src/app.module.ts#default" {
			t.Errorf("forRootAsync %s why %q refused %q name %s dialect %s autoload %v", a.Root, a.OptionsWhy, a.Refused, a.Name, a.Dialect, a.AutoLoad)
		}
		if !reflect.DeepEqual(entityNames(a), []string{"User"}) || len(a.Incomplete) != 0 {
			t.Errorf("forRootAsync entities %v incomplete %v", entityNames(a), a.Incomplete)
		}
		if b.Form != "forRoot" || !b.Claimable() || b.Name != "second" || b.Root != "src/app.module.ts#second" || !reflect.DeepEqual(entityNames(b), []string{"Post"}) {
			t.Errorf("forRoot %s why %q name %s entities %v", b.Root, b.OptionsWhy, b.Name, entityNames(b))
		}
		if !hasLine(diags, "1 TypeOrmModule.forFeature calls in test files are not read") {
			t.Errorf("diagnostics %v", diags)
		}
		for _, row := range []string{"TO-K3/`TypeOrmModule.forRootAsync(", "TO-K3/`TypeOrmModule.forRoot(<options>)`", "TO-K3/`dataSourceFactory`", "TO-K4/`TypeOrmModule.forFeature(", "TO-K4/`autoLoadEntities: true`", "TO-K5"} {
			if tags[row] == 0 {
				t.Errorf("row %s not tagged", row)
			}
		}
	})
	t.Run("12.x name", func(t *testing.T) {
		src := pkg03(base)
		src["package-lock.json"] = lock("0.3.31", "12.0.3", "")
		src["src/app.module.ts"] = `import { TypeOrmModule } from '@nestjs/typeorm';
import './post.module';
export const imports = [TypeOrmModule.forRoot({ type: 'postgres', autoLoadEntities: true }, 'second')];
`
		p, _, tags := discover(t, src)
		g := p.Groups[0]
		if g.Name != "second" || !g.NameKnown || !reflect.DeepEqual(entityNames(g), []string{"Post"}) || tags["TO-K3/`forRoot(options, name)`"] == 0 {
			t.Errorf("name %s(%v) entities %v", g.Name, g.NameKnown, entityNames(g))
		}
	})
	cases := []struct{ name, root, why string }{
		{"useClass", `TypeOrmModule.forRootAsync({ useClass: Options })`, "useClass"},
		{"token", `TypeOrmModule.forRootAsync({ inject: [ConfigService], useFactory: (c) => c.get('other') })`, "no registerAs"},
		{"twice", `TypeOrmModule.forRootAsync({ inject: [ConfigService], useFactory: (c) => c.get('twice') })`, "2 registerAs calls"},
		{"not config", `TypeOrmModule.forRootAsync({ useFactory: (c) => c.get('database') })`, "not an object literal"},
		{"factory", `TypeOrmModule.forRootAsync({ useFactory: () => ({ type: 'postgres' }), dataSourceFactory: (o) => make(o) })`, "dataSourceFactory"},
		{"no options", `TypeOrmModule.forRoot()`, "without options"},
		// Revision 3 addendum 2: a try statement that returns, a try
		// outside a useFactory, an async useFactory, an async block
		// factory with other statements.
		{"try return", `TypeOrmModule.forRootAsync({ useFactory: () => { try { return { type: 'sqlite' }; } catch {} return { type: 'postgres' }; } })`, "return inside the try statement"},
		{"try loop", `TypeOrmModule.forRootAsync({ useFactory: () => { try { for (;;) {} } catch {} return { type: 'postgres' }; } })`, "return inside the try statement"},
		{"async factory", `TypeOrmModule.forRootAsync({ useFactory: async () => ({ type: 'postgres' }) })`, "async factory"},
		{"block factory", `TypeOrmModule.forRootAsync({ useFactory: () => ({ type: 'postgres' }), dataSourceFactory: async (o) => { const ds = await new DataSource(o).initialize(); log(ds); return ds; } })`, "dataSourceFactory"},
	}
	for _, f := range []string{
		"async (o) => new DataSource(o).initialize()",
		"async (o) => await new DataSource(o).initialize()",
		"async (options) => { const dataSource = await new DataSource(options).initialize(); return dataSource; }",
		"async (o) => { return new DataSource(o); }",
	} {
		t.Run("factory "+f, func(t *testing.T) {
			src := pkg03(base)
			src["src/app.module.ts"] = "import { TypeOrmModule } from '@nestjs/typeorm';\nimport { DataSource } from 'typeorm';\nexport const imports = [TypeOrmModule.forRootAsync({ useFactory: () => { let l = 1; try { l = JSON.parse('2'); } catch { l = 3; } finally { l = 4; } return { type: 'postgres', autoLoadEntities: true, logging: l }; }, dataSourceFactory: " + f + " })];\n"
			p, _, tags := discover(t, src)
			g := p.Groups[0]
			if !g.Claimable() || g.Dialect != "postgres" || tags["TO-K3/`try`"] == 0 || tags["TO-K3/`dataSourceFactory`"] == 0 {
				t.Errorf("why %q dialect %s", g.OptionsWhy, g.Dialect)
			}
		})
	}
	// OP2: a factory's parameters and locals shadow the
	// module's names; a const local bound to a literal is read as it.
	shadow := []struct {
		name, module, factory string
		claimable             bool
		dialect               string
		entities              []string
		incomplete            string
	}{
		{"local entities", "const entities = [User];", `() => { const entities = load(); return { type: 'postgres', entities }; }`, true, "postgres", nil, "entities"},
		{"parameter entities", "const entities = [User];", `(entities) => ({ type: 'postgres', entities })`, true, "postgres", nil, "entities"},
		{"literal local", "const type = 'mysql';", `() => { const type = 'postgres'; return { type, entities: [User] }; }`, true, "postgres", []string{"User"}, ""},
		{"let local", "const type = 'mysql';", `() => { let type = 'postgres'; return { type, entities: [User] }; }`, false, "", nil, ""},
		{"shadowed options", "const opts = { type: 'mysql', entities: [User] };", `() => { const opts = make(); return opts; }`, false, "", nil, ""},
		{"shadowed spread", "const extra = { logging: true };", `() => { const extra = make(); return { type: 'postgres', entities: [User], ...extra }; }`, false, "", nil, ""},
		{"module name", "const entities = [User];", `() => ({ type: 'postgres', entities })`, true, "postgres", []string{"User"}, ""},
	}
	for _, c := range shadow {
		t.Run("scope "+c.name, func(t *testing.T) {
			src := pkg03(base)
			src["src/app.module.ts"] = "import { TypeOrmModule } from '@nestjs/typeorm';\nimport { User } from './entity/User';\n" + c.module + "\nexport const imports = [TypeOrmModule.forRootAsync({ useFactory: " + c.factory + " })];\n"
			p, _, _ := discover(t, src)
			g := p.Groups[0]
			if g.Claimable() != c.claimable {
				t.Fatalf("claimable %v (why %q refused %q), want %v", g.Claimable(), g.OptionsWhy, g.Refused, c.claimable)
			}
			if !c.claimable {
				return
			}
			if g.Dialect != c.dialect || !reflect.DeepEqual(entityNames(g), c.entities) {
				t.Errorf("dialect %s entities %v, want %s %v", g.Dialect, entityNames(g), c.dialect, c.entities)
			}
			if c.incomplete != "" && len(g.Incomplete) == 0 {
				t.Errorf("incomplete %q, want a site", g.Incomplete)
			}
		})
	}
	t.Run("scope selector function", func(t *testing.T) {
		src := pkg03(base)
		src["src/data-source.ts"] = "import { DataSource } from 'typeorm';\nimport { User } from './entity/User';\nconst type = 'mysql';\nfunction opts() { const type = 'postgres'; return { type, entities: [User] }; }\nexport const ds = new DataSource(opts());\n"
		p, _, _ := discover(t, src)
		for _, g := range p.Groups {
			if g.File == "src/data-source.ts" && (!g.Claimable() || g.Dialect != "postgres") {
				t.Errorf("why %q dialect %s", g.OptionsWhy, g.Dialect)
			}
		}
	})
	t.Run("scope registerAs", func(t *testing.T) {
		src := pkg03(base)
		src["src/config/db.ts"] = "import { registerAs } from '@nestjs/config';\nimport { DataSourceOptions } from 'typeorm';\nimport { User } from '../entity/User';\nconst entities = [User];\nexport const DbConfig = registerAs('database', (entities) => ({ type: 'postgres', entities }));\n"
		src["src/app.module.ts"] = "import { ConfigService } from '@nestjs/config';\nimport { TypeOrmModule } from '@nestjs/typeorm';\nexport const imports = [TypeOrmModule.forRootAsync({ inject: [ConfigService], useFactory: (c) => c.get('database') })];\n"
		p, _, _ := discover(t, src)
		g := p.Groups[0]
		if !g.Claimable() || len(g.Entities) != 0 || len(g.Incomplete) == 0 {
			t.Errorf("why %q entities %v incomplete %q", g.OptionsWhy, entityNames(g), g.Incomplete)
		}
	})
	t.Run("try in a selector", func(t *testing.T) {
		src := pkg03(base)
		src["src/data-source.ts"] = "import { DataSource } from 'typeorm';\nfunction opts() { try { x(); } catch {} return { type: 'postgres' }; }\nexport const ds = new DataSource(opts());\n"
		p, _, _ := discover(t, src)
		found := false
		for _, g := range p.Groups {
			if g.File == "src/data-source.ts" {
				found = true
				if g.Claimable() || !strings.Contains(g.OptionsWhy, "is not read") {
					t.Errorf("why %q", g.OptionsWhy)
				}
			}
		}
		if !found {
			t.Error("no data source group")
		}
	})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := pkg03(base)
			src["src/app.module.ts"] = "import { ConfigService } from '@nestjs/config';\nimport { TypeOrmModule } from '@nestjs/typeorm';\nexport const imports = [" + c.root + "];\n"
			p, diags, _ := discover(t, src)
			g := p.Groups[0]
			if g.Claimable() || !strings.Contains(g.OptionsWhy, c.why) {
				t.Errorf("why %q, want %q", g.OptionsWhy, c.why)
			}
			if !hasLine(diags, codeNotProven, "are not read") {
				t.Errorf("diagnostics %v", diags)
			}
		})
	}
	// Revision 4 A4: a root reaches the forFeature calls of the files its
	// static imports load; two roots each get their own; a file no root
	// reaches, a relative import that resolves to no file and a require
	// call leave the list incomplete.
	t.Run("import reach", func(t *testing.T) {
		root := func(db, imports string) string {
			return "import { TypeOrmModule } from '@nestjs/typeorm';\n" + imports + "export const imports = [TypeOrmModule.forRoot({ type: 'sqlite', database: '" + db + "', autoLoadEntities: true })];\n"
		}
		post := strings.Replace(base["src/post.module.ts"], ", 'second'", "", 1)
		cases := []struct {
			name     string
			extra    map[string]string
			want     map[string][]string // group file -> entities
			open     map[string]string   // group file -> incomplete reason part ("" none)
			features int
		}{
			{"two roots", map[string]string{
				"src/a.module.ts":    root("a", "import { UserModule } from './user.module';\n"),
				"src/b.module.ts":    root("b", "export * from './features';\n"),
				"src/features.ts":    "export { PostModule } from './post.module';\n",
				"src/post.module.ts": post,
			}, map[string][]string{"src/a.module.ts": {"User"}, "src/b.module.ts": {"Post"}}, map[string]string{"src/a.module.ts": "", "src/b.module.ts": ""}, 2},
			{"unreached", map[string]string{
				"src/a.module.ts":    root("a", "import './user.module';\n"),
				"src/post.module.ts": post,
			}, map[string][]string{"src/a.module.ts": {"User"}}, map[string]string{"src/a.module.ts": "no root module's static imports reach"}, 2},
			{"unresolved", map[string]string{
				"src/a.module.ts":    root("a", "import './user.module';\nimport './missing.module';\n"),
				"src/b.module.ts":    root("b", "import './post.module';\n"),
				"src/post.module.ts": post,
			}, map[string][]string{"src/a.module.ts": {"User"}, "src/b.module.ts": {"Post"}}, map[string]string{"src/a.module.ts": "./missing.module resolves to no file", "src/b.module.ts": ""}, 2},
			// The forRoot sits in a database module the application's
			// AppModule imports next to the feature module (nest-admin);
			// a file only another entry loads (cli.ts, which may itself be
			// loaded dynamically) leaves the list incomplete.
			{"app entry", map[string]string{
				"src/database.module.ts": root("a", ""),
				"src/app.module.ts":      "import './database.module';\nimport './user.module';\n",
				"src/post.module.ts":     post,
				"src/cli.ts":             "import './post.module';\n",
			}, map[string][]string{"src/database.module.ts": {"User"}}, map[string]string{"src/database.module.ts": "src/post.module.ts:4: a TypeOrmModule.forFeature call in a file no root module's static imports reach"}, 2},
			{"require", map[string]string{
				"src/a.module.ts":    root("a", "import './user.module';\nexport function load() { return require('./post.module'); }\n"),
				"src/b.module.ts":    root("b", "import './post.module';\n"),
				"src/post.module.ts": post,
			}, map[string][]string{"src/a.module.ts": {"User"}, "src/b.module.ts": {"Post"}}, map[string]string{"src/a.module.ts": "a require call", "src/b.module.ts": ""}, 2},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				src := pkg03(base)
				delete(src, "src/user.module.spec.ts")
				for k, v := range c.extra {
					src[k] = v
				}
				p, _, _ := discover(t, src)
				if len(p.Features) != c.features {
					t.Fatalf("%d features", len(p.Features))
				}
				for _, g := range p.Groups {
					want, ok := c.want[g.File]
					if !ok {
						continue
					}
					if !reflect.DeepEqual(entityNames(g), want) {
						t.Errorf("%s entities %v, want %v", g.File, entityNames(g), want)
					}
					switch part := c.open[g.File]; {
					case part == "" && len(g.Incomplete) != 0:
						t.Errorf("%s incomplete %v", g.File, g.Incomplete)
					case part != "" && !hasLine(g.Incomplete, part):
						t.Errorf("%s incomplete %v, want %q", g.File, g.Incomplete, part)
					}
				}
			})
		}
	})
	t.Run("function", func(t *testing.T) {
		src := pkg03(base)
		src["src/app.module.ts"] = `import { TypeOrmModule } from '@nestjs/typeorm';
import './user.module';
export const imports = [TypeOrmModule.forRoot({ type: 'postgres', autoLoadEntities: true, entities: [] })];
export function register() { return TypeOrmModule.forFeature([]); }
`
		p, diags, _ := discover(t, src)
		g := p.Groups[0]
		if !g.Claimable() || !hasLine(g.Incomplete, "inside a function is not read") || !reflect.DeepEqual(entityNames(g), []string{"User"}) {
			t.Errorf("why %q incomplete %q entities %v", g.OptionsWhy, g.Incomplete, entityNames(g))
		}
		if !hasLine(diags, "inside a function is not read", "may be incomplete") {
			t.Errorf("diagnostics %v", diags)
		}
	})
}

// TestDiscoverOrmconfig covers ormconfig.json (an object and an array of
// named options) read when the package has no data source, the D forms
// (.yml, 1.x) and ormconfig.ts.
func TestDiscoverOrmconfig(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		p, _, tags := discover(t, pkg03(map[string]string{"ormconfig.json": `{"type": "postgres", "entities": ["src/entity/*.ts"]}`}))
		g := p.Groups[0]
		if g.Form != "ormconfig" || !g.Claimable() || g.Dialect != "postgres" || len(g.Entities) != 2 || tags["TO-K1/ormconfig"] == 0 {
			t.Errorf("group %+v", g)
		}
	})
	t.Run("array", func(t *testing.T) {
		p, _, _ := discover(t, pkg03(map[string]string{"ormconfig.json": `[{"name": "a", "type": "postgres", "entities": ["src/entity/User.ts"]}, {"name": "b", "type": "sqlite"}]`}))
		if len(p.Groups) != 2 || p.Groups[0].Root != "ormconfig.json#a" || p.Groups[1].Root != "ormconfig.json#b" || len(p.Groups[0].Entities) != 1 {
			t.Errorf("groups %v", describe([]*Package{p}))
		}
	})
	t.Run("script", func(t *testing.T) {
		p, _, _ := discover(t, pkg03(map[string]string{"ormconfig.ts": `export default { type: 'sqlite', entities: ['src/entity/*.ts'] };`}))
		if len(p.Groups) != 1 || !p.Groups[0].Claimable() || len(p.Groups[0].Entities) != 2 {
			t.Errorf("groups %v", describe([]*Package{p}))
		}
	})
	t.Run("datasource wins", func(t *testing.T) {
		p, _, _ := discover(t, pkg03(map[string]string{
			"ormconfig.json": `{"type": "postgres"}`,
			"src/ds.ts":      "import { DataSource } from 'typeorm';\nexport const ds = new DataSource({ type: 'sqlite' });\n",
		}))
		if len(p.Groups) != 1 || p.Groups[0].Form != "datasource" {
			t.Errorf("groups %v", describe([]*Package{p}))
		}
	})
	t.Run("D forms", func(t *testing.T) {
		src := pkg03(map[string]string{"ormconfig.yml": "type: postgres\n", "ormconfig.json": `{"type": "postgres"}`})
		src["package-lock.json"] = lock("1.1.3", "", "")
		p, diags, _ := discover(t, src)
		if len(p.Groups) != 0 || !hasLine(diags, "ormconfig.yml file is not read (row D)") || !hasLine(diags, "TypeORM 1.x does not read ormconfig files") {
			t.Errorf("groups %d %v", len(p.Groups), diags)
		}
	})
}

// TestDiscoverCompilerFlags covers TO-V3: an entity file whose tsconfig
// chain does not prove experimentalDecorators makes the group D; an
// unresolved package `extends` leaves the flags unknown; a child config
// overrides its relative parent.
func TestDiscoverCompilerFlags(t *testing.T) {
	ds := "import { DataSource } from 'typeorm';\nexport const ds = new DataSource({ type: 'postgres', entities: ['src/entity/*.ts'] });\n"
	cases := []struct{ name, config, why string }{
		{"off", `{"compilerOptions": {"experimentalDecorators": false}}`, "is false"},
		{"absent", `{"compilerOptions": {}}`, "is not set in the tsconfig chain"},
		// A tsconfig.json extending a package outside the repository is
		// unread (row TS-C1): its bare imports claim no import edge
		// (config_unread), but `typeorm` is declared by package.json, so
		// it is still the package for the model reader (S3L) and only the
		// flags stay unknown.
		{"package extends", `{"extends": "@tsconfig/node20/tsconfig.json"}`, "may be set by @tsconfig/node20"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := pkg03(map[string]string{"src/ds.ts": ds, "tsconfig.json": c.config})
			p, _, _ := discover(t, src)
			g := p.Groups[0]
			if g.Claimable() || !strings.Contains(g.Refused, c.why) || !strings.Contains(g.Refused, "TO-V3") {
				t.Errorf("refused %q, want %q", g.Refused, c.why)
			}
		})
	}
	t.Run("chain", func(t *testing.T) {
		src := pkg03(map[string]string{
			"src/ds.ts":     ds,
			"tsconfig.json": `{"compilerOptions": {"experimentalDecorators": false, "strict": true}}`,
			"src/tsconfig.json": `{
  // a child config
  "extends": "../tsconfig.json",
  "compilerOptions": {"experimentalDecorators": true, "strictNullChecks": false}
}`,
		})
		p, _, _ := discover(t, src)
		g := p.Groups[0]
		e := g.Entities[0]
		if !g.Claimable() || e.Flags.Config != "src/tsconfig.json" || *e.Flags.Values["strict"] != true || *e.Flags.Values["strictNullChecks"] != false {
			t.Errorf("refused %q flags %+v", g.Refused, e.Flags)
		}
	})
}

// TestDiscoverPackageShadow covers the S3 review's B2 case: a package.json
// of the repository named typeorm that is not a declared workspace member
// may be what `typeorm` installs (a `file:` dependency), so the resolver
// refuses the import (unlinked_package) and it is not read as the package:
// no group.
func TestDiscoverPackageShadow(t *testing.T) {
	ds := "import { DataSource } from 'typeorm';\nexport const ds = new DataSource({ type: 'postgres', entities: ['src/entity/*.ts'] });\n"
	p, _, _ := discover(t, pkg03(map[string]string{"src/ds.ts": ds, "vendor/typeorm/package.json": `{"name": "typeorm", "main": "index.js"}`}))
	if len(p.Groups) != 0 {
		t.Errorf("groups read through a shadowed typeorm import: %v", describe([]*Package{p}))
	}
}
