import { Client, use } from './tokens';
export function run() {
  const client = use(Client);
  client.get('/t012b-items');
}
const moduleClient = use(Client);
export function unsupported() { moduleClient.get('/t012b-items'); }
export function multiple() {
  const client = use(Client), unused = 1;
  client.get('/t012b-items');
}
