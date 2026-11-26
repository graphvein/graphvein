// Unchanged in every revision: an unresolved call on an untyped receiver
// that the traversal from formatRow never reaches (R3-2 blind spot).
export function callsHelper(o: any) {
  return o.formatRow(2);
}
