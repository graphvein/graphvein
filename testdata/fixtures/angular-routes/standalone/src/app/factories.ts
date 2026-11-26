import { ModuleWithProviders, NgModule } from '@angular/core';
import { provideRouter } from '@angular/router';
import HomePage from './pages/home';

@NgModule({})
export class FeatureModule {
  static forRoot(): ModuleWithProviders<FeatureModule> {
    return { ngModule: FeatureModule, providers: [provideRouter([{ path: 'from-static', component: HomePage }])] };
  }
}

export function neverCalled() {
  return provideRouter([{ path: 'never-called', component: HomePage }]);
}
