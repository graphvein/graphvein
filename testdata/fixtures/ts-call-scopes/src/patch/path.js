// TSX-26: a write through a named import is a write of unknown owner too
// (the import may hold any object); a computed key there may write any
// name: every resolved member call of the file stays unresolved.
import * as api from "./api.js";
import { tools } from "./api.js";

tools[Symbol.iterator] = null;

export function usePath() {
  api.keep(); // TSX-26: any key written through a named import: no edge
  api.m(); // TSX-26: no edge
}
