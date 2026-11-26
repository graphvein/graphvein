async function erase(id: string) { return fetch(`/t034-api/t034-items/${id}`, {method: 'DELETE'}); }
export function T9(cb: (id: string) => Promise<Response> = erase) { erase('t9'); return cb; }
export function T10(erase: (id: string) => number = () => 0) { erase('t10'); }
