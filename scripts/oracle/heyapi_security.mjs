// hey-api security oracle (T026, docs/extract/auth.md
// "Oracle"): reads a client/sdk.gen.ts that @hey-api/openapi-ts generated
// with the TypeScript compiler (an independent parser) and writes
// {"METHOD url": security array} for every generated request call whose
// options object has literal url and security properties.
// Usage: node heyapi_security.mjs <node_modules dir> <sdk.gen.ts> <out.json>
import { createRequire } from 'node:module';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const [modules, sdk, out] = process.argv.slice(2);
const require = createRequire(join(modules, 'noop.js'));
const ts = require('typescript');
const source = ts.createSourceFile(sdk, readFileSync(sdk, 'utf8'), ts.ScriptTarget.Latest, true);

function value(node) {
  if (ts.isStringLiteral(node)) return node.text;
  if (ts.isArrayLiteralExpression(node)) return node.elements.map(value);
  if (ts.isObjectLiteralExpression(node)) {
    const o = {};
    for (const p of node.properties) {
      if (!ts.isPropertyAssignment(p) || !ts.isIdentifier(p.name)) throw new Error('unexpected property ' + p.getText());
      o[p.name.text] = value(p.initializer);
    }
    return o;
  }
  throw new Error('unexpected value ' + node.getText());
}

const result = {};
function visit(node) {
  if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression) && node.arguments.length === 1 && ts.isObjectLiteralExpression(node.arguments[0])) {
    const props = {};
    for (const p of node.arguments[0].properties) {
      if (ts.isPropertyAssignment(p) && ts.isIdentifier(p.name)) props[p.name.text] = p.initializer;
    }
    if (props.url && ts.isStringLiteral(props.url)) {
      const key = node.expression.name.text.toUpperCase() + ' ' + props.url.text;
      result[key] = props.security ? value(props.security) : null;
    }
  }
  ts.forEachChild(node, visit);
}
visit(source);
writeFileSync(out, JSON.stringify(result, null, 1) + '\n');
