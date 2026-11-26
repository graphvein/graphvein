import axios from 'axios';
import {instance, API} from './instance';
const METHOD = 'PATCH';
const PATH = '/items';
const inherited = axios.create();
const own = axios.create();
own.defaults.baseURL = '/api';
const conflict = axios.create();
conflict.defaults.baseURL = '/api';
conflict.defaults.baseURL = '/different';
const opaque = axios.create();
opaque.defaults.baseURL = process.env.API;
const alias = instance;
export function extra(config: object, id: string) {
  fetch('/api/items', config);
  axios.request({url: PATH, method: METHOD});
  axios('/items');
  axios({url:'/items', method:'put', baseURL:API});
  inherited.get('/items');
  own.get('/items');
  conflict.get('/items');
  opaque.get('/items');
  alias.request({url:'/items', method:'GET'});
  instance('/items', {method:'DELETE'});
  axios.get('/items', {baseURL:process.env.API});
  fetch(`/api/items/${ /* identity trivia */ id }#fragment`);
}
export function verb(url: string, method: string = 'POST') { return fetch('/api' + url, {method}); }
export function useVerb() { verb('/items'); verb('/items', 'PUT'); }
export const object = { send(url: string) { return fetch('/api' + url); } };
export function useObject() { object.send('/items'); }
export class Owner { field = fetch('/api/items'); }
export function objectPattern({fetch}: {fetch:Function}) { fetch('/api/items'); }
export function globalShadow(window: {fetch:Function}) { window.fetch('/api/items'); }
export async function generics() {
  await axios.get<{id:string}>('/items');
}
export function sameNamed(url: string) { ['/opaque'].map((url) => fetch(url)); }
export function useSameNamed() { sameNamed('/items'); }
export function fallback(url: string, options: {method?:string|null} = {}) { return fetch('/api' + url, {method:options.method || 'GET'}); }
export function useFallback() {
  fallback('/items', {method:''});
  fallback('/items', {method:'POST'});
  fallback('/items', {method:null});
}
const envClient = axios.create({baseURL:process.env.API});
function opaqueURL() { return '/api/items'; }
export function opaqueEnv() { return envClient.get(opaqueURL()); }
