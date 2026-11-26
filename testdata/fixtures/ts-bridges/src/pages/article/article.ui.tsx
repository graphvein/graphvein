import { Card } from '../../components/card';

export function ArticlePage() {
  return (
    <div className="article">
      <ArticleBody />
      <Card title="x" />
    </div>
  );
}

function ArticleBody() {
  return <p>body</p>;
}
