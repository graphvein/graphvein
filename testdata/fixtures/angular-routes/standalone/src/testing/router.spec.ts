import { bootstrapApplication } from '@angular/platform-browser';
import { provideRouter } from '@angular/router';
import { specOnlyProviders } from '../app/app.config';
import { App } from '../app/pages/app';
import { routes } from '../app/app.routes';
import { REPORT_ROUTES } from '../app/reports.routes';
import HomePage from '../app/pages/home';

export const testProviders = [
  provideRouter(routes),
  provideRouter([{ path: 'harness', children: REPORT_ROUTES }, { path: 'test-only', component: HomePage }]),
];

bootstrapApplication(App, { providers: specOnlyProviders });
