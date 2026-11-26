'use client';

import useSWR from 'swr';

const fetcher = (url: string) => fetch(url).then((res) => res.json());

export function Items() {
  const { data } = useSWR('/api/items', fetcher);
  const { data: keyed } = useSWR(['/api/items', 1], fetcher);
  return data ?? keyed;
}

export async function load() {
  await fetch('/api/items');
  await fetch('/api/items', { method: 'HEAD' });
  await fetch('/api/items/42', { method: 'DELETE' });
  await fetch('/api/cart/a/b', { method: 'POST' });
  await fetch('/api/docs');
  await fetch('/api/slot', { method: 'OPTIONS' });
  await fetch('/api/health');
  await fetch('/api/users/7', { method: 'PATCH' });
  await fetch('/api/star');
  await fetch('/api/legacy/old');
  await fetch('https://api.example.com/api/items');
}
