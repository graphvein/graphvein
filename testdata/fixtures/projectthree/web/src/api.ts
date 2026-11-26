// Calls through one base that both backends could serve.
export function loadUser() {
  return fetch(`${import.meta.env.VITE_API_URL}/t022r/user`);
}
export function loadArticles() {
  return fetch(`${import.meta.env.VITE_API_URL}/t022r/articles`);
}
export function login() {
  return fetch(`${import.meta.env.VITE_API_URL}/t022r/login`, { method: 'POST' });
}
