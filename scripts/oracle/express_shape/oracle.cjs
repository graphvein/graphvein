// The Express and Next.js Pages Router API shape oracle (rows SH-E1..SH-E36
// of docs/contract/claims/route-shape-express.md). express_shape.sh
// prepares the scratch directories and runs it; see README.md.
//
// It compares the shapes Graphvein stores for testdata/fixtures/express-shape
// (the golden, or a snapshot) with:
//   1. the fixture served on 127.0.0.1 by Express 4, Express 5 and Next.js's
//      apiResolver: every claimed status must be observed, every claimed
//      content type and empty body must match, and for every `fact` schema
//      a known-valid request (cases.json) is mutated into requests that
//      must be refused and requests that must be accepted;
//   2. zod's toJSONSchema and Joi's describe() of the fixture schemas;
//   3. the TypeScript checker for every `intended` and `returned` model.
// Exit status 1 when anything disagrees.
'use strict';
const fs = require('fs');
const path = require('path');
const http = require('http');
const Module = require('module');

const args = {};
for (let i = 2; i < process.argv.length; i += 2) args[process.argv[i].replace(/^--/, '')] = process.argv[i + 1];
for (const k of ['work', 'tools', 'next', 'claims', 'cases', 'out']) {
  if (!args[k]) { console.error(`oracle: --${k} is required`); process.exit(2); }
}
const work = path.resolve(args.work);
const out = path.join(work, 'out');
const counts = { requests: 0, refused_as_claimed: 0, accepted_as_claimed: 0, statuses_observed: 0, content_types_agree: 0, empty_bodies_agree: 0, body_keys_agree: 0, expectations_met: 0, schema_facts_agree: 0, schema_facts_not_claimed: 0, ts_agree: 0, disagree: 0 };
const disagreements = [];
const notes = [];
function disagree(msg) { counts.disagree++; disagreements.push(msg); console.log('DISAGREE', msg); }

// Thrown validation errors of async handlers reject after or instead of
// a response (Express 4 does not catch them): log and go on.
process.on('unhandledRejection', (e) => notes.push('unhandled rejection: ' + (e && e.message ? e.message.split('\n')[0] : String(e))));

// Claims: a golden ({routes: [{identity, shape}]}) or an NDJSON snapshot.
function loadClaims(file) {
  const text = fs.readFileSync(file, 'utf8');
  const shapes = {};
  const add = (identity, shape) => { if (shape) shapes[decodeURIComponent(identity.replace(/^route:/, ''))] = shape; };
  if (file.endsWith('.ndjson')) {
    for (const line of text.split('\n')) {
      if (!line.trim()) continue;
      const r = JSON.parse(line);
      if (r.type === 'node' && r.kind === 'route') add(r.identity, r.attributes && r.attributes.shape);
    }
  } else {
    for (const r of JSON.parse(text).routes) add(r.identity, r.shape);
  }
  return shapes;
}
const shapes = loadClaims(args.claims);
const cases = JSON.parse(fs.readFileSync(args.cases, 'utf8'));

// ---- 1. Served apps ----------------------------------------------------

function versionOf(dir, pkg) {
  const p = require.resolve(pkg + '/package.json', { paths: [dir] });
  return JSON.parse(fs.readFileSync(p, 'utf8')).version;
}
const ts = require(path.join(args.tools, 'node_modules', 'typescript'));
const origLoad = Module._load;
// The OpenAPI validator only rejects; a pass-through shows the handler's
// own statuses (row SH-E32 claims nothing from the spec).
Module._load = function (req, ...rest) {
  if (req === 'express-openapi-validator') return { middleware: () => (q, s, n) => n() };
  return origLoad.call(this, req, ...rest);
};
const express4 = require(path.join(out, 'src', 'app.js')).app;
const validated = require(path.join(out, 'src', 'validated.js')).api;
const express5 = require(path.join(out, 'v5', 'src', 'server.js')).app5;
const versions = {
  express4: versionOf(path.join(out, 'src'), 'express'),
  express5: versionOf(path.join(out, 'v5', 'src'), 'express'),
  next: versionOf(args.next, 'next'),
};
if (!versions.express4.startsWith('4.') || !versions.express5.startsWith('5.')) {
  console.error('oracle: wrong Express versions', versions);
  process.exit(2);
}
const { apiResolver } = require(require.resolve('next/dist/server/api-utils/node/api-resolver.js', { paths: [args.next] }));
function loadPage(file) {
  const src = ts.transpileModule(fs.readFileSync(file, 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true } }).outputText;
  const m = new Module(file);
  m.paths = Module._nodeModulePaths(args.next);
  m._compile(src, file);
  return m.exports;
}
const pageModules = {};
function pages(req, res) {
  const u = new URL(req.url, 'http://x');
  const c = req.headers['x-oracle-page'];
  const spec = JSON.parse(Buffer.from(c, 'base64').toString('utf8'));
  if (!pageModules[spec.file]) pageModules[spec.file] = loadPage(path.join(work, spec.file));
  const query = Object.assign(Object.fromEntries(u.searchParams), spec.params || {});
  apiResolver(req, res, query, pageModules[spec.file], { previewModeId: '', previewModeEncryptionKey: '', previewModeSigningKey: '' }, false, false, u.pathname);
}
const handlers = { express4, express5, validated, pages };

function listen(handler) {
  return new Promise((resolve) => {
    const server = http.createServer(handler);
    server.listen(0, '127.0.0.1', () => resolve(server));
  });
}

// send issues one request; a request with no answer within the timeout
// was not served (an async handler that threw under Express 4).
function send(port, c) {
  return new Promise((resolve) => {
    const headers = Object.assign({}, c.headers || {});
    let data = null;
    if (c.body !== undefined) {
      data = JSON.stringify(c.body);
      headers['content-type'] = 'application/json';
      headers['content-length'] = Buffer.byteLength(data);
    }
    if (c.page) headers['x-oracle-page'] = Buffer.from(JSON.stringify(c.page)).toString('base64');
    const r = http.request({ host: '127.0.0.1', port, method: c.method, path: c.url, headers }, (s) => {
      const chunks = [];
      s.on('data', (d) => chunks.push(d));
      s.on('end', () => {
        clearTimeout(timer);
        const body = Buffer.concat(chunks);
        resolve({ status: s.statusCode, type: s.headers['content-type'] || null, length: body.length, text: body.toString('utf8') });
      });
    });
    const timer = setTimeout(() => { r.destroy(); resolve({ status: null, type: null, length: 0, text: '', timeout: true }); }, 1500);
    r.on('error', () => {});
    if (data) r.write(data);
    r.end();
  });
}

const refused = (o) => o.timeout || o.status >= 400;
const accepted = (o) => !o.timeout && o.status < 400;

function factBody(shape) {
  return (shape.body.items || []).find((i) => i.provenance.kind === 'fact');
}
function factQuery(shape) {
  return (shape.query.items || []).filter((i) => i.provenance.kind === 'fact');
}
function clone(v) { return v === undefined ? undefined : JSON.parse(JSON.stringify(v)); }

// wrongKinds are values of another kind than the claimed type, including
// strings a converting validator would turn into the claimed type.
function wrongKinds(schema) {
  switch (schema.type) {
    case 'string': return [5, true];
    case 'number': return ['5', true];
    case 'integer': return ['5', 1.5];
    case 'boolean': return ['true', 1];
    case 'object': return ['x', 1];
    case 'array': return ['x', 1];
    case 'enum': {
      const outs = [];
      const strings = schema.enum.filter((v) => typeof v === 'string');
      outs.push(strings.length ? 'zz-not-in-enum' : 'x');
      for (const v of strings) if (v.toUpperCase() !== v) outs.push(v.toUpperCase());
      for (const v of schema.enum) if (typeof v === 'number') outs.push(String(v));
      return outs;
    }
  }
  return [];
}

// bodyMutations lists [label, body, expectation] for a valid sample.
function bodyMutations(schema, sample) {
  const list = [];
  const set = (root, p, value, remove) => {
    const copy = clone(root);
    let o = copy;
    for (let i = 0; i < p.length - 1; i++) o = o[p[i]];
    if (remove) delete o[p[p.length - 1]]; else o[p[p.length - 1]] = value;
    return copy;
  };
  const walk = (s, value, p) => {
    if (!s || s.type === 'unknown' || s.type === 'ref') return;
    if (p.length > 0) {
      for (const w of wrongKinds(s)) list.push([`${p.join('.')}=${JSON.stringify(w)}`, set(sample, p, w), 'refuse']);
      if (s.nullable) list.push([`${p.join('.')}=null`, set(sample, p, null), 'accept']);
      else list.push([`${p.join('.')}=null`, set(sample, p, null), 'refuse']);
    }
    if (s.type === 'object' && value && typeof value === 'object' && !Array.isArray(value)) {
      for (const f of s.fields || []) {
        const present = Object.prototype.hasOwnProperty.call(value, f.name);
        if (present && f.presence === 'required') list.push([`-${[...p, f.name].join('.')}`, set(sample, [...p, f.name], undefined, true), 'refuse']);
        if (present && f.presence === 'optional') list.push([`-${[...p, f.name].join('.')}`, set(sample, [...p, f.name], undefined, true), 'accept']);
        if (present) walk(f.schema, value[f.name], [...p, f.name]);
      }
      const extra = '__oracle_extra';
      if (s.additional === 'forbidden') list.push([`+${[...p, extra].join('.')}`, set(sample, [...p, extra], 'x'), 'refuse']);
      if (s.additional === 'allowed') list.push([`+${[...p, extra].join('.')}`, set(sample, [...p, extra], 'x'), 'accept']);
    }
    if (s.type === 'array' && Array.isArray(value) && value.length > 0 && s.items) {
      for (const w of wrongKinds(s.items)) list.push([`${p.join('.')}[0]=${JSON.stringify(w)}`, set(sample, [...p, 0], w), 'refuse']);
    }
  };
  walk(schema, sample, []);
  if (schema.type === 'object' && (schema.fields || []).some((f) => f.presence === 'required')) list.push(['no body', undefined, 'refuse']);
  return list;
}

// queryMutations lists [label, url, expectation] for a valid sample URL.
function queryMutations(items, url, server) {
  const list = [];
  const u = new URL(url, 'http://x');
  for (const it of items) {
    const present = u.searchParams.has(it.name);
    const with_ = (fn) => { const v = new URL(u); fn(v.searchParams); return v.pathname + v.search; };
    if (present && it.presence === 'required') list.push([`-${it.name}`, with_((q) => q.delete(it.name)), 'refuse']);
    if (present && it.presence === 'optional') list.push([`-${it.name}`, with_((q) => q.delete(it.name)), 'accept']);
    if (!present || !it.schema) continue;
    switch (it.schema.type) {
      case 'string':
        // qs (Express 4) parses name[x]=1 as an object.
        if (server === 'express4') list.push([`${it.name}[x]=1`, with_((q) => { q.delete(it.name); q.set(it.name + '[x]', '1'); }), 'refuse']);
        break;
      case 'number': case 'integer':
        list.push([`${it.name}=abc`, with_((q) => q.set(it.name, 'abc')), 'refuse']);
        list.push([`${it.name}=5 (a string)`, with_((q) => q.set(it.name, '5')), 'refuse']);
        break;
      case 'boolean':
        list.push([`${it.name}=true (a string)`, with_((q) => q.set(it.name, 'true')), 'refuse']);
        break;
    }
  }
  return list;
}

async function serve() {
  const servers = {};
  for (const [name, h] of Object.entries(handlers)) servers[name] = await listen(h);
  const observed = {};
  const note = (route, o, label) => { (observed[route] = observed[route] || []).push(Object.assign({ label }, o)); };
  for (const c of cases.requests) {
    const shape = shapes[c.route];
    if (!shape) { disagree(`${c.route}: no stored shape`); continue; }
    const port = servers[c.server].address().port;
    const o = await send(port, c);
    counts.requests++;
    note(c.route, o, `${c.method} ${c.url}`);
    if (c.expectBody !== undefined) {
      let got = null;
      try { got = JSON.parse(o.text); } catch (e) { got = o.text; }
      if (JSON.stringify(got) === JSON.stringify(c.expectBody)) counts.expectations_met++;
      else disagree(`${c.route}: ${c.method} ${c.url} answered ${o.text}, expected ${JSON.stringify(c.expectBody)}`);
    }
    if (!c.valid) continue;
    if (!accepted(o)) { disagree(`${c.route}: the valid sample ${c.method} ${c.url} was not accepted (${o.status})`); continue; }
    const body = factBody(shape);
    if (body && body.schema) {
      for (const [label, mutated, want] of bodyMutations(body.schema, c.body)) {
        const m = await send(port, Object.assign({}, c, { body: mutated }));
        counts.requests++;
        note(c.route, m, label);
        const ok = want === 'refuse' ? refused(m) : accepted(m);
        if (ok) counts[want === 'refuse' ? 'refused_as_claimed' : 'accepted_as_claimed']++;
        else disagree(`${c.route}: body ${label} should be ${want === 'refuse' ? 'refused' : 'accepted'}, answered ${m.timeout ? 'nothing' : m.status}`);
      }
    }
    const query = factQuery(shape);
    if (query.length) {
      for (const [label, url, want] of queryMutations(query, c.url, c.server)) {
        const m = await send(port, Object.assign({}, c, { url }));
        counts.requests++;
        note(c.route, m, label);
        const ok = want === 'refuse' ? refused(m) : accepted(m);
        if (ok) counts[want === 'refuse' ? 'refused_as_claimed' : 'accepted_as_claimed']++;
        else disagree(`${c.route}: query ${label} should be ${want === 'refuse' ? 'refused' : 'accepted'}, answered ${m.timeout ? 'nothing' : m.status}`);
      }
    }
  }
  for (const s of Object.values(servers)) s.close();
  // Statuses, content types and empty bodies.
  for (const [route, list] of Object.entries(observed)) {
    const items = shapes[route].responses.items.filter((i) => i.provenance.kind === 'read_in_handler');
    for (const it of items) {
      const hits = list.filter((o) => String(o.status) === it.status);
      if (hits.length === 0) {
        const why = (cases.unobservable || {})[`${route} ${it.status}`];
        if (why) notes.push(`${route} ${it.status} not observed: ${why}`);
        else disagree(`${route}: claimed status ${it.status} never observed (${[...new Set(list.map((o) => o.status))].join(', ')})`);
        continue;
      }
      counts.statuses_observed++;
      for (const o of hits) {
        if (it.content_type) {
          if ((o.type || '').split(';')[0].trim() === it.content_type) counts.content_types_agree++;
          else disagree(`${route}: ${o.label} ${o.status} content type ${o.type}, claimed ${it.content_type}`);
        }
        if (it.schema && it.schema.type === 'object' && (it.schema.fields || []).length) {
          // An object literal sent (row SH-E27): JSON has only its keys
          // (an undefined value drops one); another key means something
          // else wrote the body.
          let got = null;
          try { got = JSON.parse(o.text); } catch (e) { got = null; }
          const names = new Set(it.schema.fields.map((f) => f.name));
          if (got && typeof got === 'object' && !Array.isArray(got) && Object.keys(got).every((k) => names.has(k))) counts.body_keys_agree++;
          else disagree(`${route}: ${o.label} ${o.status} body ${o.text.slice(0, 80)} has keys outside the claimed ${[...names]}`);
        }
        if (it.schema && it.schema.type === 'null') {
          if (o.length === 0) counts.empty_bodies_agree++;
          else disagree(`${route}: ${o.label} ${o.status} body of ${o.length} bytes, claimed empty`);
        }
      }
    }
    const claimed = new Set(items.map((i) => i.status));
    const unclaimed = [...new Set(list.filter((o) => o.status !== null && !claimed.has(String(o.status))).map((o) => o.status))];
    if (unclaimed.length) notes.push(`${route}: observed, not claimed: ${unclaimed.join(', ')}`);
  }
}

// ---- 2. Schema libraries -----------------------------------------------

function loadSchemas() {
  const own = require(path.join(out, 'src', 'schemas.js'));
  const { z } = require(path.join(args.tools, 'node_modules', 'zod'));
  const Joi = require(path.join(args.tools, 'node_modules', 'joi'));
  return { own, z, Joi };
}

// fromZod reduces a zod input JSON schema to type, nullable, fields.
function fromZod(js) {
  let nullable = false;
  if (js.anyOf) {
    const alts = js.anyOf.filter((a) => a.type !== 'null');
    nullable = alts.length !== js.anyOf.length;
    js = alts[0];
  }
  const o = { type: js.type, nullable };
  if (js.type === 'array') o.items = fromZod(js.items);
  if (js.type === 'object') {
    const req = new Set(js.required || []);
    o.additional = js.additionalProperties === false ? 'forbidden' : 'allowed';
    o.fields = {};
    for (const [k, v] of Object.entries(js.properties || {})) o.fields[k] = [req.has(k) ? 'required' : 'optional', fromZod(v)];
  }
  return o;
}

// fromJoi reduces a Joi describe() to what Joi enforces: number and
// boolean are 'converts' unless convert is off on the key or above.
function fromJoi(d, strict) {
  const flags = d.flags || {};
  if (d.preferences && d.preferences.convert !== undefined) strict = d.preferences.convert === false;
  let type = d.type;
  if (type === 'number' && (d.rules || []).some((r) => r.name === 'integer')) type = 'integer';
  const allow = d.allow || [];
  const presence = flags.presence || 'optional';
  const emptied = flags.empty && (flags.empty.allow || []).some((v) => v === null);
  const nullable = emptied ? presence !== 'required' : allow.includes(null);
  if (flags.only) {
    type = flags.insensitive ? 'insensitive enum' : 'enum';
  }
  if ((type === 'number' || type === 'integer' || type === 'boolean') && !strict && !flags.only) type = 'converts';
  if (flags.only && !flags.insensitive && !strict && ['number', 'boolean'].includes(d.type)) type = 'converts';
  const o = { type, nullable, presence };
  if (o.type === 'enum') o.enum = allow.filter((v) => v !== null);
  if (d.type === 'array') o.items = d.items && d.items.length ? fromJoi(d.items[0], strict) : { type: 'unknown' };
  if (d.type === 'object') {
    o.additional = flags.unknown ? 'allowed' : 'forbidden';
    o.fields = {};
    for (const [k, v] of Object.entries(d.keys || {})) {
      const f = fromJoi(v, strict);
      o.fields[k] = [f.presence, f];
    }
  }
  return o;
}

function fromGraphvein(s) {
  const o = { type: s.type, nullable: !!s.nullable };
  if (s.type === 'enum') o.enum = s.enum;
  if (s.type === 'array') o.items = fromGraphvein(s.items);
  if (s.type === 'object') {
    o.additional = s.additional;
    o.fields = {};
    for (const f of s.fields || []) o.fields[f.name] = [f.presence, fromGraphvein(f.schema)];
  }
  return o;
}

function compareSchema(name, o, g, at) {
  const check = (what, a, b) => {
    if (b === 'unknown') { counts.schema_facts_not_claimed++; return; }
    if (JSON.stringify(a) === JSON.stringify(b)) counts.schema_facts_agree++;
    else disagree(`${name} ${at} ${what}: library ${JSON.stringify(a)}, Graphvein ${JSON.stringify(b)}`);
  };
  if (g.type === 'unknown') {
    // Not claimed; a library type Joi converts or an insensitive enum must
    // not be claimed, which holds here.
    counts.schema_facts_not_claimed++;
    return;
  }
  if (o.type === 'converts' || o.type === 'insensitive enum') {
    disagree(`${name} ${at}: Graphvein claims ${g.type}, the library accepts other kinds (${o.type})`);
    return;
  }
  check('type', o.type, g.type);
  check('nullable', o.nullable, g.nullable);
  if (o.type === 'enum' && g.type === 'enum') check('values', o.enum, g.enum);
  if (o.type === 'array' && g.type === 'array') compareSchema(name, o.items, g.items, at + '[]');
  if (o.type === 'object' && g.type === 'object') {
    check('additional', o.additional, g.additional);
    const on = Object.keys(o.fields).sort(), gn = Object.keys(g.fields).sort();
    if (JSON.stringify(on) === JSON.stringify(gn)) counts.schema_facts_agree++;
    else disagree(`${name} ${at} field names: library ${on}, Graphvein ${gn}`);
    for (const k of on) {
      if (!g.fields[k]) continue;
      check(`presence ${k}`, o.fields[k][0], g.fields[k][0]);
      compareSchema(name, o.fields[k][1], g.fields[k][1], `${at}.${k}`);
    }
  }
}

function schemas() {
  const { own, z, Joi } = loadSchemas();
  for (const c of cases.schemas) {
    const shape = shapes[c.route];
    if (!shape) { disagree(`${c.route}: no stored shape`); continue; }
    let g;
    if (c.section === 'query') {
      const fields = factQuery(shape).map((p) => ({ name: p.name, presence: p.presence, schema: p.schema }));
      g = { type: 'object', additional: 'unknown', fields };
    } else {
      const item = (shape.body.items || []).find((i) => i.provenance.kind === (c.kind || 'fact'));
      if (!item) { disagree(`${c.route}: no ${c.kind || 'fact'} body`); continue; }
      g = item.schema;
    }
    // An inline schema of the fixture (celebrate arguments) is written out
    // in cases.json and built here with the pinned Joi.
    const schema = c.expr ? new Function('Joi', 'z', 'S', `return ${c.expr};`)(Joi, z, own) : own[c.schema];
    const o = c.lib === 'zod' ? fromZod(z.toJSONSchema(schema, { io: 'input' })) : fromJoi(schema.describe(), false);
    const gv = fromGraphvein(g);
    // The root's own presence is the route's concern (row SH-E14, checked
    // by the requests); keys are compared.
    compareSchema(`${c.route} (${c.schema || c.expr})`, o, gv, '$');
  }
}

// ---- 3. TypeScript -----------------------------------------------------

function typescript() {
  const srcRoot = work;
  const files = fs.readdirSync(path.join(srcRoot, 'src')).filter((f) => f.endsWith('.ts')).map((f) => path.join(srcRoot, 'src', f));
  const program = ts.createProgram(files, { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS, esModuleInterop: true, skipLibCheck: true, strict: true, typeRoots: [path.join(srcRoot, 'node_modules', '@types')] });
  const checker = program.getTypeChecker();
  const say = (ok, msg) => { if (ok) counts.ts_agree++; else disagree('typescript ' + msg); };
  const hasUndefined = (t) => t.isUnion() && t.types.some((x) => x.flags & ts.TypeFlags.Undefined);
  function schemaOf(t) {
    if (t.flags & ts.TypeFlags.String) return 'string';
    if (t.flags & ts.TypeFlags.Number) return 'number';
    if (t.flags & ts.TypeFlags.Boolean) return 'boolean';
    if (t.isUnion()) {
      const nullable = t.types.some((x) => x.flags & ts.TypeFlags.Null);
      const rest = t.types.filter((x) => !(x.flags & (ts.TypeFlags.Null | ts.TypeFlags.Undefined)));
      if (rest.length === 1) return schemaOf(rest[0]) + (nullable ? '|null' : '');
      if (rest.every((x) => x.isStringLiteral())) return 'enum:' + rest.map((x) => x.value).sort().join(',') + (nullable ? '|null' : '');
      if (rest.length === 2 && rest.every((x) => x.flags & ts.TypeFlags.BooleanLiteral)) return 'boolean' + (nullable ? '|null' : '');
      return 'union';
    }
    if (checker.isArrayType(t)) return 'array<' + schemaOf(checker.getTypeArguments(t)[0]) + '>';
    if (t.aliasSymbol) return 'ref:' + t.aliasSymbol.name;
    if (t.symbol && t.symbol.name !== '__type') return 'ref:' + t.symbol.name;
    return 'object';
  }
  function gvSchema(s, models) {
    switch (s.type) {
      case 'array': return 'array<' + gvSchema(s.items, models) + '>';
      case 'ref': {
        const m = models.find((x) => x.name === s.ref);
        if (m && m.schema.type === 'enum') return 'enum:' + m.schema.enum.slice().sort().join(',') + (s.nullable ? '|null' : '');
        return 'ref:' + s.ref + (s.nullable ? '|null' : '');
      }
      case 'enum': return 'enum:' + s.enum.slice().sort().join(',') + (s.nullable ? '|null' : '');
      case 'object': return 'object';
    }
    return s.type + (s.nullable ? '|null' : '');
  }
  function declType(file, name) {
    const sf = program.getSourceFile(path.join(srcRoot, file));
    if (!sf) return null;
    for (const st of sf.statements) {
      if ((ts.isInterfaceDeclaration(st) || ts.isTypeAliasDeclaration(st)) && st.name.text === name) return checker.getTypeAtLocation(st.name);
    }
    return null;
  }
  function compareFields(label, type, fields, models) {
    const props = checker.getPropertiesOfType(type);
    say(JSON.stringify(props.map((p) => p.name).sort()) === JSON.stringify(fields.map((f) => f.name).sort()), `${label}: properties ${props.map((p) => p.name)} vs ${fields.map((f) => f.name)}`);
    for (const p of props) {
      const f = fields.find((x) => x.name === p.name);
      if (!f) continue;
      const pt = checker.getTypeOfSymbolAtLocation(p, p.valueDeclaration);
      // A key may be missing when it is `?` or its type includes
      // undefined (JSON drops an undefined property).
      const optional = (p.flags & ts.SymbolFlags.Optional) !== 0 || hasUndefined(pt);
      say((f.presence === 'optional') === optional && f.presence !== 'required', `${label}.${p.name}: may be missing ${optional} vs presence ${f.presence}`);
      if (f.schema.type === 'object') { compareFields(`${label}.${p.name}`, pt, f.schema.fields, models); continue; }
      say(schemaOf(pt) === gvSchema(f.schema, models), `${label}.${p.name}: compiler ${schemaOf(pt)} vs graphvein ${gvSchema(f.schema, models)}`);
    }
  }
  function typesOnLine(file, line) {
    const sf = program.getSourceFile(path.join(srcRoot, file));
    const names = new Set();
    if (!sf) return names;
    const visit = (n) => {
      const l = sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1;
      if (l === line && (ts.isExpression(n) || ts.isParameter(n))) {
        const t = checker.getTypeAtLocation(n);
        const add = (x) => {
          if (!x) return;
          if (x.aliasSymbol) names.add(x.aliasSymbol.name);
          if (x.symbol) names.add(x.symbol.name);
          const aw = checker.getAwaitedType(x);
          if (aw && aw !== x && aw.symbol) names.add(aw.symbol.name);
        };
        add(t);
        for (const key of ['body', 'query']) { const p = t.getProperty && t.getProperty(key); if (p) add(checker.getTypeOfSymbolAtLocation(p, n)); }
      }
      ts.forEachChild(n, visit);
    };
    visit(sf);
    return names;
  }
  for (const [route, s] of Object.entries(shapes)) {
    if (!route.startsWith('src/')) continue;
    for (const m of s.models) {
      const t = declType(m.provenance.file, m.name);
      if (!t) { say(false, `${route}: model ${m.name} not declared in ${m.provenance.file}`); continue; }
      if (m.schema.type === 'object') compareFields(m.name, t, m.schema.fields, s.models);
      else say(schemaOf(t) === gvSchema(m.schema, s.models) || (m.schema.type === 'enum' && schemaOf(t) === 'enum:' + m.schema.enum.slice().sort().join(',')), `${m.name}: ${schemaOf(t)} vs ${gvSchema(m.schema, s.models)}`);
    }
    const items = [...s.body.items, ...s.responses.items, ...s.query.items].filter((i) => i.provenance.kind === 'intended' || i.provenance.kind === 'returned');
    for (const it of items) {
      if (!it.schema || it.schema.type !== 'ref') continue;
      const names = typesOnLine(it.provenance.file, it.provenance.line);
      say(names.has(it.schema.ref), `${route}: ${it.provenance.rule} site ${it.provenance.file}:${it.provenance.line} types ${[...names]} lack ${it.schema.ref}`);
    }
    const q = s.query.items.filter((i) => i.provenance.kind === 'intended');
    if (q.length) {
      const sites = typesOnLine(q[0].provenance.file, q[0].provenance.line);
      const name = [...sites].find((n) => declType('src/types.ts', n));
      const t = name && declType('src/types.ts', name);
      if (!t) say(false, `${route}: query type not found at the site`);
      else compareFields(`${route} query ${name}`, t, q.map((i) => ({ name: i.name, presence: i.presence, schema: i.schema })), s.models);
    }
  }
}

(async () => {
  await serve();
  schemas();
  typescript();
  const result = { versions, counts, disagreements, notes };
  fs.writeFileSync(args.out, JSON.stringify(result, null, 1));
  console.log(JSON.stringify({ versions, counts }, null, 1));
  const seen = {};
  for (const n of notes) seen[n] = (seen[n] || 0) + 1;
  for (const [n, k] of Object.entries(seen)) console.log('note:', n, k > 1 ? `(${k} times)` : '');
  process.exit(counts.disagree === 0 ? 0 : 1);
})();
