import { getArticle } from './api/generated/articles/articles';
export function call() { return getArticle('slug'); }
