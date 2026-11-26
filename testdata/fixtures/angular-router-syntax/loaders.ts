import { Component, Namespace } from './targets';
export const routes = [
  { path: 'default', loadComponent: () => import('./targets') },
  { path: 'returned', loadChildren: function () { return (import(`./targets`)); } },
  { path: 'member', loadComponent: () => import('./targets').then(m => m.Component) },
  { path: 'function-member', loadChildren: function () { return import('./targets').then(function (m) { return (m.M); }); } },
  { path: 'sync', loadComponent: () => Component },
  { path: 'chain', 'loadChildren': function () { return Namespace.Routes; } },
  { path: 'not-function', loadComponent: Component },
  { path: 'async', loadComponent: async () => import('./targets') },
  { path: 'generator', loadChildren: function* () { return Component; } },
  { path: 'dynamic', loadComponent: () => import(specifier) },
  { path: 'substitution', loadChildren: () => import(`./${name}`) },
  { path: 'escape', loadComponent: () => import('./tar\gets') },
  { path: 'destructure', loadComponent: () => import('./targets').then(({ Component }) => Component) },
  { path: 'zero-then', loadComponent: () => import('./targets').then() },
  { path: 'two-then', loadComponent: () => import('./targets').then(m => m.Component, failed) },
  { path: 'string', loadChildren: './targets#M' },
  { path: 'multi', loadComponent: () => { setup(); return Component; } },
  { path: 'other', loadComponent: () => factory() },
  { path: 'wrong-member', loadComponent: () => import('./targets').then(m => other.Component) },
  { path: 'computed-member', loadComponent: () => import('./targets').then(m => m['Component']) },
  { path: 'async-then', loadComponent: () => import('./targets').then(async m => m.Component) },
];
export function shadowLoader(Component: unknown) {
  return { loadComponent: () => Component };
}
export const parameterLoader = { loadComponent: (Component) => Component };
