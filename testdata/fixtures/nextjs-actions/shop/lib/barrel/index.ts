// A barrel (T024): its re-export clauses load the modules they
// name, so the server page that imports this file reaches both.
export { barrelRemove } from '../barrel-target';
export * from '../star-target';
export type { Shape } from '../types-only';
