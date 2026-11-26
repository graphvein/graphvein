// Helper of typeorm_catalog.sh: the TypeORM oracle of T018g6
// (ADR 0064). Never run by CI. Own code; needs Node 22 and, in the scratch
// kits, typeorm, reflect-metadata, pg, the SQLite drivers and typescript
// (transpile only, no type check).
//
// Subcommands:
//
//   specs PRESET ROOT SCRATCH PG
//       Write the spec of every run of PRESET (fixture or corpus; ROOT is
//       the fixture directory or the directory holding the corpus
//       checkouts), copy each run's directories into SCRATCH/runs/<name>/
//       tree (applying the run's type substitutions to the copy only), and
//       print "<name>|<dialect>|<database>|<spec file>" per run.
//   run SPEC OUT
//       Load one run's entity modules in this process (every module
//       outside the run's allow list is a stub), let TypeORM create the
//       tables (`synchronize` with `dropSchema` on a fresh scratch
//       database), or apply the run's migration classes (`migrationsRun`),
//       and write the run record: TypeORM's own metadata, the error
//       initialize() threw (if any) and the catalog.
//   merge OUT RUN...
//       Merge run records into the dump TestTypeORMOracle reads.
//
// A run spec (written by specs) holds:
//   name, repo           the run's name and the repository directory the
//                        Go test links (relative to its root)
//   groups               the model group sets (models/typeorm/<root>) the
//                        catalog is compared with
//   dialect, driver      postgres or sqlite; the TypeORM `type`
//   cross                true when the groups claim another dialect (the
//                        run checks the provider-independent facts only)
//   expect_failure       the error initialize() must throw (TO-P1), or ""
//   mode                 "sync" or "migrations"
//   pair                 for a migrations run, the sync run it is diffed
//                        with
//   tree, kit            the scratch copy and the scratch install
//   entities             tree-relative entity modules (every exported
//                        class with @Entity metadata is an entity)
//   migrations           tree-relative migration modules (mode
//                        "migrations")
//   allow                tree-relative path prefixes whose modules run for
//                        real; every other module of the tree and every
//                        package but typeorm, reflect-metadata and
//                        typeorm-naming-strategies is a stub
//   alias                {request prefix: tree-relative path}
//   string_enums         {package: [export]}: the export is an object whose
//                        every member is the string "<export>.<member>"
//                        (an enum of a stubbed package used as a default;
//                        the default is never claimed)
//   env                  environment variables set before loading
//   compiler             TypeScript options over the defaults (CommonJS,
//                        ES2020, legacy decorators with metadata)
//   options              DataSource options over the harness's own
//                        (entityPrefix, ...); snake: SnakeNamingStrategy
//   schemas              Postgres schemas created before initialize()
//   db                   {url} or {file}

"use strict";

const fs = require("node:fs");
const path = require("node:path");
const Module = require("node:module");

function die(msg) {
  process.stderr.write(`typeorm_catalog.cjs: ${msg}\n`);
  process.exit(1);
}

function readJSON(p) {
  return JSON.parse(fs.readFileSync(p, "utf8"));
}

function writeJSON(p, v) {
  fs.writeFileSync(p, JSON.stringify(v, null, 1) + "\n");
}

// makeStub is a permissive stand-in for a module that must not run: a
// callable Proxy that returns itself for every property, call and
// construction, except `prototype` (a real object, so `class X extends
// stub` works), `then` (never a thenable), Symbol.iterator (an empty
// iterator), Symbol.toPrimitive and toString (a label) and __esModule.
function makeStub(label) {
  const target = function () {};
  const proto = {};
  let proxy;
  proxy = new Proxy(target, {
    get(_t, key) {
      if (key === "prototype") return proto;
      if (key === "then") return undefined;
      if (key === "__esModule") return true;
      if (key === Symbol.iterator) return function* () {};
      if (key === Symbol.toPrimitive || key === "toString" || key === "valueOf") return () => `[stub ${label}]`;
      return proxy;
    },
    apply() {
      return proxy;
    },
    construct() {
      return proxy;
    },
  });
  return proxy;
}

function stringEnum(name) {
  return new Proxy({}, { get: (_t, k) => (typeof k === "string" ? `${name}.${k}` : undefined) });
}

const BUILTINS = ["path", "util", "crypto", "assert", "assert/strict", "events", "url", "buffer", "os"];
const PACKAGES = ["typeorm", "reflect-metadata", "typeorm-naming-strategies"];

// install hooks Module._load for requests made from files in the tree and
// the .ts loader (typescript transpileModule: decorator metadata needs the
// TypeScript emitter; Node's type stripping emits none).
function install(spec, kitRequire) {
  const tree = path.resolve(spec.tree);
  const ts = kitRequire("typescript");
  const allow = spec.allow || [];
  const alias = spec.alias || {};
  const enums = spec.string_enums || {};
  const stubs = new Map();
  const loaded = [];
  const realLoad = Module._load;
  const stub = (key) => {
    if (!stubs.has(key)) stubs.set(key, makeStub(key));
    return stubs.get(key);
  };
  const resolveFile = (target) => {
    const cands = [target, target + ".ts", path.join(target, "index.ts")];
    if (target.endsWith(".js")) cands.unshift(target.slice(0, -3) + ".ts");
    return cands.find((c) => fs.existsSync(c) && fs.statSync(c).isFile());
  };
  Module._load = function (request, parent, isMain) {
    const from = parent && parent.filename;
    if (!from || !from.startsWith(tree + path.sep)) return realLoad.apply(this, arguments);
    let target = null;
    const al = Object.keys(alias).find((a) => request === a || request.startsWith(a.endsWith("/") ? a : a + "/"));
    if (al) target = path.join(tree, alias[al], request.slice(al.length));
    else if (request.startsWith(".")) target = path.resolve(path.dirname(from), request);
    if (target !== null) {
      const file = resolveFile(target);
      const rel = file ? path.relative(tree, file).split(path.sep).join("/") : request;
      if (file && allow.some((a) => rel.startsWith(a))) {
        loaded.push(rel);
        return realLoad.call(this, file, parent, isMain);
      }
      return stub(rel);
    }
    const bare = request.replace(/^node:/, "");
    if (BUILTINS.includes(bare)) return realLoad.apply(this, arguments);
    const pkg = request.startsWith("@") ? request.split("/").slice(0, 2).join("/") : request.split("/")[0];
    if (enums[pkg]) {
      const m = { __esModule: true };
      for (const e of enums[pkg]) m[e] = stringEnum(e);
      return m;
    }
    if (PACKAGES.includes(pkg)) return kitRequire(request);
    return stub(request);
  };
  require.extensions[".ts"] = function (module, filename) {
    const out = ts.transpileModule(fs.readFileSync(filename, "utf8"), {
      fileName: filename,
      compilerOptions: Object.assign(
        {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2020,
          experimentalDecorators: true,
          emitDecoratorMetadata: true,
          esModuleInterop: true,
          strict: true,
          useDefineForClassFields: false,
        },
        spec.compiler || {},
      ),
    });
    module._compile(out.outputText, filename);
  };
  return { stubs, loaded };
}

// The catalog readers. Postgres: pg_catalog (columns with format_type, the
// default expression and identity; constraints p, u, f, c; indexes that are
// no constraint; enum types). SQLite: sqlite_master (the CREATE TABLE text
// names the constraints), pragma table_xinfo, index_list, index_info and
// foreign_key_list.
const PG_ACTIONS = { a: "NO ACTION", r: "RESTRICT", c: "CASCADE", n: "SET NULL", d: "SET DEFAULT" };

async function pgCatalog(q) {
  const tables = [];
  const rels = await q(
    `select c.oid, n.nspname, c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
     where c.relkind = 'r' and n.nspname not in ('pg_catalog', 'information_schema') and n.nspname not like 'pg_toast%' order by 2, 3`,
  );
  for (const t of rels) {
    const cols = await q(
      `select a.attname, format_type(a.atttypid, a.atttypmod) as type, a.attnotnull, pg_get_expr(d.adbin, d.adrelid) as def, a.attidentity
       from pg_attribute a left join pg_attrdef d on d.adrelid = a.attrelid and d.adnum = a.attnum
       where a.attrelid = $1 and a.attnum > 0 and not a.attisdropped order by a.attnum`,
      [t.oid],
    );
    const cons = await q(
      `select con.conname, con.contype, pg_get_constraintdef(con.oid) as def,
         array(select a.attname::text from unnest(con.conkey) with ordinality k(n, o) join pg_attribute a on a.attrelid = con.conrelid and a.attnum = k.n order by k.o) as cols,
         (select n2.nspname::text from pg_class c2 join pg_namespace n2 on n2.oid = c2.relnamespace where c2.oid = con.confrelid) as fschema,
         (select c2.relname::text from pg_class c2 where c2.oid = con.confrelid) as ftable,
         array(select a.attname::text from unnest(con.confkey) with ordinality k(n, o) join pg_attribute a on a.attrelid = con.confrelid and a.attnum = k.n order by k.o) as fcols,
         con.confdeltype, con.confupdtype
       from pg_constraint con where con.conrelid = $1 order by con.conname`,
      [t.oid],
    );
    const idx = await q(
      `select i.relname as name, ix.indisunique as uniq, pg_get_expr(ix.indpred, ix.indrelid) as pred, am.amname as method,
         array(select a.attname::text from unnest(ix.indkey) with ordinality k(n, o) join pg_attribute a on a.attrelid = ix.indrelid and a.attnum = k.n order by k.o) as cols
       from pg_index ix join pg_class i on i.oid = ix.indexrelid join pg_am am on am.oid = i.relam
       where ix.indrelid = $1 and not exists (select 1 from pg_constraint c where c.conindid = ix.indexrelid) order by i.relname`,
      [t.oid],
    );
    const pk = cons.find((c) => c.contype === "p");
    tables.push({
      schema: t.nspname,
      name: t.relname,
      sql: null,
      columns: cols.map((c) => ({ name: c.attname, type: c.type, not_null: c.attnotnull, default: c.def, identity: c.attidentity || "", autoincrement: !!c.attidentity || /^nextval\(/.test(c.def || "") })),
      primary_key: pk ? pk.cols : [],
      primary_key_name: pk ? pk.conname : null,
      uniques: cons.filter((c) => c.contype === "u").map((c) => ({ name: c.conname, columns: c.cols })),
      foreign_keys: cons
        .filter((c) => c.contype === "f")
        .map((c) => ({ name: c.conname, columns: c.cols, ref_schema: c.fschema, ref_table: c.ftable, ref_columns: c.fcols, on_delete: PG_ACTIONS[c.confdeltype], on_update: PG_ACTIONS[c.confupdtype] })),
      checks: cons.filter((c) => c.contype === "c").map((c) => ({ name: c.conname, def: c.def })),
      indexes: idx.map((i) => ({ name: i.name, columns: i.cols, unique: i.uniq, partial: i.pred !== null, method: i.method })),
    });
  }
  const enums = await q(
    `select n.nspname, t.typname, array(select e.enumlabel::text from pg_enum e where e.enumtypid = t.oid order by e.enumsortorder) as labels
     from pg_type t join pg_namespace n on n.oid = t.typnamespace where t.typtype = 'e' and n.nspname not in ('pg_catalog', 'information_schema') order by 1, 2`,
  );
  return { tables, enums: enums.map((e) => ({ schema: e.nspname, name: e.typname, values: e.labels })) };
}

// sqlConstraints reads the named and unnamed table constraints TypeORM
// writes into a SQLite CREATE TABLE text.
function sqlConstraints(sql) {
  const out = [];
  const re = /(?:CONSTRAINT "([^"]+)" )?(UNIQUE|FOREIGN KEY|PRIMARY KEY|CHECK)\s*\(/g;
  let m;
  while ((m = re.exec(sql)) !== null) {
    // Table constraints follow a comma or the opening paren; a CHECK may
    // also be a column constraint (TypeORM's inline simple-enum CHECK,
    // AbstractSqliteQueryRunner.buildCreateColumnSql), always unnamed.
    const before = sql.slice(0, m.index).trimEnd();
    if (m[2] !== "CHECK" && !before.endsWith(",") && !before.endsWith("(")) continue;
    let depth = 0;
    let i = m.index + m[0].length - 1;
    const start = i;
    for (; i < sql.length; i++) {
      if (sql[i] === "(") depth++;
      else if (sql[i] === ")" && --depth === 0) break;
    }
    out.push({ name: m[1] || null, kind: m[2], body: sql.slice(start + 1, i) });
  }
  return out;
}

function quotedList(s) {
  return [...s.matchAll(/"([^"]+)"/g)].map((x) => x[1]);
}

async function sqliteCatalog(all) {
  const tables = [];
  for (const t of await all(`select name, sql from sqlite_master where type = 'table' and name not like 'sqlite_%' order by name`)) {
    const n = t.name.replace(/"/g, '""');
    const cols = await all(`pragma table_xinfo("${n}")`);
    const il = await all(`pragma index_list("${n}")`);
    const fk = await all(`pragma foreign_key_list("${n}")`);
    const cons = sqlConstraints(t.sql || "");
    const indexes = [];
    const uniques = [];
    for (const i of il) {
      const info = await all(`pragma index_info("${i.name.replace(/"/g, '""')}")`);
      const icols = info.map((x) => x.name);
      if (i.origin === "u") {
        const c = cons.find((x) => x.kind === "UNIQUE" && quotedList(x.body).join(",") === icols.join(","));
        uniques.push({ name: c ? c.name : null, columns: icols });
      } else if (i.origin === "c") {
        indexes.push({ name: i.name, columns: icols, unique: !!i.unique, partial: !!i.partial, method: "" });
      }
    }
    const groups = new Map();
    for (const f of fk) {
      if (!groups.has(f.id)) groups.set(f.id, { columns: [], ref_columns: [], ref_table: f.table, on_delete: f.on_delete, on_update: f.on_update });
      groups.get(f.id).columns.push(f.from);
      groups.get(f.id).ref_columns.push(f.to);
    }
    const fks = [...groups.values()].map((g) => {
      const c = cons.find((x) => x.kind === "FOREIGN KEY" && quotedList(x.body).join(",") === g.columns.join(","));
      return { name: c ? c.name : null, columns: g.columns, ref_schema: null, ref_table: g.ref_table, ref_columns: g.ref_columns, on_delete: g.on_delete, on_update: g.on_update };
    });
    const pkCons = cons.find((x) => x.kind === "PRIMARY KEY");
    const autoinc = /"([^"]+)" integer PRIMARY KEY AUTOINCREMENT/i.exec(t.sql || "");
    tables.push({
      schema: null,
      name: t.name,
      sql: t.sql,
      columns: cols.map((c) => ({ name: c.name, type: c.type, not_null: !!c.notnull, default: c.dflt_value, identity: "", autoincrement: autoinc !== null && autoinc[1] === c.name })),
      primary_key: cols
        .filter((c) => c.pk > 0)
        .sort((a, b) => a.pk - b.pk)
        .map((c) => c.name),
      primary_key_name: pkCons ? pkCons.name : null,
      uniques,
      foreign_keys: fks,
      checks: cons.filter((x) => x.kind === "CHECK").map((x) => ({ name: x.name, def: x.body })),
      indexes,
    });
  }
  return { tables, enums: [] };
}

async function run(specPath, outPath) {
  const spec = readJSON(specPath);
  const kitRequire = Module.createRequire(path.join(path.resolve(spec.kit), "package.json"));
  // Package versions from the kit's own files (some packages do not export
  // package.json).
  const version = (p) => readJSON(path.join(path.resolve(spec.kit), "node_modules", p, "package.json")).version;
  for (const [k, v] of Object.entries(spec.env || {})) process.env[k] = v;
  const hook = install(spec, kitRequire);
  kitRequire("reflect-metadata");
  const typeorm = kitRequire("typeorm");
  const record = {
    name: spec.name,
    repo: spec.repo,
    groups: spec.groups,
    dialect: spec.dialect,
    driver: spec.driver,
    cross: !!spec.cross,
    mode: spec.mode,
    pair: spec.pair || "",
    expect_failure: spec.expect_failure || "",
    typeorm: version("typeorm"),
    typescript: version("typescript"),
    drivers: {},
    load_errors: [],
    init_error: null,
    harness: { stubs: [], loaded: [], schemas_created: [] },
    metadata: [],
    catalog: null,
  };
  for (const d of ["pg", "sqlite3", "better-sqlite3"]) {
    try {
      record.drivers[d] = version(d);
    } catch {
      // not installed in this kit
    }
  }
  const tree = path.resolve(spec.tree);
  const classes = [];
  const load = (rel) => {
    try {
      const m = require(path.join(tree, rel));
      for (const v of Object.values(m)) if (typeof v === "function" && !classes.includes(v)) classes.push(v);
    } catch (e) {
      record.load_errors.push({ file: rel, error: String((e && e.message) || e).split("\n")[0] });
    }
  };
  for (const rel of spec.entities) load(rel);
  const storage = typeorm.getMetadataArgsStorage();
  const entities = classes.filter((c) => storage.tables.some((t) => t.target === c));
  const migrations = [];
  if (spec.mode === "migrations") {
    const before = classes.length;
    for (const rel of spec.migrations) load(rel);
    for (const c of classes.slice(before)) if (typeof c.prototype.up === "function") migrations.push(c);
  }
  const opts = Object.assign(
    {
      type: spec.driver,
      entities,
      synchronize: spec.mode === "sync",
      dropSchema: true,
      migrations,
      migrationsRun: spec.mode === "migrations",
      logging: false,
    },
    spec.dialect === "postgres" ? { url: spec.db.url } : { database: spec.db.file },
    spec.options || {},
  );
  if (spec.snake) {
    const { SnakeNamingStrategy } = kitRequire("typeorm-naming-strategies");
    opts.namingStrategy = new SnakeNamingStrategy();
  }
  if (spec.dialect === "postgres" && (spec.schemas || []).length > 0) {
    const pg = kitRequire("pg");
    const client = new pg.Client({ connectionString: spec.db.url });
    await client.connect();
    for (const s of spec.schemas) {
      await client.query(`create schema if not exists "${s}"`);
      record.harness.schemas_created.push(s);
    }
    await client.end();
  }
  let ds = null;
  try {
    ds = new typeorm.DataSource(opts);
    await ds.initialize();
  } catch (e) {
    record.init_error = String((e && e.message) || e).split("\n")[0];
  }
  if (ds !== null) {
    if (!ds.isInitialized) {
      try {
        await ds.buildMetadatas();
      } catch {
        // the metadata of a failing run is not needed
      }
    }
    for (const em of ds.entityMetadatas || []) {
      record.metadata.push({ target: typeof em.target === "function" ? em.target.name : String(em.target), table: em.tableName, schema: em.schema || null, synchronize: em.synchronize, type: em.tableType });
    }
  }
  if (ds !== null && ds.isInitialized) {
    record.catalog = spec.dialect === "postgres" ? await pgCatalog((s, p) => ds.query(s, p)) : await sqliteCatalog((s) => ds.query(s));
    await ds.destroy();
  }
  record.harness.stubs = [...hook.stubs.keys()].sort();
  record.harness.loaded = [...new Set(hook.loaded)].sort();
  writeJSON(outPath, record);
  const tables = record.catalog ? record.catalog.tables.length : 0;
  process.stderr.write(`typeorm_catalog.cjs: ${spec.name}: ${entities.length} entities, ${tables} tables${record.init_error ? ", initialize() threw: " + record.init_error : ""}${record.load_errors.length ? ", load errors " + record.load_errors.length : ""}\n`);
}

// copyTree copies dir (relative to the repository) into the run tree.
function copyTree(repoDir, tree, dirs) {
  for (const d of dirs) {
    const from = path.join(repoDir, d);
    if (!fs.existsSync(from)) die(`${from} does not exist`);
    fs.cpSync(from, path.join(tree, d), { recursive: true, filter: (src) => !/[\\/](node_modules|\.git)([\\/]|$)/.test(src) });
  }
}

// substitute applies a run's type substitutions to its copy; each must
// match exactly the stated number of times (a corpus that moved fails).
function substitute(tree, subs) {
  for (const s of subs || []) {
    const f = path.join(tree, s.file);
    const text = fs.readFileSync(f, "utf8");
    const n = text.split(s.from).length - 1;
    if (n !== s.count) die(`${s.file}: ${JSON.stringify(s.from)} occurs ${n} times, want ${s.count}`);
    fs.writeFileSync(f, text.split(s.from).join(s.to));
  }
}

function list(dir, re) {
  return fs
    .readdirSync(dir, { recursive: true })
    .map((f) => String(f).split(path.sep).join("/"))
    .filter((f) => re.test(f))
    .sort();
}

// The fixture runs (testdata/fixtures/typeorm-models): every claimable
// group's entities on each dialect it claims (sqlite through both drivers
// on 0.3.31, better-sqlite3 on 1.x), and the provable TO-P1 failures, which
// must fail as the contract says.
function fixtureRuns(root) {
  const app = ["Account", "Profile", "Note", "Archive", "Tagged", "Holder", "Member", "Tag", "Odd"].map((c) => `app/src/entity/${c}.ts`);
  const appAllow = ["app/src/entity/", "app/src/constants/", "app/src/utils/"];
  const k31 = "t0331";
  const runs = [
    { name: "app-sqlite", groups: ["app/src/data-source.ts"], dialect: "sqlite", driver: "sqlite", kit: k31, dirs: ["app"], entities: app, allow: appAllow },
    { name: "app-bs3", groups: ["app/src/data-source.ts"], dialect: "sqlite", driver: "better-sqlite3", kit: k31, dirs: ["app"], entities: app, allow: appAllow },
    { name: "app-postgres", groups: ["app/src/data-source.ts"], dialect: "postgres", driver: "postgres", kit: k31, dirs: ["app"], entities: app, allow: appAllow, env: { DB_TYPE: "postgres" } },
    { name: "app-pgonly", groups: ["app/src/pg-data-source.ts"], dialect: "postgres", driver: "postgres", kit: k31, dirs: ["app"], entities: ["app/src/entity/Typed.ts", "app/src/entity/Audited.ts"], allow: appAllow, schemas: ["audit"] },
    { name: "f2-sqlite", groups: ["failures/src/f2.ts"], dialect: "sqlite", driver: "sqlite", kit: k31, dirs: ["failures"], entities: ["failures/src/entity/PgDatetime.ts"], allow: ["failures/src/entity/"] },
    { name: "f2-bs3", groups: ["failures/src/f2.ts"], dialect: "sqlite", driver: "better-sqlite3", kit: k31, dirs: ["failures"], entities: ["failures/src/entity/PgDatetime.ts"], allow: ["failures/src/entity/"] },
    { name: "forms-postgres", groups: ["forms/src/app.module.ts#default", "forms/src/data-source.ts"], dialect: "postgres", driver: "postgres", kit: k31, dirs: ["forms"], entities: ["forms/src/entity/thing.entity.ts"], allow: ["forms/src/entity/"] },
    { name: "forms-sqlite", groups: ["forms/src/app.module.ts#second"], dialect: "sqlite", driver: "sqlite", kit: k31, dirs: ["forms"], entities: ["forms/src/entity/thing.entity.ts"], allow: ["forms/src/entity/"] },
    { name: "forms-bs3", groups: ["forms/src/app.module.ts#second"], dialect: "sqlite", driver: "better-sqlite3", kit: k31, dirs: ["forms"], entities: ["forms/src/entity/thing.entity.ts"], allow: ["forms/src/entity/"] },
    { name: "ormcfg-sqlite", groups: ["ormcfg/ormconfig.json"], dialect: "sqlite", driver: "sqlite", kit: k31, dirs: ["ormcfg"], entities: ["ormcfg/src/entity/Widget.ts"], allow: ["ormcfg/src/entity/"] },
    { name: "ormcfg-bs3", groups: ["ormcfg/ormconfig.json"], dialect: "sqlite", driver: "better-sqlite3", kit: k31, dirs: ["ormcfg"], entities: ["ormcfg/src/entity/Widget.ts"], allow: ["ormcfg/src/entity/"] },
    { name: "snake-postgres", groups: ["snake/src/data-source.ts"], dialect: "postgres", driver: "postgres", kit: k31, dirs: ["snake"], entities: ["snake/src/entity/SnakeUser.ts"], allow: ["snake/src/entity/"], snake: true },
    { name: "v1-bs3", groups: ["v1/src/data-source.ts"], dialect: "sqlite", driver: "better-sqlite3", kit: "t113", dirs: ["v1"], entities: ["Item", "Shelf", "Book", "Reader"].map((c) => `v1/src/entity/${c}.ts`), allow: ["v1/src/entity/"] },
  ];
  // TO-P1: each provable failure fails on its dialect.
  const fail = (name, group, dialect, driver, kit, dir, entity, expect) => ({ name, groups: [group], dialect, driver, kit, dirs: [dir], entities: [entity], allow: [path.posix.dirname(entity) + "/"], expect_failure: expect });
  runs.push(
    fail("p1-1", "failures/src/f1.ts", "postgres", "postgres", k31, "failures", "failures/src/entity/NoKey.ts", "does not have a primary column"),
    fail("p1-2", "failures/src/f2.ts", "postgres", "postgres", k31, "failures", "failures/src/entity/PgDatetime.ts", "is not supported by"),
    fail("p1-3", "failures/src/f3.ts", "sqlite", "sqlite", k31, "failures", "failures/src/entity/NoAnnotation.ts", "Data type \"Object\""),
    fail("p1-4", "failures/src/f4.ts", "sqlite", "sqlite", k31, "failures", "failures/src/entity/Quoted.ts", "syntax error"),
    fail("p1-5", "failures/src/f5.ts", "postgres", "postgres", k31, "failures", "failures/src/entity/Fraction.ts", "invalid input syntax"),
    fail("p1-6", "failures/src/f6.ts", "sqlite", "sqlite", k31, "failures", "failures/src/entity/SqliteEnum.ts", "is not supported by"),
    fail("p1-7", "v1/src/legacy-data-source.ts", "sqlite", "sqlite", "t113", "v1", "v1/src/entity/Item.ts", "Wrong driver"),
    fail("p1-8", "v1/src/count-data-source.ts", "sqlite", "better-sqlite3", "t113", "v1", "v1/src/entity/Counted.ts", "RelationCount"),
    fail("p1-9", "v1/src/index-type-data-source.ts", "sqlite", "better-sqlite3", "t113", "v1", "v1/src/entity/Indexed.ts", "Unsupported index type"),
    // Revision 4 A1, A5: string defaults postgres rejects, a length on a
    // type without one (the metadata validator), a precision on a type
    // without a type modifier.
    fail("p1-10", "failures/src/f10.ts", "postgres", "postgres", k31, "failures", "failures/src/entity/PgKeyword.ts", "invalid input syntax for type timestamp"),
    fail("p1-11", "failures/src/f11.ts", "postgres", "postgres", k31, "failures", "failures/src/entity/PgUuidText.ts", "invalid input syntax for type uuid"),
    fail("p1-13-int", "failures/src/f13.ts", "postgres", "postgres", k31, "failures", "failures/src/entity/IntLength.ts", "does not support length property"),
    fail("p1-13-text", "failures/src/f13.ts", "postgres", "postgres", k31, "failures", "failures/src/entity/TextLength.ts", "does not support length property"),
    fail("p1-13-sqlite", "failures/src/f13s.ts", "sqlite", "sqlite", k31, "failures", "failures/src/entity/IntLength.ts", "does not support length property"),
    fail("p1-14-int", "failures/src/f14.ts", "postgres", "postgres", k31, "failures", "failures/src/entity/IntPrecision.ts", "syntax error"),
    fail("p1-14-text", "failures/src/f14.ts", "postgres", "postgres", k31, "failures", "failures/src/entity/TextPrecision.ts", "type modifier is not allowed"),
  );
  for (const r of runs) r.repo = ".";
  return runs;
}

// The corpus runs: seerr (both dialects, and its migrations on both),
// nest-admin (MySQL facts mapped: SQLite through both drivers and the
// Postgres copy for the provider-independent facts, TA2), Maintainerr
// (better-sqlite3; the Postgres copy for the provider-independent facts,
// TA3) and aqualink-app (Postgres copy with geometry as text, TA3).
function corpusRuns(root) {
  const seerrEntities = list(path.join(root, "seerr", "server", "entity"), /^[A-Za-z]+\.ts$/).map((f) => `server/entity/${f}`);
  const seerr = { repo: "seerr", groups: ["server/datasource.ts"], kit: "t0331", dirs: ["server"], entities: seerrEntities, allow: ["server/entity/", "server/utils/DbColumnHelper.ts", "server/utils/dbType.ts", "server/constants/"], alias: { "@server/": "server/" }, env: { NODE_ENV: "test" } };
  const seerrMig = (d) => list(path.join(root, "seerr", "server", "migration", d), /\.ts$/).map((f) => `server/migration/${d}/${f}`);
  const na = {
    repo: "nest-admin",
    groups: ["src/shared/database/database.module.ts"],
    kit: "t0322",
    dirs: ["src"],
    entities: [
      "auth/entities/access-token.entity.ts", "auth/entities/refresh-token.entity.ts", "system/dept/dept.entity.ts", "system/dict-item/dict-item.entity.ts",
      "system/dict-type/dict-type.entity.ts", "system/log/entities/captcha-log.entity.ts", "system/log/entities/login-log.entity.ts", "system/log/entities/task-log.entity.ts",
      "system/menu/menu.entity.ts", "system/param-config/param-config.entity.ts", "system/role/role.entity.ts", "system/task/task.entity.ts", "todo/todo.entity.ts",
      "tools/storage/storage.entity.ts", "user/user.entity.ts",
    ].map((f) => `src/modules/${f}`),
    allow: ["src/common/entity/", "src/modules/"],
    alias: { "~/": "src/" },
    env: { NODE_ENV: "test" },
    compiler: { strict: false, strictNullChecks: false, target: 9, useDefineForClassFields: true },
    cross: true,
  };
  const mtDir = "apps/server/src/modules";
  const mt = {
    repo: "maintainerr",
    groups: ["apps/server/src/datasource-config.ts", "apps/server/src/app/app.module.ts"],
    kit: "t112",
    dirs: ["apps/server/src", "packages/contracts/src"],
    entities: list(path.join(root, "maintainerr", mtDir), /entities\/[^/]+\.entities\.ts$/).map((f) => `${mtDir}/${f}`),
    allow: [`${mtDir}/`, "packages/contracts/src/"],
    alias: { "@maintainerr/contracts": "packages/contracts/src/index" },
    string_enums: { "@nestjs/schedule": ["CronExpression"] },
    env: { NODE_ENV: "test" },
    compiler: { strict: false, strictNullChecks: false, target: 10, useDefineForClassFields: true },
  };
  const aqDir = "packages/api";
  const aqEntities = list(path.join(root, "aqualink-app", aqDir, "src"), /\.entity\.ts$/).map((f) => `${aqDir}/src/${f}`);
  const aq = {
    repo: "aqualink-app",
    groups: ["packages/api/ormconfig.ts"],
    kit: "t0331",
    dirs: [`${aqDir}/src`],
    entities: aqEntities,
    allow: aqEntities.concat(["sites/schemas/source-type.enum.ts", "time-series/metrics.enum.ts", "monitoring/schemas/monitoring-metric.enum.ts", "wind-wave-data/wind-wave-data.types.ts"].map((f) => `${aqDir}/src/${f}`)),
    compiler: { strict: true, target: 2, useDefineForClassFields: false },
    env: { NODE_ENV: "test" },
    snake: true,
  };
  // every occurrence in the run's entity modules, counted on the checkout
  const replaceAll = (repo, files, from, to) => {
    const out = [];
    for (const f of files) {
      const n = fs.readFileSync(path.join(root, repo, f), "utf8").split(from).length - 1;
      if (n > 0) out.push([f, from, to, n]);
    }
    if (out.length === 0) die(`${repo}: ${from} occurs in no entity module`);
    return out;
  };
  const naSubs = replaceAll("nest-admin", na.entities, "type: 'tinyint'", "type: 'smallint'").concat(replaceAll("nest-admin", na.entities, "type: 'datetime'", "type: 'timestamp'"));
  const mtPgSubs = replaceAll("maintainerr", mt.entities, "type: 'datetime'", "type: 'timestamp'");
  const aqSubs = replaceAll("aqualink-app", aq.entities, "type: 'geometry'", "type: 'text'").concat(replaceAll("aqualink-app", aq.entities, "@Index({ spatial: true })", "@Index({ spatial: false })"));
  const subs = (l) => l.map(([file, from, to, count]) => ({ file, from, to, count }));
  return [
    { ...seerr, name: "seerr-sqlite", dialect: "sqlite", driver: "sqlite" },
    { ...seerr, name: "seerr-bs3", dialect: "sqlite", driver: "better-sqlite3" },
    { ...seerr, name: "seerr-postgres", dialect: "postgres", driver: "postgres", env: { NODE_ENV: "test", DB_TYPE: "postgres" } },
    { ...seerr, name: "seerr-sqlite-migrations", dialect: "sqlite", driver: "sqlite", mode: "migrations", pair: "seerr-sqlite", migrations: seerrMig("sqlite") },
    { ...seerr, name: "seerr-postgres-migrations", dialect: "postgres", driver: "postgres", mode: "migrations", pair: "seerr-postgres", migrations: seerrMig("postgres"), env: { NODE_ENV: "test", DB_TYPE: "postgres" } },
    { ...na, name: "na-sqlite", dialect: "sqlite", driver: "sqlite" },
    { ...na, name: "na-bs3", dialect: "sqlite", driver: "better-sqlite3" },
    { ...na, name: "na-postgres", dialect: "postgres", driver: "postgres", subs: subs(naSubs) },
    { ...mt, name: "mt-bs3", dialect: "sqlite", driver: "better-sqlite3" },
    { ...mt, name: "mt-postgres", dialect: "postgres", driver: "postgres", cross: true, subs: subs(mtPgSubs) },
    { ...aq, name: "aq-postgres", dialect: "postgres", driver: "postgres", subs: subs(aqSubs) },
  ];
}

function specs(preset, root, scratch, pg) {
  root = path.resolve(root);
  scratch = path.resolve(scratch);
  const runs = preset === "fixture" ? fixtureRuns(root) : preset === "corpus" ? corpusRuns(root) : die(`unknown preset ${preset}`);
  const lines = [];
  for (const r of runs) {
    const dir = path.join(scratch, "runs", r.name);
    fs.rmSync(dir, { recursive: true, force: true });
    const tree = path.join(dir, "tree");
    fs.mkdirSync(tree, { recursive: true });
    copyTree(path.join(root, r.repo), tree, r.dirs);
    substitute(tree, r.subs);
    const db = r.dialect === "postgres" ? { url: pg.replace(/\/[^/?]*(\?|$)/, `/typeorm_oracle_${r.name.replace(/-/g, "_")}$1`) } : { file: path.join(dir, "db.sqlite3") };
    const spec = {
      name: r.name,
      repo: r.repo,
      groups: r.groups.map((g) => `models/typeorm/${g}`),
      dialect: r.dialect,
      driver: r.driver,
      cross: !!r.cross,
      expect_failure: r.expect_failure === undefined ? "" : r.expect_failure,
      failure: r.expect_failure !== undefined,
      mode: r.mode || "sync",
      pair: r.pair || "",
      tree,
      kit: path.join(scratch, "kits", r.kit),
      entities: r.entities,
      migrations: r.migrations || [],
      allow: r.allow,
      alias: r.alias || {},
      string_enums: r.string_enums || {},
      env: r.env || {},
      compiler: r.compiler || {},
      options: r.options || {},
      snake: !!r.snake,
      schemas: r.schemas || [],
      db,
    };
    writeJSON(path.join(dir, "spec.json"), spec);
    lines.push(`${r.name}|${r.dialect}|${r.dialect === "postgres" ? `typeorm_oracle_${r.name.replace(/-/g, "_")}` : "-"}|${path.join(dir, "spec.json")}`);
  }
  process.stdout.write(lines.join("\n") + "\n");
}

function merge(out, files) {
  writeJSON(out, { runs: files.map((f) => readJSON(f)) });
}

const [cmd, ...args] = process.argv.slice(2);
if (cmd === "specs") specs(...args);
else if (cmd === "run")
  run(...args).catch((e) => {
    die(String((e && e.stack) || e));
  });
else if (cmd === "merge") merge(args[0], args.slice(1));
else die("usage: typeorm_catalog.cjs specs|run|merge ...");
