// Eight calls deep: the header is read at the end (depth limits 2, 6, 10).
export function c1(r: Request) { return c2(r); }
export function c2(r: Request) { return c3(r); }
export function c3(r: Request) { return c4(r); }
export function c4(r: Request) { return c5(r); }
export function c5(r: Request) { return c6(r); }
export function c6(r: Request) { return c7(r); }
export function c7(r: Request) { return c8(r); }
export function c8(r: Request) { return r.headers.get('x-deep'); }

// Three calls deep: the response helper chain.
export function r1(v: string) { return r2(v); }
export function r2(v: string) { return r3(v); }
export function r3(v: string) { return Response.json({ deep: v }, { status: 203 }); }

// A helper without parameters that returns another helper's response: its
// returns are not cached (only response-building returns are), so the
// chain stops there.
export function bare() { return r3('x'); }
