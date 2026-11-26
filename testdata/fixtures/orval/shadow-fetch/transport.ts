export async function apiFetch(url: string, options: RequestInit = {}) {
  const fetch = (value: string) => value;
  const probe = fetch('/t012e/mock');
  const response = await globalThis.fetch(url, options);
  return response;
}
