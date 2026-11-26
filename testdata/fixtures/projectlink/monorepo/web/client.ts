// Calls of the web scope, which declares a base per server.
export function usersFromUsers() {
  return fetch(`${import.meta.env.VITE_USERS}/t022/users`);
}
export function usersFromOrders() {
  return fetch(`${import.meta.env.VITE_ORDERS}/t022/users`);
}
// No declaration covers VITE_SHARED: the ambiguity stays.
export function sharedHealth() {
  return fetch(`${import.meta.env.VITE_SHARED}/t022/mono/health`);
}
// Linked inside the repository (only the users server has it): scopes
// never change it, although the base names the orders server.
export function userFromOrders(id: string) {
  return fetch(`${import.meta.env.VITE_ORDERS}/t022/users/${id}`);
}
// Linked inside the repository to the users server (only it has
// /t022/users/:id), although the base names the orders server: the web
// scope's declaration contradicts the stored link, which the project view
// flags as "conflicts with declaration" (T022 B9) and never overrides.
export function userFortyTwoFromOrders() {
  return fetch(`${import.meta.env.VITE_ORDERS}/t022/users/42`);
}
