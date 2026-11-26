// A call into a declaration of a file row TS-F2 refuses is not an edge:
// the declaration is not emitted.
import { load } from "./rejected-client";
export function caller() { return load(); }
