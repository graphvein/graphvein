import { Routes } from '@angular/router';
import { DocsPage } from './pages/docs';

export const REPORT_ROUTES: Routes = [
  { path: ':year', component: DocsPage },
];
