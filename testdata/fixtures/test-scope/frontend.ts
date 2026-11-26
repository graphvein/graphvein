export function send() {
  fetch('/t011g/mixed');
  fetch('/t011g/only');
  fetch('/t011g/two');
  fetch('/t011g/registered');
  fetch('/t011g/test/child');
  fetch('/t011g/helper/child');
  fetch('/t011g/express');
  fetch('/t011g/rust');
  fetch('/t011g/config');
  fetch('/t011g/excluded');
  fetch(`http://localhost:${port}/t011g/mixed`);
}

import { get } from './tests/wrapper';
export function throughWrapper() { get('/t011g/only'); }
