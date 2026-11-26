import type { RouteObject } from 'react-router';

export const articleRoute = {
  path: 'article/:slug',
  lazy: async () => {
    const [{ ArticlePage: Component }, { articleLoader: loader }] = await Promise.all([
      import('~pages/article/article.ui'),
      import('~pages/article/article.loader'),
    ]);
    return { Component, loader };
  },
  children: [
    {
      path: 'delete',
      lazy: async () => {
        const { deleteAction: action } = await import('./article.actions');
        return { action, loader: (await import('./article.loader')).articleLoader };
      },
    },
    {
      path: 'static',
      loader: () => null,
      lazy: () => import('./article.actions'),
    },
  ],
} satisfies RouteObject;
