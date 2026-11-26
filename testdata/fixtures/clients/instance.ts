import axios from 'axios';
export const API = '/api';
export const instance = axios.create({baseURL: API});
export function factory() { return axios.create({baseURL: '/api'}); }
