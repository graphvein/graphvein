import {Component, NgModule, Pipe} from '@angular/core';
import {CommonModule} from '@angular/common';
@Pipe({name: 'legacy'})
export class LegacyPipe {}
@Component({template: '{{value | legacy}}'})
export class LegacyHost {}
@NgModule({imports: [CommonModule], declarations: [LegacyHost, LegacyPipe]})
export class LegacyModule {}
