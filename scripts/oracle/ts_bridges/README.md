# TypeScript bridge oracle (T026)

`oracle.cjs` checks every `renders`, `passes_function` and route
`routes_to` edge of a snapshot against the TypeScript compiler: the symbol
of the JSX tag or passed name at each evidence span (aliases followed,
React.lazy consts to the imported module's default export, lazy route keys
to an export of a module imported inside the lazy function) must be the
target node's declaration. It also counts the JSX tags the compiler
resolves to a repository function or class that Graphvein does not claim.
Contract: `docs/lang/typescript-bridges.md`.

Install the compiler outside the repository as
`scripts/oracle/ts_structure/README.md` says (`typescript@6.0.3`,
Apache-2.0, `npm ci --ignore-scripts`), then:

```bash
graphvein index <scratch copy> --emit snap.ndjson
node scripts/oracle/ts_bridges/oracle.cjs --ts-dir "$SCRATCH/oracle" \
  --snapshot snap.ndjson --root <scratch copy> --project tsconfig.json --out out.json
```

`stats.disagree` must be 0. Evidence in files outside the compiled
programs is `not_compared`.
