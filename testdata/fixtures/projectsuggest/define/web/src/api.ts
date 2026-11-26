// Calls of the web member. Every URL uses /b6b so the repository's own
// self-index links no other fixture.
export function users() {
  return fetch(`${__API_URL__}/b6b/users`);
}
export function staticBase() {
  return fetch(`${__STATIC__}/b6b/static`);
}
export function service() {
  return fetch(`${process.env.SERVICE_URL}/b6b/service`);
}
export function self() {
  return fetch(`${process.env.SELF_URL}/b6b/self`);
}
export function cache() {
  return fetch(`${process.env.CACHE_URL}/b6b/cache`);
}
export function noPort() {
  return fetch(`${process.env.NOPORT_URL}/b6b/noport`);
}
export function quoted() {
  return fetch(`${__QUOTED__}/b6b/quoted`);
}
