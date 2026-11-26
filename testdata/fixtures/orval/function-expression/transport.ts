export const apiFetch = async function(url: string, options: RequestInit = {}) {
  const response = await fetch<Response>(url, options);
  return response;
};
