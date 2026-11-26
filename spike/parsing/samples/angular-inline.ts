import { Component } from '@angular/core';

@Component({
  selector: 'app-multiline',
  template: `
<section *ngIf="ready">
  <p *ngFor="let item of items" [title]="item.name" (click)="select(item)">{{ item.name | uppercase }}</p>
  <input [(ngModel)]="name" />
</section>
@if (ready) {
  @for (item of items; track item.id) {
    <app-item [item]="item" />
  } @empty { <p>No items</p> }
} @else { <p>Loading</p> }
`
})
class MultilineComponent {}

@Component({ template: '<strong>{{ name }}</strong>' })
class SingleLineComponent {}

// A template property outside Component metadata must not be extracted.
const unrelated = { template: '<aside>Ignore me</aside>' };
