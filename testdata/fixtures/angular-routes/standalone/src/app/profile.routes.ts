import { Routes } from '@angular/router';
import { DocsPage } from './pages/docs';

const routes: Routes = [
  {
    path: '',
    children: [
      { path: ':username', component: DocsPage, children: [{ path: 'favorites', loadComponent: () => import('./pages/home') }] },
    ],
  },
];

export default routes;
