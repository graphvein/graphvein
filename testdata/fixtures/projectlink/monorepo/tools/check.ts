// A call in the member's default scope (no consumes): the in-repository
// ambiguity stands.
export function checkUsers() {
  return fetch(`${import.meta.env.VITE_USERS}/t022/users`);
}
