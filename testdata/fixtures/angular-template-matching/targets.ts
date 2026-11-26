import {Component, Directive, Pipe} from '@angular/core';
@Component({selector: 'app-child', standalone: true, template: ''})
export class Child {}
@Directive({selector: '[dir]', standalone: true})
export class Dir {}
@Directive({selector: '[dirOf]', standalone: true})
export class Secondary {}
@Pipe({name: 'local', standalone: true})
export class LocalPipe {}
@Pipe({name: 'duplicate', standalone: true})
export class DuplicateOne {}
@Pipe({name: 'duplicate', standalone: true})
export class DuplicateTwo {}
@Pipe({name: UNKNOWN, standalone: true})
export class UnknownPipe {}
@Directive({selector: '.classy', standalone: true})
export class ClassSelector {}
@Directive({selector: 'button:not([off])', standalone: true})
export class NotSelector {}
@Directive({selector: SELECTOR, standalone: true})
export class UnknownSelector {}
@Directive({selector: '[a="VALUE"]', standalone: true})
export class ValueSelector {}
@Directive({selector: 'button[dir],app-other[x]', standalone: true})
export class Compound {}
@Directive({selector: '[xChange][x][click]', standalone: true})
export class Bindings {}
@Directive({selector: '[i18n]', standalone: true})
export class I18n {}
@Directive({selector: '[x]', standalone: true})
export class X {}
@Component({selector: 'app-both', standalone: true, template: ''})
export class BothOne {}
@Component({selector: 'app-both', standalone: true, template: ''})
export class BothTwo {}
@Directive({selector: 'app-both', standalone: true})
export class BothDirective {}
