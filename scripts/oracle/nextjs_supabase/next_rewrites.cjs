// Next.js rewrite oracle for T024a2 (TestNextRewritesOracle). Reads JSON
// lines {source, destination, path} on stdin (source and destination
// already prefixed with the base path, path a literal request path) and
// prints, per line, what the `next` package's own helpers do with them:
// getPathMatch (as filesystem.js builds custom routes: strict, unnamed
// parameters removed) and prepareDestination (as resolve-routes.js calls
// it). Pure helpers only (next/dist/shared/lib); nothing is built or run.
//
// usage: node next_rewrites.cjs <next-package-dir>/dist < cases.jsonl > out.jsonl
'use strict';
const fs = require('fs');
const path = require('path');
const Module = require('module');

const dist = path.resolve(process.argv[2]);
// The helpers require `next/dist/...`; resolve that to this package.
const resolve = Module._resolveFilename;
Module._resolveFilename = function (request, ...rest) {
  if (request.startsWith('next/dist/')) {
    request = path.join(dist, request.slice('next/dist/'.length));
  }
  return resolve.call(this, request, ...rest);
};
const { getPathMatch } = require(path.join(dist, 'shared/lib/router/utils/path-match'));
const { prepareDestination } = require(path.join(dist, 'shared/lib/router/utils/prepare-destination'));

for (const line of fs.readFileSync(0, 'utf8').split('\n').filter(Boolean)) {
  const c = JSON.parse(line);
  const out = { source: c.source, destination: c.destination, path: c.path };
  try {
    const match = getPathMatch(c.source, { strict: true, removeUnnamedParams: true });
    const params = match(c.path);
    out.matched = Boolean(params);
    if (params) {
      const { parsedDestination } = prepareDestination({ appendParamsToQuery: true, destination: c.destination, params, query: {} });
      out.external = Boolean(parsedDestination.protocol);
      out.pathname = parsedDestination.pathname;
    }
  } catch (e) {
    out.error = String(e.message).slice(0, 200);
  }
  console.log(JSON.stringify(out));
}
