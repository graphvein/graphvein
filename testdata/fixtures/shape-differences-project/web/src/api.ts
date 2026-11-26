const API = import.meta.env.VITE_API;

// SD-11 (cross-repository link), SD-8 and SD-9.
export function invite(email: string) {
  return fetch(`${API}/w4p/users`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, role: 'viewer' }),
  });
}
