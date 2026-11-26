// TSX-26: a computed non-literal key on a receiver of unknown owner may
// write any member of any owner: every member call of the file stays
// unresolved.

export function assignAll(target, key, value) {
  target[key] = value;
}
export class Store {
  get() {}
  run() {
    this.get(); // TSX-26: any key written through a parameter: no edge
    Store.make(); // TSX-26: no edge
  }
  static make() {}
}
export const box = {
  open() {},
  use() {
    box.open(); // TSX-26: no edge
  },
};
