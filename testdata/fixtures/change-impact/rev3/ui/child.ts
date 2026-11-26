import {Component} from '@angular/core';

@Component({selector: 'app-child', standalone: true, template: '<p>child</p>'})
export class ChildComponent {
  count = 1;
}
