import { ext, fetcher } from "./other.js";

export function helper() {}
export function parse() {}
export class Box {
  static make() {}
}
const fmt = () => 1;
const api = { get() {} };

// TSX-1: a var declaration lands in the function scope, also from a block.
export function hoisted() {
  [1].forEach(function () {
    if (Math.random()) {
      var helper = 1;
    }
    helper(); // the var: no edge
  });
  helper(); // the module function, although link time would read the nested arrow below: edge (TSX-22)
  [1].forEach(function () {
    var helper = () => 2;
  });
}

// TSX-2: let and const bind in their block only.
export function blocks() {
  {
    const parse = 1;
    parse(); // the const: no edge
  }
  parse(); // the module function: edge
  for (let helper of [1]) {
    helper(); // the loop binding: no edge
  }
}

// TSX-3: a function declaration binds in its scope.
export function nested() {
  function parse() {}
  parse(); // nested.parse: edge
}

// TSX-4: destructured names, in an anonymous arrow too.
export function patterns(rows) {
  rows.map(({ helper }) => helper()); // no edge
  rows.map(([parse]) => parse()); // no edge
}

// TSX-5: parameters of anonymous functions and arrows.
export function params(rows) {
  rows.map(function (parse) {
    return parse(); // no edge
  });
  rows.map((helper) => helper()); // no edge
  rows.map((x) => helper(x)); // the module function: edge
}

// TSX-6: a catch parameter binds in its clause.
export function caught() {
  try {
    parse();
  } catch (helper) {
    helper(); // no edge
  }
  helper(); // edge
}

// TSX-7: a class binds its name; a nearer class of one name is another binding.
export function classes() {
  Box.make(); // module Box: edge (the static member resolver of ADR 0026; the name lookup alone would read the nested class)
  {
    class Box {
      static make() {}
    }
    Box.make(); // classes.Box.make: edge
  }
}

// TSX-8 and TSX-9: a declarator names a function or an object owner.
export function declarators() {
  fmt(); // edge
  api.get(); // edge
}

// TSX-10: a declarator-bound named function expression's own name.
export const counter = function count(n) {
  return n && count(n - 1); // no edge
};

// TSX-18: an unbound named function expression's own name.
export function outer2() {
  [1].map(function step(n) {
    return n && step(n - 1); // outer2.step: edge
  });
}

// TSX-11: an import binding, shadowed in an arrow.
export function imported(rows) {
  ext(); // cross-file edge
  rows.map((ext) => ext()); // no edge
}

// TSX-13 and TSX-14: this in a class method, an arrow inside it, and elsewhere.
export class Panel {
  open() {
    this.close(); // edge
    [1].map(() => this.close()); // edge
    [1].map(function () {
      this.close(); // no edge
    });
  }
  close() {}
}
export const menu = {
  open() {
    this.close(); // no edge
  },
  close() {},
};

// TSX-16: two declarations of one name in one scope.
export function twice() {
  function step() {}
  function step() {}
  step(); // no edge
}

// TSX-17: a name no scope binds.
export function globals() {
  setTimeout(); // no edge
  [1].forEach(function () {
    function hidden() {}
    hidden(); // globals.hidden: edge
  });
  hidden(); // declared inside the anonymous function, invisible here: no edge
}

// TSX-20: this and the class name reach only members of the matching
// staticness (this is the class in a static method, an instance elsewhere).
export class Runner {
  static helper() {}
  run() {
    this.helper(); // a static method from an instance: no edge
    this.start(); // TSX-13: edge
  }
  static boot() {
    this.start(); // an instance method from the class: no edge
    this.helper(); // TSX-13: edge
    Runner.helper(); // TSX-7: edge
    Runner.start(); // an instance method on the class: no edge
  }
  get ready() {
    return this.start(); // TSX-13 in an accessor: edge
  }
  start() {}
}

// TSX-21: a member call reaches only an own property of the bound
// declaration.
export function setup(app) {
  app.get("/a", () => {
    const store = {
      load() {
        return 1;
      },
    };
    return store.load(); // setup.store.load: edge (TSX-9)
  });
  app.get("/b", () => {
    const store = { load: fetcher };
    return store.load(); // another object of the same qualified name: no edge
  });
}
export function toolbox() {
  function inner() {}
  inner(); // toolbox.inner: edge
}
export function useToolbox() {
  toolbox.inner(); // a nested declaration is no property of the function: no edge
}
export const shelf = {
  get load() {
    return () => 1;
  },
  run() {
    shelf.load(); // a getter: the call reaches its value: no edge
  },
};

// TSX-22: the lexical binding is a never-written declaration that link time
// would not pick by qualified name.
function format() {}
export class Report {
  format() {}
  render() {
    format(); // the module function, not Report.format: edge
    this.format(); // TSX-13: edge
  }
}

// TSX-22 with a repeated qualified name: the minified-bundle shape, one
// module function per wrapper and an object member of the same name.
[1].forEach(function () {
  function pad() {}
  const table = {
    pad() {
      return pad(); // the wrapper's pad, not table.pad: edge
    },
  };
});
[2].forEach(function () {
  function pad() {}
  const table = {
    pad() {
      return pad(); // the second wrapper's pad (pad@2): edge
    },
  };
});

// TSX-24: a repeated qualified name when link time would pick a function of
// the name, not a nearer method (the repeated rule of ADR 0011).
export function hooks2() {
  [1].map(() => {
    function visit() {}
    visit(); // hooks2.visit is repeated: no edge
  });
  [2].map(() => {
    function visit() {}
    visit(); // link time would pick the first visit: no edge
  });
}

// TSX-23: an import binding when a declaration of the file has the name.
export class Viewer {
  ext() {}
  show() {
    ext(); // the import, not Viewer.ext: cross-file edge
  }
}
