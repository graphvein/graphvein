import { ApplicationConfig } from '@angular/core';
import { provideRouter } from '@angular/router';
import { routes } from './app.routes';
import HomePage from './pages/home';

export const appConfig: ApplicationConfig = { providers: [provideRouter(routes)] };

export const unusedConfig: ApplicationConfig = { providers: [provideRouter([{ path: 'unbootstrapped', component: HomePage }])] };
export const mergedConfig: ApplicationConfig = { providers: [provideRouter([{ path: 'merged', component: HomePage }])] };
export const specOnlyProviders = [provideRouter([{ path: 'spec-bootstrapped', component: HomePage }])];
