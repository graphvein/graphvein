// TSX-19: a binding some code writes after its declaration. The first two
// cases have the shape of a promise resolver captured by assignment and of
// a test stub replaced by a setter.
export function holdUpdates() {
  let release: () => void = () => {};
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { released, done: () => release() }; // no edge: the binding holds resolve
}
export function waitForRelease() {
  let release: () => void = () => {};
  new Promise<void>((resolve) => {
    release = resolve;
  });
  release(); // no edge: the binding holds resolve
}

let infoImpl = () => ({ version: 1 });
export function getInfo() {
  return infoImpl(); // no edge: setInfo replaces it
}
export function setInfo(next: () => { version: number }) {
  infoImpl = next;
}

function swap() {}
let pick = function () {};
var next = () => 1;
let last = () => 2;
let count = () => 3;
let stable = () => 4;
const fixed = () => 5;
export function writes() {
  swap(); // no edge: assigned below
  pick(); // no edge: destructuring assignment
  next(); // no edge: for-of target
  last(); // no edge: logical assignment
  count(); // no edge: ++
  stable(); // TSX-8: never written: edge
  fixed(); // TSX-8: a const binding cannot be written: edge
}
swap = () => {};
[pick] = [() => {}];
for (next of [() => 2]) {
}
last ||= () => 3;
count++;
