import { Component, NgModule, Injectable } from '@angular/core';

@Component({ selector: 'app-docs', template: '' })
export class DocsPage {}

@Component({ selector: 'app-legacy', template: '', standalone: false })
export class LegacyPage {}

@NgModule({})
export class DocsModule {}

@Injectable({ providedIn: 'root' })
export class TitleResolver {}

export class Plain {}
