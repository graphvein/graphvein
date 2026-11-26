export function baseArgument() {
  const url = new URL('/bc/users', __API_URL__);
  return fetch(url.href);
}
export function envArgument() {
  const url = new URL('/bc/users', import.meta.env.VITE_API);
  return fetch(url.href);
}
export function anchored() {
  const url = new URL('/acme/settings', __API_URL__);
  return fetch(url.href);
}
export function assignedOrigin() {
  const requestUrl = new URL('http://localhost:3000/bc/orders');
  const apiUrl = new URL(__API_URL__);
  requestUrl.protocol = apiUrl.protocol;
  requestUrl.host = apiUrl.host;
  return fetch(requestUrl.toString(), { method: 'POST' });
}
export function hostOnly() {
  const requestUrl = new URL('http://localhost:3000/bc/users');
  const apiUrl = new URL(__API_URL__);
  requestUrl.host = apiUrl.host;
  return fetch(requestUrl.href);
}
export function twoSources() {
  const requestUrl = new URL('http://localhost:3000/bc/users');
  const apiUrl = new URL(__API_URL__);
  const other = new URL(__GLOBAL_API__);
  requestUrl.protocol = apiUrl.protocol;
  requestUrl.host = other.host;
  return fetch(requestUrl.href);
}
export function rewrittenSource() {
  const requestUrl = new URL('http://localhost:3000/bc/users');
  const apiUrl = new URL(__API_URL__);
  apiUrl.host = 'localhost:4000';
  requestUrl.protocol = apiUrl.protocol;
  requestUrl.host = apiUrl.host;
  return fetch(requestUrl.href);
}
export function unknownArgument() {
  const url = new URL('/bc/users', __COUNT__);
  return fetch(url.href);
}
