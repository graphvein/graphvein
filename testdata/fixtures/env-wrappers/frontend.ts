import {send, nested, third, fourth, defaultPost, unknownDefault, mixed, opaque, repeated, propertyMethod, noDefault, shadowedDefault, opaqueConstDefault} from './transport';
const BASE = (process.env.API as string | undefined) || 'https://service.example';
const POST = {method: 'POST'};
const PLAIN = {headers: {accept: 'application/json'}};

export function run(options: RequestInit) {
  send('/t015b-requests');
  send('/t015b-requests', undefined);
  send('/t015b-requests', {method: 'POST'});
  send('/t015b-requests', {method: 'PATCH'});
  send('/t015b-requests', {method: 'DELETE'});
  send('/t015b-requests', options);
  send('/t015b-requests', POST);
  send('/t015b-requests', {...options, method: 'POST'});
  send('/t015b-requests', {method: 'POST', ...options});
  send('/t015b-requests', {...POST});
  send('/t015b-requests', {...PLAIN});
  send('/t015b-requests', {...undefined});
  send('/t015b-requests', {...null});
  send('/t015b-requests', {...{...POST, ...PLAIN}});
  send('/t015b-requests', {method: undefined});
  send('/t015b-requests', {method: 'DELETE', ...{method: undefined}});
  send('t015b-requests', {method: 'POST'});
  nested('/t015b-requests', {method: 'PATCH'});
  third('/t015b-requests', {method: 'DELETE'});
  fourth('/t015b-requests', {method: 'POST'});
  defaultPost('/t015b-requests');
  defaultPost('/t015b-requests', undefined);
  unknownDefault('/t015b-requests');
  mixed('/t015b-requests');
  mixed('/t015b-requests', {method: 'PATCH'});
  opaque('/t015b-requests');
  repeated('/t015b-requests', {method: 'POST'});
  fetch(`${BASE}/t015b-requests`);
  fetch(`${process.env.API ?? 'https://service.example'}/t015b-requests`);
  fetch('/t015b-requests', {...options, method: 'POST'});
  fetch('/t015b-requests', {method: 'POST', ...options});
  fetch('/t015b-requests', {...POST});
  fetch('/t015b-requests', {...PLAIN});
  fetch('/t015b-requests', {...undefined});
  fetch('/t015b-requests', {...null});
  fetch('/t015b-requests', {method: undefined});
  fetch('/t015b-requests', {method: 'GET', ...{method: 'POST'}, method: 'PATCH'});
  fetch('/t015b-requests', {method: 'GET', ...{method: 'POST'}});
  const local = {method: 'DELETE'};
  fetch('/t015b-requests', {...local});
  fetch('/t015b-requests', {...options, ...{...POST}});
}

export function shadow(undefined: RequestInit) {
  send('/t015b-requests', undefined);
  fetch('/t015b-requests', {...undefined});
}

export function loadItem(id: string) { return send(`/t015b-requests/${id}`); }
export function load() { return loadItem('one'); }
export function propertyCalls(options: RequestInit) {
  propertyMethod('/t015b-requests', {...POST});
  propertyMethod('/t015b-requests', {...PLAIN});
  propertyMethod('/t015b-requests', {...options});
}
export function assertedBase() {
  fetch(`${(process.env.API as string)}/t015b-requests`);
}
const UNDEFINED_METHOD = {method: undefined};
export function provenUndefined(undefined: RequestInit) {
  fetch('/t015b-requests', {...UNDEFINED_METHOD});
}
export function localShadow() {
  const undefined = {method: 'DELETE'};
  fetch('/t015b-requests', {...undefined});
}
export function defaults() {
  noDefault('/t015b-requests');
  shadowedDefault('/t015b-requests');
  opaqueConstDefault('/t015b-requests');
}
