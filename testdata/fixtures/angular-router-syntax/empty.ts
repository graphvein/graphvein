export const emptySpecifiers = [
  { loadComponent: () => import('') },
  { loadChildren: () => import('').then(m => m.Routes) },
];
