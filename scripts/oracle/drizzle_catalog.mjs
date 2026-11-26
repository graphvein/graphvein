// Helper of drizzle_catalog.sh: the Drizzle oracle of T018g2
// (ADR 0059). Never run by CI. Own code; needs Node 22.13 or later
// (node:sqlite, fs.globSync).
//
// Subcommands:
//
//   prepare REPO PKG SCHEMAS DIALECT CASING DROPS STUBS DEST
//       Copy the schema modules of one run, and every module their runtime
//       imports reach, from the repository REPO into DEST (the same
//       repository-relative paths), and write DEST/PKG/oracle.config.ts, a
//       config with the run's literal dialect, casing and absolute schema
//       paths (the project's own config is never loaded: kaneo's imports
//       dotenv). SCHEMAS are the config's schema patterns (comma separated,
//       relative to PKG, the working directory). DROPS
//       ("file:Decl;file:Decl.prop", files relative to REPO) are top-level
//       declarations or object entries removed from the copy: forms that
//       stop drizzle-kit loading the schema or Postgres applying it, none
//       of them claimed. STUBS ("file=name,name;...") are modules replaced
//       by stubs whose exports throw when called (imports only called inside
//       functions). PKG/tsconfig.json's compilerOptions.paths are kept in a
//       scratch tsconfig so drizzle-kit resolves the same aliases. A copied
//       module with a top-level call statement or await is refused: every
//       module is read before it runs (a side effect at load would run
//       here). Prints the copied modules and the packages they import.
//   sqlite SQL DB OUT
//       Apply drizzle-kit's SQL to a new SQLite file and write its catalog.
//   record OUT REPO CONFIG DIALECT KIT ORM COMMAND DROPS SQL CATALOG
//       Write one run record.
//   merge OUT RUN...
//       Merge run records into the dump TestDrizzleOracle reads.
//   normalize-sql DUMP REQUESTS RUN
//       Print the psql script that normalises run RUN's claimed expression
//       texts through the run's database (TestDrizzleOracle writes the
//       requests).
//   normalized DUMP RUN RESULTS
//       Store psql's results of normalize-sql in the dump.

import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

function die(msg) {
  process.stderr.write(`drizzle_catalog.mjs: ${msg}\n`);
  process.exit(1);
}

function readJSON(p) {
  return JSON.parse(fs.readFileSync(p, "utf8"));
}

function writeJSON(p, v) {
  fs.writeFileSync(p, JSON.stringify(v, null, 1) + "\n");
}

// stripJSONComments removes // and /* */ comments and trailing commas
// outside strings (tsconfig.json is JSON with comments).
function stripJSONComments(s) {
  let out = "";
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === '"') {
      let j = i + 1;
      while (j < s.length && s[j] !== '"') j += s[j] === "\\" ? 2 : 1;
      out += s.slice(i, j + 1);
      i = j;
    } else if (c === "/" && s[i + 1] === "/") {
      while (i < s.length && s[i] !== "\n") i++;
      out += "\n";
    } else if (c === "/" && s[i + 1] === "*") {
      i = s.indexOf("*/", i + 2) + 1;
      if (i === 0) break;
    } else {
      out += c;
    }
  }
  return out.replace(/,(\s*[}\]])/g, "$1");
}

// scanStatement returns the index just past the statement that starts at
// start: the first ";" outside brackets, strings, template literals and
// comments.
function scanStatement(text, start) {
  const stack = [];
  for (let i = start; i < text.length; i++) {
    const c = text[i];
    const top = stack[stack.length - 1];
    if (top === "`") {
      if (c === "\\") i++;
      else if (c === "`") stack.pop();
      else if (c === "$" && text[i + 1] === "{") {
        stack.push("${");
        i++;
      }
      continue;
    }
    if (c === "'" || c === '"') {
      let j = i + 1;
      while (j < text.length && text[j] !== c) j += text[j] === "\\" ? 2 : 1;
      i = j;
    } else if (c === "/" && text[i + 1] === "/") {
      while (i < text.length && text[i] !== "\n") i++;
    } else if (c === "/" && text[i + 1] === "*") {
      i = text.indexOf("*/", i + 2) + 1;
    } else if (c === "`") {
      stack.push("`");
    } else if (c === "(" || c === "[" || c === "{") {
      stack.push(c);
    } else if (c === ")" || c === "]" || c === "}") {
      stack.pop();
    } else if (c === ";" && stack.length === 0) {
      return i + 1;
    }
  }
  return -1;
}

// applyDrops removes the declarations and entries drops names for file.
function applyDrops(text, file, drops) {
  for (const d of drops) {
    const dot = d.indexOf(".");
    const decl = dot < 0 ? d : d.slice(0, dot);
    const prop = dot < 0 ? undefined : d.slice(dot + 1);
    const re = new RegExp(`^(export\\s+)?(const|let|var)\\s+${decl}\\b`, "m");
    const m = re.exec(text);
    if (!m) die(`drop ${file}:${d}: no top-level declaration ${decl}`);
    const end = scanStatement(text, m.index);
    if (end < 0) die(`drop ${file}:${d}: declaration ${decl} does not end with ";"`);
    if (prop === undefined) {
      text = text.slice(0, m.index) + `// dropped by the oracle: ${decl}\n` + text.slice(end);
      continue;
    }
    const body = text.slice(m.index, end);
    const lines = body.split("\n");
    // An entry is "key: value" or a spread "...expr".
    const entry = (l) => {
      const t = l.trim();
      return prop.startsWith("...") ? t === prop + "," || t === prop : new RegExp(`^${prop}\\s*:`).test(t);
    };
    const hits = lines.map((l, i) => [l, i]).filter(([l]) => entry(l));
    if (hits.length !== 1) die(`drop ${file}:${d}: ${hits.length} lines start with ${prop}:`);
    lines.splice(hits[0][1], 1);
    text = text.slice(0, m.index) + lines.join("\n") + text.slice(end);
  }
  return text;
}

// importSpecifiers lists the module specifiers a TypeScript module loads at
// run time: import and export-from declarations that are not type-only
// (esbuild drops an import whose named bindings are all types), and bare
// side-effect imports. Dynamic import() and require() are refused.
function importSpecifiers(text, file) {
  const out = [];
  const re = /^\s*(import|export)\s+(type\s+)?([\s\S]*?)\bfrom\s*["']([^"']+)["']|^\s*import\s*["']([^"']+)["']/gm;
  let m;
  while ((m = re.exec(text))) {
    if (m[5]) {
      out.push(m[5]);
      continue;
    }
    if (m[2]) continue;
    const clause = m[3].trim();
    const named = /^\{([\s\S]*)\}$/.exec(clause);
    if (named && named[1].split(",").every((x) => !x.trim() || /^type\s/.test(x.trim()))) {
      if (m[1] === "import") continue;
    }
    out.push(m[4]);
  }
  if (/\b(require\s*\(|import\s*\()/.test(text.replace(/\/\/.*$/gm, ""))) die(`${file}: dynamic import or require is not followed; review it`);
  return out;
}

// topLevelCall finds a top-level statement that runs code at load other
// than a declaration: a call statement or await at column 0.
function topLevelCall(text) {
  const m = /^(await\b|[A-Za-z_$][\w$.]*\s*\()/m.exec(text);
  return m ? m[0] : "";
}

function prepare(repo, pkg, schemas, dialect, casing, drops, stubs, dest) {
  repo = path.resolve(repo);
  dest = path.resolve(dest);
  const pkgDir = path.join(repo, pkg);
  const dropMap = {};
  for (const d of drops.split(";").filter(Boolean)) {
    const i = d.lastIndexOf(":");
    (dropMap[d.slice(0, i)] ??= []).push(d.slice(i + 1));
  }
  const stubMap = {};
  for (const s of stubs.split(";").filter(Boolean)) {
    const [f, names] = s.split("=");
    stubMap[f] = names.split(",").filter(Boolean);
  }
  // tsconfig paths of the working directory's tsconfig.json.
  let aliases = [];
  let tsPaths = null;
  const tsconfig = path.join(pkgDir, "tsconfig.json");
  if (fs.existsSync(tsconfig)) {
    const opts = JSON.parse(stripJSONComments(fs.readFileSync(tsconfig, "utf8"))).compilerOptions ?? {};
    if (opts.paths) {
      tsPaths = { baseUrl: opts.baseUrl ?? ".", paths: opts.paths };
      const base = path.join(pkgDir, tsPaths.baseUrl);
      for (const [k, v] of Object.entries(opts.paths)) aliases.push([k.replace(/\*$/, ""), path.join(base, v[0].replace(/\*$/, ""))]);
    }
  }
  const resolveFile = (p) => {
    for (const c of [p, p + ".ts", p + ".tsx", p + ".mts", p + ".js", path.join(p, "index.ts"), path.join(p, "index.js")]) {
      if (fs.existsSync(c) && fs.statSync(c).isFile()) return c;
    }
    return null;
  };
  // A matched directory contributes the files directly in it with an
  // extension drizzle-kit loads (prepareFilenames), as
  // discovery reads it.
  const loadable = new Set([".ts", ".js", ".mjs", ".cjs", ".mts", ".cts"]);
  const files = [];
  for (const s of schemas.split(",")) {
    const matched = [];
    for (const f of fs.globSync(s, { cwd: pkgDir }).map((f) => path.join(pkgDir, f))) {
      if (!fs.statSync(f).isDirectory()) {
        matched.push(f);
        continue;
      }
      for (const e of fs.readdirSync(f, { withFileTypes: true })) {
        if (e.isFile() && loadable.has(path.extname(e.name))) matched.push(path.join(f, e.name));
      }
    }
    matched.sort();
    if (matched.length === 0) die(`schema ${s} matches no file in ${pkgDir}`);
    files.push(...matched);
  }
  const seen = new Set();
  const external = new Map();
  const todo = [...files];
  while (todo.length) {
    const f = todo.shift();
    if (seen.has(f)) continue;
    seen.add(f);
    const rel = path.relative(repo, f);
    if (rel.startsWith("..")) die(`${f} is outside the repository`);
    const target = path.join(dest, rel);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    if (stubMap[rel]) {
      const body = stubMap[rel].map((n) => `export const ${n} = () => {\n  throw new Error("oracle stub: ${rel} ${n}");\n};\n`).join("");
      fs.writeFileSync(target, `// Stub written by the Drizzle oracle: ${rel} is only called inside functions.\n${body}`);
      continue;
    }
    let text = fs.readFileSync(f, "utf8");
    if (dropMap[rel]) {
      text = applyDrops(text, rel, dropMap[rel]);
      delete dropMap[rel];
    }
    const call = topLevelCall(text);
    if (call) die(`${rel}: top-level statement ${JSON.stringify(call)} runs at load; stub or drop it after reading it`);
    fs.writeFileSync(target, text);
    for (const spec of importSpecifiers(text, rel)) {
      let p = null;
      if (spec.startsWith(".")) p = path.resolve(path.dirname(f), spec);
      else {
        const a = aliases.find(([k]) => spec.startsWith(k));
        if (a) p = path.join(a[1], spec.slice(a[0].length));
      }
      if (p === null) {
        const name = spec.startsWith("@") ? spec.split("/").slice(0, 2).join("/") : spec.split("/")[0];
        if (!name.startsWith("node:")) {
          if (!external.has(name)) external.set(name, new Set());
          external.get(name).add(rel);
        }
        continue;
      }
      const r = resolveFile(p);
      if (!r) die(`${rel}: import ${spec} resolves to no file`);
      todo.push(r);
    }
  }
  const left = Object.keys(dropMap);
  if (left.length) die(`drops name files no schema module reaches: ${left.join(", ")}`);
  for (const f of Object.keys(stubMap)) if (!seen.has(path.join(repo, f))) die(`stub ${f} is not reached`);
  // Every external package must resolve from the scratch install, never
  // from the corpus checkout.
  const req = createRequire(path.join(dest, "package.json"));
  for (const name of external.keys()) {
    try {
      req.resolve(name, { paths: [dest] });
    } catch {
      die(`package ${name} (imported by ${[...external.get(name)].join(", ")}) is not installed in the scratch kit`);
    }
  }
  const destPkg = path.join(dest, pkg);
  fs.mkdirSync(destPkg, { recursive: true });
  if (tsPaths) writeJSON(path.join(destPkg, "tsconfig.json"), { compilerOptions: tsPaths });
  const schema = files.map((f) => path.join(dest, path.relative(repo, f)));
  const config = { dialect, schema, out: path.join(destPkg, "oracle-out") };
  if (casing !== "-") config.casing = casing;
  fs.writeFileSync(path.join(destPkg, "oracle.config.ts"), `export default ${JSON.stringify(config, null, 2)};\n`);
  for (const f of [...seen].sort()) process.stdout.write(`module ${path.relative(repo, f)}${stubMap[path.relative(repo, f)] ? " (stub)" : ""}\n`);
  for (const [name, users] of [...external].sort()) process.stdout.write(`package ${name} (${users.size} modules)\n`);
}

async function sqliteCatalog(sqlPath, dbPath, outPath) {
  const { DatabaseSync } = await import("node:sqlite");
  fs.rmSync(dbPath, { force: true });
  const db = new DatabaseSync(dbPath);
  db.exec(fs.readFileSync(sqlPath, "utf8"));
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
      const cols = db.prepare(`PRAGMA index_xinfo(${q(r.name)})`).all().filter((c) => c.key).map((c) => c.name ?? "");
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
  writeJSON(outPath, { tables, enums: [] });
}

function record(out, repo, config, dialect, kit, orm, command, drops, sqlPath, catalogPath) {
  writeJSON(out, {
    dir: repo,
    config,
    dialect,
    drizzle_kit: kit,
    drizzle_orm: orm,
    command,
    dropped: drops.split(";").filter(Boolean),
    sql: fs.readFileSync(sqlPath, "utf8"),
    catalog: catalogPath === "-" ? null : readJSON(catalogPath),
  });
}

function merge(out, paths) {
  writeJSON(out, { runs: paths.map(readJSON) });
}

// pgQuote quotes an identifier.
const pgQuote = (n) => '"' + n.replaceAll('"', '""') + '"';

// normalizeSQL writes one transaction per request: the claimed text is put
// into the run's database the way drizzle-kit's DDL puts it, read back
// through pg_get_expr / pg_get_constraintdef / pg_get_indexdef, and rolled
// back. A text Postgres rejects prints nothing (the comparison then fails).
function normalizeSQL(dumpPath, reqPath, run) {
  const reqs = readJSON(reqPath).filter((r) => r.run === Number(run));
  const lines = ["\\set ON_ERROR_STOP 0", "SET client_min_messages = error;"];
  reqs.forEach((r, i) => {
    const t = (r.schema ? pgQuote(r.schema) + "." : "") + pgQuote(r.table);
    const rel = `'${t.replaceAll("'", "''")}'::regclass`;
    let stmt, read;
    switch (r.kind) {
      case "default":
        stmt = `ALTER TABLE ${t} ALTER COLUMN ${pgQuote(r.column)} SET DEFAULT ${r.text}`;
        read = `SELECT pg_get_expr(d.adbin, d.adrelid) FROM pg_attrdef d JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum WHERE d.adrelid = ${rel} AND a.attname = '${r.column.replaceAll("'", "''")}'`;
        break;
      case "generated":
        stmt = `ALTER TABLE ${t} ADD COLUMN gv_oracle_norm ${r.type} GENERATED ALWAYS AS (${r.text}) STORED`;
        read = `SELECT pg_get_expr(d.adbin, d.adrelid) FROM pg_attrdef d JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum WHERE d.adrelid = ${rel} AND a.attname = 'gv_oracle_norm'`;
        break;
      case "check":
        stmt = `ALTER TABLE ${t} ADD CONSTRAINT gv_oracle_norm CHECK (${r.text})`;
        read = `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = ${rel} AND conname = 'gv_oracle_norm'`;
        break;
      case "where":
        stmt = `CREATE INDEX gv_oracle_norm ON ${t} (${pgQuote(r.column)}) WHERE ${r.text}`;
        read = `SELECT pg_get_expr(i.indpred, i.indrelid) FROM pg_index i WHERE i.indexrelid = 'gv_oracle_norm'::regclass`;
        break;
      case "expression":
        stmt = `CREATE INDEX gv_oracle_norm ON ${t} ((${r.text}))`;
        read = `SELECT pg_get_indexdef('gv_oracle_norm'::regclass, 1, true)`;
        break;
      default:
        die(`request kind ${r.kind}`);
    }
    // The statement runs with the table's schema first, so unqualified
    // names in the claimed text resolve as drizzle-kit's DDL does; the
    // text is read back under the default search_path, as the catalog
    // dump reads it, so a type of a non-public schema (an enum default:
    // 'a'::app.role) is qualified on both sides.
    if (r.schema) lines.push(`SET search_path = ${pgQuote(r.schema)}, public;`);
    else lines.push("RESET search_path;");
    lines.push("BEGIN;", stmt + ";", "RESET search_path;", `SELECT '${i}' || E'\\t' || to_json((${read}))::text;`, "ROLLBACK;");
  });
  process.stdout.write(lines.join("\n") + "\n");
}

function normalized(dumpPath, reqPath, run, resultsPath) {
  const reqs = readJSON(reqPath).filter((r) => r.run === Number(run));
  const dump = readJSON(dumpPath);
  const target = dump.runs[Number(run)];
  target.normalized ??= {};
  const got = {};
  for (const line of fs.readFileSync(resultsPath, "utf8").split("\n")) {
    const tab = line.indexOf("\t");
    if (tab > 0 && /^\d+$/.test(line.slice(0, tab))) got[line.slice(0, tab)] = JSON.parse(line.slice(tab + 1));
  }
  reqs.forEach((r, i) => {
    target.normalized[r.key] = got[i] ?? null;
  });
  writeJSON(dumpPath, dump);
}

const [cmd, ...args] = process.argv.slice(2);
switch (cmd) {
  case "prepare":
    if (args.length !== 8) die("prepare REPO PKG SCHEMAS DIALECT CASING DROPS STUBS DEST");
    prepare(...args);
    break;
  case "sqlite":
    if (args.length !== 3) die("sqlite SQL DB OUT");
    await sqliteCatalog(...args);
    break;
  case "record":
    if (args.length !== 10) die("record OUT REPO CONFIG DIALECT KIT ORM COMMAND DROPS SQL CATALOG");
    record(...args);
    break;
  case "merge":
    if (args.length < 2) die("merge OUT RUN...");
    merge(args[0], args.slice(1));
    break;
  case "normalize-sql":
    if (args.length !== 3) die("normalize-sql DUMP REQUESTS RUN");
    normalizeSQL(...args);
    break;
  case "normalized":
    if (args.length !== 4) die("normalized DUMP REQUESTS RUN RESULTS");
    normalized(...args);
    break;
  default:
    die("usage: prepare | sqlite | record | merge | normalize-sql | normalized");
}
