import { createBrowserRouter } from 'react-router';
import { ArticlePage } from './page';
import { articleLoader } from './loader';

export const router = createBrowserRouter([{ path: '/article', Component: ArticlePage, loader: articleLoader }]);
