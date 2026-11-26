import { Injectable, inject } from '@angular/core';
class Local {}

export function enclosing() {
  class Local {}
  @Injectable()
  class FieldScope { x = inject(Local); }
  return FieldScope;
}

@Injectable()
export class ConstructorBlock {
  constructor() { { const Local = 1; inject(Local); } }
}

export function calleeBlock(flag: boolean) {
  if (flag) { const inject = (v) => v; inject(Local); }
}

export const twice = () => () => inject(Local);

export function loopToken() {
  for (let Local = 0; Local < 1; Local++) { inject(Local); }
}

export function catchToken() {
  try {} catch (Local) { inject(Local); }
}

export function namespaceToken() {
  { const Local = { value: 1 }; inject(Local.value); }
}

export function unaffected(flag: boolean) {
  if (flag) { const Local = 1; }
  inject(Local);
}
