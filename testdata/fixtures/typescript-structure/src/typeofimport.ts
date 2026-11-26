export async function mocked(io: any) { const actual = await io<typeof import("node:fs")>(); return actual; }
export function afterTypeof() {}
