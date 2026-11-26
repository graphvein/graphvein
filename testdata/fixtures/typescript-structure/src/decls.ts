// declaration forms
export function plain(a: number): number { return a; }
export function over(a: string): string;
export function over(a: number): number;
export function over(a: any): any { return a; }
declare function ambient(x: number): void;
export declare function ambientExported(): void;
export default function () { return 1; }
export const arrow = (x: number) => x;
const local = function named() {};
export let a1 = 1, a2 = () => 2;
export const { d1, d2 } = { d1: 1, d2: 2 };
export const [e1] = [1];
@sealed
export class Decorated {
  @prop() field = 1;
  @method()
  // a comment between decorator and member
  run() {}
  @method() go() {}
  get v() { return 1; }
  set v(x) {}
  static s() {}
  #priv() {}
  ['computed']() {}
  'quoted'() {}
  constructor(x: number);
  constructor(x: any) {}
  handlers = { onClick() {} };
  typed: { sig(): void } = { sig() {} };
}
export abstract class Abs { abstract m(): void; }
interface Merged { a: number }
interface Merged { b: number }
namespace NS { export function inner() {} export namespace Deep { export const k = 1; } }
namespace NS { export function inner() {} }
function NS() {}
declare module "ambient-mod" { export function fromAmbient(): void; interface InAmbient {} }
declare global { interface Window { probe: number } }
declare type DT = number;
export type Alias = | { a: 1 } // trailing comment
  | { b: 2 } // second
export enum E { A }
declare const enum CE { B }
export const obj = { method() {}, nested: { deep() {} }, ['comp']: { c() {} }, arrowProp: () => 1 };
const priv = { pm() {} };
function outer() { function inner() {} class InFn { m() {} } return inner; }
const ClassExpr = class Named { m() {} };
const Anon = class { m() {} };
call(function cb() {});
call(class Arg { m() {} });
export const tagged = (x: number): string => sql<string>`select ${x}`;
const casted = (null as unknown as { sig(): void });
export function _dup() {}
export function _dup() {}
export default class {}
