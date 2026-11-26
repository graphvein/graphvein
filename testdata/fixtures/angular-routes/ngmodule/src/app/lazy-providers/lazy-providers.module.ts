import { Component, NgModule } from '@angular/core';
import { provideRouter } from '@angular/router';

@Component({ selector: 'app-lazy-providers', template: '' })
export class LazyProvidersComponent {}

const LAZY_PROVIDERS = [provideRouter([{ path: 'x', component: LazyProvidersComponent }])];

@NgModule({ declarations: [LazyProvidersComponent], providers: LAZY_PROVIDERS })
export class LazyProvidersModule {}
