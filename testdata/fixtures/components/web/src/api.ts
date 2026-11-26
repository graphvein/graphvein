import axios from 'axios';
import { label } from '../../shared/format';

export function loadItems() {
  return axios.get('/t026/items').then((r) => label(r.data));
}

export function health() {
  return axios.get('/t026/health');
}

export function remote() {
  return axios.get('https://api.example.test/t026/health');
}

export function missing() {
  return axios.get('/t026/missing');
}

declare function currentPath(): string;

export function dynamic() {
  return axios.get(currentPath());
}
