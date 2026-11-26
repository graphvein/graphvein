// A whitespace-only JSON re-indenter for the try-it response view (T029,
// OX-22). The response body is shown as the server sent it: parsing it
// into JavaScript values and printing them again would change what the
// user reads (integers above 2^53 rounded, duplicate keys collapsed to the
// last one, "1.10" printed as "1.1"). This module instead cuts the text
// into JSON tokens, checks the grammar (RFC 8259), and prints the same
// tokens, byte for byte, with new whitespace between them. Anything it
// cannot prove is whole, well-formed JSON is left as received.

/** Token is one JSON token, its text exactly as received. */
interface Token {
  kind: "punct" | "string" | "number" | "literal";
  text: string;
}

const NUMBER = /-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/y;
// JSON forbids raw control characters (U+0000..U+001F) in a string.
// eslint-disable-next-line no-control-regex
const STRING = /"(?:[^"\\\u0000-\u001f]|\\["\\/bfnrt]|\\u[0-9a-fA-F]{4})*"/y;
const LITERAL = /true|false|null/y;
const SPACE = /[ \t\n\r]*/y;

/** MAX_DEPTH bounds the indentation: a deeper body is shown as received
 * rather than growing by two spaces per level on every line. */
const MAX_DEPTH = 100;

/** tokenize cuts text into JSON tokens, or returns null when a character
 * is not part of any token (a bad escape, a control character in a
 * string, a number JSON does not allow, a byte-order mark). */
function tokenize(text: string): Token[] | null {
  const out: Token[] = [];
  let i = 0;
  const at = (re: RegExp): string | null => {
    re.lastIndex = i;
    const m = re.exec(text);
    return m === null ? null : m[0];
  };
  for (;;) {
    i += (at(SPACE) ?? "").length;
    if (i >= text.length) return out;
    const c = text[i] ?? "";
    let tok: Token | null = null;
    if ("{}[]:,".includes(c)) tok = { kind: "punct", text: c };
    else if (c === '"') {
      const s = at(STRING);
      if (s !== null) tok = { kind: "string", text: s };
    } else if (c === "-" || (c >= "0" && c <= "9")) {
      const n = at(NUMBER);
      if (n !== null) tok = { kind: "number", text: n };
    } else {
      const l = at(LITERAL);
      if (l !== null) tok = { kind: "literal", text: l };
    }
    if (tok === null) return null;
    out.push(tok);
    i += tok.text.length;
  }
}

/** IndentResult is a body re-indented token for token, or the reason it
 * is shown as received. */
export type IndentResult = { ok: true; text: string; duplicateKeys: string[] } | { ok: false; reason: string };

function isValue(t: Token): boolean {
  return t.kind !== "punct" || t.text === "{" || t.text === "[";
}

/** indentJson re-indents a JSON text by two spaces without touching a
 * token: numbers, strings (escapes included), key order and repeated keys
 * stay as received. ok is false when the text is not exactly one JSON
 * value (cut by a limit, not JSON at all, trailing data). duplicateKeys
 * names keys that repeat inside one object: most JSON readers keep only
 * the last, so the user is told. */
export function indentJson(text: string): IndentResult {
  const tokens = tokenize(text);
  if (tokens === null) return { ok: false, reason: "not valid JSON" };
  if (tokens.length === 0) return { ok: false, reason: "empty" };
  // The grammar, iteratively (a deep body must not overflow the stack).
  // stack holds the open containers; each object frame keeps its keys.
  const stack: { kind: "{" | "["; keys: Set<string> | null; count: number }[] = [];
  const duplicates = new Set<string>();
  let out = "";
  let expect: "value" | "key" | "key-or-close" | "value-or-close" | "colon" | "comma-or-close" | "end" = "value";
  const pad = () => "\n" + "  ".repeat(stack.length);
  for (const t of tokens) {
    const top = stack[stack.length - 1];
    switch (expect) {
      case "value":
      case "value-or-close":
        if (expect === "value-or-close" && t.text === "]" && t.kind === "punct") {
          stack.pop();
          out += top !== undefined && top.count > 0 ? pad() + "]" : "]";
          expect = stack.length === 0 ? "end" : "comma-or-close";
          break;
        }
        if (!isValue(t)) return { ok: false, reason: "not valid JSON" };
        if (top !== undefined && top.kind === "[") {
          top.count++;
          out += pad();
        }
        out += t.text;
        if (t.kind === "punct") {
          if (stack.length >= MAX_DEPTH) return { ok: false, reason: `nested deeper than ${String(MAX_DEPTH)} levels, too deep to re-indent` };
          stack.push({ kind: t.text === "{" ? "{" : "[", keys: t.text === "{" ? new Set() : null, count: 0 });
          expect = t.text === "{" ? "key-or-close" : "value-or-close";
        } else {
          expect = stack.length === 0 ? "end" : "comma-or-close";
        }
        break;
      case "key":
      case "key-or-close":
        if (expect === "key-or-close" && t.kind === "punct" && t.text === "}") {
          stack.pop();
          out += top !== undefined && top.count > 0 ? pad() + "}" : "}";
          expect = stack.length === 0 ? "end" : "comma-or-close";
          break;
        }
        if (t.kind !== "string" || top === undefined || top.keys === null) return { ok: false, reason: "not valid JSON" };
        {
          // The decoded key decides a repeat: "a" and "a" are one key.
          const key = JSON.parse(t.text) as string;
          if (top.keys.has(key)) duplicates.add(key);
          top.keys.add(key);
        }
        top.count++;
        out += pad() + t.text;
        expect = "colon";
        break;
      case "colon":
        if (t.kind !== "punct" || t.text !== ":") return { ok: false, reason: "not valid JSON" };
        out += ": ";
        expect = "value";
        break;
      case "comma-or-close":
        if (t.kind !== "punct" || top === undefined) return { ok: false, reason: "not valid JSON" };
        if (t.text === ",") {
          out += ",";
          // A comma is followed by a key or a value, never a close.
          expect = top.kind === "{" ? "key" : "value";
          break;
        }
        if (t.text !== (top.kind === "{" ? "}" : "]")) return { ok: false, reason: "not valid JSON" };
        stack.pop();
        out += pad() + t.text;
        expect = stack.length === 0 ? "end" : "comma-or-close";
        break;
      case "end":
        return { ok: false, reason: "more than one JSON value" };
    }
  }
  if (expect !== "end") return { ok: false, reason: "incomplete JSON" };
  // The proof: the printed text has exactly the received tokens, in
  // order; only whitespace between them changed.
  const again = tokenize(out);
  if (again === null || again.length !== tokens.length || again.some((t, i) => t.text !== tokens[i]?.text)) return { ok: false, reason: "not valid JSON" };
  return { ok: true, text: out, duplicateKeys: [...duplicates] };
}
