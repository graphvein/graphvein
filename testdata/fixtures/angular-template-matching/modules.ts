import {Component, NgModule} from '@angular/core';
import {CommonModule} from '@angular/common';
@Component({selector: 'app-public', standalone: false, template: ''})
export class Public {}
@Component({selector: 'app-private', standalone: false, template: ''})
export class Private {}
@NgModule({declarations: [Public, Private], imports: [CommonModule], exports: [Public]})
export class BaseModule {}
@NgModule({exports: [BaseModule]})
export class MiddleModule {}
@Component({standalone: false, template: '<app-public/><app-private/>'})
export class ModuleHost {}
@NgModule({declarations: [ModuleHost, ModuleHost], imports: [MiddleModule]})
export class HostModule {}
@Component({standalone: true, imports: [MiddleModule], template: '<app-public/><app-private/>'})
export class StandaloneModuleHost {}
@Component({standalone: false, template: '<app-public/>'})
export class CycleHost {}
@NgModule({declarations: [CycleHost], imports: [CycleB]})
export class CycleA {}
@NgModule({imports: [CycleA]})
export class CycleB {}
@Component({standalone: false, template: '<app-public/>'})
export class Duplicate {}
@Component({standalone: false, template: '<app-public/>'})
export class DuplicateModuleHost {}
@NgModule({declarations: [Duplicate, DuplicateModuleHost]})
export class DuplicateA {}
@NgModule({declarations: [Duplicate]})
export class DuplicateB {}
@NgModule({exports: [Private]})
export class InvalidExportsModule {}
@Component({standalone: true, imports: [InvalidExportsModule], template: '<app-private/>'})
export class InvalidExportsHost {}
