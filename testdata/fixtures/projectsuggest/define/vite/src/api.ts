// Calls of the vite member, through define constants only.
export function fixed() {
  return fetch(`${import.meta.env.VITE_FIXED}/b6b/fixed`);
}
export function alias() {
  return fetch(`${import.meta.env.VITE_API}/b6b/alias`);
}
