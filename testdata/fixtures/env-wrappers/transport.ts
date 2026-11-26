const BASE = (import.meta.env.API as string | undefined) ?? 'https://service.example';
const POST = {method: 'POST'};
const HEADERS = {headers: {accept: 'application/json'}};

export function send(path: string, options: RequestInit = {}) {
  return fetch(`${BASE}${path}`, {...options, headers: {accept: 'application/json'}});
}
export function nested(path: string, options: RequestInit = {}) {
  return send(path, options);
}
export function third(path: string, options: RequestInit = {}) {
  return nested(path, options);
}
export function fourth(path: string, options: RequestInit = {}) {
  return third(path, options);
}
export function defaultPost(path: string, options: RequestInit = POST) {
  return fetch(`${BASE}${path}`, {...options});
}
export function unknownDefault(path: string, options = makeOptions()) {
  return fetch(`${BASE}${path}`, {...options});
}
export function mixed(path: string, options: RequestInit = {}) {
  return fetch(`${BASE}${path}`, {...POST, ...options, ...HEADERS});
}
export function opaque(path: string) {
  return fetch(`${getBase()}${path}`);
}
export function repeated(path: string, options = {}) {
  fetch(`${BASE}${path}`, {...options});
  return fetch(`${BASE}${path}`, {...options});
}
export function propertyMethod(path: string, options: RequestInit = {}) {
  return fetch(`${BASE}${path}`, {method: options.method ?? 'GET'});
}
const OPAQUE = makeOptions();
export function noDefault(path: string, options?: RequestInit) {
  return fetch(`${BASE}${path}`, {...options});
}
export function shadowedDefault(path: string, options: RequestInit = POST) {
  const POST = {method: 'DELETE'};
  return fetch(`${BASE}${path}`, {...options});
}
export function opaqueConstDefault(path: string, options: RequestInit = OPAQUE) {
  return fetch(`${BASE}${path}`, {...options});
}
