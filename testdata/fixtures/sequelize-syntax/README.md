# sequelize-syntax fixture

Own code (no third-party text) for T018g3: the version 2 rows
(and, since Revision 2, the version 3 rows G9 and S6) of
the cached TypeScript model payload (`docs/lang/typescript-models.md`,
ADR 0061). Together with `drizzle-syntax` it covers every row of the
payload table; the Sequelize claims themselves are tested by the
`sequelize-models` fixture of the later units. The files are syntax
samples and do not form a working application.

| file | rows |
|---|---|
| `lib/sequelize.ts` | G4 (value and inline type import); S1 with `new Sequelize(...)` and `new Sequelize` without arguments (E-new); S2; SD (`export { }`) |
| `models/Order.ts` | G4; S4 (an exported abstract class, a decorated class with type arguments and `implements`, a plain exported class); S3 (`X.init(...)`, an association, a project wrapper call, `removeAttribute`, a chained assignment `exports.count = Order.count = 1`); E-new (`new DataTypes.VIRTUAL(...)`), E-function (a named `function` hook) with B-expression and B-return, E-binary (`\|\|`), E-unknown (`this`, a regular expression) |
| `models/index.js` | G5; S5 (`const { DataTypes, Op: Operators, ...rest } = require("sequelize")`); SD (`"use strict"`, an array pattern, a `for ... of` loop); S1 (`require`, `getConfig()`, `new Sequelize(...)`, the models object); S3 (the associate loop with E-unary `typeof` and E-binary `===`; `module.exports = {...}`) |
| `models/area.js` | G6 (`define` inside a factory, no Sequelize import); S5; S3 (`module.exports = (sequelize, DataTypes) => {...}`); B-declaration (with and without an initializer), B-destructure (a defaulted and a nested entry), B-expression (E-assign `area.associate = ...` whose arrow body holds association calls), B-class, B-unknown (`if`, a function declaration), B-return; E-unary (`!false`), E-number (`-1.5`) |
| `models/legacy.js` | G6 (a module-level `init`, the base class from a project module); S4 (JavaScript `class_heritage`); S3 |
| `config/config.js` | G7 (`dialect: "sqlite"`); S3 (`module.exports = {...}`); E-binary, E-unknown (a regular expression) |
| `utils/getConfig.js` | G8 (`module.exports = () => { ...; return config[env]; }`, `config` a relative `require`); E-index |
| `models/helpers.ts` | G9 (an attribute helper: `export default function (DataTypes) { return {...} }`, no Sequelize import, version 3); S2 (the default function as E-function); S6 (an exported and a plain function declaration); SD (an async function) |
| `plain/other.js` | GD: `require("sequelize")` only in a comment, `sequelize-typescript`, `customElements.define("x", Class)`, a non-literal `init` and one inside `if`, a non-Sequelize `dialect`, and a selector over a package `require` |

`payload.golden.json` is the expected cached payload per file (`null`
outside the gate); the test removes it from its temporary copy before
indexing. `../sequelize-syntax.golden.json` is the graph golden (TypeScript
and JavaScript symbol records only: the payload adds no record). No file
here needs an inert name.
