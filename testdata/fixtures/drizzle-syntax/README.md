# drizzle-syntax fixture

Own code (no third-party text) for T018g2: the cached TypeScript
model payload (`docs/lang/typescript-models.md`, ADR 0059). It covers every
version 1 row of the payload table (`../sequelize-syntax` covers the
version 2 rows, ADR 0061); the Drizzle claims themselves are tested by the
`drizzle-models` fixture of the later units.

| file | rows |
|---|---|
| `schema.ts` | G1; S1 (tables, an enum, a `customType`, `relations()`, a module-level `sql` const, an object of columns, a column const); E-call, E-member, E-identifier, E-string, E-number (`-1`), E-boolean, a tagged `sql` without substitutions, E-tagged with substitutions, E-object, E-property, E-shorthand, E-spread-entry, E-method (literal and non-literal return), E-array, E-spread, E-arrow (expression and block bodies, a typed return), E-wrap; comments between entries; SD (imports) |
| `drizzle.config.ts` | G2, S2 (`defineConfig({...}) satisfies Config`), E-template |
| `plain/drizzle.config.js` | G3 (default config name, no `drizzle-kit` import), S2 naming a const |
| `queries.ts` | GD (imports only the root `drizzle-orm` module) |
| `unknowns.ts` | E-entry-unknown (computed key, getter, async and generator methods), E-unknown (conditional, async arrow, optional chain), destructured/defaulted/rest parameters, `let`/`var`/`as any`, SD (ambient `declare`, function, `export { }`, `export * from`, a declaration inside a function). Since payload version 2 (ADR 0061) the `function` expression, `new Date()`, the destructuring declaration (S5) and the block arrow's statements are captured; the file's first comment predates that |

`payload.golden.json` is the expected cached payload per file (`null`
outside the gate); the test removes it from its temporary copy before
indexing. `../drizzle-syntax.golden.json` is the graph golden (TypeScript
symbol records only: the payload adds no record). No file here needs an
inert name.
