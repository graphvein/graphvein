import {Component} from '@angular/core';
import {CommonModule} from '@angular/common';
import {Child, Dir, Secondary, LocalPipe, DuplicateOne, DuplicateTwo, UnknownPipe, ClassSelector, NotSelector, UnknownSelector, ValueSelector, Compound, Bindings, I18n, X, BothOne, BothTwo, BothDirective} from './targets';
@Component({selector: 'app-closed', standalone: true, imports: [Child, Dir, LocalPipe], template: '<app-closed/><app-child dir />{{value | local | missing}}<app-missing/>'})
export class Closed {}
@Component({selector: 'app-open', standalone: true, imports: [Child, CommonModule, LocalPipe], template: '<app-child/><app-external/>{{value | local | external}}'})
export class Open {}
@Component({standalone: true, imports: [ValueSelector, Compound, Bindings, I18n, X], templateUrl: './attributes.html'})
export class Attributes {}
@Component({standalone: true, imports: [Dir, Secondary], template: '<div *dir="items"/><div *dir="items" *other="x"/><ng-template dir></ng-template>'})
export class Structural {}
@Component({standalone: true, imports: [Secondary], template: '<div *dir="items"/>'})
export class SecondaryOnly {}
@Component({standalone: true, imports: [ClassSelector, NotSelector, UnknownSelector], template: '<app-uncertain/>'})
export class Unmodeled {}
@Component({standalone: true, imports: [Child, X], template: '<div ngNonBindable><app-child [x]="a"></app-child></div>'})
export class NonBindable {}
@Component({standalone: true, imports: [BothOne, BothTwo, BothDirective], template: '<app-both/>'})
export class Multiple {}
@Component({standalone: true, imports: [LocalPipe], templateUrl: './pipes.html'})
export class Pipes {}
@Component({standalone: true, imports: [DuplicateOne, DuplicateTwo], template: '{{value | duplicate}}'})
export class DuplicatePipes {}
@Component({standalone: true, imports: [LocalPipe, UnknownPipe], template: '{{value | local | absent}}'})
export class UnknownPipes {}
@Component({standalone: true, imports: [Child], templateUrl: './shared.html'})
export class SharedOne {}
@Component({standalone: true, imports: [Child], templateUrl: './shared.html'})
export class SharedTwo {}
@Component({standalone: true, imports: [Child], deferredImports: [Child], template: '<app-child/><app-later/>'})
export class Deferred {}
@Component({standalone: UNKNOWN, template: '<app-child/>'})
export class UnknownStandalone {}
@Component({standalone: true, template: '<APP-X/><ns:app-x/><ng-content><app-missing/></ng-content><script>text</script><style>text</style><link rel="stylesheet" href="x.css">'})
export class Names {}
@Component({standalone: true, templateUrl: '.templates/x.html'})
export class DotPath {}
@Component({standalone: true, templateUrl: '/shared.html'})
export class RootedPath {}
