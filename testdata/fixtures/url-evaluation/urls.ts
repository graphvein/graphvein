export const URLs = { ITEMS: 'items', DOT: '/api/dot' } as const;
export function getURL(key: string, params = {}, v2 = false) {
  let url = URLs[key];
  for (const k of Object.keys(params)) { url += `/${params[k]}`; }
  return `${v2 ? '/v2/' : '/api/'}${url}`;
}
export function choose(flag: boolean) { return flag ? '/api/a' : '/api/b'; }
export function defaults(url = '/api/a') { return url; }
export function values(params = {}) {
  let url = '/api/items';
  for (const v of Object.values(params)) { url += `/${v}`; }
  return url;
}
export function keys(params = {}) {
  let url = '/api/items';
  for (const k in params) { url += `/${params[k]}`; }
  return url;
}
export function folded(flag: boolean) {
  let url = '/api/b';
  if (flag) { url = '/api/a'; } else { url += ''; }
  return url;
}
export function recursive() { return recursive(); }
export function level4() { return '/api/a'; }
export function level3() { return level4(); }
export function level2() { return level3(); }
export function level1() { return level2(); }
export async function asynchronous() { return '/api/a'; }
export function unsupported() { throw new Error('no'); return '/api/a'; }
export const mutated = { path: '/api/a' };
mutated.path = '/api/b';
export const spread = { ...URLs, path: '/api/a' };
export const updated = { path: '/api/a' };
updated.path++;
export const deleted = { path: '/api/a' };
delete deleted.path;
export const assigned = { path: '/api/a' };
Object.assign(assigned, { path: '/api/b' });
