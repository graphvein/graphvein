import { getArticle } from '~api/article-data/article-data';
export function call() { return getArticle('slug'); }
