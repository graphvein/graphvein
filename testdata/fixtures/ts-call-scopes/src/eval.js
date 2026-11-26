function helper() {}

// TSX-15: a direct eval may declare var bindings in its function.
export function evaluated(code) {
  eval(code);
  helper(); // no edge
}

// TSX-19: the eval above may assign any binding it sees, helper included.
export function after() {
  helper(); // no edge
}
