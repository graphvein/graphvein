import { Routes } from '@angular/router';
import { provideRouter } from '@angular/router';
import { externalGuard } from 'guards-lib';
import HomePage, { NotFoundPage } from './pages/home';
import { DocsPage, DocsModule, LegacyPage, TitleResolver, Plain } from './pages/docs';
import { AuthGuard, UserResolver, canEdit, makeGuard } from './guards';
import { DYNAMIC } from './segments';
import { shared } from './shared.routes';
import { ROUTE_PROVIDERS } from './route-providers';

const matchLegacy = () => null;
const base = { component: HomePage };
const redirectFn = () => 'docs';
const lazyBase = { loadComponent: () => import('./pages/home'), canActivate: [AuthGuard] };

export const routes: Routes = [
  { path: '', component: HomePage, pathMatch: 'full', title: 'Home' },
  {
    path: 'docs',
    title: TitleResolver,
    children: [
      { path: '', component: DocsPage },
      { path: ':id', component: DocsPage, canDeactivate: [canEdit] },
      { path: '**', redirectTo: '', pathMatch: 'full' },
    ],
  },
  { matcher: matchLegacy, component: DocsPage },
  { path: 'aside', component: DocsPage, outlet: 'side', children: [{ path: 'inner', component: DocsPage }] },
  { path: 'old', redirectTo: 'docs', pathMatch: 'full' },
  { path: 'fn-redirect', redirectTo: redirectFn },
  { path: 'inline-redirect', redirectTo: () => 'docs' },
  { path: 'dup', component: HomePage },
  { path: 'dup', component: DocsPage },
  { path: DYNAMIC, component: HomePage },
  {
    path: 'guarded',
    component: DocsPage,
    canActivate: [AuthGuard, canEdit, () => true, externalGuard, makeGuard()],
    canMatch: [AuthGuard],
    resolve: { user: UserResolver, inline: () => 1, missing: Unknown },
  },
  { path: 'scoped', component: HomePage, providers: [provideRouter([{ path: 'route-providers', component: HomePage }])] },
  { path: 'wrong-role', component: DocsModule },
  { path: 'plain', component: Plain },
  { path: 'standalone-false', component: LegacyPage },
  ...shared,
  { ...base, path: 'partial' },
  { path: 'method', loadComponent() { return import('./pages/home'); } },
  { path: 'lazy-default', loadComponent: () => import('./pages/home') },
  { path: 'lazy-member', loadComponent: () => import('./pages/docs').then(m => m.DocsPage) },
  { path: 'lazy-sync', loadComponent: () => NotFoundPage },
  { path: 'lazy-module-component', loadComponent: () => import('./pages/docs').then(m => m.DocsModule) },
  { path: 'lazy-legacy', loadComponent: () => import('./pages/docs').then(m => m.LegacyPage) },
  { path: 'admin', loadChildren: () => import('./admin.routes') },
  { path: 'profile', loadChildren: () => import('./profile.routes') },
  { path: 'reports', loadChildren: () => import('./reports.routes').then(m => m.REPORT_ROUTES) },
  { path: 'sync-children', loadChildren: () => shared },
  { path: 'invalid-children', loadChildren: () => import('./pages/home') },
  { path: 'async-children', loadChildren: async () => import('./admin.routes') },
  { path: 'provider-const', component: HomePage, providers: ROUTE_PROVIDERS },
  { ...lazyBase, path: 'spread-lazy' },
  { path: 'anon', loadChildren: () => import('./anon.routes') },
  { path: 'twins', loadChildren: () => import('./twins.routes') },
  { path: 'both-children', children: [{ path: 'kid', component: HomePage }], loadChildren: () => import('./admin.routes') },
  { path: ('paren'), component: HomePage },
  { path: 'cast' as const, component: HomePage },
  { path: '**', component: NotFoundPage },
];
