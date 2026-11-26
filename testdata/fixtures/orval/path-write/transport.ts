export async function apiFetch(url: string, options: RequestInit = {}) {
  const requestUrl = new URL(url, deployment);
  const apiUrl = new URL(deployment);
  requestUrl.protocol = apiUrl.protocol;
  requestUrl.pathname = "/t012e/rewritten";
  const response = await fetch(requestUrl.toString(), { ...options, headers: headers(options.headers) });
  return response;
}
export function useFetch() { return () => {}; }
