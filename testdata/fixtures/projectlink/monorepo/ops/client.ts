// Calls of the ops scope, which consumes both servers: still ambiguous.
export function opsHealth() {
  return fetch(`${import.meta.env.VITE_OPS}/t022/mono/health`);
}
