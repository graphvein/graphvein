// A fetch wrapper: it forwards its URL parameter after a literal prefix.
export function apiGet(path: string) {
  return fetch(`/t016d-api${path}`);
}
