import { getArticle, getArticle as renamed, getGetArticleQueryOptions as options, useGetArticle, getGetArticleQueryKey, getGetArticleUrl, createArticle } from './api/generated/article-data/article-data';
import * as ns from './api/generated/article-data/article-data';
import { getList } from './api/generated/single/single';
import { getArticle as wrongFile } from './api/generated/single/single';
import { queryClient } from './queryClient';
import { useQuery, useSuspenseQuery, useQueryClient } from '@tanstack/react-query';
export function app() {
  getArticle('a');
  renamed('b');
  ns.getArticle('c');
  getList({limit: 2});
  createArticle({title: 'test'});
  queryClient.fetchQuery(options('a'));
  queryClient.ensureQueryData((options('a') as Options));
  queryClient.prefetchQuery(options('a'));
  useQuery(options('a'));
  useSuspenseQuery(options('a'));
  const local = useQueryClient();
  local.fetchQuery(options('a'));
  useGetArticle('a');
  getGetArticleQueryKey('a');
  getGetArticleUrl('a');
  const saved = options('a');
  useQuery(saved);
  useQuery({...options('a')});
  useQuery(options('a', {query: {queryFn: custom}}));
  useGetArticle('a', {query: {queryFn: custom}});
  useQuery(options('a', {['query']: {['queryFn']: custom}}));
  useQuery(options('a', {query: {queryFn() { return custom(); }}}));
  wrongFile('a');
}
class QueryClient { fetchQuery(value: unknown) {} }
export function shadowed() {
  const local = new QueryClient();
  local.fetchQuery(options('a'));
}
