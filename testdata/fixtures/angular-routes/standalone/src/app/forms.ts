import { Component } from '@angular/core';
import { provideRouter as configure } from '@angular/router';
import * as ng from '@angular/router';
import type { provideRouter as typed } from '@angular/router';
import { provideRouter as other } from 'other-router';
import HomePage from './pages/home';
import { satisfied, mutable, spreadRoutes } from './lists';

const local = [{ path: 'local-const', component: HomePage }];

export const aliasedConfig = [configure(local)];
export const namespacedConfig = [ng.provideRouter(satisfied)];
export const spreadConfig = [configure(spreadRoutes)];
export const mutableConfig = [configure(mutable)];
export const typedConfig = [typed([{ path: 'typed', component: HomePage }])];
export const otherConfig = [other([{ path: 'other', component: HomePage }])];

export function calleeShadow(configure: (routes: unknown) => unknown) {
  return configure([{ path: 'shadowed-callee', component: HomePage }]);
}

export function inlineShadow() {
  const HomePage = class {};
  return configure([{ path: 'inline-shadow', component: HomePage }]);
}

@Component({ selector: 'app-shell', template: '', providers: [configure([{ path: 'component-providers', component: HomePage }])] })
export class Shell {}
