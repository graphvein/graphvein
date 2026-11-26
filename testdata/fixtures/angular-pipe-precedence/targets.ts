import {NgModule, Pipe} from '@angular/core';
import {CommonModule} from '@angular/common';
@Pipe({name: 'local'})
export class LocalPipe {}
@Pipe({name: 'local'})
export class OtherPipe {}
@Pipe({name: UNKNOWN})
export class UnknownPipe {}
@NgModule({imports: [LocalPipe], exports: [LocalPipe]})
export class ClosedModule {}
@NgModule({imports: [LocalPipe, CommonModule], exports: [LocalPipe, CommonModule]})
export class OpenModule {}
@NgModule({imports: [LocalPipe, CommonModule], exports: [CommonModule, LocalPipe]})
export class OpenFirstModule {}
