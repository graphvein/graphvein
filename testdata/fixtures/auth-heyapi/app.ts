import { client } from './client/client.gen';
import { listItems } from './client/sdk.gen';

const SCHEMES = [{ scheme: 'bearer', type: 'http' }];

// A hand-written hey-api call: security is not a literal array (DA-H1:
// unknown).
export function custom() {
  return client.get({ security: SCHEMES, url: '/t026/custom' });
}

// A hand-written call with a computed key in its security entry.
export function computed(key: string) {
  return client.post({ security: [{ [key]: 'bearer', type: 'http' }], url: '/t026/computed' });
}

export function load() {
  return listItems();
}
