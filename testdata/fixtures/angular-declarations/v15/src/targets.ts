import {Component, Directive, Pipe, Injectable, NgModule} from '@angular/core';
import {SELECTOR} from './constants';
@Component({selector:'declaration', template:'<p>declaration</p>'})
export class Declared {}
@Directive({selector:SELECTOR, exportAs:'a, b'})
export class Attribute {}
@Pipe({name:'local'})
export class LocalPipe {}
@Pipe({name:'standalone',standalone:true,pure:false})
export class StandalonePipe {}
@Component({standalone:true,template:'<p />'})
export class Standalone {}
@Injectable()
export class Service {}
@Directive({})
export class Selectorless {}
@NgModule({jit:false,imports:[Service]})
export class JitModule {}
@Component({template:'<p>duplicate</p>'})
export class Duplicate {}
@Component({template:'<p>single</p>'})
export class Single {}
export class Plain {}
@Component({template:'<p>array only</p>'})
export class ArrayOnly {}
@Component({template:'<p>dynamic namespace</p>'})
export class DynamicTarget {}
