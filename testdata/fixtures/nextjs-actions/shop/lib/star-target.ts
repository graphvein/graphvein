import { deleteItem } from '@/app/actions';

// As lib/barrel-target.ts, reached from the server through the barrel's
// `export * from`.
export function starRemove() {
  return deleteItem(14);
}
