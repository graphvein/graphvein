import axios from 'axios';
import * as namespace from 'axios';
import {instance, factory} from './instance';
const required = require('axios');
const METHOD = 'post';
const PREFIX = '/api';
const ENV = import.meta.env.API;
const client = axios.create({baseURL: process.env.API});
const local = factory();
axios.defaults.baseURL = '/api';
function getURL() { return '/unique'; }
export function main(id: string, method: string, url: string) {
  fetch('/api/items');
  window.fetch('/api/items', {method: METHOD});
  globalThis.fetch('/api/items', {method: 'put'});
  self.fetch('/api/items', {method: 'patch'});
  fetch('/api/items', {method: 'delete'});
  fetch('/api/items', {method: 'head'});
  fetch('/api/items', {method: 'options'});
  fetch('/api/items', {method});
  fetch(url);
  fetch(new URL('/api/items', url));
  fetch(new Request('/api/items'));
  axios.get('/items');
  axios.delete('/items');
  axios.head('/items');
  axios.options('/items');
  axios.post('/items', {});
  axios.put('/items', {});
  axios.patch('/items', {});
  axios.postForm('/items', {});
  axios.putForm('/items', {});
  axios.patchForm('/items', {});
  axios.request({url:'/items', method:METHOD});
  axios({url:'/items'});
  axios('/items', {method:'post'});
  namespace.default.get('/items');
  required.get('/items');
  instance.get('/items');
  local.get('/items');
  client.get('/items', {baseURL:'/api'});
  axios.get('https://example.test/api/items?ignored#more');
  fetch(`/api/items/${id}?query=ignored`);
  fetch('/api/items/fixed');
  fetch('/api/tie');
  fetch('/api/missing');
  fetch('/api/express-head', {method:'HEAD'});
  fetch('/api/rust-head', {method:'HEAD'});
  fetch('/api/python-head', {method:'HEAD'});
  fetch('/api/python-explicit', {method:'HEAD'});
  fetch('/api/any', {method:'TRACE'});
  fetch('/api/optional');
  fetch('/api/wild/a/b');
  fetch(`${getURL()}/resource`);
  fetch(`${import.meta.env.API}/items`);
  fetch(`${ENV}/items`);
  fetch(`${process.env.API ?? '/api'}/items`);
  client.get(`/${id}`);
  client.get('/items');
  fetch('items');
  fetch(PREFIX + '/items');
  fetch('/api/items/part-' + id);
  fetch('/api/items', {});
}
export function one(url: string, options: {method?:string} = {}) { return fetch('/api' + url, {method:options.method ?? 'GET'}); }
export function two(url: string) { return one(url); }
export function three(url: string) { return two(url); }
export function four(url: string) { return three(url); }
export function cycleA(url: string) { return cycleB(url); }
export function cycleB(url: string) { return cycleA(url); }
export function wrappers() {
  one('/items');
  one('/items', {method:'POST'});
  two('/items');
  three('/items');
  four('/items');
  cycleA('/items');
}
export function shadows(fetch: Function) { fetch('/api/items'); }
