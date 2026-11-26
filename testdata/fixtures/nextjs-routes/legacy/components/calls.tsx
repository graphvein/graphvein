'use client';

// Row NX-L3 (T024): Next.js checks the file system before
// afterFiles rules and every route before fallback rules, so a route that
// surely serves the path wins over a rule with an external destination.
export function Calls() {
  // Linked: the static route serves it before the afterFiles rule.
  fetch('/api/head');
  // Linked: a route serves it before the fallback rule.
  fetch('/api/mixed', { method: 'POST' });
  // Linked: the dynamic route serves it before the fallback rule.
  fetch('/api/items/3');
  // Withheld: no route serves it, so the fallback rule sends it to the
  // old site (nextjs_rewrite "/:path*").
  fetch('/api/nothing');
  return null;
}
