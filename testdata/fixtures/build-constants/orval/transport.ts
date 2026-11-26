export async function apiFetch<T>(url: string, options: RequestInit = {}) {
  const requestUrl = new URL(url, __API_URL__);
  const apiUrl = new URL(__API_URL__);
  requestUrl.protocol = apiUrl.protocol;
  requestUrl.host = apiUrl.host;
  const response = await fetch(requestUrl.toString(), { ...options, headers: options.headers });
  return response as T;
}
