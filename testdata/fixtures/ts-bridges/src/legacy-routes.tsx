import { lazy } from 'react';
import * as React from 'react';
import { Route, createRoutesFromElements } from 'react-router-dom';
import { Route as FakeRoute } from './fake-route';
import { Shell } from './components/shell';
import { Home } from './components/home';
import { rootLoader } from './lib/loaders';
import MemoCard from './components/card';

const LazyDefault = lazy(() => import('./pages/lazy-default'));
const LazyNamespace = React.lazy(() => import('./pages/lazy-default'));
const NotReact = makeLazy(() => import('./pages/lazy-default'));

function makeLazy(load: () => unknown) {
  return load;
}

// RR2: <Route> elements of react-router-dom, nested children included.
export const legacy = createRoutesFromElements(
  <Route path="/" element={<Shell><Home /></Shell>}>
    <Route path="a" element={<LazyDefault />} />
    <Route index loader={rootLoader} />
    {true && <Route path={'b' + 1} element={<LazyNamespace />} />}
    <Route path="c" element={<NotReact />} />
    <Route path="d" element={<MemoCard />} />
  </Route>,
);

// A local component named Route is not react-router's: no ui_route.
export const fake = <FakeRoute path="x" element={<Home />} />;
