export async function load(x) { return await (x.ready || Promise.resolve()).then(() => 1); }
export async function inner() { return await (helper()).valueOf(); }
function helper() { return 1; }
