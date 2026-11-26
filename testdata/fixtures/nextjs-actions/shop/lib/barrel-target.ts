import { deleteItem } from '@/app/actions';

// Imported directly by the client form and reached from the server page
// only through the barrel's `export { barrelRemove } from`: in both
// bundles, so boundary unknown with shared_module (T024).
export function barrelRemove() {
  return deleteItem(13);
}
