// TSX-26: a require binding is a receiver of unknown owner; a write
// through it, or through a destructured require, leaves the file's
// resolved calls with that key unresolved.
const lib = require("./lib.cjs");
const { helpers } = require("./lib.cjs");

lib.start = function () {};
helpers.check = function () {};

class Runner {
  start() {}
  check() {}
  stop() {}
  run() {
    this.start(); // TSX-26: start written through a require binding: no edge
    this.check(); // TSX-26: check written through a destructured require: no edge
    this.stop(); // TSX-13: edge
    lib.stop(); // TSX-2: a require binding: no edge
  }
}
module.exports = { Runner };
