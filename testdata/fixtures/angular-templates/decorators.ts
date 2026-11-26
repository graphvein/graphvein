import { Component as C, Directive, Pipe, Injectable, NgModule } from '@angular/core';
import * as core from '@angular/core';
@C({ selector: 'first', template: '<p>{{ value | inline }}</p>', imports: [], standalone: false })
export class First {}
@core.Component(({ template: `<b [title]="value | title">{{ value }}</b>
<i>{{ value | later }}</i>` } as const))
export default class Second {}
@Directive({ selector: '[dir]', exportAs: 'a,b' })
class Dir {}
@Pipe({ name: 'pipe', standalone: true })
class P {}
@Injectable({ providedIn: 'root' })
class Service {}
@NgModule({ declarations: [First], imports: [], exports: [First] })
class Module {}
@Zero()
class Zero {}
@Other({ template: '<u />', jit: true, selector: '', standalone, factory() { return 1; } })
class Other {}
@C({ template: `${value}` })
class Substitution {}
@C({ template: '<p>escaped\n</p>' })
class Escape {}
@C({ template: value })
class Nonliteral {}
@C({ ...metadata })
class Spread {}
@C({ [key]: '<p />' })
class Computed {}
