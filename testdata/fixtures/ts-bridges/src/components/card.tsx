import { memo } from 'react';

function CardImpl({ title }: { title: string }) {
  return <h3>{title}</h3>;
}

// A memo-wrapped default export is an expression, not a declaration: a tag
// bound to it resolves to nothing (D).
export default memo(CardImpl);

export function Card({ title }: { title: string }) {
  return <CardImpl title={title} />;
}
