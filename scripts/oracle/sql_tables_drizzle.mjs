// Helper of sql_tables.sh: the Drizzle toSQL() fixture oracle of T018h
// (ADR 0060, Revision 1 O6, Revision 4). Never run by CI. Own code; needs
// Node 24 (built-in TypeScript type stripping, module.registerHooks).
//
//   node sql_tables_drizzle.mjs TREE REQUEST...
//
// TREE is the scratch copy of the group's schema modules (drizzle_catalog.mjs
// prepare wrote it under the scratch drizzle-kit install, so drizzle-orm
// resolves to the pinned package). Each REQUEST (site-N.request.json,
// written by TestSQLTablesOracle) holds a claimed CT-Z1/CT-Z2 builder's
// source text and the bindings it may reference. The text is evaluated
// with its receiver bound to drizzle-orm's pg-proxy database (its callback
// never runs: nothing connects), table consts bound to the copied schema
// modules' exports, drizzle-orm imports to the package, other bindings to
// their source text, and anything else to a placeholder value. The
// builder's toSQL() SQL is written next to the request as site-N.sql.
// This file must sit in the scratch install directory so that its
// drizzle-orm import resolves there.

import fs from "node:fs";
import path from "node:path";
import { registerHooks } from "node:module";
import { pathToFileURL } from "node:url";

function die(msg) {
  process.stderr.write(`sql_tables_drizzle.mjs: ${msg}\n`);
  process.exit(1);
}

// Extensionless relative imports in the copied modules (bundler style)
// resolve to their .ts file, as drizzle-kit's loader resolves them.
registerHooks({
  resolve(specifier, context, next) {
    try {
      return next(specifier, context);
    } catch (err) {
      if (!specifier.startsWith(".")) throw err;
      for (const ext of [".ts", ".tsx", ".mts", "/index.ts"]) {
        try {
          return next(specifier + ext, context);
        } catch {}
      }
      throw err;
    }
  },
});

const [tree, ...requests] = process.argv.slice(2);
if (!tree || requests.length === 0) die("usage: sql_tables_drizzle.mjs TREE REQUEST...");

const { drizzle } = await import("drizzle-orm/pg-proxy");
const db = drizzle(async () => {
  throw new Error("the oracle never runs a query");
});

const modules = new Map();
async function load(spec) {
  if (!modules.has(spec)) modules.set(spec, await import(spec));
  return modules.get(spec);
}
const treeFile = (file) => pathToFileURL(path.join(tree, file)).href;

for (const file of requests) {
  const req = JSON.parse(fs.readFileSync(file, "utf8"));
  const defs = new Map();
  for (const b of req.bindings) {
    switch (b.kind) {
      case "db":
        defs.set(b.name, () => db);
        break;
      case "table": {
        const m = await load(treeFile(b.file));
        if (!(b.export in m)) die(`${req.key}: ${b.file} exports no ${b.export}`);
        defs.set(b.name, () => m[b.export]);
        break;
      }
      case "object": {
        const o = {};
        for (const p of b.properties ?? []) o[p.name] = (await load(treeFile(p.file)))[p.export];
        defs.set(b.name, () => o);
        break;
      }
      case "namespace": {
        const m = await load(treeFile(b.file));
        defs.set(b.name, () => m);
        break;
      }
      case "external": {
        const m = await load(b.module);
        defs.set(b.name, () => (b.export === "*" ? m : m[b.export]));
        break;
      }
      case "expr":
        defs.set(b.name, () => evaluate(b.text));
        break;
      default:
        defs.set(b.name, () => "x");
    }
  }
  const cache = new Map();
  const scope = new Proxy(
    {},
    {
      has: (_, k) => typeof k === "string" && defs.has(k),
      get: (_, k) => {
        if (typeof k !== "string" || !defs.has(k)) return undefined;
        if (!cache.has(k)) cache.set(k, defs.get(k)());
        return cache.get(k);
      },
    },
  );
  // Sloppy-mode Function so that `with` scopes the bindings.
  function evaluate(text) {
    return new Function("scope", `with (scope) { return (${text}); }`)(scope);
  }
  let value;
  try {
    value = evaluate(req.text);
  } catch (err) {
    die(`${req.key}: evaluating ${JSON.stringify(req.text)}: ${err.stack ?? err}`);
  }
  let sql;
  if (value && typeof value.toSQL === "function") {
    sql = value.toSQL().sql;
  } else if (value && typeof value.getSQL === "function") {
    sql = db.dialect.sqlToQuery(value.getSQL()).sql;
  } else {
    die(`${req.key}: ${JSON.stringify(req.text)} is no query builder`);
  }
  fs.writeFileSync(file.replace(/\.request\.json$/, ".sql"), sql + "\n");
}
