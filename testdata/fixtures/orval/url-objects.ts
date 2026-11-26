export function assigned() {
  const url = new URL('https://fixture/t012e/url/assigned');
  url.protocol = deploymentProtocol;
  url.host = deploymentHost;
  return fetch(url.toString());
}
export function literal() {
  const url = new URL('https://fixture/t012e/url/literal', unknownBase);
  return fetch(url.href);
}
export function unknownBase() {
  const url = new URL('/t012e/url/unknown', deployment);
  return fetch(url.href);
}
export function pathnameWrite() {
  const url = new URL('https://fixture/t012e/url/pathname');
  url.pathname = '/t012e/url/rewritten';
  return fetch(url.href);
}
export function searchParams() {
  const url = new URL('https://fixture/t012e/url/search');
  url.searchParams.set('x', '1');
  return fetch(url.toString());
}
export function dotSegment() {
  const url = new URL('https://fixture/t012e/url/../dot');
  return fetch(url.toString());
}
export function relative() {
  const url = new URL('t012e/url/relative', 'https://fixture');
  return fetch(url.href);
}
export function localAlias() {
  const url = new URL('/t012e/url/alias', 'https://fixture/ignored-base-path');
  const text = url.href;
  return fetch(text);
}
export function twoAliases() {
  const url = new URL('https://fixture/t012e/url/aliases');
  const one = url.href;
  const two = one;
  return fetch(two);
}
export function hostAssigned() {
  const url = new URL('/t012e/url/host', unknownBase);
  url.host = deploymentHost;
  return fetch(url.href);
}
export function hostnameAssigned() {
  const url = new URL('/t012e/url/hostname', unknownBase);
  url.hostname = deploymentHost;
  return fetch(url.href);
}
export function portAssigned() {
  const url = new URL('/t012e/url/port', unknownBase);
  url.port = deploymentPort;
  return fetch(url.href);
}
export function conditionalAssignment() {
  const url = new URL('https://fixture/t012e/url/conditional');
  if (condition) url.host = deploymentHost;
  return fetch(url.href);
}
export function escapes() {
  const url = new URL('https://fixture/t012e/url/escape');
  mutate(url);
  return fetch(url.href);
}
export function shadow(URL: unknown) {
  const url = new URL('https://fixture/t012e/url/shadow');
  return fetch(url.href);
}
export function encodedDot() {
  const url = new URL('https://fixture/t012e/url/%2e%2e/dot');
  return fetch(url.href);
}
export function invalidPercent() {
  const url = new URL('https://fixture/t012e/url/%oops');
  return fetch(url.href);
}
export function whitespace() {
  const url = new URL('https://fixture/t012e/url/white space');
  return fetch(url.href);
}
export function networkPath() {
  const url = new URL('//elsewhere/t012e/url/network', 'https://fixture');
  return fetch(url.href);
}
const moduleURL = new URL('https://fixture/t012e/url/module');
export function moduleUse() { return fetch(moduleURL.href); }
