export function prefixed() { return fetch(`${__API_URL__}/bc/users`); }
export function concatenated(id: string) { return fetch(__API_URL__ + '/bc/users/' + id); }
export function viaWrapper() { return concatenated('7'); }
export function declaredGlobal() { return fetch(`${__GLOBAL_API__}/bc/global`); }
export function numeric() { return fetch(`${__COUNT__}/bc/users`); }
export function defined() { return fetch(`${__DEFINED__}/bc/users`); }
export function assignedGlobal() { return fetch(`${__ASSIGNED__}/bc/users`); }
export function letDeclared() { return fetch(`${__LET_URL__}/bc/users`); }
export function moduleOnly() { return fetch(`${__MODULE_ONLY__}/bc/users`); }
export function undeclared() { return fetch(`${__UNDECLARED__}/bc/users`); }
export function shadowed(__API_URL__: string) { return fetch(`${__API_URL__}/bc/users`); }
export function anchoredPrefix() { return fetch(`${__API_URL__}/acme/settings`); }
