import {Component, NgModule, Pipe, forwardRef} from '@angular/core';
import {CommonModule, DatePipe} from '@angular/common';
import {LocalPipe, OtherPipe, UnknownPipe, ClosedModule, OpenModule, OpenFirstModule} from './targets';
const LOCAL = [LocalPipe];
const EXTERNAL = [CommonModule];
@Component({imports: [CommonModule, LocalPipe], template: '{{value | local}} {{other | local}}'})
export class After {}
@Component({imports: [LocalPipe, CommonModule], template: '{{value | local}}'})
export class Before {}
@Component({imports: [LocalPipe, CommonModule, LocalPipe], template: '{{value | local}}'})
export class Repeat {}
@Component({imports: [OpenModule], template: '{{value | local}}'})
export class OpenUnit {}
@Component({imports: [OpenFirstModule], template: '{{value | local}}'})
export class OpenFirstUnit {}
@Component({imports: [CommonModule, ClosedModule], template: '{{value | local}}'})
export class ClosedUnit {}
@Component({imports: [CommonModule, LocalPipe, DatePipe], template: '{{value | local}}'})
export class RepeatedExternalPackage {}
@Component({imports: [ClosedModule, CommonModule, LocalPipe], template: '{{value | local}}'})
export class FirstContribution {}
@Component({imports: [OtherPipe, LocalPipe], template: '{{value | local}}'})
export class DuplicateNames {}
@Component({imports: [CommonModule, LocalPipe, OtherPipe], template: '{{value | local}}'})
export class OpenDuplicateNames {}
@Component({imports: [LocalPipe, UnknownPipe], template: '{{value | local}}'})
export class UnknownName {}
@Component({imports: [CommonModule, LocalPipe, UnknownPipe], template: '{{value | local}}'})
export class OpenUnknownName {}
@Component({imports: [CommonModule, LocalPipe], template: '<button (click)="value | local"></button><input [(value)]="value | local"/>@for (item of items; track item | local) { {{item}} }'})
export class InvalidContexts {}
@Component({imports: [...EXTERNAL, ...LOCAL], template: '{{value | local}}'})
export class SpreadAfter {}
@Component({imports: [CommonModule, forwardRef(() => LocalPipe)], template: '{{value | local}}'})
export class ForwardAfter {}
@Component({imports: [CommonModule, {ngModule: ClosedModule}], template: '{{value | local}}'})
export class ObjectAfter {}
@Pipe({name: 'declared', standalone: false})
export class DeclaredPipe {}
@Component({standalone: false, template: '{{value | declared}}'})
export class DeclaredHost {}
@NgModule({imports: [CommonModule.forRoot()], declarations: [DeclaredHost, DeclaredPipe]})
export class DeclaringModule {}
