import HomePage from './pages/home';

function provideRouter(routes: unknown) {
  return routes;
}

export const localConfig = provideRouter([{ path: 'local-factory', component: HomePage }]);
