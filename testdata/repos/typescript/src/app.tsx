import React from 'react';
import main, { greet as hello, double } from './lib';
import * as lib from './lib';
export interface Props { title: string; }
export type Title = string;
export enum Mode { Small, Large }
export const version = 1;
export const App = () => { hello('a'); double(2); lib.greet('b'); return <div>{main()}</div>; };
@Component({ selector: 'demo' })
export class View {
  @Injectable()
  render() { return this.label(); }
  label() { return hello('view'); }
}
export const expression = function () { return double(3); };
export function outer() {
  function inner() { return double(4); }
  return inner();
}
export function shadow(double: () => number) { return double(); }
