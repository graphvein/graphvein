// The frontend's client calls. Every URL uses /t022b7 so the repository's
// own self-index links no other fixture.
export function listUsers() {
  return fetch(`${import.meta.env.VITE_API}/t022b7/users`);
}
export function createUser() {
  return fetch(`${import.meta.env.VITE_API}/t022b7/users`, { method: 'POST' });
}
