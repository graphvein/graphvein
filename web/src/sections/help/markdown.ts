// The guide's Markdown (docs/guide/*.md) as a small, fixed subset, parsed
// at build time by the Vite plugin in vite.config.ts (T045). The same
// files are the public text of the guide, so they stay plain Markdown that
// any renderer reads the same way; this parser accepts only the forms the
// Help page renders and rejects everything else with the file and line, so
// an unsupported form fails the build instead of showing as raw text.
//
// Blocks: headings (#, ##, ###), paragraphs, "- " and "1. " lists (an item
// continues on lines indented to its text), pipe tables with a separator
// row, and the example marker `<!-- graphvein:example fact|inferred|unknown -->`,
// an HTML comment, so other renderers show nothing there. The marker
// takes the paragraph after it, which must be the label's fixed
// illustrative example: "**Example:** `<subject>` · <label> · <note>",
// where the label is "Fact · 1.0", "Inferred · <0.n>" or "Unknown" as the
// marker's kind says. Other renderers show that paragraph as written; the
// Help page shows it with the label as a chip.
// Inline: `code`, **strong** and [text](href). No other HTML, no single
// * or _ emphasis, no images.
//
// It has no dependency and touches no DOM or file system: the plugin hands
// it the text, and the tests call it directly.

/** Inline is one piece of a line of text. */
export type Inline =
  | { t: "text"; v: string }
  | { t: "code"; v: string }
  | { t: "strong"; c: Inline[] }
  | { t: "link"; href: string; c: Inline[] };

/** ExampleKind names a live example of the Start here page (claim rows
 * HP-1 to HP-3 in docs/contract/claims/help.md). */
export type ExampleKind = "fact" | "inferred" | "unknown";

/** FixedExample is the illustrative example the guide writes for a label
 * (not taken from any repository): a subject in code, the label as the
 * plain style words it, and a short note. */
export interface FixedExample {
  subject: string;
  label: string;
  note: Inline[];
}

/** Block is one block of a guide page. A heading's slug is unique within
 * the whole guide; the Help page addresses it as #/help?at=<slug>. */
export type Block =
  | { t: "heading"; level: 1 | 2 | 3; c: Inline[]; slug: string }
  | { t: "paragraph"; c: Inline[] }
  | { t: "list"; ordered: boolean; items: Inline[][] }
  | { t: "table"; head: Inline[][]; rows: Inline[][][] }
  | { t: "example"; kind: ExampleKind; fixed: FixedExample };

/** GuidePage is one file of docs/guide. */
export interface GuidePage {
  file: string;
  blocks: Block[];
}

/** GuideSyntaxError names the file and line of a form the parser does not
 * accept. */
export class GuideSyntaxError extends Error {
  constructor(file: string, line: number, message: string) {
    super(`${file}:${String(line)}: ${message}`);
    this.name = "GuideSyntaxError";
  }
}

const EXAMPLE = /^<!-- graphvein:example (fact|inferred|unknown) -->$/;
const FIXED_PREFIX = "**Example:** ";
const FIXED = /^`([^`]+)` · (Fact · 1\.0|Inferred · 0\.\d+|Unknown)(?: · (.+))?$/;
const FIXED_KIND: Record<string, ExampleKind> = { Fact: "fact", Inferred: "inferred", Unknown: "unknown" };
const HEADING = /^(#{1,3}) (.+)$/;
const BULLET = /^- (.*)$/;
const NUMBERED = /^(\d+)\. (.*)$/;
const SEPARATOR = /^\|(\s*:?-{3,}:?\s*\|)+$/;

/** plainText is the text of inline pieces without markup. */
export function plainText(c: readonly Inline[]): string {
  return c.map((i) => (i.t === "text" || i.t === "code" ? i.v : plainText(i.c))).join("");
}

/** slugOf is a heading's anchor: lower case letters and digits, words
 * joined by "-". */
export function slugOf(text: string): string {
  return text
    .toLowerCase()
    .replace(/['’"“”]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/** parseInline reads the inline forms of one run of text. */
export function parseInline(src: string, file: string, line: number, inStrong = false): Inline[] {
  const out: Inline[] = [];
  let text = "";
  const flush = () => {
    if (text !== "") out.push({ t: "text", v: text });
    text = "";
  };
  let i = 0;
  while (i < src.length) {
    const ch = src.charAt(i);
    if (ch === "`") {
      const end = src.indexOf("`", i + 1);
      if (end === -1) throw new GuideSyntaxError(file, line, "unclosed `code`");
      flush();
      out.push({ t: "code", v: src.slice(i + 1, end) });
      i = end + 1;
    } else if (src.startsWith("**", i)) {
      if (inStrong) throw new GuideSyntaxError(file, line, "nested **strong**");
      const end = src.indexOf("**", i + 2);
      if (end === -1) throw new GuideSyntaxError(file, line, "unclosed **strong**");
      flush();
      out.push({ t: "strong", c: parseInline(src.slice(i + 2, end), file, line, true) });
      i = end + 2;
    } else if (ch === "[") {
      const close = src.indexOf("](", i);
      const end = close === -1 ? -1 : src.indexOf(")", close + 2);
      if (close === -1 || end === -1) throw new GuideSyntaxError(file, line, "a [ that is not a [text](href) link");
      const href = src.slice(close + 2, end);
      if (href === "" || /\s/.test(href)) throw new GuideSyntaxError(file, line, `link target ${JSON.stringify(href)}`);
      flush();
      out.push({ t: "link", href, c: parseInline(src.slice(i + 1, close), file, line, inStrong) });
      i = end + 1;
    } else if (ch === "*" || ch === "<" || ch === "]" || (ch === "_" && /\W/.test(src.charAt(i - 1) || " "))) {
      throw new GuideSyntaxError(file, line, `unsupported markup ${JSON.stringify(ch)} (only \`code\`, **strong** and [text](href))`);
    } else if (ch === "!" && src.charAt(i + 1) === "[") {
      throw new GuideSyntaxError(file, line, "images are not supported");
    } else {
      text += ch;
      i++;
    }
  }
  flush();
  return out;
}

function tableCells(row: string, file: string, line: number): Inline[][] {
  if (!row.endsWith("|")) throw new GuideSyntaxError(file, line, "a table row must end with |");
  return row
    .slice(1, -1)
    .split("|")
    .map((cell) => parseInline(cell.trim(), file, line));
}

/** fixedExample reads the text after "**Example:** " of a kind's fixed
 * example; the label must be the one of the marker's kind. */
function fixedExample(kind: ExampleKind, src: string, file: string, line: number): FixedExample {
  const m = FIXED.exec(src);
  if (m === null) throw new GuideSyntaxError(file, line, "an example reads \"**Example:** `subject` · <label> · <note>\" with the label Fact · 1.0, Inferred · 0.n or Unknown");
  const label = m[2] ?? "";
  const labelKind = FIXED_KIND[label.split(" ")[0] ?? ""];
  if (labelKind !== kind) throw new GuideSyntaxError(file, line, `the ${kind} example is labelled ${label}`);
  return { subject: m[1] ?? "", label, note: m[3] === undefined ? [] : parseInline(m[3], file, line) };
}

/** parseGuidePage parses one guide file. slugs holds the slugs already
 * used by earlier pages, so every anchor of the guide is unique; it is
 * updated in place. */
export function parseGuidePage(file: string, src: string, slugs: Set<string> = new Set()): GuidePage {
  const lines = src.replace(/\r\n/g, "\n").split("\n");
  const blocks: Block[] = [];
  let n = 0;
  const at = () => n + 1;
  // paragraphAt reads the paragraph starting at line n, joined into one
  // line, and moves n past it.
  const paragraphAt = (): string => {
    let text = (lines[n] ?? "").trim();
    n++;
    while (n < lines.length) {
      const l = lines[n] ?? "";
      if (l.trim() === "" || /^(#|\||- |\d+\. |<)/.test(l)) break;
      text += ` ${l.trim()}`;
      n++;
    }
    return text;
  };
  while (n < lines.length) {
    const raw = lines[n] ?? "";
    if (raw.trim() === "") {
      n++;
      continue;
    }
    if (/^\s/.test(raw)) throw new GuideSyntaxError(file, at(), "unexpected indentation");
    const example = EXAMPLE.exec(raw);
    if (example !== null) {
      const kind = example[1] as ExampleKind;
      const markerLine = at();
      n++;
      while (n < lines.length && (lines[n] ?? "").trim() === "") n++;
      const start = at();
      const text = paragraphAt();
      if (!text.startsWith(FIXED_PREFIX)) throw new GuideSyntaxError(file, markerLine, `the ${kind} example marker needs a "**Example:** \`...\` · <label>" paragraph after it`);
      blocks.push({ t: "example", kind, fixed: fixedExample(kind, text.slice(FIXED_PREFIX.length), file, start) });
      continue;
    }
    if (raw.startsWith("<")) throw new GuideSyntaxError(file, at(), "HTML is not supported");
    if (raw.startsWith("```") || raw.startsWith(">")) throw new GuideSyntaxError(file, at(), "code blocks and quotes are not supported");
    const heading = HEADING.exec(raw);
    if (heading !== null) {
      const c = parseInline(heading[2] ?? "", file, at());
      const slug = slugOf(plainText(c));
      if (slug === "") throw new GuideSyntaxError(file, at(), "a heading needs letters or digits");
      if (slugs.has(slug)) throw new GuideSyntaxError(file, at(), `heading anchor ${slug} is used twice`);
      slugs.add(slug);
      blocks.push({ t: "heading", level: (heading[1] ?? "#").length as 1 | 2 | 3, c, slug });
      n++;
      continue;
    }
    if (raw.startsWith("#")) throw new GuideSyntaxError(file, at(), "only #, ## and ### headings");
    if (raw.startsWith("|")) {
      const head = tableCells(raw, file, at());
      n++;
      if (!SEPARATOR.test(lines[n] ?? "")) throw new GuideSyntaxError(file, at(), "a table needs a |---| separator row");
      n++;
      const rows: Inline[][][] = [];
      while (n < lines.length && (lines[n] ?? "").startsWith("|")) {
        const cells = tableCells(lines[n] ?? "", file, at());
        if (cells.length !== head.length) throw new GuideSyntaxError(file, at(), `${String(cells.length)} cells, the header has ${String(head.length)}`);
        rows.push(cells);
        n++;
      }
      blocks.push({ t: "table", head, rows });
      continue;
    }
    const bullet = BULLET.exec(raw);
    const numbered = NUMBERED.exec(raw);
    if (bullet !== null || numbered !== null) {
      const ordered = numbered !== null;
      const items: Inline[][] = [];
      while (n < lines.length) {
        const l = lines[n] ?? "";
        const m = ordered ? NUMBERED.exec(l) : BULLET.exec(l);
        if (m === null) break;
        const start = at();
        let text = (ordered ? m[2] : m[1]) ?? "";
        const indent = ordered ? `${m[1] ?? ""}. `.length : 2;
        n++;
        while (n < lines.length && (lines[n] ?? "").startsWith(" ".repeat(indent)) && (lines[n] ?? "").trim() !== "") {
          text += ` ${(lines[n] ?? "").trim()}`;
          n++;
        }
        items.push(parseInline(text, file, start));
      }
      blocks.push({ t: "list", ordered, items });
      continue;
    }
    const start = at();
    const text = paragraphAt();
    if (text.startsWith(FIXED_PREFIX)) throw new GuideSyntaxError(file, start, "an **Example:** paragraph needs an example marker before it");
    blocks.push({ t: "paragraph", c: parseInline(text, file, start) });
  }
  const first = blocks[0];
  if (first?.t !== "heading" || first.level !== 1) throw new GuideSyntaxError(file, 1, "a guide page starts with one # heading");
  if (blocks.some((b, i) => i > 0 && b.t === "heading" && b.level === 1)) throw new GuideSyntaxError(file, 1, "a guide page has one # heading");
  return { file, blocks };
}

/** parseGuide parses the guide's files in file-name order (the names start
 * with their position: 1-start-here.md, 2-sections.md, ...). */
export function parseGuide(files: Readonly<Record<string, string>>): GuidePage[] {
  const slugs = new Set<string>();
  return Object.keys(files)
    .sort((a, b) => a.localeCompare(b, "en"))
    .map((name) => parseGuidePage(name, files[name] ?? "", slugs));
}

/** linksOf lists every link target of the guide, with where it is. */
export function linksOf(pages: readonly GuidePage[]): { file: string; href: string }[] {
  const out: { file: string; href: string }[] = [];
  const walk = (file: string, c: readonly Inline[]) => {
    for (const i of c) {
      if (i.t === "link") out.push({ file, href: i.href });
      if (i.t === "strong" || i.t === "link") walk(file, i.c);
    }
  };
  for (const p of pages) {
    for (const b of p.blocks) {
      if (b.t === "heading" || b.t === "paragraph") walk(p.file, b.c);
      else if (b.t === "list") for (const it of b.items) walk(p.file, it);
      else if (b.t === "table") for (const r of [b.head, ...b.rows]) for (const cell of r) walk(p.file, cell);
      else walk(p.file, b.fixed.note);
    }
  }
  return out;
}
