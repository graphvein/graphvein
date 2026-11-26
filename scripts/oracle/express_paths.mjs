// Express path oracle (T028, docs/contract/claims/express5-paths.md):
// serves the apps of testdata/fixtures/express5-paths with the installed
// Express on 127.0.0.1, sends the GET requests listed in oracle.json and
// records which route answered each (every fixture handler answers with
// its name). For Express 5 it also registers each pattern of the group's
// `throws` list on a fresh Router and records whether Express throws.
//
// Every request is sent as listed and, when it does not end in `/`, with a
// trailing slash too, so the claims meet both slash forms.
//
// Usage: node express_paths.mjs <fixture copy> <output.json>
// Only the groups whose `majors` include the installed Express major run;
// the directory of each group's module has its own node_modules link.
import { createServer } from 'node:http';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const [root, out] = process.argv.slice(2);
const plan = JSON.parse(readFileSync(join(root, 'oracle.json'), 'utf8'));
const results = [];
const throws = [];
const versions = {};
for (const group of plan.groups) {
  const module = `${group.dir}/${group.module}`;
  const require = createRequire(join(root, module));
  let version;
  try {
    version = require('express/package.json').version;
  } catch {
    continue; // no Express linked for this group under this run
  }
  const major = Number(version.split('.')[0]);
  if (!group.majors.includes(major)) {
    continue;
  }
  versions.express = version;
  // The versions Express itself loads: router from express (Express 5),
  // path-to-regexp from router (Express 5) or express (Express 4).
  const fromExpress = createRequire(require.resolve('express'));
  let fromRouter = null;
  try {
    fromRouter = createRequire(fromExpress.resolve('router'));
    versions.router = packageVersion(fromExpress.resolve('router'), 'router');
  } catch {
    // Express 4 has no router package.
  }
  versions['path-to-regexp'] = packageVersion((fromRouter ?? fromExpress).resolve('path-to-regexp'), 'path-to-regexp');
  const apps = await import(pathToFileURL(join(root, module)).href);
  for (const [name, requests] of Object.entries(group.apps)) {
    for (const listed of requests) {
      const forms = listed.endsWith('/') ? [listed] : [listed, `${listed}/`];
      for (const path of forms) {
        results.push({ module, app: name, path, ...(await send(apps[name], path)) });
      }
    }
  }
  if (group.throws) {
    const express = require('express');
    for (const pattern of group.throws.patterns) {
      let error = null;
      try {
        express.Router().get(pattern, () => {});
      } catch (e) {
        error = String(e.message).split(';')[0];
      }
      throws.push({ module: `${group.dir}/${group.throws.module}`, pattern, threw: error !== null, error });
    }
  }
}

// packageVersion reads the version of the package whose entry file is
// entry (a package's exports map may hide its package.json).
function packageVersion(entry, name) {
  const marker = `/node_modules/${name}/`;
  const dir = entry.slice(0, entry.lastIndexOf(marker) + marker.length);
  return JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8')).version;
}

async function send(app, path) {
  const server = createServer(app);
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  const response = await fetch(`http://127.0.0.1:${port}${path}`);
  const body = response.headers.get('content-type')?.includes('json') ? await response.json() : {};
  await new Promise((resolve) => server.close(resolve));
  return { status: response.status, route: body.route ?? null };
}
writeFileSync(out, JSON.stringify({ versions, results, throws }, null, 2) + '\n');
