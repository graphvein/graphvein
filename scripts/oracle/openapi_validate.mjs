// OpenAPI 3.1 validation oracle for generated documents (T029, claim
// contract docs/contract/claims/openapi-generation.md, "Validation").
//
// Usage: node openapi_validate.mjs <modules dir> <document.json>...
//
// <modules dir> is a node_modules directory outside the repository holding
// @seriousme/openapi-schema-validator@2.11.0 (MIT) and its dependency ajv,
// installed with `npm install --ignore-scripts
// @seriousme/openapi-schema-validator@2.11.0` and read before use. Each
// document is validated against the OpenAPI 3.1 schema, and every Schema
// Object (components, parameters, bodies, responses) is compiled as JSON
// Schema 2020-12 with the components as $defs. Prints one line per
// invalid document and exits 1 when any is invalid. A test-only oracle:
// never a dependency of the binary.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const [modules, ...files] = process.argv.slice(2);
const load = (p) => import(pathToFileURL(join(modules, p)).href);
const { Validator } = await load("@seriousme/openapi-schema-validator/index.js");
const Ajv2020 = (await load("ajv/dist/2020.js")).default;

let invalid = 0;
for (const file of files) {
  const spec = JSON.parse(readFileSync(file, "utf8"));
  const errors = [];
  const v = new Validator();
  const res = await v.validate(structuredClone(spec));
  if (!res.valid || v.version !== "3.1") errors.push(["openapi", v.version, JSON.stringify(res.errors).slice(0, 600)]);
  const toDefs = (s) => JSON.parse(JSON.stringify(s).replaceAll("#/components/schemas/", "#/$defs/"));
  const defs = toDefs((spec.components && spec.components.schemas) || {});
  const schemas = Object.entries(defs).map(([k, s]) => ["components/" + k, s]);
  for (const [p, item] of Object.entries(spec.paths || {})) {
    for (const [m, op] of Object.entries(item)) {
      if (m.startsWith("x-")) continue;
      for (const prm of op.parameters || []) schemas.push([`${m} ${p} ${prm.name}`, toDefs(prm.schema)]);
      for (const [ct, mt] of Object.entries((op.requestBody || {}).content || {})) if (mt.schema) schemas.push([`${m} ${p} body ${ct}`, toDefs(mt.schema)]);
      for (const [st, r] of Object.entries(op.responses || {})) for (const [ct, mt] of Object.entries(r.content || {})) if (mt.schema) schemas.push([`${m} ${p} ${st} ${ct}`, toDefs(mt.schema)]);
    }
  }
  const ajv = new Ajv2020({ strict: false, validateSchema: true });
  for (const [name, s] of schemas) {
    try { ajv.compile({ ...s, $defs: defs }); } catch (e) { errors.push(["schema", name, String(e).slice(0, 300)]); }
  }
  if (errors.length) { invalid++; console.log("INVALID", file, JSON.stringify(errors)); }
}
console.log(`validated ${files.length} documents, ${invalid} invalid`);
process.exit(invalid ? 1 : 0);
