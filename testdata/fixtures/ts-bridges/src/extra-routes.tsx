import { Home } from './components/home';
import { Shell } from './components/shell';

export const extraRoutes = [
  { path: 'shell', element: <Shell><Home /></Shell> },
  { path: `tpl`, Component: Shell },
];

// Not reached from any router: no ui_route node.
export const unused = { path: 'unused', Component: Home };
