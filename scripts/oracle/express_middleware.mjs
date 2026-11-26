// Express middleware oracle (T026 AG-B4b, row DA-E1): serves the fixture
// apps of testdata/fixtures/auth-express with the installed Express on
// 127.0.0.1, sends the requests listed in oracle.json (and its `more`
// modules, T026 AG-RF) and records, per
// request, the route that answered and the middleware that ran before it
// (each fixture middleware records its name as written).
//
// Usage: node express_middleware.mjs <fixture copy> <output.json>
import { createServer } from 'node:http';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const [root, out] = process.argv.slice(2);
const plan = JSON.parse(readFileSync(join(root, 'oracle.json'), 'utf8'));
const version = createRequire(join(root, 'package.json'))('express/package.json').version;
const major = Number(version.split('.')[0]);
// plan.more lists further modules, each with its own requests; a module
// with a major runs only on that Express major (wildcard syntax differs).
const groups = [{ module: plan.module, major: null, requests: plan.requests }, ...(plan.more ?? [])];
const results = [];
const skipped = [];
for (const group of groups) {
  if (group.major !== null && group.major !== major) {
    skipped.push(group.module);
    continue;
  }
  const apps = await import(pathToFileURL(join(root, group.module)).href);
  for (const [name, method, path] of group.requests) {
    await send(apps, name, method, path);
  }
}

async function send(apps, name, method, path) {
  const server = createServer(apps[name]);
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  const response = await fetch(`http://127.0.0.1:${port}${path}`, { method });
  const body = response.headers.get('content-type')?.includes('json') ? await response.json() : {};
  results.push({ root: name, method, path, status: response.status, route: body.route ?? null, trace: body.trace ?? [] });
  await new Promise((resolve) => server.close(resolve));
}
writeFileSync(out, JSON.stringify({ express: version, skipped, results }, null, 2) + '\n');
