'use client';

import axios from 'axios';
import useSWR from 'swr';
import useSWRImmutable from 'swr/immutable';

const fetcher = (url: string) => fetch(url).then((res) => res.json());

export function Lists(id: string, ready: boolean, key: string) {
  // NX-L2: a template key with an environment base and an inline fetch fetcher.
  const { data: a } = useSWR(`${process.env.NEXT_PUBLIC_BASE_PATH ?? ''}/api/items`, (url: string) => fetch(url).then((r) => r.json()));
  // NX-L2: a conditional key whose null alternative drops out, a named fetcher.
  const { data: b } = useSWR(ready ? `/api/docs/${id}` : null, fetcher);
  // NX-L2: an inline axios fetcher.
  const { data: c } = useSWRImmutable('/api/health', (u) => axios.get(u).then((r) => r.data));
  // D: a block-bodied inline fetcher.
  const { data: d } = useSWR('/api/items', async (url: string) => {
    const r = await fetch(url);
    return r.json();
  });
  // D: an inline fetcher with request options.
  const { data: e } = useSWR('/api/items', (url: string) => fetch(url, { method: 'POST' }).then((r) => r.json()));
  // D: a key from a parameter.
  const { data: f } = useSWR(key, fetcher);
  // D: a function key.
  const { data: g } = useSWR(() => (ready ? '/api/items' : null), fetcher);
  // D: a local-state key without a fetcher.
  const { data: h } = useSWR('local-state', null);
  // D: an inline fetcher that does not fetch its key.
  const { data: i } = useSWR('/api/items', (url: string) => fetch('/api/health').then((r) => r.json()));
  return [a, b, c, d, e, f, g, h, i];
}
