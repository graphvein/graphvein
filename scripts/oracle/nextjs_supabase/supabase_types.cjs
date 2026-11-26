// Generated-types oracle of TestSupabaseTypesOracle (T024c3, opt-in;
// scratch only). Reads each Supabase generated types file named on the
// command line with the TypeScript 6.0.3 checker (install as in
// scripts/oracle/ts_structure/README.md, point TS_DIR at it), strict null
// checks on, and prints one JSON object: per file, the Database type's
// schemas, each schema's Tables entries with their Row properties (whether
// `null` is a member of each property's written union, and of its checked
// type), Insert property names and Relationships tuple elements. Never
// builds or runs checkout code; each file is checked on its own with the
// default library.
//
// usage: TS_DIR=<install> node supabase_types.cjs <file>... > out.json
'use strict';
const ts = require(process.env.TS_DIR + '/node_modules/typescript');

function props(checker, type) {
  return checker.getPropertiesOfType(type);
}

function propType(checker, type, name, at) {
  const p = checker.getPropertyOfType(type, name);
  return p ? checker.getTypeOfSymbolAtLocation(p, at) : null;
}

// writtenNullable is the compiler's syntax tree reading of the property's
// declared type: true when `null` is a member of the written top-level
// union, null for a bare unknown or any, false otherwise. This is the
// generator's encoding of a nullable column (`T | null`); a Json column
// that is NOT NULL is written `Json`, whose alias includes null, so the
// checker's type (checkerNullable) cannot tell the two apart.
function writtenNullable(sym) {
  const d = sym.valueDeclaration;
  const node = d && d.type;
  if (!node) return 'no type node';
  if (node.kind === ts.SyntaxKind.UnknownKeyword || node.kind === ts.SyntaxKind.AnyKeyword) return null;
  const isNull = (n) => ts.isLiteralTypeNode(n) && n.literal.kind === ts.SyntaxKind.NullKeyword;
  if (ts.isUnionTypeNode(node)) return node.types.some(isNull);
  return isNull(node);
}

// checkerNullable: true when null is a member of the property's type, null
// for unknown and any, false otherwise.
function checkerNullable(type) {
  if (type.flags & (ts.TypeFlags.Unknown | ts.TypeFlags.Any)) return null;
  if (type.flags & ts.TypeFlags.Null) return true;
  if (type.isUnion()) return type.types.some((t) => t.flags & ts.TypeFlags.Null);
  return false;
}

function literal(type) {
  return type && type.isStringLiteral() ? type.value : null;
}

function strings(checker, type) {
  if (!type || !checker.isTupleType(type)) return null;
  return checker.getTypeArguments(type).map(literal);
}

function readFile(file) {
  const program = ts.createProgram([file], { strict: true, noEmit: true, skipLibCheck: true, target: ts.ScriptTarget.ES2022 });
  const checker = program.getTypeChecker();
  const sf = program.getSourceFile(file);
  const out = { file, diagnostics: ts.getPreEmitDiagnostics(program, sf).length, schemas: [] };
  let decl = null;
  for (const s of sf.statements) {
    if ((ts.isTypeAliasDeclaration(s) || ts.isInterfaceDeclaration(s)) && s.name.text === 'Database') decl = s;
  }
  if (decl === null) return out;
  const db = checker.getTypeAtLocation(decl.name);
  for (const schema of props(checker, db)) {
    if (schema.name === '__InternalSupabase') continue;
    const st = checker.getTypeOfSymbolAtLocation(schema, decl);
    const tables = propType(checker, st, 'Tables', decl);
    const entry = { name: schema.name, tables: [] };
    for (const t of tables ? props(checker, tables) : []) {
      const tt = checker.getTypeOfSymbolAtLocation(t, decl);
      const row = propType(checker, tt, 'Row', decl);
      const insert = propType(checker, tt, 'Insert', decl);
      const rels = propType(checker, tt, 'Relationships', decl);
      const table = { name: t.name, row: [], insert: [], relationships: [] };
      for (const c of row ? props(checker, row) : []) {
        table.row.push({ name: c.name, nullable: writtenNullable(c), checker_nullable: checkerNullable(checker.getTypeOfSymbolAtLocation(c, decl)) });
      }
      for (const c of insert ? props(checker, insert) : []) table.insert.push(c.name);
      if (rels && checker.isTupleType(rels)) {
        for (const r of checker.getTypeArguments(rels)) {
          table.relationships.push({
            name: literal(propType(checker, r, 'foreignKeyName', decl)),
            columns: strings(checker, propType(checker, r, 'columns', decl)),
            referenced_relation: literal(propType(checker, r, 'referencedRelation', decl)),
            referenced_columns: strings(checker, propType(checker, r, 'referencedColumns', decl)),
          });
        }
      }
      entry.tables.push(table);
    }
    out.schemas.push(entry);
  }
  return out;
}

process.stdout.write(JSON.stringify({ typescript: ts.version, files: process.argv.slice(2).map(readFile) }) + '\n');
