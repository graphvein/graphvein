class Base { static inherited() {} }
export class Child extends Base {}
export class Instance { run() {} }
export class Accessor { static get run() { return () => 1; } }
export class Field { static run = () => 1; }
export class FunctionField { static run = function() {}; }
export class Private { static #run() {} }
export class Repeated { static run() {} run() {} }
export class Merged { static run() {} }
export namespace Merged { export const extra = 1; }
export class Assigned { static run() {} static safe() {} }
export class Defined { static run() {} }
export class DefinedMany { static run() {} }
export class AssignedMany { static run() {} }
export class Deleted { static run() {} }
export class Updated { static run() {} }
export class DynamicMutation { static run() {} }
export declare class Ambient { static run(): void; }
export const Expression = class Expression { static run() {} };
export class Own extends Base { static direct() {} }
export class Replay { static run() {} }
declare const computed: string;
export class Computed { static run() {} static [computed] = () => 1; }
export class StaticThis { static run() {} static { this.run = () => 1; } }
