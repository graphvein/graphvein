// TSX-26: a require binding and a destructured require are receivers of
// unknown owner: a write through them leaves the file's resolved calls
// with that key unresolved, import calls included.
import * as api from "./api.js";

const lib = require("./lib.cjs");
const { helpers } = require("./lib.cjs");

lib.keep = function () {};
helpers.m ??= function () {};

export function useRequired() {
  api.keep(); // TSX-26: keep written through a require binding: no edge
  api.m(); // TSX-26: m written through a destructured require: no edge
  api.inc(); // TSX-11: edge
}
