import {Component as C, Directive, Pipe, Injectable, NgModule} from '@angular/core';
import * as core from '@angular/core';
import {BarrelComponent} from './barrel';
import {Good} from './modules';
import {StandalonePipe} from './targets';
import type {Component as TypeComponent} from '@angular/core';
import {selector} from './constants';
@C({selector:'alias',templateUrl:'./page.html',1:'irrelevant'})
export class Alias {}
@core.Component(({templateUrl:'page.html'} as const))
export class Namespace {}
@C({templateUrl:'../shared.html'})
export class ParentPath {}
@C({templateUrl:'/root.html'})
export class Rooted {}
@C({templateUrl:'missing.html'})
export class Missing {}
@C({templateUrl:'page.html',template:unknownTemplate})
export class Both {}
@C({})
export class Absent {}
@C({template:'<p>inline</p>'})
export class Inline {}
@C({template:'<p>escaped\n</p>'})
export class Escape {}
@BarrelComponent({template:'<p>indirect</p>'})
export class Indirect {}
@C({template:'<p>conflict</p>'})
@Directive({selector:'[conflict]'})
export class Conflict {}
@C({template:'<p>repeated</p>'})
@C({template:'<p>again</p>'})
export class Repeated {}
@Injectable()
@C({template:'<p>both</p>',standalone:true,imports:[Good,StandalonePipe]})
export class InjectableComponent {}
@C({jit:false,templateUrl:'missing.html',standalone:true})
export class JitComponent {}
@C(({template:'<p />'} satisfies object))
export class Satisfies {}
@C(({template:'<p />'})!)
export class NonNull {}
@C(<object>{template:'<p />'})
export class TypeAssertion {}
@C('invalid')
export class NonObject {}
@C({}, {})
export class TwoArguments {}
@C({template:'<p />',...spread})
export class Spread {}
@C({[key]:'x',template:'<p />'})
export class Computed {}
@C({template:'<p />',factory(){return 1;}})
export class Method {}
@Directive({selector:unknownSelector,standalone:'wrong'})
export class UnknownSelector {}
@C({standalone:false,imports:[Good],template:'<p />'})
export class NotStandalone {}
@Pipe()
export class NoMetadataPipe {}
@NgModule()
export class EmptyModule {}
@Directive({selector:selector()})
export class HelperSelector {}
@C({templateUrl:'page.html'})
@Injectable({}, {})
export class InvalidSecondaryAfter {}
@Injectable({}, {})
@C({templateUrl:'page.html'})
export class InvalidSecondaryBefore {}
@Pipe({})
export class MissingPipeName {}
@TypeComponent({template:'<p>type only</p>'})
export class TypeOnly {}
export function shadow(core: unknown) {
  @core.Component({template:'<p>parameter shadow</p>'})
  class ParameterShadow {}
  return ParameterShadow;
}
