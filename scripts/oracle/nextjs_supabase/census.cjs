// The T024 investigation census (scratch only). Reads a corpus checkout with the
// TypeScript 6.0.3 compiler API and the `next` package's own path helpers
// (pure modules of next/dist/shared/lib, at the corpus's locked version);
// never builds or runs corpus code.
//
// usage: node census.cjs <corpus-name> <checkout> <next-dist-dir> <graphvein.ndjson> <pgtables.json|-> > out.jsonl
'use strict';
const fs = require('fs');
const path = require('path');
const cp = require('child_process');
const ts = require(process.env.TS_DIR + '/node_modules/typescript');

const [corpus, root, nextDist, ndjsonPath, pgPath] = process.argv.slice(2);
const appPaths = require(path.join(nextDist, 'shared/lib/router/utils/app-paths'));
const routeRegex = require(path.join(nextDist, 'shared/lib/router/utils/route-regex'));
const sortedRoutes = require(path.join(nextDist, 'shared/lib/router/utils/sorted-routes'));
const pgTables = pgPath && pgPath !== '-' ? JSON.parse(fs.readFileSync(pgPath, 'utf8')) : null;

const out = [];
const emit = (r) => out.push(Object.assign({ corpus }, r));

const files = cp.execFileSync('git', ['-C', root, 'ls-files', '-z'], { maxBuffer: 1 << 28 })
  .toString().split('\0').filter(Boolean);
const fileSet = new Set(files);
const isCode = (f) => /\.(ts|tsx|js|jsx|mjs|cjs|mts|cts)$/.test(f) && !/\.d\.[mc]?ts$/.test(f);
const PAGE_EXT = ['tsx', 'ts', 'jsx', 'js'];

// --- Next projects: a directory with a next.config.* file.
const projects = files.filter((f) => /(^|\/)next\.config\.(js|mjs|ts|cjs|mts)$/.test(f)).map((f) => path.posix.dirname(f));
function dirExists(d) { const p = d + '/'; return files.some((f) => f.startsWith(p)); }
function appDirOf(p) {
  const pre = p === '.' ? '' : p + '/';
  if (dirExists(pre + 'app')) return pre + 'app';
  if (dirExists(pre + 'src/app')) return pre + 'src/app';
  return null;
}
function pagesDirOf(p) {
  const pre = p === '.' ? '' : p + '/';
  if (dirExists(pre + 'pages')) return pre + 'pages';
  if (dirExists(pre + 'src/pages')) return pre + 'src/pages';
  return null;
}

// --- Next's own view (oracle side): transcribed getPageFromPath and the
// ignorePartFilter of route-discovery.js, then the package's normalizeAppPath.
function getPageFromPath(pagePath) {
  let page = pagePath;
  for (const ext of [...PAGE_EXT].sort((a, b) => b.length - a.length)) {
    if (page.endsWith('.' + ext)) { page = page.slice(0, -(ext.length + 1)); break; }
  }
  if (page.endsWith('/index')) page = page.slice(0, -6);
  return page === '' ? '/' : page;
}
function nextAppRoute(rel) {
  const parts = rel.split('/');
  if (parts.some((p) => p.startsWith('_'))) return { privateFolder: true };
  return { route: appPaths.normalizeAppPath(getPageFromPath('/' + rel)) };
}
function nextTemplate(route) {
  // Next's own parameter reading (getRouteRegex groups) mapped onto
  // Graphvein's template spelling.
  const segs = route.split('/').filter(Boolean);
  const groups = routeRegex.getRouteRegex(route).groups;
  let optional = false;
  const t = segs.map((s) => {
    const m = /^\[\[?(?:\.\.\.)?([^\]]+)\]\]?$/.exec(s);
    if (!m) return s;
    const g = groups[m[1]];
    if (!g) return '?' + s;
    if (g.optional) optional = true;
    return g.repeat ? `{*${m[1]}}` : `{${m[1]}}`;
  });
  return { template: '/' + t.join('/'), optional };
}
// --- Graphvein's proposed rule (NX-P rows), written independently.
function proposedTemplate(rel) {
  const dirs = rel.split('/').slice(0, -1);
  const out = []; let optional = false; const forms = new Set();
  for (const d of dirs) {
    if (d.startsWith('_')) return { d: 'private_folder' };
    if (/^\((\.{1,3}|\.\.\)\(\.\.)\)/.test(d)) return { d: 'intercepting_segment' };
    if (/^\(.*\)$/.test(d)) { forms.add('group'); continue; }
    if (d.startsWith('@')) { forms.add('parallel_slot'); continue; }
    let m;
    if ((m = /^\[\[\.\.\.([A-Za-z_$][\w$]*)\]\]$/.exec(d))) { out.push(`{*${m[1]}}`); optional = true; forms.add('optional_catch_all'); continue; }
    if ((m = /^\[\.\.\.([A-Za-z_$][\w$]*)\]$/.exec(d))) { out.push(`{*${m[1]}}`); forms.add('catch_all'); continue; }
    if ((m = /^\[([A-Za-z_$][\w$-]*)\]$/.exec(d))) { out.push(`{${m[1]}}`); forms.add('dynamic'); continue; }
    if (/[\[\]]/.test(d)) return { d: 'bracket_unrecognised' };
    if (d.includes('%')) return { d: 'percent_segment' };
    out.push(d);
  }
  return { template: '/' + out.join('/'), optional, forms: [...forms].sort() };
}

// --- TypeScript program per project (or per repo when no project).
function tsconfigFor(dir) {
  for (let d = dir; ; d = path.posix.dirname(d)) {
    const c = (d === '.' ? '' : d + '/') + 'tsconfig.json';
    if (fileSet.has(c)) return c;
    const j = (d === '.' ? '' : d + '/') + 'jsconfig.json';
    if (fileSet.has(j)) return j;
    if (d === '.' || d === '/' || d === '') return null;
  }
}
const programs = new Map();
function programFor(file) {
  const cfg = tsconfigFor(path.posix.dirname(file)) || '';
  if (programs.has(cfg)) return programs.get(cfg);
  let options = { allowJs: true, jsx: ts.JsxEmit.Preserve, noResolve: false, moduleResolution: ts.ModuleResolutionKind.Bundler, module: ts.ModuleKind.ESNext };
  if (cfg) {
    const parsed = ts.getParsedCommandLineOfConfigFile(path.join(root, cfg), {}, Object.assign({}, ts.sys, { onUnRecoverableConfigFileDiagnostic: () => {} }));
    if (parsed) options = Object.assign(parsed.options, { allowJs: true, noEmit: true, types: [] });
  }
  const base = cfg ? path.posix.dirname(cfg) : '.';
  const roots = files.filter((f) => isCode(f) && (base === '.' || f.startsWith(base + '/'))).map((f) => path.join(root, f));
  const program = ts.createProgram({ rootNames: roots, options });
  const entry = { program, checker: program.getTypeChecker() };
  programs.set(cfg, entry);
  return entry;
}
const rel = (abs) => path.relative(root, abs).split(path.sep).join('/');
function sfOf(file) { const { program, checker } = programFor(file); return { sf: program.getSourceFile(path.join(root, file)), checker, program }; }
function line(sf, node) { return sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1; }
function directives(stmts) {
  const ds = [];
  for (const s of stmts) {
    if (ts.isExpressionStatement(s) && ts.isStringLiteral(s.expression)) ds.push(s.expression.text); else break;
  }
  return ds;
}
const VERBS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];
function hasMod(n, k) { return (ts.getCombinedModifierFlags ? (n.modifiers || []).some((m) => m.kind === k) : false); }
function exportsOf(sf) {
  // name -> form
  const ex = {};
  for (const s of sf.statements) {
    const exported = (s.modifiers || []).some((m) => m.kind === ts.SyntaxKind.ExportKeyword);
    const isDefault = (s.modifiers || []).some((m) => m.kind === ts.SyntaxKind.DefaultKeyword);
    if (ts.isFunctionDeclaration(s) && exported) {
      const async = (s.modifiers || []).some((m) => m.kind === ts.SyntaxKind.AsyncKeyword);
      ex[isDefault ? 'default' : s.name && s.name.text] = { form: async ? 'async_function' : 'function', line: line(sf, s), node: s };
    } else if (ts.isVariableStatement(s) && exported) {
      for (const d of s.declarationList.declarations) {
        if (ts.isIdentifier(d.name)) {
          let init = d.initializer; let form = 'const_other';
          while (init && (ts.isParenthesizedExpression(init) || ts.isAsExpression(init) || ts.isSatisfiesExpression(init))) init = init.expression;
          if (init && (ts.isArrowFunction(init) || ts.isFunctionExpression(init))) {
            form = (init.modifiers || []).some((m) => m.kind === ts.SyntaxKind.AsyncKeyword) ? 'const_async_arrow' : 'const_arrow';
          } else if (init && ts.isCallExpression(init)) form = 'const_call';
          else if (init && ts.isIdentifier(init)) form = 'const_alias';
          else if (init && (ts.isStringLiteral(init) || ts.isNumericLiteral(init) || init.kind === ts.SyntaxKind.TrueKeyword || init.kind === ts.SyntaxKind.FalseKeyword)) form = 'const_literal';
          ex[d.name.text] = { form, line: line(sf, d), node: d };
        } else {
          // export const { handlers: { GET, POST } } = NextAuth(...)
          const names = [];
          const walk = (b) => { if (ts.isIdentifier(b)) names.push(b.text); else if (b.elements) b.elements.forEach((e) => walk(e.name || e)); };
          walk(d.name);
          for (const n of names) ex[n] = { form: 'const_destructure', line: line(sf, d), node: d };
        }
      }
    } else if (ts.isExportDeclaration(s)) {
      if (!s.exportClause) { ex['*' + (s.moduleSpecifier ? s.moduleSpecifier.text : '')] = { form: 'export_star', line: line(sf, s) }; continue; }
      if (ts.isNamedExports(s.exportClause)) {
        for (const e of s.exportClause.elements) {
          ex[e.name.text] = { form: s.moduleSpecifier ? 'reexport_from' : 'export_specifier', line: line(sf, s), node: e, from: s.moduleSpecifier && s.moduleSpecifier.text };
        }
      }
    } else if (ts.isExportAssignment(s)) {
      ex['default'] = { form: 'export_default_expr', line: line(sf, s) };
    } else if (ts.isClassDeclaration(s) && exported) {
      ex[s.name ? s.name.text : 'default'] = { form: 'class', line: line(sf, s) };
    }
  }
  return ex;
}

// --- graphvein output
const gv = { nodes: [], byFileName: new Map(), callsIn: new Map(), clientCalls: [] };
for (const l of fs.readFileSync(ndjsonPath, 'utf8').split('\n')) {
  if (!l) continue; const r = JSON.parse(l);
  if (r.type === 'node') {
    gv.byFileName.set(r.file + '#' + r.name, r);
    if (r.kind === 'client_call') gv.clientCalls.push(r);
  } else if (r.type === 'edge' && r.kind === 'calls') {
    gv.callsIn.set(r.target, (gv.callsIn.get(r.target) || 0) + 1);
  }
}

// ===== A. route handlers and pages API
const routes = [];
for (const p of projects) {
  const appDir = appDirOf(p);
  const pagesDir = pagesDirOf(p);
  const appAll = appDir ? files.filter((f) => f.startsWith(appDir + '/') && /\/(route|page)\.(tsx|ts|jsx|js)$/.test('/' + f.slice(appDir.length + 1))) : [];
  const nextRoutes = [];
  for (const f of appAll) {
    const r = f.slice(appDir.length + 1);
    const isRoute = /(^|\/)route\.(tsx|ts|jsx|js)$/.test(r);
    const nx = nextAppRoute(r);
    if (!nx.privateFolder) nextRoutes.push(nx.route);
    if (!isRoute) continue;
    const prop = proposedTemplate(r);
    const rec = { kind: 'route_handler', project: p, file: f, app_dir: appDir };
    if (nx.privateFolder) { rec.next = 'private_folder_excluded'; } else {
      rec.next_route = nx.route;
      const nt = nextTemplate(nx.route);
      rec.next_template = nt.template; rec.next_optional = nt.optional;
    }
    if (prop.d) rec.proposed = { d: prop.d }; else rec.proposed = prop;
    rec.agree = prop.d ? (prop.d === 'private_folder' && nx.privateFolder) : (!nx.privateFolder && prop.template === rec.next_template && prop.optional === rec.next_optional);
    const { sf } = sfOf(f);
    const ex = exportsOf(sf);
    rec.methods = {}; rec.other_exports = [];
    for (const [n, e] of Object.entries(ex)) {
      if (VERBS.includes(n)) rec.methods[n] = e.form + (e.from ? ' ' + e.from : '');
      else rec.other_exports.push(n + ':' + e.form);
    }
    rec.directives = directives(sf.statements);
    const fnode = gv.byFileName.get(f + '#' + path.posix.basename(f));
    rec.main_emits = 'file node; declarations; no route node (Next.js routing not modelled)';
    routes.push(rec);
    emit(rec);
  }
  if (appDir) {
    let conflict = null;
    try { sortedRoutes.getSortedRoutes([...new Set(nextRoutes)]); } catch (e) { conflict = String(e.message).slice(0, 200); }
    emit({ kind: 'project', project: p, app_dir: appDir, pages_dir: pagesDir, app_routes: nextRoutes.length, sorted_routes_error: conflict });
  } else emit({ kind: 'project', project: p, app_dir: null, pages_dir: pagesDir });
  if (pagesDir) {
    for (const f of files.filter((f) => f.startsWith(pagesDir + '/api/') && /\.(tsx|ts|jsx|js)$/.test(f))) {
      const r = '/' + f.slice(pagesDir.length + 1);
      const { sf } = sfOf(f);
      const ex = exportsOf(sf);
      emit({ kind: 'pages_api', project: p, file: f, next_route: getPageFromPath(r), default_export: ex.default ? ex.default.form : null, exports: Object.keys(ex).sort(), main_emits: 'file node; no route node' });
    }
  }
}

// ===== B. server actions
const actionDecls = new Map(); // declaration node -> record
const fileDirectives = new Map();
for (const f of files.filter(isCode)) {
  const { sf } = sfOf(f);
  if (!sf) continue;
  const ds = directives(sf.statements);
  fileDirectives.set(f, ds);
  if (ds.includes('use server')) {
    const ex = exportsOf(sf);
    const names = Object.keys(ex).sort();
    emit({ kind: 'action_module', file: f, exports: names.length, client_file: ds.includes('use client') });
    for (const [n, e] of Object.entries(ex)) {
      const rec = { kind: 'server_action', scope: 'module', file: f, name: n, line: e.line, form: e.form + (e.from ? ' ' + e.from : '') };
      const g = gv.byFileName.get(f + '#' + n);
      rec.main_node = g ? g.kind : null;
      rec.main_calls_in = g ? (gv.callsIn.get(g.identity) || 0) : 0;
      rec.callers = [];
      if (e.node) actionDecls.set(e.node, rec);
      emit(rec);
    }
  }
  const visit = (node) => {
    if ((ts.isFunctionDeclaration(node) || ts.isFunctionExpression(node) || ts.isArrowFunction(node) || ts.isMethodDeclaration(node)) && node.body && ts.isBlock(node.body)) {
      const ds2 = directives(node.body.statements);
      if (ds2.includes('use server')) {
        let name = node.name && node.name.text; let declNode = node;
        if (!name && node.parent && ts.isVariableDeclaration(node.parent) && ts.isIdentifier(node.parent.name)) { name = node.parent.name.text; declNode = node.parent; }
        const async = (node.modifiers || []).some((m) => m.kind === ts.SyntaxKind.AsyncKeyword);
        const rec = { kind: 'server_action', scope: 'inline', file: f, name: name || null, line: line(sf, node), form: (ts.isArrowFunction(node) ? 'arrow' : ts.isMethodDeclaration(node) ? 'method' : 'function') + (async ? '_async' : '_sync') + (name ? '' : '_anonymous'), in_client_file: ds.includes('use client') };
        const g = name && gv.byFileName.get(f + '#' + name);
        rec.main_node = g ? g.kind : null; rec.callers = [];
        actionDecls.set(declNode, rec);
        emit(rec);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}

// callers: every identifier whose (alias-followed) symbol declares an action
function declOfIdent(checker, id) {
  let s = checker.getSymbolAtLocation(id);
  if (!s) return null;
  if (s.flags & ts.SymbolFlags.Alias) { try { s = checker.getAliasedSymbol(s); } catch (e) { return null; } }
  return (s.declarations || [])[0] || null;
}
function exportSpecTarget(checker, decl) {
  // export { x } / export { x as y } inside a 'use server' file: map to local decl
  return decl;
}
const actionByLocalDecl = new Map();
for (const [decl, rec] of actionDecls) {
  actionByLocalDecl.set(decl, rec);
  if (ts.isExportSpecifier(decl)) {
    const { checker } = sfOf(rec.file);
    const local = checker.getExportSpecifierLocalTargetSymbol(decl);
    if (local && local.declarations) for (const d of local.declarations) actionByLocalDecl.set(d, rec);
  }
}
const callerRecs = [];
for (const f of files.filter(isCode)) {
  const { sf, checker } = sfOf(f);
  if (!sf) continue;
  const client = (fileDirectives.get(f) || []).includes('use client');
  const visit = (node) => {
    if (ts.isIdentifier(node) && node.parent && !(ts.isImportSpecifier(node.parent) || ts.isImportClause(node.parent) || ts.isExportSpecifier(node.parent) || (ts.isVariableDeclaration(node.parent) && node.parent.name === node) || (ts.isFunctionDeclaration(node.parent) && node.parent.name === node) || (ts.isPropertyAccessExpression(node.parent) && node.parent.name === node) || ts.isPropertyAssignment(node.parent) && node.parent.name === node)) {
      const d = declOfIdent(checker, node);
      const rec = d && actionByLocalDecl.get(d);
      if (rec) {
        const p = node.parent;
        let form = 'value_flow';
        if (ts.isJsxExpression(p) && p.parent && ts.isJsxAttribute(p.parent)) {
          const an = p.parent.name.getText(sf);
          form = an === 'action' ? 'jsx_action' : an === 'formAction' ? 'jsx_formAction' : 'jsx_prop:' + an;
        } else if (ts.isCallExpression(p) && p.expression === node) form = 'direct_call';
        else if (ts.isCallExpression(p) && p.arguments[0] === node) {
          const cal = p.expression.getText(sf);
          form = /(^|\.)useActionState$/.test(cal) ? 'useActionState' : /(^|\.)useFormState$/.test(cal) ? 'useFormState' : 'argument:' + cal.slice(0, 40);
        } else if (ts.isCallExpression(p) && p.arguments.includes(node)) form = 'argument:' + p.expression.getText(sf).slice(0, 40);
        else if (ts.isPropertyAccessExpression(p) && p.expression === node) form = 'member:' + p.name.text;
        else if (ts.isShorthandPropertyAssignment(p)) form = 'object_shorthand';
        else if (ts.isAwaitExpression(p)) form = 'await';
        const c = { kind: 'action_use', file: f, line: line(sf, node), action: rec.file + '#' + rec.name, form, client_file: client, same_file: f === rec.file };
        rec.callers.push(form);
        callerRecs.push(c);
        emit(c);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}

// ===== C. client calls to own route handlers
function matchRoute(template, method) {
  if (!template) return [];
  const cs = template.split('/').filter(Boolean);
  const hits = [];
  for (const r of routes) {
    if (!r.proposed || !r.proposed.template) continue;
    const rs = r.proposed.template.split('/').filter(Boolean);
    let ok = true; let i = 0;
    for (; i < rs.length; i++) {
      if (rs[i].startsWith('{*')) { ok = cs.length > i || r.proposed.optional; i = cs.length; break; }
      if (i >= cs.length) { ok = false; break; }
      if (rs[i].startsWith('{')) continue;
      if (rs[i] !== cs[i]) { ok = false; break; }
    }
    if (ok && i === cs.length && (rs.length <= cs.length || rs[rs.length - 1].startsWith('{*'))) {
      const m = method && r.methods[method] ? 'method_exported' : method === 'HEAD' && r.methods.GET ? 'head_via_get' : 'method_missing';
      hits.push(r.file + ' ' + r.proposed.template + ' ' + m);
    }
  }
  return hits;
}
for (const c of gv.clientCalls) {
  const a = c.attributes || {};
  const temps = (a.paths || []).map((p) => p.template);
  const hits = [].concat(...temps.map((t) => matchRoute(t, a.method)));
  emit({ kind: 'client_call', file: c.file, line: c.span ? c.span.start_line : null, library: a.library, method: a.method, status_main: a.status, templates: temps, base: a.base, url_expression: (a.url_expression || '').slice(0, 80), own_route_hits: hits });
}
// literal '/api/...' strings handed to non-fetch calls (SWR keys, wrappers)
for (const f of files.filter(isCode)) {
  const { sf } = sfOf(f);
  if (!sf) continue;
  const visit = (node) => {
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && /^\/api\//.test(node.text) && node.parent && ts.isCallExpression(node.parent) && node.parent.arguments[0] === node) {
      const callee = node.parent.expression.getText(sf);
      if (!/^(fetch|window\.fetch|axios\.\w+)$/.test(callee)) {
        const t = node.text.split('?')[0].replace(/\/+$/, '');
        emit({ kind: 'url_literal_call', file: f, line: line(sf, node), callee: callee.slice(0, 40), url: node.text, own_route_hits: matchRoute(t, null) });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}

// ===== D. supabase
const SB_FACTORIES = {
  '@supabase/supabase-js': ['createClient'],
  '@supabase/ssr': ['createBrowserClient', 'createServerClient'],
  '@supabase/auth-helpers-nextjs': ['createClientComponentClient', 'createServerComponentClient', 'createRouteHandlerClient', 'createMiddlewareClient', 'createServerActionClient', 'createPagesBrowserClient', 'createPagesServerClient'],
};
function importOf(checker, id) {
  // returns {module, name} when id resolves to an import of an external module
  const s = checker.getSymbolAtLocation(id);
  if (!s || !s.declarations) return null;
  const d = s.declarations[0];
  if (ts.isImportSpecifier(d)) {
    const mod = d.parent.parent.parent.moduleSpecifier.text;
    return { module: mod, name: (d.propertyName || d.name).text };
  }
  return null;
}
function strip(e) {
  while (e && (ts.isParenthesizedExpression(e) || ts.isAwaitExpression(e) || ts.isNonNullExpression(e) || ts.isAsExpression(e))) e = e.expression;
  return e;
}
// classify an expression as a supabase client: returns {how, depth} or {d: reason}
function classifyClient(checker, sf, expr, depth, seen) {
  expr = strip(expr);
  if (!expr) return { d: 'none' };
  if (depth > 3) return { d: 'depth' };
  if (ts.isCallExpression(expr)) {
    const callee = strip(expr.expression);
    if (ts.isIdentifier(callee)) {
      const imp = importOf(checker, callee);
      if (imp && SB_FACTORIES[imp.module] && SB_FACTORIES[imp.module].includes(imp.name)) return { how: 'factory:' + imp.module + '.' + imp.name };
      const d = declOfIdent(checker, callee);
      if (d && !seen.has(d)) {
        seen.add(d);
        let fn = d;
        if (ts.isVariableDeclaration(d)) fn = strip(d.initializer);
        if (fn && (ts.isFunctionDeclaration(fn) || ts.isArrowFunction(fn) || ts.isFunctionExpression(fn))) {
          const rets = [];
          if (!ts.isBlock(fn.body)) rets.push(fn.body);
          else { const w = (n) => { if (ts.isReturnStatement(n)) rets.push(n.expression); if (!ts.isFunctionLike(n)) ts.forEachChild(n, w); }; ts.forEachChild(fn.body, w); }
          const cs = rets.map((r) => classifyClient(checker, fn.getSourceFile(), r, depth + 1, seen));
          if (cs.length && cs.every((c) => c.how)) return { how: 'wrapper(' + cs[0].how + ')', wrapper: rel(fn.getSourceFile().fileName) };
          return { d: 'wrapper_unproven' };
        }
        if (imp) return { d: 'external_call:' + imp.module };
      }
      return { d: 'call_unresolved' };
    }
    return { d: 'call_other' };
  }
  if (ts.isIdentifier(expr)) {
    const d = declOfIdent(checker, expr);
    if (!d) return { d: 'ident_unresolved' };
    if (seen.has(d)) return { d: 'cycle' };
    seen.add(d);
    if (ts.isVariableDeclaration(d)) {
      if (!ts.isIdentifier(d.name)) return { d: 'destructured' };
      const flags = ts.getCombinedNodeFlags(d.parent);
      if (!(flags & ts.NodeFlags.Const)) {
        return { d: 'let_or_var' };
      }
      if (!d.initializer) return { d: 'no_initializer' };
      const c = classifyClient(checker, d.getSourceFile(), d.initializer, depth + 1, seen);
      if (c.how) return { how: 'const ' + c.how, imported: d.getSourceFile() !== sf };
      return c;
    }
    if (ts.isBindingElement(d)) return { d: 'destructured' };
    if (ts.isParameter(d)) {
      const t = d.type ? d.type.getText(d.getSourceFile()) : '';
      return { d: /SupabaseClient/.test(t) ? 'param_typed_SupabaseClient' : 'param:' + t.slice(0, 30) };
    }
    return { d: 'decl:' + ts.SyntaxKind[d.kind] };
  }
  if (ts.isPropertyAccessExpression(expr)) return { d: 'member:' + expr.getText(sf).slice(0, 30) };
  return { d: 'expr:' + ts.SyntaxKind[expr.kind] };
}
const READ_OPS = new Set(['select']);
const WRITE_OPS = new Set(['insert', 'update', 'upsert', 'delete']);
for (const f of files.filter(isCode)) {
  const { sf, checker } = sfOf(f);
  if (!sf) continue;
  const text = sf.text;
  if (!/supabase|\.from\(|\.rpc\(/.test(text)) continue;
  const visit = (node) => {
    if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression)) {
      const m = node.expression.name.text;
      const recv = node.expression.expression;
      if (m === 'from' || m === 'rpc') {
        // receiver forms: <c>.from, <c>.schema('s').from, <c>.storage.from
        let r = strip(recv); let schema = null; let surface = 'db';
        if (ts.isCallExpression(r) && ts.isPropertyAccessExpression(r.expression) && r.expression.name.text === 'schema') {
          const a = r.arguments[0]; schema = a && ts.isStringLiteral(a) ? a.text : '<dynamic>'; r = strip(r.expression.expression);
        }
        if (ts.isPropertyAccessExpression(r) && r.name.text === 'storage') { surface = 'storage'; r = strip(r.expression); }
        const c = classifyClient(checker, sf, r, 0, new Set());
        // Only sites whose receiver looks supabase-ish or is proven
        const recvText = r.getText(sf);
        const looks = /supabase/i.test(recvText) || c.how;
        if (!looks) { ts.forEachChild(node, visit); return; }
        const a0 = node.arguments[0];
        const name = a0 && (ts.isStringLiteral(a0) || ts.isNoSubstitutionTemplateLiteral(a0)) ? a0.text : null;
        // chain ops above
        const ops = []; let cur = node; let selectArg = null;
        while (cur.parent && ts.isPropertyAccessExpression(cur.parent) && cur.parent.parent && ts.isCallExpression(cur.parent.parent)) {
          const op = cur.parent.name.text; ops.push(op);
          if (op === 'select' && selectArg === null) { const sa = cur.parent.parent.arguments[0]; selectArg = sa ? (ts.isStringLiteral(sa) || ts.isNoSubstitutionTemplateLiteral(sa) ? sa.text : '<dynamic>') : ''; }
          cur = cur.parent.parent;
        }
        const first = ops.find((o) => READ_OPS.has(o) || WRITE_OPS.has(o)) || null;
        const rec = { kind: 'supabase_site', file: f, line: line(sf, node), method: m, surface, schema, name, name_expr: name === null && a0 ? a0.getText(sf).slice(0, 40) : undefined, receiver: recvText.slice(0, 40), client: c.how || null, client_d: c.d || null, client_imported: !!c.imported, ops: ops.slice(0, 8), op: first };
        if (selectArg !== null) rec.select = selectArg.length > 80 ? selectArg.slice(0, 80) + '...' : selectArg;
        rec.embedded = selectArg && selectArg !== '<dynamic>' ? /\(/.test(selectArg) : false;
        if (pgTables && surface === 'db' && m === 'from' && name) {
          const key = (schema || 'public') + '.' + name;
          rec.pg = pgTables.tables.includes(key) ? 'table' : pgTables.views.includes(key) ? 'view' : 'absent';
        }
        if (pgTables && m === 'rpc' && name) rec.pg = pgTables.functions.includes((schema || 'public') + '.' + name) ? 'function' : 'absent';
        rec.main_emits = 'nothing (no Supabase rows; D)';
        emit(rec);
      }
      if (ts.isPropertyAccessExpression(recv) && recv.name.text === 'auth' && /supabase/i.test(recv.expression.getText(sf))) {
        emit({ kind: 'supabase_auth', file: f, line: line(sf, node), method: m });
      }
      if (m === 'channel' && /supabase/i.test(recv.getText(sf))) emit({ kind: 'supabase_channel', file: f, line: line(sf, node) });
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}

for (const r of out) process.stdout.write(JSON.stringify(r) + '\n');
