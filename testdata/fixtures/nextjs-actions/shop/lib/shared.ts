import { deleteItem } from '@/app/actions';

// Imported by the server page and by the client form: in both bundles,
// so its direct call has boundary unknown (R1).
export function sharedRemove() {
  return deleteItem(12);
}
