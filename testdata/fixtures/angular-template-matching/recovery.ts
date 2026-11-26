import {Component} from '@angular/core';
import {Child} from './targets';
@Component({standalone: true, imports: [Child], template: '<app-child/> @mystery { <app-child/> } <app-child/>'})
export class UnknownBlock {}
@Component({standalone: true, imports: [Child], template: '@empty { <app-child/> } <app-child/>'})
export class OrphanEmpty {}
@Component({standalone: true, imports: [Child], template: '@if (ok) { <app-child/> } <div></div> @else { <app-child/> }'})
export class OrphanElse {}
@Component({standalone: true, imports: [Child], template: '@if (ok) { <app-child/> } <!-- connected --> @else { <app-child/> }'})
export class ConnectedElse {}
@Component({standalone: true, imports: [Child], template: '{{ value! }} <app-child/>'})
export class TextRecovery {}
@Component({standalone: true, imports: [Child], template: '<app-child [config]="value()!"/>'})
export class ExpressionRecovery {}
@Component({standalone: true, imports: [Child], template: '@case (1) { <app-child/> } @default { <app-child/> }'})
export class OrphanSwitch {}
