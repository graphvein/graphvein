import { createBrowserRouter } from 'react-router';
import { ArticlePage, PreviewPage } from './page';
import { articleLoader } from './loader';

export const router = createBrowserRouter([
  { path: '/article', Component: ArticlePage, loader: articleLoader },
  { path: '/preview', Component: PreviewPage },
]);
