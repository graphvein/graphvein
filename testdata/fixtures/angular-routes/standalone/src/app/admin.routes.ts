import { Routes } from '@angular/router';
import { DocsPage, LegacyPage } from './pages/docs';

export default [
  { path: '', component: DocsPage },
  { path: 'legacy', component: LegacyPage },
] satisfies Routes;
