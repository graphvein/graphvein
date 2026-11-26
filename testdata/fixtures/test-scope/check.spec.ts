export function testSend() {
  fetch('/t011g/mixed');
  fetch('/t011g/only');
}

import { get } from './tests/wrapper';
export function throughWrapper() { get('/t011g/only'); }
