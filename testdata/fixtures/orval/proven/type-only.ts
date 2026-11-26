import type { getArticle as RequestType } from './api/generated/article-data/article-data';
import { type getArticle as InlineType } from './api/generated/article-data/article-data';
import type * as Types from './api/generated/article-data/article-data';

export function typeOnlyImportsSupplyNoRuntimeBinding() {
  RequestType('a');
  InlineType('a');
  Types.getArticle('a');
}
