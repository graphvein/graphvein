// TSX-26: a namespace import is a receiver of unknown owner. A write of
// one of its properties in this file leaves every resolved call of the
// file whose key path has that name unresolved.
import * as api from "./api.js";

function other() {}

api.m = other;
api.inc += 1;
delete api.gone;
Object.defineProperty(api, "defined", { value: other });

export function useApi() {
  api.m(); // TSX-26: m written through the namespace: no edge
  api.inc(); // TSX-26: compound write: no edge
  api.gone(); // TSX-26: delete: no edge
  api.defined(); // TSX-26: defineProperty with a literal key: no edge
  api.keep(); // TSX-11: edge
  api.nested(); // TSX-11: edge
}
