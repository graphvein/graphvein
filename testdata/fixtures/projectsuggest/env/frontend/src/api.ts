// The frontend's only call: through VITE_API. Every URL uses /b6b so the
// repository's own self-index links no other fixture.
export function users() {
  return fetch(`${import.meta.env.VITE_API}/b6b/env-users`);
}
