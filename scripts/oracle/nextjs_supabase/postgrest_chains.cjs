// Supabase chain oracle (T024c2, rows SB-T1 and SB-T2): evaluates each
// claimed fixture chain with @supabase/postgrest-js's query builder, which
// builds the method, URL and schema of a request without sending it, and
// prints them. Nothing connects: fetch is a stub that throws, and the
// builder is never awaited.
//
//   node postgrest_chains.cjs <postgrest-js package dir> <chains.json>
//
// chains.json: [{"id", "receiver", "chain", "schema", "consts", "bound"}];
// chain is the source text of the chain, starting with the receiver's
// text, which is replaced by a PostgrestClient for the client's schema;
// consts binds the bounded consts the chain names to their literals. A
// chain whose receiver is a const builder binding (`const q =
// supabase.from('t'); q.select()`, T024 review) carries bound {name,
// text}: the chain starts with the binding's name, which is replaced by
// the binding's value text (starting with the receiver) before the chain
// is evaluated (T024 review). Any other free name in the chain reads as
// a placeholder (see below).
'use strict';
const fs = require('fs');
const path = require('path');
const Module = require('module');

const [pkg, input] = process.argv.slice(2);
const stub = () => { throw new Error('postgrest_chains: no request is sent'); };
const load = Module._load;
Module._load = function (request, parent, isMain) {
  if (request === '@supabase/node-fetch') return stub;
  return load.call(this, request, parent, isMain);
};
// 1.18 ships dist/cjs, 1.9 (chatbot-ui's lock) dist/main.
const dist = fs.existsSync(path.join(pkg, 'dist', 'cjs', 'index.js')) ? 'cjs' : 'main';
const { PostgrestClient } = require(path.join(pkg, 'dist', dist, 'index.js'));
const chains = JSON.parse(fs.readFileSync(input, 'utf8'));
// placeholder stands for a free name of a chain's arguments (a filter
// value such as session.user.id): any member of it, or a call of it, is
// the placeholder again, and it reads as the string 'placeholder'. The
// builder's method, URL path and schema do not depend on filter values.
const placeholder = new Proxy(function () {}, {
  get: (_, k) => (k === Symbol.toPrimitive ? () => 'placeholder' : k === 'toString' || k === 'valueOf' ? () => 'placeholder' : k === 'toJSON' ? () => 'placeholder' : placeholder),
  apply: () => placeholder,
});
const out = [];
for (const c of chains) {
  if (c.bound) {
    if (!c.chain.startsWith(c.bound.name) || /[\w$]/.test(c.chain.charAt(c.bound.name.length))) {
      out.push({ id: c.id, error: 'chain does not start with its const builder binding' });
      continue;
    }
    c.chain = c.bound.text + c.chain.slice(c.bound.name.length);
  }
  if (!c.chain.startsWith(c.receiver)) {
    out.push({ id: c.id, error: 'chain does not start with its receiver' });
    continue;
  }
  const client = new PostgrestClient('http://oracle.invalid/rest/v1', { schema: c.schema === 'public' ? undefined : c.schema, fetch: stub });
  const scope = new Proxy({}, {
    has: (_, k) => typeof k === 'string' && k !== 'client' && !(k in globalThis),
    get: (_, k) => (k === Symbol.unscopables ? undefined : (c.consts && Object.prototype.hasOwnProperty.call(c.consts, k) ? c.consts[k] : placeholder)),
  });
  try {
    // eslint-disable-next-line no-new-func
    const build = new Function('client', 'scope', 'with (scope) { return client' + c.chain.slice(c.receiver.length) + '; }');
    const b = build(client, scope);
    out.push({ id: c.id, method: b.method, path: decodeURIComponent(b.url.pathname), schema: b.schema || 'public', prefer: b.headers && b.headers.Prefer || '' });
  } catch (err) {
    out.push({ id: c.id, error: String(err && err.message || err) });
  }
}
process.stdout.write(JSON.stringify(out, null, 1) + '\n');
