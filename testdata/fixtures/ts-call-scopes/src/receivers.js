// TSX-26: a member write whose receiver's owner the model does not know
// may write the member of any owner: every class, object or `this` member
// call of the file whose key path has the written name stays unresolved.
// A receiver known to hold no owner (a fresh literal, a function) is not
// such a write.

function other() {}
function make() {
  return {};
}

// `this` in an object-literal method.
export const panel = {
  init() {
    this.load = other;
  },
  load() {},
  show() {},
};
export function usePanel() {
  panel.init(); // TSX-9: init is not written: edge
  panel.load(); // TSX-26: this in an object method writes load: no edge
  panel.show(); // TSX-9: edge
}

// An alias of a member path: the prototype, a nested object.
export class Proto {
  m() {}
  run() {
    this.m(); // TSX-26: written through an alias of Proto.prototype: no edge
  }
}
const proto = Proto.prototype;
proto.m = other;
export const api = {
  users: {
    list() {},
  },
  use() {
    api.users.list(); // TSX-26: written through an alias of api.users: no edge
  },
};
const u = api.users;
u.list = other;

// A destructured binding.
export const reg = {
  h: {
    go() {},
  },
  run() {
    reg.h.go(); // TSX-26: written through a destructured name: no edge
  },
};
const { h } = reg;
h.go = other;

// A variable assigned `this` after its declaration.
export class Later {
  mm() {}
  run() {
    let that;
    that = this;
    that.mm = other;
    this.mm(); // TSX-26: written through a name that holds this: no edge
  }
}

// `this` in a plain function, a parameter, a call result, a value a key
// of an owner holds; compound and logical assignment, delete and a
// literal-key defineProperty.
export function plain() {
  this.ping = other;
}
export function patch(target) {
  target.tick ||= other;
  target.count += 1;
  delete target.drop;
  Object.defineProperty(target, "spin", { value: other });
}
export const settings = {
  fetch() {},
};
export class Board {
  constructor() {
    this.cfg = settings;
    this.cfg.fetch = other;
    make().beep = other; // TSX-3: edge
  }
  ping() {}
  tick() {}
  count() {}
  drop() {}
  spin() {}
  beep() {}
  stay() {}
  run() {
    this.ping(); // TSX-26: this in a plain function: no edge
    this.tick(); // TSX-26: a logical assignment through a parameter: no edge
    this.count(); // TSX-26: a compound assignment through a parameter: no edge
    this.drop(); // TSX-26: delete through a parameter: no edge
    this.spin(); // TSX-26: defineProperty through a parameter: no edge
    this.beep(); // TSX-26: a call result: no edge
    this.stay(); // TSX-13: never written: edge
    settings.fetch(); // TSX-26: written through this.cfg: no edge
  }
}

// Fresh literals and functions hold no owner: their writes block nothing.
export const kit = {
  extra() {},
  label() {},
  merged() {},
  use() {
    kit.extra(); // TSX-9: an array literal's property: edge
    kit.label(); // TSX-9: a function's property: edge
    kit.merged(); // TSX-9: Object.assign onto a fresh object: edge
  },
};
const bag = [];
bag.extra = 1;
other.label = "other";
export const merged = Object.assign({}, kit, { merged: 1 });

// Module-level static calls (the static member resolver of ADR 0026)
// follow the same rule.
export class Clock {
  static now() {}
  static keep() {}
}
export function setNow(x) {
  x.now = other;
}
Clock.now(); // TSX-26: written through a parameter: no edge
Clock.keep(); // ADR 0026: edge

// A class name repeated in sibling anonymous scopes (the minified bundle
// shape): the binding model leaves the call to the ADR 0026 resolver,
// which follows the same rule.
(() => {
  class Twin {
    static now() {}
    static keep() {}
  }
  return Twin;
})();
(() => {
  class Twin {
    static now() {}
    static keep() {}
    static run() {
      Twin.now(); // TSX-21, then ADR 0026: written through a parameter: no edge
      Twin.keep(); // TSX-21, then ADR 0026: edge
    }
  }
  return Twin;
})();
