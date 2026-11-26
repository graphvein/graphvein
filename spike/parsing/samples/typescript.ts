import { Component, Injectable } from '@angular/core';
@Component({ selector: 'app-items', templateUrl: './angular.html' })
export class ItemsComponent { items: string[] = []; }
@Injectable()
export class ItemsService { list(): string[] { return []; } }
@Component({ selector: 'app-inline', template: `<p *ngIf="ready">{{ name | uppercase }}</p>` })
export class InlineComponent {}
export function identity<T>(value: T): T { return value; }
