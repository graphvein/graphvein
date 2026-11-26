export const first = {
  save() { return 1; },
  nested: { "leaf": { save() { return 2; } } },
  [key]: { save() { return 3; } },
};
let second = { save() { return 4; } };
var third = { save() { return 5; } };
consume({ save() { return 6; } });
function factory() {
  const local = { save() { return 7; } };
  return { save() { return 8; } };
}
function overloaded(value: string): number;
function overloaded(value: string): number;
function overloaded(value: string): number { return 1; }
