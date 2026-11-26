import fs = require("fs");

export function read() {}
namespace tools {
  export function x() {}
}

// TSX-12: import-equals, namespace and enum names are bindings with no
// declaration a call can resolve to.
export function others() {
  fs.read(); // no edge
  tools.x(); // no edge
  read(); // edge
}
export function enums() {
  enum read {
    A,
  }
  read(); // no edge
}
