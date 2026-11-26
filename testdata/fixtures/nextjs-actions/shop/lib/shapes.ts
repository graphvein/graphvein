import { deleteItem } from '@/app/actions';

export interface Point {
  x: number;
}

// Imported by a 'use client' file for a type only: boundary unknown.
export function prune() {
  return deleteItem(11);
}
