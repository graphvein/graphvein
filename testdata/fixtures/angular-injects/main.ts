import { Injectable, Component, Directive, Inject as Inj, Attribute, Optional, Self, Input, inject, forwardRef, HostAttributeToken, InjectionToken, NgModule, TemplateRef } from '@angular/core';
import * as core from '@angular/core';
import { Router } from '@angular/router';
import DefaultToken, { Named as Renamed, TestToken, Shape, Alias, Enumeration, TOKEN } from './tokens';
import * as ns from './tokens';
import { BarrelToken, TOKEN as BarrelTokenConst, Inject as BarrelInject, acquire } from './barrel';
import type TypeDefault from './tokens';
import type { Named as TypeClause } from './tokens';
import { type Named as TypeSpecifier } from './tokens';
import type * as typeNS from './tokens';
class Local {}
function OtherDecorator(...args) { return () => {}; }
const LOCAL_TOKEN = new InjectionToken('local');
@Injectable()
export class Types<T> {
  constructor(
    local: Local, named: Renamed, defaultToken: DefaultToken, namespaced: ns.Named,
    barrel: BarrelToken, external: Router, generic: TemplateRef<Shape>,
    nullable: Local | null, reverseNullable: null | Local,
    undef: Local | undefined, array: Local[], primitive: string,
    iface: Shape, alias: Alias, typeParameter: T, enumeration: Enumeration,
    typeDefault: TypeDefault, typeClause: TypeClause, typeSpecifier: TypeSpecifier,
    typeNamespace: typeNS.Named, missing,
    @Inj(Local) override: Shape, @core.Inject(Local) namespaceDecorator: Shape,
    @BarrelInject(Renamed) barrelDecorator: Local,
    @OtherDecorator() otherDecorator: Local,
    @Inj() zero: Local, @Inj(Local, Renamed) two: Local, @Inj bare: Local,
    @Inj('str') stringToken: Local, @Inj(forwardRef(() => Late)) late: Shape,
    @Inj(LOCAL_TOKEN) localToken: Shape, @Inj(BarrelTokenConst) importedToken: Shape,
    @Attribute('x') attribute: string, @Attribute() invalidAttribute: string,
    @Optional() @Self() flags: Local, @Input() invalidDecorator: Local,
  ) {}
}
@Injectable()
export class Sites {
	other = core.runInInjectionContext(null, () => new Local());
	[inject(Router)] = 1;
  local = inject(Local);
  external = inject(Router, { optional: true });
  alias = acquire(Renamed);
  static ignoredStatic = inject(Local);
  ignoredNested = () => inject(Local);
  token = inject(TOKEN);
  forward = inject(forwardRef(function () { return Late; }));
  attribute = inject(new HostAttributeToken('x'));
  constructor(value = inject(TestToken)) {
    inject(Local);
    const nested = () => inject(Local);
  }
  method() { inject(Local); }
}
@Injectable()
export class Base { constructor(a: Local) {} }
@Injectable()
export class Inherited extends Base { own = inject(Local); }
@Injectable()
export class Own extends Base { constructor(a: Renamed) { super(new Local()); } }
@Injectable()
export class Late {}
@Injectable()
export class Merge { constructor(a: Local, b: Local) {} }
@Injectable()
export class TypeShadow<Local> { constructor(value: Local) {} }
export function outer<Local>() {
  @Injectable()
  class NestedTypeShadow { constructor(value: Local) {} }
  return NestedTypeShadow;
}
@Injectable()
export class Overloads {
  constructor(a: string);
  constructor(a: Local) {}
}
@Injectable({providedIn: 'root', useExisting: Local})
export class OwnOverride {}
const OPTIONS = {useClass: Local};
@Injectable({...OPTIONS})
export class SpreadOverride {}
@Injectable()
export class UsesOverrides { constructor(a: Renamed, b: Router, c: OwnOverride, d: SpreadOverride, e: TestToken) {} }
@Component({selector: 'conflict', template: ''})
@Directive({selector: 'conflict'})
export class Conflict { constructor(x: Local) {} }
@Component({selector: 'jit', template: '', jit: true})
export class Jit { value = inject(Local); }
@Component(42)
export class Invalid { constructor(x: Local) {} }
@Component({...OPTIONS, template: ''})
export class Partial { constructor(x: Local) {} }
export class Plain { constructor(x: Local) {} }
@Injectable(({providedIn: 'root'}))
export class C1Parentheses { constructor(x: Local) {} }
const CONFIG = {providedIn: 'root'};
@Injectable(CONFIG)
export class C1Reference {}
@Injectable({} as Shape)
export class C1Assertion {}
@Component({template: ''})
@Injectable(CONFIG)
export class C1CombinedAfter {}
@Injectable(CONFIG)
@Component({template: ''})
export class C1CombinedBefore {}
export const interceptor = () => { inject(Local); inject(Router); };
export function declared() { inject(Renamed); const nested = () => inject(Local); }
export function defaults(ignored = inject(Local)) {}
export const routes = [{canActivate: [() => inject(Local)]}];
const TestBed = {inject(token) { return token; }};
TestBed.inject(Local);
inject(Local);
