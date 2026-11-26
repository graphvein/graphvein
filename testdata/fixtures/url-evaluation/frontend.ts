import { URLs, getURL, choose, defaults, values, keys, folded, recursive, level1, asynchronous, unsupported, mutated, spread, updated, deleted, assigned } from './urls';
export function calls(flag: boolean, id: string) {
  fetch(URLs.DOT);
  fetch(URLs['DOT']);
  fetch(getURL('ITEMS', { 2: 'b', 1: 'a', z: 'c' }));
  fetch(getURL('ITEMS', { id }));
  fetch(defaults(undefined));
  fetch('' || '/api/a');
  fetch('/api/b' || '/api/a');
  fetch('' ?? '/api/a');
  fetch(choose(true));
  fetch(choose(flag));
  fetch(`/api/a${flag ? '?q=1' : ''}`);
  fetch(flag ? '/api/a' : id ? '/api/b' : flag ? '/api/c' : id ? '/api/d' : '/api/e');
  fetch(mutated.path);
  fetch(spread.path);
  const local = '/api/a';
  fetch(local);
  let mutable = '/api/a';
  fetch(mutable);
  fetch(recursive());
  fetch(level1());
  fetch(asynchronous());
  fetch('/api/twins', { method: 'POST' });
  fetch('/api/twins/', { method: 'POST' });
  fetch('/api/express', { method: 'POST' });
  fetch(values({ 2: 'b', 1: 'a', z: 'c' }));
  fetch(keys({ 2: 'b', 1: 'a', z: 'c' }));
  fetch(folded(true));
  fetch(folded(flag));
  const object = { id };
  fetch(getURL('ITEMS', object));
  fetch(getURL('ITEMS', undefined));
  fetch(`/api/coerce/${undefined}/${null}/${false}/${12}`);
  fetch(updated.path);
  fetch(deleted.path);
  fetch(assigned.path);
  fetch(unsupported());
  { const local = '/api/b'; fetch(local); }
  function nested() { fetch(later); }
  const later = '/api/a';
  { fetch(after); const after = '/api/a'; }
}
