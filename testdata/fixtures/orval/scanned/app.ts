import { getArticle } from './api/generated/article-data/article-data';
export function call() { return getArticle('slug'); }
