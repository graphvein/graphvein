import { Routes } from '@angular/router';
import { NotFoundPage } from './pages/home';
import { LegacyPage } from './pages/docs';

export const shared: Routes = [{ path: 'help', component: NotFoundPage }, { path: 'legacy-shared', component: LegacyPage }];
