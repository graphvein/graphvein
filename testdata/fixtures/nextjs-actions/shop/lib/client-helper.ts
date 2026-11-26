import { deleteItem } from '@/app/actions';

// No directive, but imported by a 'use client' file through a value
// import: row SA-C3, boundary client (client module graph).
export function removeAll() {
  return deleteItem(9);
}
