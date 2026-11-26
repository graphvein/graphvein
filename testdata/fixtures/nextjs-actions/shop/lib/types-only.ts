import { deleteItem } from '@/app/actions';

export type Shape = { id: number };

// Reached only through `import type`: boundary unknown.
export function cleanup() {
  return deleteItem(10);
}
