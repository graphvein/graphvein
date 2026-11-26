// Next.js App Router route handler and server action shape oracle (T029
// SH-N). Compares the shapes Graphvein stores for the fixture
// testdata/fixtures/nextjs-shape with the fixture application served by a
// pinned Next.js release (`next start` on 127.0.0.1), with zod's
// toJSONSchema of the fixture schemas and with the TypeScript checker.
// Opt-in, never run by CI; nextjs_shape.sh drives it.
//
// Usage: node oracle.mjs --snapshot snap.ndjson --app <built app dir>
//          --base http://127.0.0.1:PORT --cases cases.json --out result.json
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const argv = process.argv.slice(2);
const arg = (name) => {
  const i = argv.indexOf(name);
  return i >= 0 ? argv[i + 1] : undefined;
};
const snapshotFile = arg('--snapshot');
const appDir = arg('--app');
const base = arg('--base');
const cases = JSON.parse(fs.readFileSync(arg('--cases'), 'utf8'));
const outFile = arg('--out');
const label = arg('--label') || '';
const require = createRequire(path.join(appDir, 'package.json'));

const counts = {};
const disagreements = [];
const notes = [];
const count = (k) => { counts[k] = (counts[k] || 0) + 1; };
const disagree = (what) => { disagreements.push(what); };

// --- the stored shapes ----------------------------------------------------
const routes = new Map();
const actions = new Map();
for (const line of fs.readFileSync(snapshotFile, 'utf8').split('\n')) {
  if (!line.trim()) continue;
  const o = JSON.parse(line);
  if (o.type !== 'node' || !o.attributes || !o.attributes.shape) continue;
  if (o.kind === 'route' && o.attributes.framework === 'nextjs' && o.file.startsWith('web/')) routes.set(o.qualified_name, o.attributes.shape);
  if (o.kind === 'server_action') actions.set(o.attributes.action, o.attributes.shape);
}

// --- HTTP --------------------------------------------------------------------
async function send(method, req) {
  const headers = { ...(req.headers || {}) };
  if (req.cookies) headers.cookie = req.cookies;
  let body;
  if (req.json !== undefined) {
    headers['content-type'] = 'application/json';
    body = req.json === null ? '' : JSON.stringify(req.json);
  } else if (req.form) {
    const fd = new FormData();
    for (const [k, v] of Object.entries(req.form)) fd.append(k, v);
    body = fd;
  } else if (req.text !== undefined) {
    headers['content-type'] = 'text/plain';
    body = req.text;
  }
  const res = await fetch(base + req.path, { method, headers, body, redirect: 'manual' });
  const text = method === 'HEAD' ? '' : await res.text();
  return { status: String(res.status), contentType: res.headers.get('content-type'), text, headers: res.headers };
}

// --- schema helpers ------------------------------------------------------------
function modelOf(shape, ref) {
  return (shape.models || []).find((m) => m.name === ref);
}

// matches reports whether a JSON value fits a stored schema (types only;
// presence of fields of kind read_in_handler is checked separately).
function fits(shape, schema, value, kind) {
  if (!schema) return true;
  if (schema.nullable && value === null) return true;
  switch (schema.type) {
    case 'unknown': return true;
    case 'null': return value === null;
    case 'string': return typeof value === 'string';
    case 'number': return typeof value === 'number';
    case 'integer': return Number.isInteger(value);
    case 'boolean': return typeof value === 'boolean';
    case 'enum': return schema.enum.includes(value);
    case 'array': return Array.isArray(value) && value.every((v) => fits(shape, schema.items, v, kind));
    case 'ref': {
      const m = modelOf(shape, schema.ref);
      return !!m && fits(shape, m.schema, value, kind);
    }
    case 'object': {
      if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
      for (const f of schema.fields || []) {
        if (!(f.name in value)) {
          if (f.presence === 'required') return false;
          continue;
        }
        if (!fits(shape, f.schema, value[f.name], kind)) return false;
      }
      if (schema.additional === 'forbidden') {
        const names = new Set((schema.fields || []).map((f) => f.name));
        if (Object.keys(value).some((k) => !names.has(k))) return false;
      }
      return true;
    }
  }
  return true;
}

// wrong is a value of another kind than the schema accepts (one zod would
// not convert), or undefined when none is derived.
function wrong(schema) {
  switch (schema.type) {
    case 'string': return 123;
    case 'number': case 'integer': return '5';
    case 'boolean': return 'true';
    case 'array': return 'x';
    case 'object': return 'x';
  }
  return undefined;
}

// --- 1. served routes ------------------------------------------------------------
async function routesCheck() {
  for (const [name, shape] of routes) {
    const [method] = name.split(' ');
    const reqs = cases.routes[name];
    if (!reqs) {
      disagree(`${name}: no requests in cases.json`);
      continue;
    }
    const observed = [];
    for (const r of reqs) {
      const got = await send(method, r);
      observed.push({ req: r, got });
      count('requests');
      // The framework behaviour a refusal rests on (a relative redirect,
      // force-static, a caught notFound(), ...): the observed status.
      if (r.expect !== undefined) {
        const want = typeof r.expect === 'string' ? r.expect : r.expect[label];
        if (want !== undefined) {
          count('expected_statuses');
          if (got.status !== want) disagree(`${name} ${r.path}: status ${got.status}, the case expects ${want}`);
        }
      }
      if (r.expect_header) {
        // The expected value, or an object of values per release.
        const [header, value] = r.expect_header;
        const want = typeof value === 'string' ? value : value[label];
        if (want !== undefined) {
          count('expected_headers');
          if (got.headers.get(header) !== want) disagree(`${name} ${r.path}: header ${header} is ${got.headers.get(header)}, the case expects ${want}`);
        }
      }
    }
    // Every claimed response is matched by some observation at its status
    // (content type, body), and every observation at a claimed status
    // matches some claimed response at that status.
    const claimed = shape.responses.items.filter((i) => i.provenance.kind === 'read_in_handler' || i.provenance.kind === 'returned');
    const mismatch = (item, o) => {
      const why = [];
      if (item.content_type !== null && o.got.contentType !== item.content_type) why.push(`content type ${JSON.stringify(o.got.contentType)}, claimed ${item.content_type}`);
      if (item.schema && item.schema.type === 'null') {
        if (o.got.text !== '') why.push(`body ${JSON.stringify(o.got.text.slice(0, 60))}, claimed empty`);
        if (o.got.contentType !== null && item.content_type === null) why.push(`content type ${o.got.contentType} on a claimed empty body without one`);
      }
      if (item.schema && item.schema.type === 'string' && o.got.text === '') why.push('empty body, claimed a string');
      if (item.schema && (item.schema.type === 'object' || item.schema.type === 'ref')) {
        let value;
        try { value = JSON.parse(o.got.text); } catch { why.push('body is not JSON'); return why; }
        if (item.provenance.kind === 'read_in_handler' && item.schema.type === 'object') {
          for (const f of item.schema.fields || []) if (!(f.name in Object(value))) why.push(`literal field ${f.name} missing`);
        } else if (!fits(shape, item.schema, value, item.provenance.kind)) {
          why.push(`body does not fit the ${item.provenance.kind} schema`);
        }
      }
      return why;
    };
    for (const item of claimed) {
      const seen = observed.filter((o) => o.got.status === item.status);
      if (seen.length === 0) {
        const key = `${name} ${item.status}`;
        if (cases.unobservable[key]) { notes.push(`${key}: unobservable (${cases.unobservable[key]})`); continue; }
        disagree(`${name}: claimed status ${item.status} (${item.provenance.rule}) never observed: ${JSON.stringify(observed.map((o) => o.got.status))}`);
        continue;
      }
      count('statuses');
      if (item.content_type !== null) count('content_types');
      if (item.schema && item.schema.type === 'null') count('empty_bodies');
      if (item.schema && item.schema.type === 'string') count('text_bodies');
      if (item.schema && item.schema.type === 'object' && item.provenance.kind === 'read_in_handler') count('literal_bodies');
      if (item.schema && item.provenance.kind === 'returned') count('typed_bodies');
      if (!seen.some((o) => mismatch(item, o).length === 0)) {
        disagree(`${name} ${item.status} (${item.provenance.rule}): no observation matches: ${seen.map((o) => mismatch(item, o).join(', ')).join(' | ')}`);
      }
    }
    for (const o of observed) {
      const at = claimed.filter((i) => i.status === o.got.status);
      if (at.length === 0) continue;
      count('observations_at_claimed_statuses');
      // A read_in_handler and a returned item at one status describe the
      // same response; it must match one of them.
      if (!at.some((i) => mismatch(i, o).length === 0)) {
        disagree(`${name} ${o.req.path} ${o.got.status}: matches no claimed response (${at.map((i) => mismatch(i, o).join(', ')).join(' | ')})`);
      }
    }
    // Default statuses must not be claimed where middleware may answer.
    // Fact bodies: refusals and acceptances derived from the valid request.
    const facts = shape.body.items.filter((i) => i.provenance.kind === 'fact');
    const valid = reqs.find((r) => r.valid);
    for (const f of facts) {
      if (!valid) {
        disagree(`${name}: a fact body and no valid request`);
        continue;
      }
      await derive(name, method, valid, f.schema, shape);
    }
  }
}

async function expect(name, method, req, refused, why) {
  const got = await send(method, req);
  count(refused ? 'refusals' : 'acceptances');
  const isRefused = Number(got.status) >= 400;
  if (isRefused !== refused) disagree(`${name}: ${why}: status ${got.status}, expected ${refused ? 'refused' : 'accepted'}`);
}

async function derive(name, method, valid, schema, shape) {
  let s = schema;
  if (s.type === 'ref') s = modelOf(shape, s.ref).schema;
  await expect(name, method, valid, false, 'the valid request');
  if (s.type !== 'object') return;
  for (const f of s.fields) {
    const body = { ...valid.json };
    if (f.presence === 'required') {
      delete body[f.name];
      await expect(name, method, { ...valid, json: body }, true, `${f.name} missing`);
    } else if (f.presence === 'optional') {
      delete body[f.name];
      await expect(name, method, { ...valid, json: body }, false, `optional ${f.name} missing`);
    }
    const w = wrong(f.schema);
    if (w !== undefined && f.schema.type !== 'unknown') {
      await expect(name, method, { ...valid, json: { ...valid.json, [f.name]: w } }, true, `${f.name} of the wrong kind`);
    }
    if (!f.schema.nullable && f.schema.type !== 'unknown' && f.schema.type !== 'null') {
      await expect(name, method, { ...valid, json: { ...valid.json, [f.name]: null } }, true, `${f.name} null`);
    }
    if (f.schema.type === 'integer') {
      await expect(name, method, { ...valid, json: { ...valid.json, [f.name]: 1.5 } }, true, `${f.name} not an integer`);
    }
  }
  if (s.additional === 'allowed') await expect(name, method, { ...valid, json: { ...valid.json, extra_key: 1 } }, false, 'an extra key');
  if (s.additional === 'forbidden') await expect(name, method, { ...valid, json: { ...valid.json, extra_key: 1 } }, true, 'an extra key');
  if (s.fields.some((f) => f.presence === 'required')) await expect(name, method, { ...valid, json: null }, true, 'no body');
}

// --- 2. server actions -------------------------------------------------------------
async function actionIds() {
  const html = await (await fetch(base + '/oracle-actions')).text();
  const ids = {};
  for (const m of html.matchAll(/<div id="action-([a-z]+)">.*?\$ACTION_ID_([0-9a-f]+)/gs)) ids[m[1]] = m[2];
  return ids;
}

async function callAction(id, args) {
  const res = await fetch(base + '/oracle-actions', {
    method: 'POST',
    headers: { 'next-action': id, accept: 'text/x-component', 'content-type': 'text/plain;charset=UTF-8' },
    body: JSON.stringify(args),
    redirect: 'manual',
  });
  return { status: res.status, text: await res.text(), redirect: res.headers.get('x-action-redirect') };
}

// actionOutcome reads a flight response: refused when the action threw.
function actionRefused(r) {
  return r.status >= 400 || /^\d+:E\{/m.test(r.text);
}

async function actionsCheck() {
  const ids = await actionIds();
  for (const [name, shape] of actions) {
    const c = cases.actions[name];
    if (!c || !c.args) continue;
    const id = ids[name];
    if (!id) {
      notes.push(`action ${name}: no action id rendered`);
      continue;
    }
    const r = await callAction(id, c.args);
    count('action_calls');
    // Results: a literal's fields appear in the returned value.
    for (const item of shape.responses.items) {
      if (item.provenance.kind !== 'read_in_handler' || !item.schema || item.schema.type !== 'object') continue;
      count('action_results');
      const values = [...r.text.matchAll(/^\d+:(\{.*\})$/gm)].map((m) => { try { return JSON.parse(m[1]); } catch { return null; } }).filter(Boolean);
      const hit = values.find((v) => (item.schema.fields || []).every((f) => f.name in v));
      if (!hit) disagree(`action ${name}: no returned value with fields ${item.schema.fields.map((f) => f.name)} in ${r.text.slice(0, 200)}`);
    }
    // Fact arguments: the schema is argument 1's; the action throws for
    // what the schema refuses there, and a further argument is not
    // checked (so the schema must not describe the whole list).
    for (const f of shape.body.items.filter((i) => i.provenance.kind === 'fact')) {
      const s = f.schema;
      if (s.type === 'array') disagree(`action ${name}: the fact schema is a list, but only argument 1 is checked`);
      if (c.extra !== undefined) {
        const more = await callAction(id, [...c.args, c.extra]);
        count('action_extra_arguments');
        if (actionRefused(more)) disagree(`action ${name}: an extra argument is refused (${more.status})`);
        const none = await callAction(id, []);
        count('action_refusals');
        if ((s.fields || []).some((fd) => fd.presence === 'required') && !actionRefused(none)) disagree(`action ${name}: no argument accepted (${none.status})`);
      }
      const ok = await callAction(id, c.args);
      count('action_acceptances');
      if (actionRefused(ok)) disagree(`action ${name}: the valid arguments are refused (${ok.status} ${ok.text.slice(0, 120)})`);
      for (const fd of s.fields || []) {
        const variants = [];
        if (fd.presence === 'required') {
          const v = { ...c.args[0] };
          delete v[fd.name];
          variants.push([v, `${fd.name} missing`]);
        }
        const w = wrong(fd.schema);
        if (w !== undefined) variants.push([{ ...c.args[0], [fd.name]: w }, `${fd.name} of the wrong kind`]);
        for (const [v, why] of variants) {
          const got = await callAction(id, [v]);
          count('action_refusals');
          if (!actionRefused(got)) disagree(`action ${name}: ${why} accepted (${got.status})`);
        }
      }
    }
  }
}

// --- 3. zod schemas ------------------------------------------------------------------
async function zodCheck() {
  const ts = require('typescript');
  const { z } = require('zod');
  for (const c of cases.schemas) {
    const file = path.join(appDir, c.file);
    const src = fs.readFileSync(file, 'utf8');
    // Evaluate the schema's declaration alone: `const Name = z.object(...)`.
    const m = src.match(new RegExp(`const ${c.name} = (z\\.object\\([\\s\\S]*?\\n?\\}\\));`));
    if (!m) { disagree(`zod ${c.name}: declaration not found`); continue; }
    const js = ts.transpileModule(`return ${m[1]};`, { compilerOptions: { target: 99 } }).outputText;
    const schema = new Function('z', js)(z);
    const json = z.toJSONSchema(schema, { io: 'input' });
    const shape = c.route ? routes.get(c.route) : actions.get(c.action);
    if (!shape) { disagree(`zod ${c.name}: no stored shape`); continue; }
    const item = shape.body.items.find((i) => i.provenance.kind === 'fact' || i.provenance.kind === 'intended');
    if (!item) { disagree(`zod ${c.name}: no stored schema`); continue; }
    const stored = item.schema;
    count('zod_schemas');
    const required = new Set(json.required || []);
    for (const f of stored.fields || []) {
      count('zod_fields');
      const p = json.properties[f.name];
      if (!p) { disagree(`zod ${c.name}.${f.name}: not a property`); continue; }
      if (item.provenance.kind === 'fact' && (f.presence === 'required') !== required.has(f.name)) disagree(`zod ${c.name}.${f.name}: presence ${f.presence}, zod required ${required.has(f.name)}`);
      const t = f.schema.type === 'unknown' ? null : f.schema.type;
      if (t && p.type !== t) disagree(`zod ${c.name}.${f.name}: type ${t}, zod ${p.type}`);
      if (t === 'array' && f.schema.items.type !== 'unknown' && p.items.type !== f.schema.items.type) disagree(`zod ${c.name}.${f.name}: items ${f.schema.items.type}, zod ${p.items.type}`);
    }
    if (Object.keys(json.properties).length !== (stored.fields || []).length) disagree(`zod ${c.name}: ${Object.keys(json.properties).length} properties, stored ${(stored.fields || []).length}`);
    const extra = json.additionalProperties === false ? 'forbidden' : 'allowed';
    if (stored.additional !== 'unknown' && stored.additional !== extra) disagree(`zod ${c.name}: additional ${stored.additional}, zod input ${extra}`);
  }
}

// --- 4. TypeScript -----------------------------------------------------------------------
function tsCheck() {
  const ts = require('typescript');
  const files = [];
  const walk = (d) => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, e.name);
      if (e.isDirectory()) { if (e.name !== 'node_modules' && e.name !== '.next') walk(p); }
      else if (/\.tsx?$/.test(e.name)) files.push(p);
    }
  };
  walk(path.join(appDir, 'app'));
  walk(path.join(appDir, 'lib'));
  const program = ts.createProgram(files, { strict: true, target: 99, module: 99, moduleResolution: 100, jsx: 1, skipLibCheck: true, noEmit: true, baseUrl: appDir });
  const checker = program.getTypeChecker();
  // 4a. Names read: the literal argument of get/getAll/has on a receiver
  // of the claimed kind.
  const receivers = { query: /^URLSearchParams$|^ReadonlyURLSearchParams$/, header: /^Headers$|^ReadonlyHeaders$/, cookie: /^RequestCookies$|^ReadonlyRequestCookies$/, body: /^FormData$/ };
  const sites = [];
  for (const sf of program.getSourceFiles()) {
    if (!sf.fileName.startsWith(appDir) || sf.fileName.includes('node_modules')) continue;
    const visit = (n) => {
      if (ts.isCallExpression(n) && ts.isPropertyAccessExpression(n.expression) && ['get', 'getAll', 'has'].includes(n.expression.name.text) && n.arguments.length >= 1 && ts.isStringLiteralLike(n.arguments[0])) {
        const t = checker.getTypeAtLocation(n.expression.expression);
        const sym = t.getSymbol() || t.aliasSymbol;
        const line = sf.getLineAndCharacterOfPosition(n.getStart()).line + 1;
        sites.push({ file: path.relative(appDir, sf.fileName), line, name: n.arguments[0].text, type: sym ? sym.getName() : checker.typeToString(t) });
      }
      ts.forEachChild(n, visit);
    };
    visit(sf);
  }
  const check = (where, section, p) => {
    if (p.provenance.kind !== 'read_in_handler') return;
    const file = p.provenance.file.replace(/^web\//, '');
    const hit = sites.find((s) => s.file === file && s.line === p.provenance.line && s.name.toLowerCase() === p.name.toLowerCase());
    count('names_read');
    if (!hit) { disagree(`${where} ${section} ${p.name}: no get/getAll/has of it at ${file}:${p.provenance.line}`); return; }
    if (!receivers[section].test(hit.type)) disagree(`${where} ${section} ${p.name}: receiver ${hit.type}`);
  };
  for (const [where, shape] of [...routes, ...[...actions].map(([k, v]) => [`action ${k}`, v])]) {
    for (const section of ['query', 'header', 'cookie']) for (const p of shape[section].items) check(where, section, p);
    for (const item of shape.body.items) {
      if (item.provenance.kind !== 'read_in_handler' || item.provenance.rule !== 'SH-N9') continue;
      for (const f of item.schema.fields) {
        const hit = sites.find((s) => s.name === f.name && receivers.body.test(s.type));
        count('form_fields');
        if (!hit) disagree(`${where} form field ${f.name}: no FormData get/getAll/has of it`);
      }
    }
  }
  // 4b. Intended and returned models against the declared types.
  const decls = new Map();
  for (const sf of program.getSourceFiles()) {
    if (!sf.fileName.startsWith(appDir) || sf.fileName.includes('node_modules')) continue;
    ts.forEachChild(sf, (n) => {
      if ((ts.isInterfaceDeclaration(n) || ts.isTypeAliasDeclaration(n)) && n.name) decls.set(n.name.text, n);
    });
  }
  const tsKind = (t) => {
    if (t.flags & ts.TypeFlags.StringLike) return 'string';
    if (t.flags & ts.TypeFlags.NumberLike) return 'number';
    if (t.flags & ts.TypeFlags.BooleanLike) return 'boolean';
    if (checker.isArrayType(t)) return 'array';
    if (t.flags & ts.TypeFlags.Object) return 'object';
    return 'other';
  };
  for (const [where, shape] of [...routes, ...[...actions].map(([k, v]) => [`action ${k}`, v])]) {
    for (const m of shape.models || []) {
      const d = decls.get(m.name);
      count('models');
      if (!d) { disagree(`${where} model ${m.name}: no declaration`); continue; }
      const type = checker.getTypeAtLocation(d.name);
      const props = checker.getPropertiesOfType(type);
      if (props.length !== (m.schema.fields || []).length) disagree(`${where} model ${m.name}: ${props.length} properties, stored ${(m.schema.fields || []).length}`);
      for (const f of m.schema.fields || []) {
        const p = props.find((x) => x.getName() === f.name);
        count('model_fields');
        if (!p) { disagree(`${where} model ${m.name}.${f.name}: no such property`); continue; }
        const optional = (p.flags & ts.SymbolFlags.Optional) !== 0;
        if ((f.presence === 'optional') !== optional) disagree(`${where} model ${m.name}.${f.name}: presence ${f.presence}, optional ${optional}`);
        let pt = checker.getTypeOfSymbolAtLocation(p, d);
        const parts = pt.isUnion() ? pt.types : [pt];
        const nullable = parts.some((x) => x.flags & ts.TypeFlags.Null);
        const rest = parts.filter((x) => !(x.flags & (ts.TypeFlags.Null | ts.TypeFlags.Undefined)));
        if (!!f.schema.nullable !== nullable) disagree(`${where} model ${m.name}.${f.name}: nullable ${!!f.schema.nullable}, declared ${nullable}`);
        if (f.schema.type !== 'unknown' && rest.length >= 1) {
          const k = tsKind(rest[0]);
          const want = f.schema.type === 'integer' ? 'number' : f.schema.type;
          if (k !== want && !(want === 'ref' && k === 'object') && !(want === 'enum')) disagree(`${where} model ${m.name}.${f.name}: type ${f.schema.type}, declared ${checker.typeToString(pt)}`);
        }
      }
    }
  }
}

(async () => {
  try {
    await routesCheck();
    await actionsCheck();
    await zodCheck();
    tsCheck();
  } catch (err) {
    disagree(`oracle error: ${err && err.stack || err}`);
  }
  const result = { label, counts, disagreements, notes };
  fs.writeFileSync(outFile, JSON.stringify(result, null, 2));
  console.log(JSON.stringify(result, null, 2));
  process.exit(disagreements.length ? 1 : 0);
})();
