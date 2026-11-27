// tree-sitter-typescript reads `await f<T>(x)` as a call of `await f`
// with type arguments; the source awaits the call `f<T>(x)`, so the call
// is read through its real callee and bound by the rows below.
import { read } from "./kinds";

const fetchAs = async <T>(command: string): Promise<T> => command as unknown as T;
const target = { m() {} };

export async function awaitedGeneric() {
  await read<string>(); // TSX-11: edge
  await fetchAs<number>("a"); // TSX-8: edge
  const v = await fetchAs<{ a: number }>("b"); // TSX-8: edge
  (await fetchAs)<string>("c"); // no edge (the awaited value is called)
  return v;
}

export async function awaitedWrite() {
  // The generic Object.assign is a member write of `m` (TSX-25).
  await Object.assign<typeof target, { m: () => void }>(target, { m: () => {} });
  target.m(); // TSX-25: no edge
}
