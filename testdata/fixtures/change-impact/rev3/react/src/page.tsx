import { ArticleContent, ArticleMeta } from './article';

export function ArticlePage() {
  return (
    <main>
      <ArticleContent body="text" />
      <ArticleMeta />
    </main>
  );
}

export function PreviewPage() {
  return <ArticleContent body="preview" />;
}
