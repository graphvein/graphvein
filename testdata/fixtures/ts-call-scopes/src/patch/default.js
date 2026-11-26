// TSX-26: a default import is a receiver of unknown owner: it may hold
// the namespace another import names.
import store, { keep, nested } from "./api.js";
import * as api from "./api.js";

function other() {}

store.inc = other;
nested.label = "x";

export function useStore() {
  api.inc(); // TSX-26: inc written through the default import: no edge
  api.keep(); // TSX-11: edge
  keep(); // TSX-11: edge
  nested(); // TSX-11: label is written on the function, no key of the call: edge
}
