import {Component} from '@angular/core';
import {ChildComponent} from './child';
import {BadgeComponent} from './badge';

@Component({selector: 'app-parent', standalone: true, imports: [ChildComponent, BadgeComponent], template: '<app-child/><app-badge/>'})
export class ParentComponent {}
