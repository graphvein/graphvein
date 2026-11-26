// Row NX-L1 (T024): a shared package may run in any of the
// repository's Next.js applications, so a route of one of them is only a
// candidate (ambiguous, nextjs_origin unknown).
export function loadStats() {
  return fetch('/api/stats');
}
