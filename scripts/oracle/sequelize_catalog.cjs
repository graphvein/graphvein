// Helper of sequelize_catalog.sh: the Sequelize oracle of T018g3
// (ADR 0061). Never run by CI. Own code; needs Node 22.13 or later
// (node:sqlite) and, in the scratch kit, sequelize, pg, sqlite3 and
// typescript (transpile only, no type check).
//
// Subcommands:
//
//   run SPEC OUT
//       Load one run's models in this process and let Sequelize create the
//       tables, then write the run record (models, sync steps, catalog).
//       SPEC is a JSON file:
//         name, repo, group     the run's name, the repository directory
//                               the Go test links (relative to its root)
//                               and the group's instance file
//         dialect               postgres or sqlite
//         tree                  the scratch copy of the repository (only
//                               the directories the run needs)
//         kit                   the scratch install (node_modules)
//         entries               modules required in order (tree-relative)
//         allow                 tree-relative path prefixes whose modules
//                               run for real; every other module of the
//                               tree, and every package but `packages`,
//                               is a stub
//         packages              bare packages loaded for real from the kit
//         builtins              Node builtins loaded for real
//         shims                 {tree-relative module: shim kind}; the
//                               kind "instance" replaces the module by
//                               {default: <instance>} plus the sequelize
//                               exports, "json:<file>" by that JSON file
//         instance              {dialect, define} for the "instance" shim
//         env                   environment variables set before loading
//         allowLoadError        a load error is recorded, not fatal (a
//                               fixture module throws by design)
//         db                    {url} (postgres) or {file} (sqlite)
//         functions             SQL run before sync (postgres stubs of
//                               database functions a migration creates)
//       Every `new Sequelize(...)` made while loading keeps its own options
//       (dialect, define, schema, ...) except the connection, which points
//       at the run's scratch database; a missing dialect is the run's
//       (assumption SQ-V4, recorded). Exactly one instance is expected.
//   specs PRESET ROOT SCRATCH KIT PG
//       Write the spec of every run of PRESET (fixture or corpus; ROOT is
//       the fixture directory or the directory holding the corpus
//       checkouts) and print "<n>|<dialect>|<spec file>" per run.
//   merge OUT RUN...
//       Merge run records into the dump TestSequelizeOracle reads.
//
// Sync: `sequelize.sync({force: true})`. When it fails, the harness
// replays Sequelize's own steps (lib/sequelize.js:353-400) model by model
// in the same order, deferring a model whose step fails to a later pass
// (up to four), and records every failure: acyclic groups in the reversed
// topological order, cyclic groups create every table without foreign
// keys and then run the alter pass per model; sqlite syncs in model order
// with foreign keys off. Schemas named by a model's `schema` option are
// created first (sync does not create them).

"use strict";

const fs = require("node:fs");
const path = require("node:path");
const Module = require("node:module");

function die(msg) {
  process.stderr.write(`sequelize_catalog.cjs: ${msg}\n`);
  process.exit(1);
}

function readJSON(p) {
  return JSON.parse(fs.readFileSync(p, "utf8"));
}

function writeJSON(p, v) {
  fs.writeFileSync(p, JSON.stringify(v, null, 1) + "\n");
}

// stub is a permissive stand-in for a module that must not run: a callable
// Proxy that returns itself for every property, call and construction,
// except `prototype` (a real object, so `class X extends stub` works),
// `then` (undefined: never a thenable), Symbol.iterator (an empty
// iterator), Symbol.toPrimitive and toString (a label) and __esModule.
function makeStub(label) {
  const target = function () {};
  const proto = {};
  let proxy;
  const handler = {
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
    has() {
      return false;
    },
  };
  proxy = new Proxy(target, handler);
  return proxy;
}

const BUILTINS = new Set(Module.builtinModules);

function run(specPath, outPath) {
  const spec = readJSON(specPath);
  const tree = path.resolve(spec.tree);
  const kit = path.resolve(spec.kit);
  const kitRequire = Module.createRequire(path.join(kit, "package.json"));
  const ts = kitRequire("typescript");
  const allow = (spec.allow || []).map((p) => path.join(tree, p));
  const packages = new Set(spec.packages || ["sequelize"]);
  const builtins = new Set(spec.builtins || ["path", "util", "crypto", "assert", "events", "url", "buffer", "stream", "os"]);
  const shims = spec.shims || {};
  const record = {
    name: spec.name,
    repo: spec.repo,
    group: spec.group,
    dialect: spec.dialect,
    load_error: null,
    harness: { dialect_supplied: false, schemas_created: [], functions: spec.functions || [], stubs: [] },
    sync: { path: "sync", failures: [] },
    models: [],
    catalog: null,
  };
  for (const [k, v] of Object.entries(spec.env || {})) process.env[k] = v;

  // The real sequelize package of the kit, its constructor wrapped so the
  // connection goes to the scratch database.
  const real = kitRequire("sequelize");
  record.sequelize = kitRequire("sequelize/package.json").version;
  const instances = [];
  const connection = (opts) => {
    const out = { ...opts };
    for (const k of ["host", "port", "username", "password", "database", "storage", "replication", "dialectOptions", "ssl", "native", "dialectModule", "dialectModulePath", "pool"]) delete out[k];
    if (!out.dialect) {
      out.dialect = spec.dialect;
      record.harness.dialect_supplied = true;
    }
    if (out.dialect !== spec.dialect) die(`${spec.name}: an instance of dialect ${out.dialect}, the run is ${spec.dialect}`);
    out.logging = false;
    if (spec.dialect === "postgres") {
      const u = new URL(spec.db.url);
      Object.assign(out, { host: u.hostname || decodeURIComponent(u.searchParams.get("host") || ""), port: u.port ? Number(u.port) : undefined, username: decodeURIComponent(u.username), password: decodeURIComponent(u.password) || undefined, database: u.pathname.slice(1) });
      if (!u.hostname && u.searchParams.get("host")) out.host = u.searchParams.get("host");
    } else {
      out.storage = spec.db.file;
    }
    return out;
  };
  const Wrapped = new Proxy(real, {
    construct(target, args) {
      let opts = {};
      const last = args[args.length - 1];
      if (last && typeof last === "object" && !Array.isArray(last)) opts = last;
      const inst = Reflect.construct(target, [connection(opts)]);
      instances.push(inst);
      return inst;
    },
    get(target, key, receiver) {
      if (key === "Sequelize" || key === "default") return Wrapped;
      return Reflect.get(target, key, receiver);
    },
  });

  const inTree = (f) => f === tree || f.startsWith(tree + path.sep);
  const allowed = (f) => allow.some((p) => f === p || f.startsWith(p.endsWith(path.sep) ? p : p + path.sep) || f.startsWith(p));
  const stubs = new Map();
  const shimCache = new Map();
  const stubFor = (label) => {
    if (!stubs.has(label)) {
      stubs.set(label, makeStub(label));
      record.harness.stubs.push(label);
    }
    return stubs.get(label);
  };

  Module._extensions[".ts"] = function (module, filename) {
    const source = fs.readFileSync(filename, "utf8");
    const out = ts.transpileModule(source, {
      fileName: filename,
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true, isolatedModules: true },
    });
    module._compile(out.outputText, filename);
  };

  const origLoad = Module._load;
  Module._load = function (request, parent, isMain) {
    const from = parent && parent.filename;
    if (!from || !inTree(from)) return origLoad.apply(this, arguments);
    const bare = request.startsWith("node:") ? request.slice(5) : request;
    if (request.startsWith(".") || path.isAbsolute(request)) {
      let file;
      try {
        file = Module._resolveFilename(request, parent, isMain);
      } catch {
        return stubFor(path.relative(tree, path.resolve(path.dirname(from), request)));
      }
      const rel = path.relative(tree, file).split(path.sep).join("/");
      const shim = shims[rel];
      if (shim === "instance") {
        if (shimCache.has(rel)) return shimCache.get(rel);
        const inst = new Wrapped({ dialect: spec.instance.dialect, define: spec.instance.define || {} });
        const { Op, DataTypes, Model, QueryTypes, Transaction } = real;
        const shimmed = { Op, DataTypes, Model, QueryTypes, Transaction, Sequelize: Wrapped, default: inst, __esModule: true };
        shimCache.set(rel, shimmed);
        return shimmed;
      }
      if (shim && shim.startsWith("json:")) {
        return readJSON(path.join(tree, shim.slice(5)));
      }
      if (inTree(file) && allowed(file)) return origLoad.apply(this, arguments);
      return stubFor(rel);
    }
    if (BUILTINS.has(bare)) {
      return builtins.has(bare) ? origLoad.apply(this, arguments) : stubFor("node:" + bare);
    }
    const pkg = request.startsWith("@") ? request.split("/").slice(0, 2).join("/") : request.split("/")[0];
    if (pkg === "sequelize" && request === "sequelize") return Wrapped;
    if (packages.has(pkg)) return kitRequire(request);
    const shim = shims[request];
    if (shim && shim.startsWith("json:")) return readJSON(path.join(tree, shim.slice(5)));
    return stubFor(request);
  };

  return (async () => {
    for (const entry of spec.entries) {
      try {
        require(path.join(tree, entry));
      } catch (e) {
        record.load_error = `${entry}: ${e && e.message}`;
        if (!spec.allowLoadError) die(`${spec.name}: loading ${entry} failed: ${e && e.stack}`);
      }
    }
    if (instances.length !== 1) die(`${spec.name}: ${instances.length} Sequelize instances were made, want 1`);
    const seq = instances[0];
    for (const m of seq.modelManager.models) {
      const t = m.getTableName();
      const attrs = {};
      for (const [k, a] of Object.entries(m.rawAttributes)) attrs[k] = { field: a.field, type: String(a.type && a.type.key), allowNull: a.allowNull, primaryKey: !!a.primaryKey };
      record.models.push({ name: m.name, table: typeof t === "string" ? t : t.tableName, schema: typeof t === "string" ? null : t.schema, attributes: attrs });
    }
    await prepare(seq, spec, record);
    await sync(seq, record);
    record.catalog = spec.dialect === "postgres" ? await pgCatalog(seq) : null;
    await seq.close();
    if (spec.dialect === "sqlite") record.catalog = await sqliteCatalog(spec.db.file);
    writeJSON(outPath, record);
  })();
}

// prepare empties the scratch database, creates the schemas the models
// name (postgres) and the stub functions.
async function prepare(seq, spec, record) {
  if (spec.dialect !== "postgres") return;
  const schemas = await seq.query("SELECT nspname FROM pg_catalog.pg_namespace WHERE nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast') AND nspname NOT LIKE 'pg\\_%'", { type: "SELECT" });
  for (const { nspname } of schemas) await seq.query(`DROP SCHEMA "${nspname}" CASCADE`);
  await seq.query("CREATE SCHEMA public");
  const wanted = new Set();
  if (seq.config.schema) wanted.add(seq.config.schema);
  if (seq.options.schema) wanted.add(seq.options.schema);
  for (const m of seq.modelManager.models) if (m._schema) wanted.add(m._schema);
  wanted.delete("public");
  for (const s of [...wanted].sort()) {
    await seq.query(`CREATE SCHEMA "${s}"`);
    record.harness.schemas_created.push(s);
  }
  for (const f of spec.functions || []) await seq.query(f);
}

function failure(step, model, e) {
  return { step, model: model.name, table: String(model.getTableName().tableName || model.getTableName()), error: String((e && (e.parent && e.parent.message)) || (e && e.message) || e) };
}

// passes runs step over the models in order, deferring a failing model to
// a later pass (up to four passes); the failures of the last pass stay.
async function passes(models, step, name, record) {
  let todo = models;
  for (let pass = 0; pass < 4 && todo.length > 0; pass++) {
    const failed = [];
    const errors = [];
    for (const m of todo) {
      try {
        await step(m);
      } catch (e) {
        failed.push(m);
        errors.push(failure(name, m, e));
      }
    }
    if (failed.length === todo.length || pass === 3) {
      record.sync.failures.push(...errors);
      return;
    }
    todo = failed;
  }
}

async function sync(seq, record) {
  try {
    await seq.sync({ force: true });
    return;
  } catch (e) {
    record.sync.first_error = String((e && e.parent && e.parent.message) || (e && e.message) || e);
  }
  const sorted = seq.modelManager.getModelsTopoSortedByForeignKey();
  if (seq.getDialect() === "sqlite") {
    record.sync.path = sorted == null ? "replay-cyclic" : "replay-acyclic";
    const order = sorted == null ? seq.modelManager.models : sorted.reverse();
    await seq.query("PRAGMA foreign_keys = OFF");
    await passes(order, (m) => m.sync({ force: true }), "sync", record);
    await seq.query("PRAGMA foreign_keys = ON");
    return;
  }
  if (sorted != null) {
    record.sync.path = "replay-acyclic";
    await passes(sorted.reverse(), (m) => m.sync({ force: true }), "sync", record);
    return;
  }
  record.sync.path = "replay-cyclic";
  await passes(seq.modelManager.models, (m) => m.sync({ force: true, withoutForeignKeyConstraints: true }), "create", record);
  for (const m of seq.modelManager.models) {
    try {
      await m.sync({ force: false, alter: true });
    } catch (e) {
      record.sync.failures.push(failure("alter", m, e));
    }
  }
}

const PG_CATALOG = `
WITH t AS (
  SELECT c.oid, n.nspname, c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r', 'p') AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\\_toast%')
SELECT json_build_object(
  'tables', (SELECT coalesce(json_agg(json_build_object(
    'schema', t.nspname, 'name', t.relname,
    'columns', (SELECT coalesce(json_agg(json_build_object(
        'name', a.attname, 'type', pg_catalog.format_type(a.atttypid, a.atttypmod), 'not_null', a.attnotnull,
        'default', pg_catalog.pg_get_expr(d.adbin, d.adrelid)) ORDER BY a.attnum), '[]')
      FROM pg_catalog.pg_attribute a
      LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
      WHERE a.attrelid = t.oid AND a.attnum > 0 AND NOT a.attisdropped),
    'constraints', (SELECT coalesce(json_agg(json_build_object(
        'name', con.conname, 'type', con.contype::text,
        'columns', (SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(con.conkey) WITH ORDINALITY k(n, ord)
                    JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.n),
        'ref_schema', fn.nspname, 'ref_table', fc.relname,
        'ref_columns', (SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(con.confkey) WITH ORDINALITY k(n, ord)
                        JOIN pg_catalog.pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.n),
        'on_delete', con.confdeltype::text, 'on_update', con.confupdtype::text,
        'definition', pg_catalog.pg_get_constraintdef(con.oid)) ORDER BY con.conname), '[]')
      FROM pg_catalog.pg_constraint con LEFT JOIN pg_catalog.pg_class fc ON fc.oid = con.confrelid
      LEFT JOIN pg_catalog.pg_namespace fn ON fn.oid = fc.relnamespace
      WHERE con.conrelid = t.oid),
    'indexes', (SELECT coalesce(json_agg(json_build_object(
        'name', ic.relname, 'unique', i.indisunique, 'primary', i.indisprimary,
        'constraint', EXISTS (SELECT 1 FROM pg_catalog.pg_constraint x WHERE x.conindid = i.indexrelid AND x.conrelid = i.indrelid),
        'columns', (SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(i.indkey::pg_catalog.int2[]) WITH ORDINALITY k(n, ord)
                    LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.n WHERE k.ord <= i.indnkeyatts),
        'method', am.amname, 'partial', i.indpred IS NOT NULL, 'expressions', i.indexprs IS NOT NULL,
        'where', pg_catalog.pg_get_expr(i.indpred, i.indrelid),
        'definition', pg_catalog.pg_get_indexdef(i.indexrelid)) ORDER BY ic.relname), '[]')
      FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid JOIN pg_catalog.pg_am am ON am.oid = ic.relam
      WHERE i.indrelid = t.oid)) ORDER BY t.nspname, t.relname), '[]') FROM t),
  'enums', (SELECT coalesce(json_agg(json_build_object('schema', n.nspname, 'name', ty.typname,
      'values', (SELECT json_agg(e.enumlabel ORDER BY e.enumsortorder) FROM pg_catalog.pg_enum e WHERE e.enumtypid = ty.oid))
      ORDER BY n.nspname, ty.typname), '[]')
    FROM pg_catalog.pg_type ty JOIN pg_catalog.pg_namespace n ON n.oid = ty.typnamespace WHERE ty.typtype = 'e')) AS catalog`;

async function pgCatalog(seq) {
  const rows = await seq.query(PG_CATALOG, { type: "SELECT" });
  return rows[0].catalog;
}

// sqliteCatalog reads the SQLite file Sequelize wrote, through node:sqlite
// (sqlite_master and the pragmas).
async function sqliteCatalog(dbPath) {
  const { DatabaseSync } = require("node:sqlite");
  const db = new DatabaseSync(dbPath, { readOnly: true });
  const q = (n) => '"' + n.replaceAll('"', '""') + '"';
  const tables = [];
  for (const { name, sql } of db.prepare("SELECT name, sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\' ORDER BY name").all()) {
    const columns = [];
    const pk = [];
    for (const r of db.prepare(`PRAGMA table_xinfo(${q(name)})`).all()) {
      columns.push({ name: r.name, type: r.type, not_null: r.notnull === 1, default: r.dflt_value, hidden: r.hidden });
      if (r.pk) pk.push([r.pk, r.name]);
    }
    const indexes = [];
    for (const r of db.prepare(`PRAGMA index_list(${q(name)})`).all()) {
      const cols = db.prepare(`PRAGMA index_info(${q(r.name)})`).all().map((c) => c.name ?? "");
      const row = db.prepare("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?").get(r.name);
      indexes.push({ name: r.name, unique: r.unique === 1, origin: r.origin, partial: r.partial === 1, columns: cols, sql: row ? row.sql : null });
    }
    const fks = new Map();
    for (const r of db.prepare(`PRAGMA foreign_key_list(${q(name)})`).all()) {
      if (!fks.has(r.id)) fks.set(r.id, { ref_table: r.table, columns: [], ref_columns: [], on_update: r.on_update, on_delete: r.on_delete });
      fks.get(r.id).columns.push(r.from);
      fks.get(r.id).ref_columns.push(r.to);
    }
    tables.push({
      schema: null,
      name,
      sql,
      columns,
      primary_key: pk.sort((a, b) => a[0] - b[0]).map((p) => p[1]),
      indexes: indexes.sort((a, b) => (a.name < b.name ? -1 : 1)),
      foreign_keys: [...fks.keys()].sort((a, b) => a - b).map((k) => fks.get(k)),
    });
  }
  db.close();
  return { tables, enums: [] };
}

// PRESETS are the runs of the fixture (testdata/fixtures/sequelize-models:
// every package but decorators/, legacy/ and mapped/) and of the corpora
// (opencollective-api and Gladys at their pins). copy lists the
// directories (relative to the repository) copied into the run's tree.
const PRESETS = {
  fixture: [
    { name: "pg", group: "pg/src/db.ts", dialect: "postgres", copy: ["pg"], entries: ["pg/src/models/index.ts"], allow: ["pg/"] },
    { name: "pgdefine", group: "pgdefine/src/db.js", dialect: "postgres", copy: ["pgdefine"], entries: ["pgdefine/src/db.js", "pgdefine/src/reaction.js"], allow: ["pgdefine/"] },
    { name: "sqlite", group: "sqlite/models/index.js", dialect: "sqlite", copy: ["sqlite"], entries: ["sqlite/models/index.js"], allow: ["sqlite/"] },
    { name: "pgassoc", group: "pgassoc/src/db.js", dialect: "postgres", copy: ["pgassoc"], entries: ["pgassoc/src/models.js"], allow: ["pgassoc/"] },
    { name: "pgcycle", group: "pgcycle/src/db.js", dialect: "postgres", copy: ["pgcycle"], entries: ["pgcycle/src/models.js"], allow: ["pgcycle/"] },
    // pgfail/src/models.js throws by design (a targetKey the target lacks;
    // src/index.js requires it, then late.js, which never loads); the
    // models defined before the throw are synced.
    { name: "pgfail", group: "pgfail/src/db.js", dialect: "postgres", copy: ["pgfail"], entries: ["pgfail/src/index.js"], allow: ["pgfail/"], allowLoadError: true },
    { name: "pgrefuse", group: "pgrefuse/src/db.js", dialect: "postgres", copy: ["pgrefuse"], entries: ["pgrefuse/src/models.js"], allow: ["pgrefuse/"] },
    // node-config is not installed: `config` is the package's
    // config/default.json; the dialect comes from the single driver (SQ-V4).
    { name: "pgconfig", group: "pgconfig/src/lib/sequelize.ts", dialect: "postgres", copy: ["pgconfig"], entries: ["pgconfig/src/models/index.ts"], allow: ["pgconfig/src/"], shims: { config: "json:pgconfig/config/default.json" } },
    // The sequelize-cli forms (task Revision 3): the
    // config entry through require(__dirname + ...)[env], and the loops.
    { name: "cli", group: "cli/models/index.js", dialect: "postgres", copy: ["cli"], entries: ["cli/models/index.js"], allow: ["cli/"], env: { NODE_ENV: "development" } },
    { name: "cliclass", group: "cliclass/src/db.js", dialect: "postgres", copy: ["cliclass"], entries: ["cliclass/src/models/index.js"], allow: ["cliclass/"] },
  ],
  // Every module these runs load for real was read before the first run
  // (the import closure restricted to the allow list, and every
  // module-level statement that is not a declaration: model definitions,
  // associations, Temporal(...) calls, prototype assignments and constant
  // tables only). Everything else is a stub.
  corpus: [
    // opencollective-api: the instance module server/lib/sequelize.ts
    // (node-config, the logger, pg defaults, query hooks) is replaced by an
    // instance of its literal dialect with no global define (open point O1,
    // SQ-V5); oc_nanoid(text) is created by a migration, so it gets a stub
    // of the same signature (the investigation).
    {
      name: "opencollective-api",
      repo: "opencollective-api",
      group: "server/lib/sequelize.ts",
      dialect: "postgres",
      copy: ["server/models", "server/constants", "server/lib", "server/types", "server/graphql/v2/enum"],
      entries: ["server/models/index.ts"],
      allow: ["server/models/", "server/constants", "server/lib/permalink/", "server/types/", "server/graphql/v2/enum/", "server/lib/sequelize-temporal"],
      packages: ["sequelize", "lodash", "moment", "moment-timezone", "uuid", "validator"],
      shims: { "server/lib/sequelize.ts": "instance" },
      instance: { dialect: "postgres" },
      functions: ["CREATE FUNCTION oc_nanoid(prefix text) RETURNS text LANGUAGE sql AS $$ SELECT prefix $$"],
    },
    // Gladys: the production entry of config/config.js through
    // utils/getConfig.js (open point O2); DuckDB, umzug, tracer, bcrypt,
    // dayjs, joi and fs are stubs.
    {
      name: "gladys",
      repo: "gladys",
      group: "server/models/index.js",
      dialect: "sqlite",
      copy: ["server/models", "server/config", "server/utils", "server/lib/external-integration"],
      entries: ["server/models/index.js"],
      allow: ["server/models/", "server/config/", "server/utils/", "server/lib/external-integration/constants"],
      packages: ["sequelize"],
      env: { NODE_ENV: "production" },
    },
  ],
};

// specs writes one spec per run of a preset under SCRATCH/run-<n>/ (its
// tree a copy of the directories the run needs) and prints
// "<n>|<dialect>|<spec>" lines; postgres runs use the database
// sequelize_oracle_<n> on the server of PG.
function specs(preset, root, scratch, kit, pg) {
  const runs = PRESETS[preset];
  if (!runs) die(`no preset ${preset}`);
  runs.forEach((r, i) => {
    const n = i + 1;
    const dir = path.join(scratch, `run-${n}`);
    fs.rmSync(dir, { recursive: true, force: true });
    const tree = path.join(dir, "tree");
    const repoRoot = path.join(root, r.repo || ".");
    for (const c of r.copy) fs.cpSync(path.join(repoRoot, c), path.join(tree, c), { recursive: true, filter: (src) => !src.split(path.sep).includes(".git") });
    const db = {};
    if (r.dialect === "postgres") {
      const u = new URL(pg);
      u.pathname = `/sequelize_oracle_${n}`;
      db.url = u.toString();
    } else {
      db.file = path.join(dir, "oracle.sqlite");
    }
    const spec = { ...r, repo: r.repo || ".", tree, kit, db };
    const file = path.join(dir, "spec.json");
    writeJSON(file, spec);
    process.stdout.write(`${n}|${r.dialect}|${file}\n`);
  });
}

function merge(out, files) {
  writeJSON(out, { runs: files.map(readJSON) });
}

const [cmd, ...args] = process.argv.slice(2);
switch (cmd) {
  case "run":
    if (args.length !== 2) die("run SPEC OUT");
    run(...args).catch((e) => die(e && e.stack));
    break;
  case "specs":
    if (args.length !== 5) die("specs PRESET ROOT SCRATCH KIT PG");
    specs(...args);
    break;
  case "merge":
    if (args.length < 2) die("merge OUT RUN...");
    merge(args[0], args.slice(1));
    break;
  default:
    die("usage: run | merge");
}
