#!/usr/bin/env node
// Bridge oracle (T026, docs/lang/typescript-bridges.md): the
// TypeScript compiler (typescript@6.0.3, installed as
// scripts/oracle/ts_structure/README.md says) resolves the symbol of every
// JSX tag and passed name Graphvein claims a `renders`, `passes_function`
// or `routes_to` edge for, and the claim must name the declaration the
// compiler finds. It also counts the JSX tags the compiler resolves to a
// function or class of the program that Graphvein does not claim
// (under-claims, reported, not a failure).
//
// Usage: node oracle.cjs --ts-dir DIR --snapshot S.ndjson --root ROOT
//          --project tsconfig.json[,other/tsconfig.json] --out OUT.json
"use strict";

const fs = require("fs");
const path = require("path");

const args = {};
for (let i = 2; i < process.argv.length; i += 2) {
  args[process.argv[i].replace(/^--/, "")] = process.argv[i + 1];
}
const ts = require(path.join(path.resolve(args["ts-dir"]), "node_modules", "typescript"));
const root = path.resolve(args.root);

const nodes = new Map();
const claims = [];
for (const line of fs.readFileSync(args.snapshot, "utf8").split("\n")) {
  if (!line.trim()) continue;
  const r = JSON.parse(line);
  if (r.type === "node") nodes.set(r.identity, r);
  if (r.type === "edge" && (r.kind === "renders" || r.kind === "passes_function" || r.kind === "routes_to")) {
    if (r.kind === "routes_to" && !r.source.startsWith("ui_route:")) continue;
    claims.push(r);
  }
}

// Programs: one per configuration, files of every program.
const programs = [];
for (const project of (args.project || "tsconfig.json").split(",")) {
  const configPath = path.join(root, project);
  const parsed = ts.getParsedCommandLineOfConfigFile(configPath, {}, { ...ts.sys, onUnRecoverableConfigFileDiagnostic: () => {} });
  if (!parsed) continue;
  const program = ts.createProgram({ rootNames: parsed.fileNames, options: { ...parsed.options, noEmit: true }, projectReferences: parsed.projectReferences });
  programs.push({ program, checker: program.getTypeChecker() });
}
function sourceOf(rel) {
  const abs = path.join(root, rel);
  for (const p of programs) {
    const sf = p.program.getSourceFile(abs);
    if (sf) return { sf, checker: p.checker };
  }
  return null;
}

// Contract spans use UTF-8 byte columns; the compiler UTF-16 offsets.
function offset(sf, line, byteColumn) {
  const start = sf.getLineStarts()[line - 1];
  const end = line < sf.getLineStarts().length ? sf.getLineStarts()[line] : sf.text.length;
  const text = sf.text.slice(start, end);
  let bytes = 0;
  let i = 0;
  while (i < text.length && bytes < byteColumn) {
    const cp = text.codePointAt(i);
    bytes += cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4;
    i += cp >= 0x10000 ? 2 : 1;
  }
  return start + i;
}

// nodeAt finds the innermost node that starts at pos and ends at end.
function nodeAt(sf, pos, end) {
  let found = null;
  function visit(n) {
    if (n.getStart(sf) === pos && n.getEnd() === end) found = n;
    if (n.getStart(sf) <= pos && n.getEnd() >= end) ts.forEachChild(n, visit);
  }
  visit(sf);
  return found;
}

function aliased(checker, s) {
  while (s && s.flags & ts.SymbolFlags.Alias) {
    const next = checker.getAliasedSymbol(s);
    if (!next || next === s) break;
    s = next;
  }
  return s;
}

// declarationOf maps a symbol to { file, line, name } of its value
// declaration; a React.lazy const maps to the imported module's default
// export.
function declarationOf(checker, s, depth) {
  s = aliased(checker, s);
  if (!s || !s.declarations || s.declarations.length === 0) return null;
  const d = s.valueDeclaration || s.declarations[0];
  if (ts.isVariableDeclaration(d) && d.initializer && (depth || 0) < 2) {
    let init = d.initializer;
    if (ts.isCallExpression(init) && init.arguments.length === 1 && /(^|\.)lazy$/.test(init.expression.getText()) && ts.isArrowFunction(init.arguments[0])) {
      const body = init.arguments[0].body;
      if (ts.isCallExpression(body) && body.expression.kind === ts.SyntaxKind.ImportKeyword) {
        const mod = checker.getSymbolAtLocation(body.arguments[0]);
        if (mod) {
          const def = checker.getExportsOfModule(mod).find((e) => e.escapedName === "default");
          const r = def && declarationOf(checker, def, (depth || 0) + 1);
          if (r) return { ...r, via: "lazy" };
        }
      }
    }
  }
  const sf = d.getSourceFile();
  const nameNode = d.name || d;
  return {
    file: path.relative(root, sf.fileName).split(path.sep).join("/"),
    line: sf.getLineAndCharacterOfPosition(nameNode.getStart(sf)).line + 1,
    name: d.name ? d.name.getText(sf) : "default",
    kind: ts.SyntaxKind[d.kind],
  };
}

function targetName(q) {
  const parts = q.replace(/@\d+$/, "").split(".");
  return parts[parts.length - 1];
}

const stats = { claims: 0, agree: 0, disagree: 0, not_compared: 0, jsx_tags_repo: 0, jsx_tags_claimed: 0 };
const rows = [];
for (const e of claims) {
  const target = nodes.get(e.target);
  for (const ev of e.evidence) {
    stats.claims++;
    const src = sourceOf(ev.file);
    if (!src || !target || !target.span) {
      stats.not_compared++;
      rows.push({ status: "not_compared", reason: src ? "target without span" : "file outside the programs", edge: e.kind, source: e.source, target: e.target, file: ev.file, span: ev.span });
      continue;
    }
    const { sf, checker } = src;
    const pos = offset(sf, ev.span.start_line, ev.span.start_column);
    const end = offset(sf, ev.span.end_line, ev.span.end_column);
    let n = nodeAt(sf, pos, end);
    let s = null;
    if (n && (ts.isJsxOpeningElement(n) || ts.isJsxSelfClosingElement(n))) {
      s = checker.getSymbolAtLocation(n.tagName);
    } else if (n && (ts.isIdentifier(n) || ts.isPropertyAccessExpression(n))) {
      const name = ts.isPropertyAccessExpression(n) ? n.name : n;
      if (ts.isShorthandPropertyAssignment(name.parent)) s = checker.getShorthandAssignmentValueSymbol(name.parent);
      else s = checker.getSymbolAtLocation(name);
    } else if (n && ts.isShorthandPropertyAssignment(n)) {
      s = checker.getShorthandAssignmentValueSymbol(n);
    }
    let decl = s ? declarationOf(checker, s) : null;
    if (!decl && n && e.source.startsWith("ui_route:")) {
      // RR5: the lazy key's evidence is the lazy property; the target
      // must be an export of a module imported inside it.
      const exportsSeen = [];
      (function walk(x) {
        if (ts.isCallExpression(x) && x.expression.kind === ts.SyntaxKind.ImportKeyword) {
          const mod = checker.getSymbolAtLocation(x.arguments[0]);
          if (mod) for (const ex of checker.getExportsOfModule(mod)) {
            const d = declarationOf(checker, ex);
            if (d) exportsSeen.push(d);
          }
        }
        ts.forEachChild(x, walk);
      })(n);
      decl = exportsSeen.find((d) => d.file === target.file && d.name === targetName(target.qualified_name)) || null;
      if (decl) decl.via = "lazy export";
    }
    const tline = target.span.start_line;
    const ok = decl && decl.file === target.file && decl.name === targetName(target.qualified_name) && Math.abs(decl.line - tline) <= 3;
    if (ok) stats.agree++;
    else stats.disagree++;
    rows.push({ status: ok ? "agree" : "DISAGREE", edge: e.kind, source: e.source, target: e.target, file: ev.file, span: ev.span, oracle: decl });
  }
}

// Under-claims: JSX tags the compiler resolves to a function or class
// declaration of the program.
const claimed = new Set();
for (const e of claims) for (const ev of e.evidence) if (e.kind === "renders") claimed.add(ev.file + ":" + ev.span.start_line + ":" + ev.span.start_column);
const missing = [];
for (const p of programs) {
  for (const sf of p.program.getSourceFiles()) {
    if (sf.fileName.includes("/node_modules/") || !sf.fileName.startsWith(root + path.sep) || sf.isDeclarationFile) continue;
    const rel = path.relative(root, sf.fileName).split(path.sep).join("/");
    (function walk(x) {
      if (ts.isJsxOpeningElement(x) || ts.isJsxSelfClosingElement(x)) {
        const s = aliased(p.checker, p.checker.getSymbolAtLocation(x.tagName));
        const d = s && (s.valueDeclaration || (s.declarations || [])[0]);
        if (d && !d.getSourceFile().fileName.includes("/node_modules/") && !d.getSourceFile().isDeclarationFile && (ts.isFunctionDeclaration(d) || ts.isClassDeclaration(d) || ts.isVariableDeclaration(d) && d.initializer && (ts.isArrowFunction(d.initializer) || ts.isFunctionExpression(d.initializer)))) {
          stats.jsx_tags_repo++;
          const lc = sf.getLineAndCharacterOfPosition(x.getStart(sf));
          const key = rel + ":" + (lc.line + 1) + ":";
          if ([...claimed].some((k) => k.startsWith(key))) stats.jsx_tags_claimed++;
          else if (missing.length < 200) missing.push({ file: rel, line: lc.line + 1, tag: x.tagName.getText(sf) });
        }
      }
      ts.forEachChild(x, walk);
    })(sf);
  }
}

fs.writeFileSync(args.out, JSON.stringify({ stats, disagreements: rows.filter((r) => r.status !== "agree"), missing_sample: missing }, null, 1));
console.log(JSON.stringify(stats));
