async function del(id) { return fetch(`/t034-api/t034-items/${id}`, {method: 'DELETE'}); }
export function T5({ del: d = del }) { del('t5b'); return d; }
export function T6(cb = del) { del('t6'); return cb; }
export function T7({ [del.name]: x }) { del('t7'); return x; }
export function T8(del = () => 0) { del('t8'); }
