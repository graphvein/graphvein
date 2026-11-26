// Server action oracle of TestServerActionsOracle (T024b, opt-in; scratch
// only). Reads a checkout with the TypeScript 6.0.3 compiler API (install
// as in scripts/oracle/ts_structure/README.md, point TS_DIR at it) and
// prints one JSON object: the actions the compiler's reading proves (the
// directive prologue of each file and function, the export names of
// 'use server' modules) and every use of an action resolved through the
// checker's alias resolution. Never builds or runs checkout code.
//
// usage: TS_DIR=<install> node server_actions.cjs <checkout> > out.json
'use strict';
const fs = require('fs');
const path = require('path');
const ts = require(process.env.TS_DIR + '/node_modules/typescript');

const root = path.resolve(process.argv[2]);
const files = [];
(function walk(dir) {
  for (const e of fs.readdirSync(path.join(root, dir), { withFileTypes: true })) {
    if (e.name === 'node_modules' || e.name === '.git') continue;
    const rel = dir ? dir + '/' + e.name : e.name;
    if (e.isDirectory()) walk(rel);
    else if (e.isFile()) files.push(rel);
  }
})('');
files.sort();
const fileSet = new Set(files);
const isCode = (f) => /\.(ts|tsx|js|jsx|mjs|cjs|mts|cts)$/.test(f) && !/\.d\.[mc]?ts$/.test(f);

// Next.js projects: a package.json declaring next (row NX-P1's test).
const projects = files.filter((f) => path.posix.basename(f) === 'package.json').filter((f) => {
  try {
    const m = JSON.parse(fs.readFileSync(path.join(root, f), 'utf8'));
    return (m.dependencies && 'next' in m.dependencies) || (m.devDependencies && 'next' in m.devDependencies);
  } catch (e) { return false; }
}).map((f) => path.posix.dirname(f));
const inProject = (f) => projects.some((p) => p === '.' || f.startsWith(p + '/'));

function tsconfigFor(dir) {
  for (let d = dir; ; d = path.posix.dirname(d)) {
    for (const name of ['tsconfig.json', 'jsconfig.json']) {
      const c = (d === '.' ? '' : d + '/') + name;
      if (fileSet.has(c)) return c;
    }
    if (d === '.' || d === '/' || d === '') return null;
  }
}
const programs = new Map();
function programFor(file) {
  const cfg = tsconfigFor(path.posix.dirname(file)) || '';
  if (programs.has(cfg)) return programs.get(cfg);
  let options = { allowJs: true, jsx: ts.JsxEmit.Preserve, moduleResolution: ts.ModuleResolutionKind.Bundler, module: ts.ModuleKind.ESNext };
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
function sfOf(file) { const { program, checker } = programFor(file); return { sf: program.getSourceFile(path.join(root, file)), checker }; }
const line = (sf, node) => sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;
function directives(stmts) {
  const ds = [];
  for (const s of stmts) {
    if (ts.isExpressionStatement(s) && ts.isStringLiteral(s.expression)) ds.push(s.expression.text); else break;
  }
  return ds;
}
const isAsync = (n) => (n.modifiers || []).some((m) => m.kind === ts.SyntaxKind.AsyncKeyword);
const has = (n, k) => (n.modifiers || []).some((m) => m.kind === k);

const actions = [];
const decls = new Map(); // declaration node -> action
for (const f of files.filter(isCode).filter(inProject)) {
  const { sf } = sfOf(f);
  if (!sf) continue;
  const ds = directives(sf.statements);
  const client = ds.includes('use client');
  if (ds.includes('use server') && !client) {
    const locals = new Map();
    for (const s of sf.statements) {
      if (ts.isFunctionDeclaration(s) && s.name) locals.set(s.name.text, s);
      if (ts.isVariableStatement(s)) for (const d of s.declarationList.declarations) if (ts.isIdentifier(d.name)) locals.set(d.name.text, d);
    }
    const imports = new Set();
    for (const s of sf.statements) {
      if (!ts.isImportDeclaration(s) || !s.importClause || s.importClause.isTypeOnly) continue;
      if (s.importClause.name) imports.add(s.importClause.name.text);
      const b = s.importClause.namedBindings;
      if (b && ts.isNamedImports(b)) for (const e of b.elements) if (!e.isTypeOnly) imports.add(e.name.text);
    }
    const add = (name, node, form) => {
      const a = { file: f, scope: 'module', name, form, line: line(sf, node) };
      actions.push(a);
      decls.set(node, a);
    };
    for (const s of sf.statements) {
      const exported = has(s, ts.SyntaxKind.ExportKeyword);
      if (ts.isFunctionDeclaration(s) && exported && isAsync(s) && !s.asteriskToken) {
        add(has(s, ts.SyntaxKind.DefaultKeyword) ? 'default' : s.name.text, s, 'async_function');
      } else if (ts.isVariableStatement(s) && exported && (s.declarationList.flags & ts.NodeFlags.Const)) {
        for (const d of s.declarationList.declarations) {
          let init = d.initializer;
          while (init && ts.isParenthesizedExpression(init)) init = init.expression;
          if (!ts.isIdentifier(d.name) || !init) continue;
          if (((ts.isArrowFunction(init) || ts.isFunctionExpression(init)) && isAsync(init)) || ts.isCallExpression(init)) add(d.name.text, d, 'const');
        }
      } else if (ts.isExportAssignment(s) && !s.isExportEquals) {
        // `export default <expression>` (server_actions.rs at next
        // 14.1.0 to 16.4.2, ExportDefaultExpr): an async arrow, a call
        // (a wrapper's result) or an identifier naming a local async
        // function, a const bound to one or to a call, or an import.
        let e = s.expression;
        while (ts.isParenthesizedExpression(e)) e = e.expression;
        if ((ts.isArrowFunction(e) || ts.isFunctionExpression(e)) && isAsync(e)) add('default', s, 'default_function');
        else if (ts.isCallExpression(e)) add('default', s, 'default_call');
        else if (ts.isIdentifier(e)) {
          const local = locals.get(e.text);
          if (!local) {
            if (imports.has(e.text)) add('default', s, 'imported_clause');
            continue;
          }
          let init = ts.isVariableDeclaration(local) ? local.initializer : local;
          while (init && ts.isParenthesizedExpression(init)) init = init.expression;
          const fn = init && (ts.isFunctionDeclaration(init) || ts.isArrowFunction(init) || ts.isFunctionExpression(init)) && isAsync(init);
          const constant = ts.isFunctionDeclaration(local) || (local.parent.flags & ts.NodeFlags.Const);
          if (constant && (fn || (init && ts.isCallExpression(init)))) {
            add('default', s, 'default_clause');
            decls.set(local, decls.get(s));
          }
        }
      } else if (ts.isExportDeclaration(s) && !s.moduleSpecifier && !s.isTypeOnly && s.exportClause && ts.isNamedExports(s.exportClause)) {
        for (const e of s.exportClause.elements) {
          if (e.isTypeOnly) continue;
          const local = locals.get((e.propertyName || e.name).text);
          if (!local) {
            // An imported binding exported again is a server reference
            // too (next-custom-transforms server_actions.rs, `export { foo }`).
            const a = { file: f, scope: 'module', name: e.name.text, form: 'imported_clause', line: line(sf, s) };
            actions.push(a);
            decls.set(e, a);
            continue;
          }
          let init = ts.isVariableDeclaration(local) ? local.initializer : local;
          while (init && ts.isParenthesizedExpression(init)) init = init.expression;
          const fn = init && (ts.isFunctionDeclaration(init) || ts.isArrowFunction(init) || ts.isFunctionExpression(init)) && isAsync(init);
          const constant = ts.isFunctionDeclaration(local) || (local.parent.flags & ts.NodeFlags.Const);
          if (constant && (fn || (init && ts.isCallExpression(init)))) {
            const a = { file: f, scope: 'module', name: e.name.text, form: 'clause', line: line(sf, s) };
            actions.push(a);
            decls.set(local, a);
            decls.set(e, a);
          }
        }
      }
    }
  }
  if (client) continue;
  const visit = (node) => {
    if ((ts.isFunctionDeclaration(node) || ts.isFunctionExpression(node) || ts.isArrowFunction(node)) && node.body && ts.isBlock(node.body) && isAsync(node) && directives(node.body.statements).includes('use server')) {
      let name = ts.isFunctionDeclaration(node) && node.name ? node.name.text : null;
      let declNode = node;
      const p = node.parent;
      // Next.js reads an inline action under any binding (const, let, var).
      if (!ts.isFunctionDeclaration(node) && p && ts.isVariableDeclaration(p) && p.initializer === node && ts.isIdentifier(p.name)) { name = p.name.text; declNode = p; }
      if (!decls.has(declNode) && !decls.has(node)) {
        const a = { file: f, scope: 'inline', name, form: 'inline', line: line(sf, node) };
        actions.push(a);
        decls.set(declNode, a);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}

// actionOf follows the identifier's symbol one alias step at a time and
// returns the first action a declaration on the way is, so an import
// through a 'use server' file's `export { a }` names that file's action
// while a direct import of the implementation does not.
function actionOf(checker, id) {
  let s = checker.getSymbolAtLocation(id);
  for (let i = 0; s && i < 16; i++) {
    for (const d of s.declarations || []) if (decls.get(d)) return decls.get(d);
    if (!(s.flags & ts.SymbolFlags.Alias)) return null;
    try { s = checker.getImmediateAliasedSymbol(s); } catch (e) { return null; }
  }
  return null;
}
// submits reports a JSX attribute that submits a form to its value
// (React form actions): action on an intrinsic form element or on
// next/form's default export, formAction on an intrinsic button or input
// element. On any other element it is a prop.
function submits(checker, attr) {
  const name = attr.name.getText();
  const el = attr.parent && attr.parent.parent;
  if (!el || !(ts.isJsxOpeningElement(el) || ts.isJsxSelfClosingElement(el))) return false;
  const tag = el.tagName;
  if (ts.isIdentifier(tag) && /^[a-z]/.test(tag.text)) {
    return (name === 'action' && tag.text === 'form') || (name === 'formAction' && (tag.text === 'button' || tag.text === 'input'));
  }
  if (name !== 'action' || !ts.isIdentifier(tag)) return false;
  const s = checker.getSymbolAtLocation(tag);
  for (const d of (s && s.declarations) || []) {
    if (ts.isImportClause(d) && d.name && ts.isStringLiteral(d.parent.moduleSpecifier) && d.parent.moduleSpecifier.text === 'next/form') return true;
  }
  return false;
}
const uses = [];
for (const f of files.filter(isCode)) {
  const { sf, checker } = sfOf(f);
  if (!sf) continue;
  const visit = (node) => {
    const p = node.parent;
    if (ts.isIdentifier(node) && p && !ts.isImportSpecifier(p) && !ts.isImportClause(p) && !ts.isExportSpecifier(p) && !(ts.isVariableDeclaration(p) && p.name === node) && !(ts.isFunctionDeclaration(p) && p.name === node) && !(ts.isPropertyAccessExpression(p) && p.name === node) && !(ts.isPropertyAssignment(p) && p.name === node) && !(ts.isParameter(p) && p.name === node)) {
      const a = actionOf(checker, node);
      if (a) {
        // A conditional alternative takes the form of the whole value.
        let v = node; let q = p; let conditional = false;
        while (q && (ts.isParenthesizedExpression(q) || (ts.isConditionalExpression(q) && q.condition !== v))) {
          conditional = conditional || ts.isConditionalExpression(q);
          v = q; q = q.parent;
        }
        let form = 'other';
        if (ts.isJsxExpression(q) && q.parent && ts.isJsxAttribute(q.parent)) {
          form = submits(checker, q.parent) ? 'form_action' : 'prop';
        } else if (ts.isCallExpression(q) && q.expression === v && !conditional) form = 'direct';
        else if (ts.isCallExpression(q) && q.arguments[0] === v && /(^|\.)(useActionState|useFormState)$/.test(q.expression.getText(sf))) form = 'action_state';
        uses.push({ file: f, line: line(sf, node), action: a.file + '#' + (a.name === null ? '@' + a.line : a.name), form, conditional });
      }
    }
    // An inline action written as the form's value.
    if (ts.isJsxAttribute(node) && node.initializer && ts.isJsxExpression(node.initializer) && node.initializer.expression) {
      const name = node.name.getText(sf);
      let v = node.initializer.expression;
      while (ts.isParenthesizedExpression(v)) v = v.expression;
      const a = submits(checker, node) && decls.get(v);
      if (a) uses.push({ file: f, line: line(sf, v), action: a.file + '#' + (a.name === null ? '@' + a.line : a.name), form: 'form_action', conditional: false });
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
}
// Module graphs of row SA-C3 (T024 review), computed with the
// compiler's own module resolution: the client graph follows the imports
// and re-exports the compiler keeps in its output (a side-effect import,
// a binding whose symbol is a value, `export *`); the server graph
// follows every import and re-export that is not type-only, and dynamic
// import() and require() of a string literal. A dynamic import or require
// the compiler cannot resolve to a checkout file, in a server-graph file,
// leaves the server graph open. A direct call is client when its file
// opens with 'use client', or is in the client graph and not in an open or
// reaching server graph.
const directivesOf = new Map();
for (const f of files.filter(isCode)) {
  const { sf } = sfOf(f);
  if (sf) directivesOf.set(f, directives(sf.statements));
}
const isClientFile = (f) => { const d = directivesOf.get(f) || []; return d.includes('use client') && !d.includes('use server'); };
const isServerFile = (f) => (directivesOf.get(f) || []).includes('use server');
function resolveIn(f, spec) {
  const { program } = programFor(f);
  const r = ts.resolveModuleName(spec, path.join(root, f), program.getCompilerOptions(), ts.sys).resolvedModule;
  if (!r || r.isExternalLibraryImport) return { external: !!r };
  const rel = path.relative(root, r.resolvedFileName).split(path.sep).join('/');
  return fileSet.has(rel) && isCode(rel) ? { file: rel } : { external: true };
}
const kept = new Map(); // file -> files the emitted module loads
const may = new Map(); // file -> files the module may load
const opens = new Map(); // file -> true when a dynamic load is not resolved
for (const f of files.filter(isCode)) {
  const { sf, checker } = sfOf(f);
  if (!sf) continue;
  const k = new Set(); const m = new Set();
  const valueBinding = (id) => {
    const sym = checker.getSymbolAtLocation(id);
    if (!sym) return true;
    try {
      const t = sym.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(sym) : sym;
      return !!(t.flags & ts.SymbolFlags.Value);
    } catch (e) { return true; }
  };
  for (const st of sf.statements) {
    if (ts.isImportDeclaration(st) && ts.isStringLiteral(st.moduleSpecifier)) {
      const c = st.importClause;
      if (c && c.isTypeOnly) continue;
      const r = resolveIn(f, st.moduleSpecifier.text);
      if (!r.file) continue;
      m.add(r.file);
      let keep = !c;
      if (c && c.name && valueBinding(c.name)) keep = true;
      const b = c && c.namedBindings;
      if (b && ts.isNamespaceImport(b)) keep = true;
      if (b && ts.isNamedImports(b)) for (const e of b.elements) if (!e.isTypeOnly && valueBinding(e.name)) keep = true;
      if (keep) k.add(r.file);
    } else if (ts.isExportDeclaration(st) && st.moduleSpecifier && ts.isStringLiteral(st.moduleSpecifier) && !st.isTypeOnly) {
      const r = resolveIn(f, st.moduleSpecifier.text);
      if (!r.file) continue;
      m.add(r.file);
      let keep = !st.exportClause || ts.isNamespaceExport(st.exportClause);
      if (st.exportClause && ts.isNamedExports(st.exportClause)) for (const e of st.exportClause.elements) if (!e.isTypeOnly && valueBinding(e.name)) keep = true;
      if (keep) k.add(r.file);
    }
  }
  const visitDyn = (n) => {
    if (ts.isCallExpression(n) && (n.expression.kind === ts.SyntaxKind.ImportKeyword || ts.isIdentifier(n.expression) && n.expression.text === 'require')) {
      const a = n.arguments[0];
      const r = a && ts.isStringLiteralLike(a) ? resolveIn(f, a.text) : {};
      if (r.file) m.add(r.file); else if (!r.external) opens.set(f, true);
    }
    ts.forEachChild(n, visitDyn);
  };
  visitDyn(sf);
  kept.set(f, k); may.set(f, m);
}
const serverEntry = (f) => projects.some((p) => {
  if (p !== '.' && !f.startsWith(p + '/')) return false;
  let rel = p === '.' ? f : f.slice(p.length + 1);
  if (rel.startsWith('src/')) rel = rel.slice(4);
  if (rel.startsWith('app/') || rel.startsWith('pages/')) return true;
  return !rel.includes('/') && /^(middleware|instrumentation|proxy)\./.test(rel);
});
const clientGraph = new Set();
const cq = files.filter((f) => kept.has(f) && isClientFile(f));
for (const f of cq) clientGraph.add(f);
while (cq.length) {
  const f = cq.shift();
  for (const g of kept.get(f) || []) if (!clientGraph.has(g) && !isServerFile(g)) { clientGraph.add(g); cq.push(g); }
}
const serverGraph = new Set();
const sq = files.filter((f) => may.has(f) && serverEntry(f) && !isClientFile(f));
for (const f of sq) serverGraph.add(f);
while (sq.length) {
  const f = sq.shift();
  for (const g of may.get(f) || []) if (!serverGraph.has(g) && !isClientFile(g)) { serverGraph.add(g); sq.push(g); }
}
const serverOpen = [...serverGraph].some((f) => opens.get(f));
for (const u of uses) {
  if (u.form !== 'direct') continue;
  u.boundary = isClientFile(u.file) || clientGraph.has(u.file) && !serverGraph.has(u.file) && !serverOpen ? 'client' : 'unknown';
}
process.stdout.write(JSON.stringify({ actions, uses }) + '\n');
