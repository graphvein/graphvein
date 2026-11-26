import { provideRouter } from '@angular/router';
import HomePage from './pages/home';
import { AuthGuard } from './guards';

const matchAll = () => null;

export const invalidConfig = [
  provideRouter([
    { path: '/rooted', component: HomePage },
    { path: 'both', component: HomePage, loadComponent: () => import('./pages/home') },
    { path: 'empty-target' },
    { component: HomePage },
    { path: '', redirectTo: 'docs' },
    { path: 'm', matcher: matchAll, component: HomePage },
    { path: '', matcher: matchAll, component: HomePage },
    { path: 'r', redirectTo: 'docs', children: [], canActivate: [AuthGuard] },
    { path: 'named', outlet: 'side' },
    { path: 'r-load', redirectTo: 'docs', loadChildren: () => import('./admin.routes'), canMatch: [AuthGuard] },
  ]),
];
