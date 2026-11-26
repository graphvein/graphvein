// TSX-25: a write of a member through the class itself, its instances or
// `this` makes calls of that member unresolved; the exact self-bind
// (`this.m = this.m.bind(this)`, `C.m = C.m.bind(C)`) keeps the method.

function mixin(target) {
  return target;
}
function throttle(f) {
  return f;
}

// Writes through `this` in the constructor: the plugin shape of a
// minified player bundle.
export class Widget {
  constructor(host) {
    this.host = host;
    this.log || (this.log = host.log);
    mixin(this); // edge
    delete this.emit;
    this.close = this.close.bind(this);
    this.save = throttle(this.save); // edge
    this.open++;
  }
  emit() {}
  close() {}
  save() {}
  open() {}
  stay() {}
  run() {
    this.emit(); // deleted: no edge
    this.close(); // TSX-13: the self-bind keeps the method: edge
    this.save(); // replaced by a wrapper: no edge
    this.open(); // ++: no edge
    this.stay(); // TSX-13: never written: edge
  }
}

// Writes through the class name: assignment, delete, a literal-key
// defineProperty, and the self-bind.
export class Registry {
  static add() {}
  static drop() {}
  static list() {}
  static keep() {}
  static use() {
    Registry.add(); // assigned below: no edge
    Registry.drop(); // deleted below: no edge
    Registry.list(); // defined below: no edge
    Registry.keep(); // TSX-7: the self-bind keeps the method: edge
  }
}
Registry.add = function () {};
delete Registry.drop;
Object.defineProperty(Registry, "list", { value: () => [] });
Registry.keep = Registry.keep.bind(Registry);

// Writes through an instance, the prototype, an alias of `this` and a
// destructuring target.
export class Player {
  play() {}
  stop() {}
  pause() {}
  seek() {}
  tick() {
    this.play(); // an instance's member is assigned: no edge
    this.stop(); // the prototype's member is assigned: no edge
    this.pause(); // an alias of this assigns it: no edge
    this.seek(); // a destructuring target: no edge
  }
  rebind(next) {
    const self = this;
    self.pause = next;
    [this.seek] = [next];
  }
}
const player = new Player();
player.play = () => {};
Player.prototype.stop = function () {};

// A computed key on an instance may write any member.
export class Deck {
  deal() {}
  shuffle() {
    this.deal(); // a computed key write: no edge
  }
}
export function patchDeck(key, value) {
  const deck = new Deck();
  deck[key] = value;
}

// `Object.assign` on `this` and a write in a static block.
export class Config {
  static boot() {}
  load() {}
  constructor(options) {
    Object.assign(this, options);
  }
  apply() {
    this.load(); // Object.assign may replace it: no edge
  }
  static start() {
    this.boot(); // the static block assigns it: no edge
  }
  static {
    this.boot = null;
  }
}

// `Object.assign` whose sources are object literals of static keys writes
// exactly those keys (a shorthand, an identifier or string key, a computed
// string literal); the kaneo review-bot shape assigns constructor
// arguments.
export class Snapshot {
  constructor(root, base, head) {
    Object.assign(this, { root, base, head });
  }
  root() {}
  git() {}
  files() {
    this.git(); // TSX-13: Object.assign writes root, base and head only: edge
    this.root(); // Object.assign writes root: no edge
  }
}
export class Client {
  static make() {}
  static load() {}
  send() {}
  sign() {}
  quote() {}
  keep() {}
  constructor(key, fetcher) {
    Object.assign(this, { key, send: fetcher, "sign": null }, { ["quote"]: 1 });
  }
  run() {
    this.send(); // a pair key: no edge
    this.sign(); // a string key: no edge
    this.quote(); // a computed string key: no edge
    this.keep(); // TSX-13: not among the keys: edge
    Client.make(); // TSX-7: an instance write leaves the class's make: edge
    Client.load(); // the class's own assign writes load: no edge
  }
}
Object.assign(Client, { load: null });
export const settings = {
  read() {},
  write() {},
};
export function useSettings() {
  settings.read(); // TSX-9: the assign writes only write: edge
  settings.write(); // Object.assign writes it: no edge
}
Object.assign(settings, { write: null });

// Any other source or entry may write every member: a spread entry, a
// spread argument, a name, a computed non-literal key, a method,
// `__proto__`.
export const viaSpread = { a() {} };
export const viaRest = { a() {} };
export const viaName = { a() {} };
export const viaComputed = { a() {} };
export const viaMethod = { a() {} };
export const viaProto = { a() {} };
export function useLoose(extra, list, key, proto) {
  viaSpread.a(); // a spread entry: no edge
  viaRest.a(); // a spread argument: no edge
  viaName.a(); // a source that is no literal: no edge
  viaComputed.a(); // a computed non-literal key: no edge
  viaMethod.a(); // a method entry: no edge
  viaProto.a(); // a __proto__ key: no edge
  Object.assign(viaSpread, { x: 1, ...extra });
  Object.assign(viaRest, ...list);
  Object.assign(viaName, extra);
  Object.assign(viaComputed, { [key]: 1 });
  Object.assign(viaMethod, { z() {} });
  Object.assign(viaProto, { ["__proto__"]: proto });
}

// A write reaches one side of a class: an instance write leaves a static
// method of the same name, a class write an instance method; an
// instance's constructor is the class.
export class Range {
  constructor(from) {
    this.from = from;
  }
  static from() {}
  to() {}
  build() {
    Range.from(); // TSX-7: only instances write from: edge
    this.to(); // TSX-13: the class writes its own to, not the instances': edge
  }
}
Range.to = null;
export class Cursor {
  static of() {}
  next() {}
  walk() {
    Cursor.of(); // TSX-7: a computed key of the prototype writes instances only: edge
    this.next(); // a computed key of the prototype may write it: no edge
  }
}
Cursor.prototype[Symbol.iterator] = function () {};
export class Shape {
  static create() {}
  make() {
    Shape.create(); // an instance's constructor writes it: no edge
  }
  patch() {
    this.constructor.create = null;
  }
}

// Object owners: a member write, a write on the key path, a computed key.
export const store = {
  load() {},
  save() {},
  nested: {
    fetch() {},
  },
};
export const table = {
  pick() {},
};
export function useStore(key) {
  store.load(); // assigned below: no edge
  store.save(); // TSX-9: never written: edge
  store.nested.fetch(); // its key path is assigned: no edge
  table.pick(); // a computed key write: no edge
  table[key] = null;
}
store.load = () => {};
store.nested = {};

// A declarator holding a class expression is the class (its prototype:
// the instances), so writes through it count.
export const Expr = class Inner {
  static m() {}
  static k() {}
  static run() {
    Inner.m(); // written through Expr: no edge
    Inner.k(); // TSX-7: never written: edge
  }
};
Expr.m = function () {};
export const ExprB = class Named {
  m() {}
  k() {}
  run() {
    this.m(); // written through ExprB.prototype: no edge
    this.k(); // TSX-13: never written: edge
  }
};
export function patchExpr() {
  ExprB.prototype.m = function () {};
}
