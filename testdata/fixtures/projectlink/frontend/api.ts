// Calls of the frontend member. Every URL uses /t022 so the repository's
// own self-index links no other fixture.
export function listUsers() {
  return fetch(`${import.meta.env.VITE_API}/t022/users`);
}
export function getUser(user: { id: string }) {
  return fetch(`${import.meta.env.VITE_API}/t022/users/${user.id}`);
}
export function createOrder() {
  return fetch(`${import.meta.env.VITE_API}/t022/orders`, { method: 'POST' });
}
export function duplicate() {
  return fetch(`${import.meta.env.VITE_API}/t022/duplicate`);
}
export function health() {
  return fetch(`${import.meta.env.VITE_API}/t022/health`);
}
export function partner() {
  return fetch('https://partner.example.com/t022/users');
}
export function either(flag: boolean) {
  return fetch(flag ? `${import.meta.env.VITE_API}/t022/users` : `${import.meta.env.VITE_API}/t022/orders`);
}
export function sameRoute() {
  return fetch(`${import.meta.env.VITE_API}/t022/same`);
}
// Alternatives with different bases: each alternative is matched with its
// own stored base expression.
export function mixed(flag: boolean, id: string) {
  return fetch(flag ? `${import.meta.env.VITE_API}/t022/users/${id}` : `${import.meta.env.VITE_PARTNER}/t022/users`);
}
declare function nextUrl(): string;
export function anywhere() {
  return fetch(nextUrl());
}
