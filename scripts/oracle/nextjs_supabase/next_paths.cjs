// Next.js route template oracle for T024a (TestNextRoutesOracle). Reads
// app-directory-relative route file paths (one per line, e.g.
// "api/(shop)/cart/[...slug]/route.ts") on stdin and prints, per path, the
// template Next.js itself derives: the private-folder filter transcribed
// from route-discovery.js, then the package's own normalizeAppPath and
// getRouteRegex groups, mapped onto Graphvein's template spelling. Pure
// path helpers only (next/dist/shared/lib); nothing is built or run.
//
// usage: node next_paths.cjs <next-package-dir>/dist < paths.txt > out.jsonl
'use strict';
const fs = require('fs');
const path = require('path');

const dist = process.argv[2];
const appPaths = require(path.join(dist, 'shared/lib/router/utils/app-paths'));
const routeRegex = require(path.join(dist, 'shared/lib/router/utils/route-regex'));

function page(rel) {
  let p = '/' + rel.replace(/\.(tsx|ts|jsx|js)$/, '');
  if (p.endsWith('/index')) p = p.slice(0, -6);
  return p === '' ? '/' : p;
}

for (const rel of fs.readFileSync(0, 'utf8').split('\n').filter(Boolean)) {
  if (rel.split('/').some((p) => p.startsWith('_'))) {
    console.log(JSON.stringify({ path: rel, private: true }));
    continue;
  }
  try {
    const route = appPaths.normalizeAppPath(page(rel));
    const groups = routeRegex.getRouteRegex(route).groups;
    let optional = false;
    const segs = route.split('/').filter(Boolean).map((s) => {
      const m = /^\[\[?(?:\.\.\.)?([^\]]+)\]\]?$/.exec(s);
      if (!m) return s;
      const g = groups[m[1]];
      if (!g) return '?' + s;
      if (g.optional) optional = true;
      return g.repeat ? `{*${m[1]}}` : `{${m[1]}}`;
    });
    console.log(JSON.stringify({ path: rel, template: '/' + segs.join('/'), optional }));
  } catch (e) {
    console.log(JSON.stringify({ path: rel, error: String(e.message).slice(0, 200) }));
  }
}
