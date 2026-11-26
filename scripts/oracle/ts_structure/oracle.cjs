#!/usr/bin/env node
// The T020 S3 investigation oracle: compares what Graphvein emits for TypeScript,
// TSX and JavaScript files (file nodes, declarations, contains edges,
// import edges) with the TypeScript compiler's own reading of the raw
// bytes. Not run by default tests. See README.md for install and usage.
//
//   node oracle.cjs --snapshot S.ndjson --root CHECKOUT --out DIR [--ts 59] [--name N]
//
// Independence: the declarations come from the compiler's parser
// (ts.createSourceFile over the raw bytes, UTF-16 offsets mapped back to
// UTF-8 byte offsets), the imports from ts.resolveModuleName with the
// options of each file's governing tsconfig/jsconfig as the compiler parses
// it (getParsedCommandLineOfConfigFile, extends chains, references), on the
// real filesystem of the checkout plus virtual node_modules links for the
// workspace members the package manager would link. Graphvein's code is not
// used; only its snapshot is read. The naming rules (identity.md) are
// transcribed, since they define the claim being checked.
"use strict";
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const nodeModule = require("module");

const args = {};
for (let i = 2; i < process.argv.length; i += 2) args[process.argv[i].replace(/^--/, "")] = process.argv[i + 1];
// --ts-dir: the directory holding node_modules/typescript (the scratch
// install of README.md); default: resolution from this script.
const tsModule = args.ts === "59" ? "typescript59" : "typescript";
const ts = require(args["ts-dir"] ? path.join(args["ts-dir"], "node_modules", tsModule) : tsModule);
const ROOT = fs.realpathSync(args.root);
const OUT = args.out;
const NAME = args.name || path.basename(ROOT);
fs.mkdirSync(OUT, { recursive: true });

// ---------- snapshot ----------
const snap = { fileDiag: new Map(), files: new Map(), nodes: new Map(), contains: new Map(), imports: new Map(), unresolved: new Map(), externals: new Map() };
const TSLANG = new Set(["typescript", "tsx", "javascript"]);
const DECL_KINDS = new Set(["function", "method", "class", "interface", "type_alias", "enum", "variable"]);
for (const line of fs.readFileSync(args.snapshot, "utf8").split("\n")) {
  if (!line) continue;
  const r = JSON.parse(line);
  if (r.type === "file") snap.files.set(r.path, r);
  else if (r.type === "node") {
    if (r.kind === "file" || DECL_KINDS.has(r.kind)) snap.nodes.set(r.identity, r);
    if (r.kind === "external") snap.externals.set(r.identity, r);
  } else if (r.type === "edge" && r.kind === "contains") {
    snap.contains.set(r.target, r);
  } else if (r.type === "edge" && r.kind === "imports") {
    for (const ev of r.evidence || []) {
      const k = ev.file;
      if (!snap.imports.has(k)) snap.imports.set(k, []);
      snap.imports.get(k).push({ span: ev.span, target: r.target });
    }
  } else if (r.type === "diagnostic" && (r.code === "file_too_large" || r.code === "syntax_error" || r.code === "parse_failed" || r.code === "parse_timeout")) {
    if (!snap.fileDiag.has(r.file)) snap.fileDiag.set(r.file, new Set());
    snap.fileDiag.get(r.file).add(r.code);
  } else if (r.type === "diagnostic" && r.code === "unresolved_import") {
    if (!snap.unresolved.has(r.file)) snap.unresolved.set(r.file, []);
    snap.unresolved.get(r.file).push(r.message);
  }
}

// ---------- byte/position helpers ----------
const sha = (buf) => "sha256:" + crypto.createHash("sha256").update(buf).digest("hex");
function normalize(buf) {
  // CRLF -> LF, then lone CR -> LF (contract v1).
  const out = Buffer.alloc(buf.length);
  let j = 0;
  for (let i = 0; i < buf.length; i++) {
    if (buf[i] === 13) { out[j++] = 10; if (buf[i + 1] === 10) i++; } else out[j++] = buf[i];
  }
  return out.subarray(0, j);
}
// Map UTF-16 offsets of the decoded text to raw UTF-8 byte offsets.
function byteOffsets(text) {
  const off = new Uint32Array(text.length + 1);
  let b = 0;
  for (let i = 0; i < text.length; i++) {
    off[i] = b;
    const c = text.charCodeAt(i);
    if (c < 0x80) b += 1;
    else if (c < 0x800) b += 2;
    else if (c >= 0xd800 && c <= 0xdbff && i + 1 < text.length) { off[i + 1] = b + 2; b += 4; i++; continue; }
    else b += 3;
  }
  off[text.length] = b;
  return off;
}
// Line starts in the raw bytes, with CRLF, CR and LF each one line end.
function lineIndex(raw) {
  const starts = [0];
  for (let i = 0; i < raw.length; i++) {
    if (raw[i] === 13) { if (raw[i + 1] === 10) i++; starts.push(i + 1); } else if (raw[i] === 10) starts.push(i + 1);
  }
  return starts;
}
function locate(starts, byte) {
  let lo = 0, hi = starts.length - 1;
  while (lo < hi) { const mid = (lo + hi + 1) >> 1; if (starts[mid] <= byte) lo = mid; else hi = mid - 1; }
  return { line: lo + 1, column: byte - starts[lo] };
}
function compact(s) {
  const chars = Array.from(s);
  let out = "", quote = "";
  for (let i = 0; i < chars.length; i++) {
    const c = chars[i];
    if (quote) {
      out += c;
      if (c === "\\" && i + 1 < chars.length) { i++; out += chars[i]; } else if (c === quote) quote = "";
      continue;
    }
    if (c === "'" || c === '"' || c === "`") { quote = c; out += c; continue; }
    if (c === "/" && chars[i + 1] === "*") { i += 2; while (i + 1 < chars.length && !(chars[i] === "*" && chars[i + 1] === "/")) i++; i++; continue; }
    if (c === "/" && chars[i + 1] === "/") { while (i < chars.length && chars[i] !== "\n") i++; continue; }
    if (!/\s/u.test(c)) out += c;
  }
  return out;
}
// contract v1 identity escaping (docs/contract/v1.md): bytes outside
// ASCII letters, digits and '-._~' (plus '/' in the file part) as %XX.
function esc(s, slash) {
  let out = "";
  for (const b of Buffer.from(s, "utf8")) {
    const c = String.fromCharCode(b);
    if (/[A-Za-z0-9._~-]/.test(c) || (slash && c === "/")) out += c; else out += "%" + b.toString(16).toUpperCase().padStart(2, "0");
  }
  return out;
}
const identity = (kind, file, q) => `${kind}:${esc(file, true)}#${esc(q, false)}`;

function scriptKind(p) {
  const l = p.toLowerCase();
  if (l.endsWith(".tsx")) return ts.ScriptKind.TSX;
  if (l.endsWith(".jsx")) return ts.ScriptKind.JSX;
  if (/\.(js|mjs|cjs)$/.test(l)) return ts.ScriptKind.JS;
  return ts.ScriptKind.TS;
}

// ---------- declarations by the identity.md rules, over the TS AST ----------
const census = {};
const bump = (k, n = 1) => { census[k] = (census[k] || 0) + n; };
function declarations(sf, file, raw, offs, starts) {
  const out = [];
  const counts = new Map();
  const span = (a, b) => {
    const s = locate(starts, offs[a]), e = locate(starts, offs[b]);
    return { start_line: s.line, start_column: s.column, end_line: e.line, end_column: e.column, _a: offs[a], _b: offs[b] };
  };
  const decoText = (n) => (ts.canHaveDecorators(n) ? ts.getDecorators(n) || [] : []).map((d) => d.getText(sf));
  const hasMod = (n, k) => !!(ts.canHaveModifiers(n) && (ts.getModifiers(n) || []).some((m) => m.kind === k));
  const add = (startPos, endPos, kind, name, q, ownerId, exported, def, decorators, form) => {
    const key = kind + "\0" + q;
    const c = (counts.get(key) || 0) + 1;
    counts.set(key, c);
    if (c > 1) q += "@" + c;
    const sp = span(startPos, endPos);
    const id = identity(kind, file, q);
    const attrs = {};
    if (exported) attrs.exported = true;
    if (def) attrs.default_export = true;
    if (decorators && decorators.length) attrs.decorators = decorators;
    out.push({ identity: id, kind, name, qualified_name: q, span: sp, content_hash: sha(normalize(raw.subarray(sp._a, sp._b))), attributes: attrs, owner: ownerId || identity("file", file, file), form });
    bump("decl:" + form);
    return { id, q };
  };
  const join = (o, n) => (o ? o + "." + n : n);
  const sigText = (n) => {
    const kids = n.getChildren(sf);
    const open = kids.find((k) => k.kind === ts.SyntaxKind.OpenParenToken);
    const close = kids.find((k) => k.kind === ts.SyntaxKind.CloseParenToken);
    let t = open && close ? sf.text.slice(open.getStart(sf), close.end) : "";
    if (n.type) {
      const colon = kids.find((k) => k.kind === ts.SyntaxKind.ColonToken && k.pos >= (close ? close.end : 0));
      t += sf.text.slice(colon ? colon.getStart(sf) : n.type.getStart(sf), n.type.end);
    }
    return compact(t);
  };
  // fn: function-like with a name; body visited with the new owner.
  const fn = (n, kind, name, startPos, ctx, exported, def, decorators, form, endPos) => {
    let q = join(ctx.owner, name);
    if (!n.body) { q += "@overload" + sigText(n); form += "_signature"; }
    const r = add(startPos, endPos === undefined ? n.end : endPos, kind, name, q, ctx.ownerId, exported, def, decorators, form);
    if (n.body) visit(n.body, { owner: r.q, ownerId: r.id });
  };
  // Span conventions of the contract (O-S3-5 (a)): `declare` (and an
  // `export` before it) is outside an ambient declaration's span.
  const declStart = (n) => {
    const mods = ts.canHaveModifiers(n) ? ts.getModifiers(n) || [] : [];
    const d = mods.find((m) => m.kind === ts.SyntaxKind.DeclareKeyword);
    return d ? ts.skipTrivia(sf.text, d.end) : n.getStart(sf);
  };
  // A body-less class member ends before its `;`.
  const memberEnd = (m) => {
    const kids = m.getChildren(sf);
    if (!m.body && kids.length > 1 && kids[kids.length - 1].kind === ts.SyntaxKind.SemicolonToken) return kids[kids.length - 2].end;
    return m.end;
  };
  // A type alias without `;` ends after a line comment that follows it on
  // its last line.
  const aliasEnd = (n) => {
    const t = sf.text;
    if (t[n.end - 1] === ";") return n.end;
    let i = n.end;
    while (i < t.length && (t[i] === " " || t[i] === "\t")) i++;
    if (t[i] === "/" && t[i + 1] === "/") {
      while (i < t.length && !"\n\r\u2028\u2029".includes(t[i])) i++;
      return i;
    }
    return n.end;
  };
  const isDefault = (n) => hasMod(n, ts.SyntaxKind.DefaultKeyword);
  const isExport = (n) => hasMod(n, ts.SyntaxKind.ExportKeyword);
  const member = (m, ctx) => {
    const K = ts.SyntaxKind;
    if (m.kind === K.MethodDeclaration || m.kind === K.GetAccessor || m.kind === K.SetAccessor || m.kind === K.Constructor) {
      const name = m.kind === K.Constructor ? "constructor" : m.name.getText(sf);
      const abstract = hasMod(m, K.AbstractKeyword);
      const inObject = ts.isObjectLiteralExpression(m.parent);
      const where = inObject ? "object_" : abstract ? "class_abstract_" : "class_";
      const what = m.kind === K.Constructor ? "constructor" : m.kind === K.MethodDeclaration ? "method" : "accessor";
      fn(m, "method", name, m.getStart(sf), ctx, false, false, decoText(m), where + what, inObject ? m.end : memberEnd(m));
      return true;
    }
    return false;
  };
  const object = (o, owner, ownerId) => {
    for (const p of o.properties) {
      if (ts.isPropertyAssignment(p) && ts.isObjectLiteralExpression(p.initializer)) {
        let nested = owner;
        if (ts.isIdentifier(p.name) || ts.isStringLiteral(p.name) || ts.isNoSubstitutionTemplateLiteral(p.name)) {
          const t = p.name.getText(sf);
          nested = join(owner, ts.isIdentifier(p.name) ? t : t.slice(1, -1));
        } else bump("object_unnamed_key_nesting");
        if (ts.isComputedPropertyName(p.name)) visit(p.name, { owner, ownerId });
        object(p.initializer, nested, ownerId);
      } else if (!member(p, { owner, ownerId })) visit(p, { owner, ownerId });
    }
  };
  const varList = (list, ctx, exported, startPos) => {
    for (const d of list.declarations) {
      const init = d.initializer;
      const name = d.name.getText(sf);
      const s = exported ? startPos : d.getStart(sf);
      if (init && (ts.isArrowFunction(init) || ts.isFunctionExpression(init))) {
        // The function's span is the declarator's.
        fn(init, "function", name, s, ctx, exported, false, null, ts.isArrowFunction(init) ? "const_arrow" : "const_function_expression", d.end);
        continue;
      }
      let ownerId = ctx.ownerId, q = join(ctx.owner, name);
      // A binding pattern names no variable (PS-S3-9): default branch.
      if (exported && !ts.isIdentifier(d.name)) bump("exported_binding_pattern");
      else if (exported) {
        const r = add(s, d.end, "variable", name, q, ctx.ownerId, true, false, null, "exported_variable");
        ownerId = r.id; q = r.q;
      }
      if (init && ts.isObjectLiteralExpression(init) && ts.isIdentifier(d.name)) object(init, q, ownerId);
      else if (init) visit(init, ctx);
    }
  };
  function visit(n, ctx) {
    const K = ts.SyntaxKind;
    switch (n.kind) {
      case K.FunctionDeclaration: {
        const exported = isExport(n), def = isDefault(n);
        if (!n.name && !def) break;
        const form = hasMod(n, K.DeclareKeyword) || (n.flags & ts.NodeFlags.Ambient) ? "ambient_function" : "function";
        fn(n, "function", n.name ? n.name.getText(sf) : "default.function", declStart(n), ctx, exported, def, null, form);
        return;
      }
      case K.FunctionExpression:
        if (n.name) { fn(n, "function", n.name.getText(sf), n.getStart(sf), ctx, false, false, null, "named_function_expression"); return; }
        break;
      case K.ExportAssignment: {
        const e = n.expression;
        if (n.isExportEquals) bump("export_equals");
        if (!n.isExportEquals && ts.isArrowFunction(e)) { fn(e, "function", "default.arrow", n.getStart(sf), ctx, true, true, null, "default_arrow"); return; }
        if (!n.isExportEquals && ts.isFunctionExpression(e) && !e.name) { fn(e, "function", "default.function", n.getStart(sf), ctx, true, true, null, "default_function_expression"); return; }
        break;
      }
      case K.ClassDeclaration:
      case K.ClassExpression: {
        const def = isDefault(n);
        if (!n.name && !def) { bump(n.kind === K.ClassExpression ? "anonymous_class_expression" : "anonymous_class"); return; }
        const name = n.name ? n.name.getText(sf) : "default.class";
        const r = add(declStart(n), n.end, "class", name, join(ctx.owner, name), ctx.ownerId, isExport(n), def, decoText(n), n.kind === K.ClassExpression ? "class_expression" : hasMod(n, K.AbstractKeyword) ? "abstract_class" : "class");
        for (const m of n.members) {
          if (!member(m, { owner: r.q, ownerId: r.id })) {
            if (ts.isPropertyDeclaration(m)) {
              if (m.initializer) visit(m.initializer, { owner: r.q, ownerId: r.id });
              if (m.name && ts.isComputedPropertyName(m.name)) visit(m.name, { owner: r.q, ownerId: r.id });
            } else visit(m, { owner: r.q, ownerId: r.id });
          }
        }
        if (n.heritageClauses) for (const h of n.heritageClauses) visit(h, ctx);
        return;
      }
      case K.InterfaceDeclaration:
      case K.TypeAliasDeclaration:
      case K.EnumDeclaration: {
        const kind = n.kind === K.InterfaceDeclaration ? "interface" : n.kind === K.TypeAliasDeclaration ? "type_alias" : "enum";
        const name = n.name.getText(sf);
        add(declStart(n), kind === "type_alias" ? aliasEnd(n) : n.end, kind, name, join(ctx.owner, name), ctx.ownerId, isExport(n), isDefault(n), null, kind);
        return;
      }
      case K.VariableStatement:
        varList(n.declarationList, ctx, isExport(n), declStart(n));
        return;
      case K.VariableDeclarationList:
        varList(n, ctx, false, n.getStart(sf));
        return;
      case K.ModuleDeclaration:
        // O-S3-6: namespace and module blocks claim nothing; their members
        // go to the default branch.
        bump(n.flags & ts.NodeFlags.Namespace ? "namespace" : n.name.kind === K.StringLiteral ? "ambient_module" : "global_or_module");
        return;
      case K.ObjectLiteralExpression:
        for (const p of n.properties) if (!member(p, ctx)) visit(p, ctx);
        return;
      // Types are never declarations (main reads method signatures inside
      // some type positions; the oracle does not).
      case K.TypeLiteral: case K.TypeReference: case K.FunctionType: case K.ConstructorType: case K.MappedType:
      case K.TypeQuery: case K.UnionType: case K.IntersectionType: case K.ConditionalType: case K.TypeOperator:
      case K.IndexedAccessType: case K.TupleType: case K.ArrayType: case K.ImportType: case K.TypePredicate:
      case K.LiteralType: case K.ParenthesizedType: case K.InferType: case K.TemplateLiteralType:
        return;
    }
    ts.forEachChild(n, (c) => visit(c, ctx));
  }
  for (const s of sf.statements) visit(s, { owner: "", ownerId: "" });
  return out;
}

// ---------- configs and resolution ----------
// Row TS-I5 (S3F): the dependency fields a package.json declares packages in.
const DEP_FIELDS = ["dependencies", "devDependencies", "peerDependencies", "optionalDependencies"];
const declaredCache = new Map();
const exists = (p) => { try { return fs.statSync(p).isFile(); } catch { return false; } };
const isDir = (p) => { try { return fs.statSync(p).isDirectory(); } catch { return false; } };

// Workspace roots and members (package manager logic: package.json
// "workspaces" and pnpm-workspace.yaml "packages" globs).
function globDirs(base, pattern) {
  const segs = pattern.replace(/^\.\//, "").replace(/\/$/, "").split("/");
  let cur = [base];
  for (const seg of segs) {
    const next = [];
    for (const d of cur) {
      if (seg === "**") {
        const walk = (x) => { next.push(x); for (const e of safeReaddir(x)) if (e.isDirectory() && e.name !== "node_modules" && !e.name.startsWith(".")) walk(path.join(x, e.name)); };
        walk(d);
      } else if (seg.includes("*") || seg.includes("?")) {
        // Package-manager globs: wildcards never match a dot directory.
        const re = new RegExp("^" + seg.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, "[^/]*").replace(/\?/g, "[^/]") + "$");
        for (const e of safeReaddir(d)) if (e.isDirectory() && re.test(e.name) && e.name !== "node_modules" && !e.name.startsWith(".")) next.push(path.join(d, e.name));
      } else if (isDir(path.join(d, seg))) next.push(path.join(d, seg));
    }
    cur = next;
  }
  return cur;
}
function safeReaddir(d) { try { return fs.readdirSync(d, { withFileTypes: true }); } catch { return []; } }
function readJSON(p) { try { return JSON.parse(fs.readFileSync(p, "utf8").replace(/^\uFEFF/, "")); } catch { return null; } }
const links = new Map(); // abs "<wsroot>/node_modules/<name>" -> abs member dir
const wsRoots = [];
(function findWorkspaces() {
  const walk = (d) => {
    const pj = path.join(d, "package.json");
    let patterns = null;
    if (exists(pj)) {
      const j = readJSON(pj);
      if (j && j.workspaces) patterns = Array.isArray(j.workspaces) ? j.workspaces : j.workspaces.packages || [];
    }
    const pnpm = path.join(d, "pnpm-workspace.yaml");
    if (exists(pnpm)) {
      patterns = patterns || [];
      let inPackages = false;
      for (const line of fs.readFileSync(pnpm, "utf8").split(/\r?\n/)) {
        if (/^packages\s*:/.test(line)) { inPackages = true; continue; }
        if (inPackages) {
          const m = line.match(/^\s*-\s*['"]?([^'"#]+?)['"]?\s*(#.*)?$/);
          if (m) patterns.push(m[1]); else if (/^\S/.test(line)) inPackages = false;
        }
      }
    }
    if (patterns) {
      const members = new Set(), excluded = new Set();
      for (const p of patterns) for (const m of globDirs(d, p.replace(/^!/, ""))) (p.startsWith("!") ? excluded : members).add(m);
      const names = {};
      for (const m of members) {
        if (excluded.has(m) || m === d) continue;
        const j = readJSON(path.join(m, "package.json"));
        if (j && typeof j.name === "string" && j.name) (names[j.name] = names[j.name] || []).push(m);
      }
      // Two members with one name: the package managers refuse; neither is linked.
      for (const [name, dirs] of Object.entries(names)) if (dirs.length === 1) links.set(path.join(d, "node_modules", name), dirs[0]);
      wsRoots.push({ root: path.relative(ROOT, d) || ".", members: Object.keys(names).length });
    }
    for (const e of safeReaddir(d)) if (e.isDirectory() && e.name !== "node_modules" && e.name !== ".git") walk(path.join(d, e.name));
  };
  walk(ROOT);
})();
function unlink(p) {
  // Map a path through a virtual node_modules link, if any.
  const i = p.lastIndexOf("/node_modules/");
  if (i < 0) return p;
  const rest = p.slice(i + "/node_modules/".length).split("/");
  // A scoped name takes two segments; a member named "@x" takes one.
  const n = rest[0].startsWith("@") && !links.has(p.slice(0, i) + "/node_modules/" + rest[0]) ? 2 : 1;
  const key = p.slice(0, i) + "/node_modules/" + rest.slice(0, n).join("/");
  const target = links.get(key);
  if (!target) return null;
  return path.join(target, ...rest.slice(n));
}
// O-S3-3: through a link, an output a member's own tsconfig.json emits
// into its outDir or declarationDir exists when its source does, and
// stands for the source (the compiler's getOutputFileNames; filled by
// computeOutputs below).
const outputs = new Map();
const linked = (p) => p.includes("/node_modules/");
const host = {
  fileExists: (p) => { const u = unlink(p); return u !== null && (exists(u) || (linked(p) && outputs.has(u))); },
  directoryExists: (p) => {
    // A parent of a link ("<root>/node_modules", "<root>/node_modules/@scope").
    for (const k of links.keys()) if (k.startsWith(p + "/")) return true;
    const u = unlink(p);
    if (u !== null && linked(p + "/")) for (const k of outputs.keys()) if (k.startsWith(u + "/")) return true;
    return u !== null && isDir(u);
  },
  readFile: (p) => { const u = unlink(p); if (u === null) return undefined; try { return fs.readFileSync(u, "utf8"); } catch { return undefined; } },
  realpath: (p) => { const u = unlink(p); if (u && linked(p) && !exists(u) && outputs.has(u)) return outputs.get(u); try { return fs.realpathSync(u || p); } catch { return u || p; } },
  getDirectories: (p) => safeReaddir(p).filter((e) => e.isDirectory()).map((e) => e.name),
  getCurrentDirectory: () => ROOT,
  useCaseSensitiveFileNames: true,
};
const parseHost = {
  ...host,
  useCaseSensitiveFileNames: true,
  readDirectory: (root, ext, exc, inc, depth) => ts.sys.readDirectory(root, ext, exc, inc, depth),
  onUnRecoverableConfigFileDiagnostic: () => {},
};
const configs = new Map();
function parseConfig(p) {
  if (configs.has(p)) return configs.get(p);
  let parsed = null;
  try {
    parsed = ts.getParsedCommandLineOfConfigFile(p, undefined, parseHost);
  } catch (e) { parsed = null; }
  const v = parsed ? { path: p, parsed, options: parsed.options, files: new Set(parsed.fileNames), refs: parsed.projectReferences || [], errors: parsed.errors.length } : null;
  if (v) v.cache = ts.createModuleResolutionCache(ROOT, (x) => x, v.options);
  configs.set(p, v);
  return v;
}
(function computeOutputs() {
  for (const member of new Set(links.values())) {
    const cfgPath = path.join(member, "tsconfig.json");
    if (!exists(cfgPath)) continue;
    const c = parseConfig(cfgPath);
    if (!c) continue;
    const o = c.options;
    if (o.noEmit || o.outFile || (!o.outDir && !o.declarationDir)) continue;
    const dirs = [o.outDir, o.declarationDir].filter(Boolean);
    for (const f of c.files) {
      if (/\.d\.[mc]?ts$/.test(f) || /\.d\.[^/]+\.ts$/.test(f) || f.endsWith(".json")) continue;
      for (const out of ts.getOutputFileNames(c.parsed, f, false)) {
        if (/\.(map|tsbuildinfo)$/.test(out)) continue;
        if (!dirs.some((d) => out.startsWith(d + "/"))) continue;
        if (!outputs.has(out)) outputs.set(out, f);
      }
    }
  }
})();
const DEFAULT_OPTIONS = { allowJs: true, allowNonTsExtensions: true, jsx: ts.JsxEmit.Preserve, target: ts.ScriptTarget.Latest, resolveJsonModule: true };
const defaultConfig = { path: "(none)", options: DEFAULT_OPTIONS, cache: ts.createModuleResolutionCache(ROOT, (x) => x, DEFAULT_OPTIONS) };
function including(cfg, abs, seen) {
  if (!cfg) return null;
  if (cfg.files.has(abs)) return cfg;
  if (seen.has(cfg.path)) return null;
  seen.add(cfg.path);
  for (const r of cfg.refs) {
    let rp = r.path;
    if (isDir(rp)) rp = path.join(rp, "tsconfig.json");
    const hit = including(parseConfig(rp), abs, seen);
    if (hit) return hit;
  }
  return null;
}
function governing(abs) {
  let d = path.dirname(abs);
  for (;;) {
    for (const n of ["tsconfig.json", "jsconfig.json"]) {
      const p = path.join(d, n);
      if (exists(p)) {
        const hit = including(parseConfig(p), abs, new Set());
        if (hit) return hit;
      }
    }
    if (d === ROOT || d === path.dirname(d)) break;
    d = path.dirname(d);
  }
  return defaultConfig;
}
function nearestConfig(abs) {
  // What main reads: nearest ancestor tsconfig.json, else nearest jsconfig.json.
  for (const n of ["tsconfig.json", "jsconfig.json"]) {
    let d = path.dirname(abs);
    for (;;) {
      if (exists(path.join(d, n))) return path.relative(ROOT, path.join(d, n));
      if (d === ROOT || d === path.dirname(d)) break;
      d = path.dirname(d);
    }
  }
  return "(none)";
}
const MR = Object.fromEntries(Object.entries(ts.ModuleResolutionKind).filter(([k, v]) => typeof v === "number").map(([k, v]) => [v, k]));
function effectiveResolution(o) {
  return ts.getEmitModuleResolutionKind ? MR[ts.getEmitModuleResolutionKind(o)] : MR[o.moduleResolution] || "?";
}

// ---------- import sites ----------
function importSites(sf) {
  const sites = [];
  const K = ts.SyntaxKind;
  const visit = (n, inAmbient) => {
    if (ts.isImportDeclaration(n) && ts.isStringLiteral(n.moduleSpecifier)) {
      const c = n.importClause;
      sites.push({ node: n, spec: n.moduleSpecifier, form: !c ? "side_effect_import" : c.isTypeOnly ? "type_only_import" : "import", ambient: inAmbient });
    } else if (ts.isExportDeclaration(n) && n.moduleSpecifier && ts.isStringLiteral(n.moduleSpecifier)) {
      sites.push({ node: n, spec: n.moduleSpecifier, form: n.isTypeOnly ? "type_only_reexport" : !n.exportClause ? "export_star" : ts.isNamespaceExport(n.exportClause) ? "export_star_as" : "export_named_from", ambient: inAmbient });
    } else if (ts.isImportEqualsDeclaration(n) && ts.isExternalModuleReference(n.moduleReference)) {
      const e = n.moduleReference.expression;
      sites.push({ node: n, spec: ts.isStringLiteral(e) ? e : null, form: "import_equals_require", ambient: inAmbient });
    } else if (ts.isCallExpression(n)) {
      const a = n.arguments[0];
      if (n.expression.kind === K.ImportKeyword) bump(a && ts.isStringLiteralLike(a) ? "site:dynamic_import_literal" : "site:dynamic_import_other");
      else if (ts.isIdentifier(n.expression) && n.expression.text === "require") bump(a && ts.isStringLiteralLike(a) && n.arguments.length === 1 ? "site:require_literal" : "site:require_other");
    } else if (ts.isImportTypeNode(n)) bump("site:import_type");
    const amb = inAmbient || (ts.isModuleDeclaration(n) && ts.isStringLiteral(n.name));
    ts.forEachChild(n, (c) => visit(c, amb));
  };
  visit(sf, false);
  bump("site:triple_slash_path", sf.referencedFiles.length);
  bump("site:triple_slash_types", sf.typeReferenceDirectives.length);
  return sites;
}

const NPM_PART = /^[a-z0-9-][a-z0-9-._]*$/;
// ---------- main ----------
const rows = [];
const stats = { files: 0, files_hash_mismatch: 0, files_not_utf8: 0, file_node_disagree: 0, parse_diagnostics_files: 0,
  decl_graphvein: 0, decl_oracle: 0, decl_agree: 0, decl_only_graphvein: 0, decl_only_oracle: 0, decl_field_disagree: 0,
  sites_oracle: 0, sites_graphvein_edges: 0, sites_matched: 0,
  decl_graphvein_in_syntax_error_files: 0, decl_oracle_in_syntax_error_files: 0, decl_agree_in_syntax_error_files: 0,
  decl_oracle_in_rejected_files: 0, claimed_disagreements: 0 };
const declClasses = {}, importClasses = {}, configUse = {}, resolutionUse = {};
const addClass = (o, k, ex) => { if (!o[k]) o[k] = { n: 0, examples: [] }; o[k].n++; if (o[k].examples.length < 5) o[k].examples.push(ex); };
const spanKey = (s) => `${s.start_line}:${s.start_column}-${s.end_line}:${s.end_column}`;
const tsFiles = [...snap.files.values()].filter((f) => TSLANG.has(f.language)).map((f) => f.path).sort();
const declByFile = new Map();
for (const n of snap.nodes.values()) if (n.kind !== "file") { if (!declByFile.has(n.file)) declByFile.set(n.file, []); declByFile.get(n.file).push(n); }

for (const file of tsFiles) {
  stats.files++;
  bump("file:" + path.extname(file).toLowerCase() + (/\.d\.[mc]?ts$/i.test(file) ? "(d)" : ""));
  const abs = path.join(ROOT, file);
  const raw = fs.readFileSync(abs);
  const norm = normalize(raw);
  if (sha(norm) !== snap.files.get(file).content_hash) { stats.files_hash_mismatch++; continue; }
  let text;
  try { text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(raw); } catch { stats.files_not_utf8++; continue; }
  if (raw[0] === 0xef && raw[1] === 0xbb && raw[2] === 0xbf) bump("file:bom");
  if (raw.includes(13)) {
    const cr = (raw.toString("latin1").match(/\r/g) || []).length, crlf = (raw.toString("latin1").match(/\r\n/g) || []).length;
    bump(cr !== crlf ? "file:lone_cr" : "file:crlf");
  }
  if (/[\u2028\u2029]/.test(text)) bump("file:ls_ps_chars");
  const offs = byteOffsets(text);
  const starts = lineIndex(raw);
  const cfg = governing(abs);
  const implied = ts.getImpliedNodeFormatForFileWorker(abs, cfg.cache.getPackageJsonInfoCache(), host, cfg.options);
  const sf = ts.createSourceFile(abs, text, { languageVersion: ts.ScriptTarget.Latest, impliedNodeFormat: typeof implied === "object" ? implied.impliedNodeFormat : implied, setExternalModuleIndicator: undefined }, true, scriptKind(file));
  // As a program sets it, so the emit format sees the package scope.
  if (typeof implied === "object") sf.packageJsonScope = implied.packageJsonScope;
  const parseErrors = sf.parseDiagnostics.length;
  const gdiag = snap.fileDiag.get(file) || new Set();
  if (parseErrors) { stats.parse_diagnostics_files++; bump("file:ts_parse_errors"); }
  if (gdiag.has("syntax_error")) bump("file:graphvein_syntax_error");
  if (parseErrors && !gdiag.has("syntax_error")) { bump("file:ts_rejects_graphvein_accepts"); rows.push({ kind: "file_note", corpus: NAME, class: "ts_parse_error_without_graphvein_syntax_error", file, message: ts.flattenDiagnosticMessageText(sf.parseDiagnostics[0].messageText, " "), line: sf.getLineAndCharacterOfPosition(sf.parseDiagnostics[0].start).line + 1 }); }
  if (!parseErrors && gdiag.has("syntax_error")) { bump("file:graphvein_syntax_error_ts_accepts"); rows.push({ kind: "file_note", corpus: NAME, class: "graphvein_syntax_error_ts_accepts", file }); }
  if (gdiag.has("file_too_large") || gdiag.has("parse_failed") || gdiag.has("parse_timeout")) {
    bump("file:graphvein_not_parsed");
    if ((declByFile.get(file) || []).length || (snap.imports.get(file) || []).length) { stats.claimed_disagreements++; addClass(declClasses, "not_parsed_but_facts", { file }); }
    continue;
  }
  // File node.
  const fileId = identity("file", file, file);
  const fn = snap.nodes.get(fileId);
  const lines = norm.toString("latin1").split("\n");
  const fspan = { start_line: 1, start_column: 0, end_line: lines.length, end_column: lines[lines.length - 1].length };
  if (!fn || spanKey(fn.span) !== spanKey(fspan) || fn.content_hash !== sha(norm)) { stats.file_node_disagree++; stats.claimed_disagreements++; addClass(declClasses, "file_node", { file }); }

  // Declarations.
  // Row TS-F2: a file tree-sitter rejects claims nothing beyond its file
  // node, whatever the compiler reads in it.
  const rejected = gdiag.has("syntax_error");
  const parsedDecls = declarations(sf, file, raw, offs, starts);
  const oracle = rejected ? [] : parsedDecls;
  if (rejected) stats.decl_oracle_in_rejected_files += parsedDecls.length;
  const mine = declByFile.get(file) || [];
  stats.decl_graphvein += mine.length;
  stats.decl_oracle += oracle.length;
  const om = new Map(oracle.map((o) => [o.identity, o]));
  const gm = new Map(mine.map((g) => [g.identity, g]));
  const tag = parseErrors || gdiag.has("syntax_error") ? "syntax_error_file:" : "";
  if (tag) { stats.decl_graphvein_in_syntax_error_files += mine.length; stats.decl_oracle_in_syntax_error_files += oracle.length; }
  for (const g of mine) {
    const o = om.get(g.identity);
    if (!o) {
      stats.decl_only_graphvein++;
      stats.claimed_disagreements++;
      // Same span but a different identity?
      const twin = oracle.find((x) => spanKey(x.span) === spanKey(g.span) || (x.span.end_line === g.span.end_line && x.span.end_column === g.span.end_column && x.kind === g.kind));
      const cls = tag + (twin ? (twin.kind !== g.kind ? "kind_differs" : "identity_differs") : "graphvein_only");
      addClass(declClasses, cls, { file, graphvein: g.identity, span: spanKey(g.span), oracle: twin && twin.identity });
      rows.push({ kind: "decl_disagreement", corpus: NAME, class: cls, file, graphvein: g.identity, graphvein_span: spanKey(g.span), oracle: twin ? twin.identity : null, oracle_form: twin ? twin.form : null, text: sliceLine(raw, starts, g.span.start_line) });
      continue;
    }
    const diffs = [];
    if (g.kind !== o.kind) diffs.push("kind");
    if (g.name !== o.name) diffs.push("name");
    if (g.qualified_name !== o.qualified_name) diffs.push("qualified_name");
    if (spanKey(g.span) !== spanKey(o.span)) diffs.push(g.span.end_line === o.span.end_line && g.span.end_column === o.span.end_column ? "span_start" : "span");
    else if (g.content_hash !== o.content_hash) diffs.push("content_hash");
    const ga = g.attributes || {}, oa = o.attributes;
    if (!!ga.exported !== !!oa.exported) diffs.push("exported");
    if (!!ga.default_export !== !!oa.default_export) diffs.push("default_export");
    if (JSON.stringify(ga.decorators || []) !== JSON.stringify(oa.decorators || [])) diffs.push("decorators");
    const ce = snap.contains.get(g.identity);
    if (!ce || ce.source !== o.owner) diffs.push("contains_source");
    else if (!(ce.evidence || []).some((e) => spanKey(e.span) === spanKey(g.span))) diffs.push("contains_evidence");
    if (diffs.length) {
      stats.decl_field_disagree++;
      stats.claimed_disagreements++;
      const cls = tag + diffs.join("+") + ":" + o.form;
      addClass(declClasses, cls, { file, identity: g.identity, graphvein: spanKey(g.span), oracle: spanKey(o.span), owner: [ce && ce.source, o.owner] });
      rows.push({ kind: "decl_disagreement", corpus: NAME, class: cls, file, identity: g.identity, graphvein_span: spanKey(g.span), oracle_span: spanKey(o.span), graphvein_owner: ce && ce.source, oracle_owner: o.owner, text: sliceLine(raw, starts, o.span.start_line) });
    } else { stats.decl_agree++; if (tag) stats.decl_agree_in_syntax_error_files++; }
  }
  for (const o of oracle) {
    if (gm.has(o.identity)) continue;
    stats.decl_only_oracle++;
    const cls = tag + "oracle_only:" + o.form;
    addClass(declClasses, cls, { file, oracle: o.identity, span: spanKey(o.span) });
    rows.push({ kind: "decl_disagreement", corpus: NAME, class: cls, file, oracle: o.identity, oracle_span: spanKey(o.span), text: sliceLine(raw, starts, o.span.start_line) });
  }

  // Imports.
  const nearest = nearestConfig(abs);
  const gov = cfg.path === "(none)" ? "(none)" : path.relative(ROOT, cfg.path);
  const cfgKey = gov === nearest ? "same" : nearest === "(none)" ? "main_none" : gov === "(none)" ? "oracle_none" : "different";
  configUse[cfgKey] = (configUse[cfgKey] || 0) + 1;
  const res = effectiveResolution(cfg.options);
  resolutionUse[res] = (resolutionUse[res] || 0) + 1;
  const gEdges = snap.imports.get(file) || [];
  stats.sites_graphvein_edges += gEdges.length;
  const used = new Set();
  for (const site of rejected ? [] : importSites(sf)) {
    bump("site:" + site.form + (site.ambient ? "(in_ambient_module)" : ""));
    // Not claim sites (O-S3-6, O-S3-7): `import = require()` and sites in
    // namespace or module blocks.
    if (site.form === "import_equals_require" || site.ambient) continue;
    stats.sites_oracle++;
    const sa = locate(starts, offs[site.node.getStart(sf)]), sb = locate(starts, offs[site.node.end]);
    const sp = { start_line: sa.line, start_column: sa.column, end_line: sb.line, end_column: sb.column };
    const spec = site.spec.text;
    const hits = gEdges.filter((e, i) => !used.has(i) && spanKey(e.span) === spanKey(sp) && used.add(i));
    const mode = ts.getModeForUsageLocation(sf, site.spec, cfg.options);
    const want = expectedOutcome(spec, abs, cfg, mode);
    let cls;
    if (hits.length > 1) cls = "DISAGREE:multiple_edges";
    else if (hits.length === 0) cls = "no_claim:" + (want.file ? "compiler_file" : want.external ? "compiler_external" : want.refuse ? "refused_" + want.refuse : "unresolved");
    else {
      stats.sites_matched++;
      const t = decodeTarget(hits[0].target);
      if (t.file) cls = want.file === t.file ? "agree_file" : "DISAGREE:file:" + (want.file ? "other_file" : want.external ? "compiler_external" : want.refuse ? "refuse_" + want.refuse : "compiler_unresolved");
      else if (t.external) cls = want.external === t.external ? "agree_external" : "DISAGREE:external:" + (want.file ? "compiler_file" : want.external ? "other_package" : want.refuse ? "refuse_" + want.refuse : "compiler_unresolved");
      else if (t.web) cls = spec.startsWith("/") && !want.file ? "agree_web_root" : "DISAGREE:web_root";
      else cls = "DISAGREE:other";
    }
    if (cls.startsWith("DISAGREE")) stats.claimed_disagreements++;
    addClass(importClasses, cls, { file, spec, graphvein: hits.map((h) => h.target).join(" | ") || "none", oracle: want.file || want.external || want.refuse || "unresolved", cfg: gov, res });
    if (!cls.startsWith("agree")) rows.push({ kind: "import_disagreement", corpus: NAME, class: cls, file, line: sp.start_line, spec, form: site.form, mode: mode === ts.ModuleKind.ESNext ? "esm" : mode === ts.ModuleKind.CommonJS ? "cjs" : "none", graphvein: hits.map((h) => h.target).join(" | ") || "none", oracle: want.file || want.external || want.refuse || "unresolved", oracle_via: want.via || null, config: gov, main_config: nearest, resolution: res });
  }
  gEdges.forEach((e, i) => {
    if (!used.has(i)) {
      stats.claimed_disagreements++;
      addClass(importClasses, "graphvein_edge_without_oracle_site", { file, span: spanKey(e.span), target: e.target });
      rows.push({ kind: "import_disagreement", corpus: NAME, class: "graphvein_edge_without_oracle_site", file, span: spanKey(e.span), graphvein: e.target, text: sliceLine(raw, starts, e.span.start_line) });
    }
  });
}
// The contract's expected outcome of one import site (rows TS-I1..I7)
// from the compiler: the file ts.resolveModuleName finds (assets through
// a host on which "<x>.d<ext>.ts" exists when "<x><ext>" does, then an
// existing relative asset), refused when the implementation-only
// resolution (noDtsResolution) finds another file (O-S3-2); for a bare
// name the compiler does not resolve: refused when a workspace link of the
// package is in reach or the name is not a valid npm package name, refused
// when a `paths` key matched it, else an external when it is a Node
// built-in or declared by a package.json at or above the file, else
// refused (expectedOutcome).
function compilerFile(spec, abs, options, mode) {
  const trace = process.env.TRACE_SPEC === spec;
  const r = ts.resolveModuleName(spec, abs, trace ? { ...options, traceResolution: true } : options, trace ? { ...host, trace: (m) => console.error(m) } : host, undefined, undefined, mode).resolvedModule;
  const inRepo = (p) => { const rel = path.relative(ROOT, p); return rel.startsWith("..") || path.isAbsolute(rel) ? null : rel; };
  if (r) { const f = inRepo(r.resolvedFileName); return f && !linked("/" + f) ? { file: f, via: r.isExternalLibraryImport ? "workspace_link" : "ts" } : { none: true }; }
  const ext = path.extname(spec.split(/[?#]/)[0]);
  if (ext && !/^\.(ts|tsx|js|jsx|mjs|cjs|mts|cts)$/i.test(ext)) {
    const ah = { ...host, fileExists: (q) => { const m = q.match(/^(.*)\.d(\.[^./]+)\.ts$/); return host.fileExists(q) || (!!m && host.fileExists(m[1] + m[2])); } };
    const r2 = ts.resolveModuleName(spec, abs, { ...options, allowArbitraryExtensions: true }, ah, undefined, undefined, mode).resolvedModule;
    if (r2) {
      const m = r2.resolvedFileName.match(/^(.*)\.d(\.[^./]+)\.ts$/);
      const real = m && !host.fileExists(r2.resolvedFileName) ? host.realpath(m[1] + m[2]) : r2.resolvedFileName;
      const f = inRepo(real);
      if (f) return { file: f, via: "asset" };
    }
    if (spec.startsWith("./") || spec.startsWith("../")) {
      const lit = path.resolve(path.dirname(abs), spec);
      if (exists(lit)) return { file: path.relative(ROOT, lit), via: "asset_exact" };
    }
  }
  return { none: true };
}
function validPackageName(name) {
  if (name.startsWith("node:")) return !name.slice(5).includes("/") && validPackageName(name.slice(5));
  if (!name || name.length > 214) return false;
  if (name.startsWith("@")) { const [scope, rest, more] = name.slice(1).split("/"); return more === undefined && !!rest && NPM_PART.test(scope) && NPM_PART.test(rest); }
  return NPM_PART.test(name);
}
function expectedOutcome(spec, abs, cfg, mode) {
  const a = compilerFile(spec, abs, cfg.options, mode);
  if (a.file) {
    const b = compilerFile(spec, abs, { ...cfg.options, noDtsResolution: true }, mode);
    if (b.file && b.file !== a.file) return { refuse: "declaration_or_implementation", via: a.file + " vs " + b.file };
    return a;
  }
  if (spec.startsWith(".") || spec.startsWith("/")) return { unresolved: true };
  const pkg = spec.startsWith("@") ? spec.split("/").slice(0, 2).join("/") : spec.split("/")[0];
  for (let d = path.dirname(abs); ; d = path.dirname(d)) {
    if (links.has(path.join(d, "node_modules", pkg))) return { refuse: "workspace_entry_missing" };
    if (d === ROOT || d === path.dirname(d)) break;
  }
  if (pathsMatched(spec, abs, cfg.options, mode)) return { refuse: "paths_target_missing" };
  if (!validPackageName(pkg)) return { refuse: "not_a_package" };
  // Row TS-I5 (S3F): an npm package only when no `paths` key of
  // the governing configuration matched (read from the compiler's own
  // resolution trace) and the specifier is a Node built-in (Node's own
  // module.isBuiltin) or a package.json at or above the file declares the
  // name (read here with JSON.parse over the raw file).
  if (nodeModule.isBuiltin(spec)) return { external: pkg, via: "node_builtin" };
  if (declaredIn(pkg, path.dirname(abs))) return { external: pkg, via: "declared" };
  return { refuse: "undeclared_package" };
}
// pathsMatched reports whether the compiler's resolution trace says a
// `paths` pattern (or exact key) matched the specifier itself.
function pathsMatched(spec, abs, options, mode) {
  if (!options.paths) return false;
  const lines = [];
  ts.resolveModuleName(spec, abs, { ...options, traceResolution: true }, { ...host, trace: (m) => lines.push(m) }, undefined, undefined, mode);
  const prefix = `Module name '${spec}', matched pattern '`;
  return lines.some((m) => m.startsWith(prefix));
}
// declaredIn walks from dir to the checkout root and reports whether a
// package.json there declares pkg in one of DEP_FIELDS.
function declaredIn(pkg, dir) {
  for (let d = dir; ; d = path.dirname(d)) {
    const pj = path.join(d, "package.json");
    let j = declaredCache.get(pj);
    if (j === undefined) {
      j = null;
      try { j = JSON.parse(fs.readFileSync(pj, "utf8")); } catch {}
      declaredCache.set(pj, j);
    }
    if (j && typeof j === "object" && DEP_FIELDS.some((f) => j[f] && typeof j[f] === "object" && Object.prototype.hasOwnProperty.call(j[f], pkg))) return true;
    if (d === ROOT || d === path.dirname(d)) return false;
  }
}
function decodeTarget(id) {
  const m = id.match(/^([a-z_]+):(.*)#(.*)$/);
  const dec = (s) => decodeURIComponent(s);
  if (m[1] === "file") return { file: dec(m[2]) };
  if (m[1] === "external") { const q = dec(m[3]); return q.startsWith("npm:") ? { external: q.slice(4) } : { web: q }; }
  return { other: id };
}
function sliceLine(raw, starts, line) {
  const a = starts[line - 1], b = line < starts.length ? starts[line] : raw.length;
  return raw.subarray(a, Math.min(b, a + 160)).toString("utf8").replace(/\r?\n$/, "");
}

const summary = { corpus: NAME, typescript: ts.version, root: ROOT, stats, workspace_roots: wsRoots, governing_vs_main_config: configUse, resolution_kinds: resolutionUse, census, declaration_classes: declClasses, import_classes: importClasses };
fs.writeFileSync(path.join(OUT, NAME + ".summary.json"), JSON.stringify(summary, null, 1));
fs.writeFileSync(path.join(OUT, NAME + ".rows.jsonl"), rows.map((r) => JSON.stringify(r)).join("\n") + (rows.length ? "\n" : ""));
console.log(`${NAME}: files ${stats.files}, decl g/o/agree ${stats.decl_graphvein}/${stats.decl_oracle}/${stats.decl_agree}, sites ${stats.sites_oracle}, rows ${rows.length}`);
