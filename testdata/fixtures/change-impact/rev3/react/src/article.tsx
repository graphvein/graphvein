export function ArticleContent({ body }: { body: string }) {
  return <p className="body article">{body.trim()}</p>;
}

export function ArticleMeta() {
  return <small>meta</small>;
}
