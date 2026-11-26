import { provideRouter } from '@angular/router';
import HomePage from './pages/home';

export const ROUTE_PROVIDERS = [provideRouter([{ path: 'route-scoped', component: HomePage }])];
