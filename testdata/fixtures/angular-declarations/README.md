# Angular declarations

Synthetic Angular metadata and module-scope cases. `v15`, `v21`, `mismatch`
and `ranges` are independent workspaces. The ranges workspace contains a nested
project for the multi-major range case. Package and lock data are inert test
inputs; no packages are installed and no fixture name needs restoring.

`v15/changed.ts` mutates an imported array through a barrel. `twice.ts` and
`third.ts` intentionally duplicate a production declaration; `module.spec.ts`
declares it independently in test scope. A repeated entry within `single.ts`
is valid. Every other unsupported form has an explicit diagnostic expectation.
