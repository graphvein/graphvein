import { RouterProvider, createBrowserRouter, redirect } from 'react-router';
import { Layout } from './components/layout';
import { Home } from './components/home';
import { rootLoader, saveAction } from './lib/loaders';
import { articleRoute } from '~pages/article/article.route';
import { extraRoutes } from './extra-routes';

const shared = { path: 'shared', Component: Home };

// RR1: the routes of a proven createBrowserRouter call.
export const browserRouter = () =>
  createBrowserRouter([
    {
      path: '/',
      Component: Layout,
      loader: rootLoader,
      action: saveAction,
      children: [{ index: true, element: <Home /> }, articleRoute, ...extraRoutes, shared],
    },
    { path: 'lazy', lazy: () => import('./pages/lazy-page') },
    { path: 'both', Component: Layout, element: <Home /> },
    { path: '*', loader: async () => redirect('/404') },
    { ...shared, path: 'open' },
  ]);

export default function App() {
  return <RouterProvider router={browserRouter()} />;
}
