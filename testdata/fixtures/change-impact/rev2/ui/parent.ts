import {Component} from '@angular/core';
import {ChildComponent} from './child';

@Component({selector: 'app-parent', standalone: true, imports: [ChildComponent], template: '<app-child/>'})
export class ParentComponent {}
