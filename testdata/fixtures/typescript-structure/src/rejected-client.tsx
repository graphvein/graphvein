// Row TS-F2: tree-sitter rejects this file (`typeof import`
// as the only type argument of a call, form G3 of T020-TSG), so it claims no
// declaration, import or calls edge; the client-call family still reads it:
// the fetch call's owner is the file node.
import { helper } from "./lib/mixed";
export function load() {
  helper();
  return fetch("/api/rejected");
}
export async function View(io: any) { return io<typeof import("node:fs")>(); }
