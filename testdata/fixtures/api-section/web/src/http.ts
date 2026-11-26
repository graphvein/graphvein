import axios from 'axios';
import {apiGet} from './client';

const remote = axios.create({baseURL: process.env.T016D_API});

export function linkedExact() {
  return fetch('/t016d-items');
}

export function linkedOrigin() {
  return fetch('http://localhost:4000/t016d-items/42');
}

export function linkedSuffix() {
  return remote.get('/t016d-users');
}

export function viaWrapper() {
  return apiGet('/t016d-users');
}

export function unmatched() {
  return fetch('/t016d-missing');
}

export function methodUnknown(method: string) {
  return fetch('/t016d-items', {method});
}

// An element of a parameter, not the parameter itself, so this is no wrapper.
export function urlUnevaluated(urls: string[]) {
  return fetch(urls[0]);
}

// Only a dynamic segment (not a parameter, so no wrapper): no suffix
// matching, and no route in any fixture is a lone DELETE or ANY parameter
// or wildcard, so it stays unmatched even in the self-index.
export function remoteUnmatched() {
  return remote.delete(`/${crypto.randomUUID()}`);
}

export function ambiguous() {
  return fetch('/t016d-tie');
}

export function testOnly() {
  return fetch('/t016d-test-only');
}

export function conditional(flag: boolean) {
  return fetch(flag ? '/t016d-items' : '/t016d-api/t016d-users');
}

export function partlyDynamic(flag: boolean, url: string) {
  return fetch(flag ? '/t016d-items' : url);
}

export function create() {
  return axios.post('/t016d-items', {});
}
