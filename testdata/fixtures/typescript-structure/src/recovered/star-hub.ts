// ADR 0074 P3: `export type *` and `export type * as ns` parse (TS-F2 before).
// `dup` is reachable through the type-only star (types-a) and the value star
// (types-e): ambiguous, no binding (T020).
export type * from "./types-a";
export * from "./types-e";
export type * as shapes from "./types-a";
