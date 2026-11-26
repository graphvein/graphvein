import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

const routes: Routes = [
  { path: 'settings', loadChildren: () => import('./settings/settings.module').then(m => m.SettingsModule) },
  { path: 'lazy-root', loadChildren: () => import('./lazy-root/lazy-root.module').then(m => m.LazyRootModule) },
  { path: 'cycle', loadChildren: () => import('./cycle/cycle-a.module').then(m => m.CycleAModule) },
  { path: 'component-children', loadChildren: () => import('./app.component').then(m => m.AppComponent) },
  { path: 'lazy-providers', loadChildren: () => import('./lazy-providers/lazy-providers.module').then(m => m.LazyProvidersModule) },
  { path: 'partial', loadChildren: () => import('./partial/partial.module').then(m => m.PartialModule) },
];

@NgModule({
  imports: [RouterModule.forRoot(routes, { useHash: true })],
  exports: [RouterModule],
})
export class AppRoutingModule {}
