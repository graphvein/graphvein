// Calls of the admin scope, which consumes the users server only.
export function adminHealth() {
  return fetch(`${import.meta.env.VITE_ADMIN}/t022/mono/health`);
}
