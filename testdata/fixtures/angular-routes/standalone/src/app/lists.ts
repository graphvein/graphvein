import { Routes } from '@angular/router';
import HomePage from './pages/home';

const head = [{ path: 'spread-head', component: HomePage }];

export const satisfied = [{ path: 'satisfied', component: HomePage }] satisfies Routes;
export const spreadRoutes: Routes = [...head, { path: 'spread-tail', component: HomePage }];
export const mutable: Routes = [{ path: 'mutable', component: HomePage }];
mutable.push({ path: 'pushed', component: HomePage });
