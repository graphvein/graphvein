// The "What Graphvein couldn't read" page's model (T040, variant A):
// summary cards, the files-without-facts groups and "Copy as text". Pure,
// so the wording rules are tested: no card adds counts of different units
// (row CR-31), nothing is called "unread" except files not read (row
// CR-32), and a file without facts is "no fact stored from this file",
// never skipped or failed (row CR-22).

import type { FileCategory, FileWithoutFacts, UnplacedRoute, UnreadCounts, UnreadPage, UnreadProject } from "@/api/unread";
import { formatCount } from "@/lib/count";
import { parseHashQuery } from "@/router";

/** Card is one summary card; value null when its part is not available. */
export interface Card {
  id: string;
  value: number | null;
  title: string;
  lines: string[];
  /** Why the value is missing, when it is. */
  missing: string | null;
}

function n(v: number | null | undefined): number | null {
  return v === undefined ? null : v;
}

function plural(v: number, one: string, many: string): string {
  return `${formatCount(v)} ${v === 1 ? one : many}`;
}

/** DOCUMENT_SCOPE_TESTS says what the generated document's lists cover
 * when tests are included: they hold production routes only (rows CR-6,
 * CR-7; T040 review), so the count is not one of test routes. */
export const DOCUMENT_SCOPE_TESTS = "Production routes only: the generated document these come from leaves test routes out";

/** DOCUMENT_SCOPE_PRODUCTION says what the lists cover without tests. */
export const DOCUMENT_SCOPE_PRODUCTION = "Production code";

/** cards builds the five cards from a page's or the project's counts.
 * notAvailable (project scope) names the members a count is missing for;
 * scope is the scope asked for ("production", "test" or "all"). */
export function cards(c: UnreadCounts, notAvailable: Record<string, string[]> = {}, scope = "production"): Card[] {
  const missing = (keys: string[]): string | null => {
    const who = new Set(keys.flatMap((k) => notAvailable[k] ?? []));
    if (keys.every((k) => n(c[k]) !== null)) return null;
    return who.size > 0 ? `not available for ${[...who].join(", ")}` : "not available";
  };
  const routes: string[] = [scope === "production" ? DOCUMENT_SCOPE_PRODUCTION : DOCUMENT_SCOPE_TESTS];
  const mounts = n(c.mounts);
  if (mounts !== null && mounts > 0) routes.push(`${plural(mounts, "mount", "mounts")} whose prefix is not a constant`);
  const patterns = n(c.dynamic_patterns);
  if (patterns !== null && patterns > 0) routes.push(`${plural(patterns, "pattern", "patterns")} built at run time`);
  const forms = n(c.router_form_files);
  if (forms !== null && forms > 0) routes.push(`${plural(forms, "file", "files")} with router forms outside the route contract`);
  const unresolvedMounts = n(c.mounts_unresolved);
  if (unresolvedMounts !== null && unresolvedMounts > 0) routes.push(`${plural(unresolvedMounts, "mount", "mounts")} whose router is not known (no path claimed)`);
  // Row CR-44: route registrations, counted in routes, not mounts.
  const withheld = n(c.routes_withheld);
  if (withheld !== null && withheld > 0) routes.push(`${plural(withheld, "route", "routes")} whose path is withheld (another pattern or route may answer it; no path claimed)`);
  const failures = n(c.route_failures);
  if (failures !== null && failures > 0) routes.push(`${plural(failures, "Go route recognition", "Go route recognitions")} failed`);

  const dynamic = n(c.dynamic_calls);
  const wrapper = n(c.wrapper_calls);
  const calls: string[] = [];
  if (dynamic !== null && wrapper !== null) calls.push(`${formatCount(dynamic)} with a URL or method not known statically · ${formatCount(wrapper)} through a wrapper`);
  const ext = n(c.unresolved_calls_external);
  const unk = n(c.unresolved_calls_unknown);
  const counted = n(c.files_counted);
  if (ext !== null && unk !== null && counted !== null) {
    calls.push(`In-code calls not resolved: ${formatCount(ext)} to external code, ${formatCount(unk)} unknown (in ${plural(counted, "file", "files")} counted)`);
  }

  const shapes: string[] = [];
  const without = n(c.routes_without_shape);
  if (without !== null) shapes.push(`${plural(without, "route has", "routes have")} no stored request or response shape`);
  const invalid = n(c.routes_shape_invalid);
  if (invalid !== null && invalid > 0) shapes.push(`${plural(invalid, "stored shape does", "stored shapes do")} not decode`);

  const files: string[] = [];
  const notRead = n(c.files_not_read);
  if (notRead !== null) files.push(`${plural(notRead, "file", "files")} not read`);
  const syntax = n(c.files_syntax_errors);
  if (syntax !== null) files.push(`${plural(syntax, "file", "files")} read with syntax errors`);
  const gaps = n(c.scanner_gaps);
  if (gaps !== null && gaps > 0) files.push(`${plural(gaps, "path", "paths")} the scanner did not index`);
  const dropped = n(c.files_records_dropped);
  if (dropped !== null && dropped > 0) files.push(`${plural(dropped, "record", "records")} dropped as invalid`);
  const structure = n(c.files_structure_outside_contract);
  if (structure !== null && structure > 0) files.push(`${plural(structure, "file", "files")} with TypeScript structure outside the contract`);

  const both = dynamic !== null && wrapper !== null ? dynamic + wrapper : null;
  return [
    { id: "route-paths", value: n(c.unplaced_routes), title: "routes without a full path", lines: routes, missing: missing(["unplaced_routes"]) },
    { id: "calls", value: both, title: "client calls not resolved", lines: calls, missing: missing(["dynamic_calls", "wrapper_calls"]) },
    { id: "shapes", value: n(c.routes_with_sections_not_read), title: "routes with parts of their shape not read", lines: shapes, missing: missing(["routes_with_sections_not_read"]) },
    { id: "depth", value: n(c.routes_depth_cap), title: "route shapes cut at the depth cap", lines: [], missing: missing(["routes_depth_cap"]) },
    { id: "files", value: n(c.files_without_facts), title: "files with no fact stored", lines: files, missing: missing(["files_without_facts"]) },
  ];
}

/** CATEGORY_LABEL names a file category (row CR-36). */
export const CATEGORY_LABEL: Record<FileCategory, string> = {
  code_no_reader: "Code in a language Graphvein has no reader for",
  code_with_reader: "Code in a language Graphvein reads",
  other: "Other files (an extension in no category, or none)",
  not_code: "Not code: documentation, images, media, data and configuration",
};

/** FILE_GROUP_ORDER puts code first and the long not-code list last
 * (developer decision 2026-11-24). */
export const FILE_GROUP_ORDER: readonly FileCategory[] = ["code_no_reader", "code_with_reader", "other", "not_code"];

/** FileGroup is one category of files without facts, by extension. */
export interface FileGroup {
  category: FileCategory;
  label: string;
  /** The server's count of the category (all files, not only listed). */
  count: number;
  /** Shown folded until opened (not code). */
  collapsed: boolean;
  extensions: { extension: string; files: FileWithoutFacts[] }[];
  /** Listed files of the category that match the filter. */
  shown: number;
}

/** fileGroups groups the listed files without facts by category and
 * extension, keeping only paths that contain filter (case-insensitive).
 * A category with no file at all is left out. */
export function fileGroups(files: readonly FileWithoutFacts[], byCategory: readonly { category: FileCategory; count: number }[], filter: string): FileGroup[] {
  const f = filter.trim().toLowerCase();
  const out: FileGroup[] = [];
  for (const category of FILE_GROUP_ORDER) {
    const count = byCategory.find((b) => b.category === category)?.count ?? files.filter((x) => x.category === category).length;
    if (count === 0) continue;
    const mine = files.filter((x) => x.category === category && (f === "" || x.file.toLowerCase().includes(f)));
    const byExt = new Map<string, FileWithoutFacts[]>();
    for (const x of mine) {
      const list = byExt.get(x.extension) ?? [];
      list.push(x);
      byExt.set(x.extension, list);
    }
    const extensions = [...byExt.entries()].map(([extension, list]) => ({ extension, files: list })).sort((a, b) => b.files.length - a.files.length || a.extension.localeCompare(b.extension));
    out.push({ category, label: CATEGORY_LABEL[category], count, collapsed: category === "not_code", extensions, shown: mine.length });
  }
  return out;
}

/** extensionLabel shows an empty extension as "no extension". */
export function extensionLabel(ext: string): string {
  return ext === "" ? "no extension" : ext;
}

/** reasonText words why a part is not available. */
export function reasonText(reason: string | null, error: string | null): string {
  switch (reason) {
    case "not_indexed":
      return "the repository is not indexed";
    case "external_indexer":
      return "the index was written by another indexer, whose readers Graphvein does not know";
    case "invalid_config":
      return `graphvein.yaml does not load${error !== null ? `: ${error}` : ""}`;
    default:
      return reason === null ? (error ?? "not available") : `${reason.replace(/_/g, " ")}${error !== null ? `: ${error}` : ""}`;
  }
}

/** UnreadParams is the page's state in the hash: #/unread?tests=1&m=<member>. */
export interface UnreadHashParams {
  tests: boolean;
  /** Project mode: a member, or null for the project scope. */
  member: string | null;
}

export function parseUnreadParams(hash: string): UnreadHashParams {
  const q = parseHashQuery(hash);
  return { tests: q.get("tests") === "1", member: q.get("m") || null };
}

export function unreadHash(p: UnreadHashParams): string {
  const q = new URLSearchParams();
  if (p.tests) q.set("tests", "1");
  if (p.member !== null) q.set("m", p.member);
  const s = q.toString();
  return s === "" ? "#/unread" : `#/unread?${s}`;
}

function where(file: string, line: number | null): string {
  return line === null ? file : `${file}:${String(line)}`;
}

function listLines(title: string, total: number, truncated: boolean, items: string[]): string[] {
  if (total === 0) return [];
  const out = [`${title} (${formatCount(total)}${truncated ? `, first ${formatCount(items.length)} listed` : ""}):`];
  for (const i of items) out.push(`  - ${i}`);
  return out;
}

/** UnplacedText is how a route without a full path is written: its
 * unknown-prefix path ("…/api/x") when it has one, else its pattern
 * marked as the pattern as written, never as if it were the path. */
export interface UnplacedText {
  text: string;
  /** "prefix unknown" or "pattern as written, no path known". */
  marker: string;
  /** The tooltip: the reason and the stored pattern. */
  title: string;
}

/** PATTERN_ONLY marks a route shown by its pattern: no path is known. */
export const PATTERN_ONLY = "pattern as written, no path known";

/** unplacedText writes one route of the generated document's
 * routes-without-a-full-path list (row CR-6) as the API view does. */
export function unplacedText(r: UnplacedRoute): UnplacedText {
  const pattern = r.pattern ?? r.route;
  if (r.prefix_unknown !== null) {
    return { text: r.prefix_unknown.path, marker: "prefix unknown", title: `${r.prefix_unknown.reason_text}; “…” is not read from code, never a full path\npattern as written: ${pattern}` };
  }
  return { text: pattern, marker: PATTERN_ONLY, title: `No path is known for this route; ${pattern} is its pattern as written in the code` };
}

/** ROUTES_WITHHELD_TITLE names the list of route registrations whose path
 * the Go linker withholds (row CR-44): the route is found, its path is
 * withheld (the words of every view, ADR 0089 Revision 5), and it is not a
 * mount. */
export const ROUTES_WITHHELD_TITLE = "Routes whose path is withheld (another pattern or route may answer it; no path claimed)";

/** pageText is one repository's page as plain text ("Copy as text"). */
export function pageText(p: UnreadPage): string[] {
  const out: string[] = [];
  out.push(`Repository ${p.repository}, scope ${p.scope}${p.index !== null ? `, index of ${p.index.finished_at} (${p.index.indexer.name} ${p.index.indexer.version})` : ", not indexed"}`);
  for (const c of cards(p.counts, {}, p.scope)) {
    out.push(`- ${c.value === null ? `? ${c.title} (${c.missing ?? "not available"})` : `${formatCount(c.value)} ${c.title}`}${c.lines.length > 0 ? `: ${c.lines.join("; ")}` : ""}`);
  }
  const rp = p.route_paths;
  out.push(...listLines("Mounts whose prefix is not a constant", rp.mounts.total, rp.mounts.truncated, rp.mounts.items.map((m) => `${m.hop} at ${where(m.file, m.line)}: ${plural(m.routes, "route", "routes")} below it`)));
  out.push(...listLines("Routes without a full path", rp.unplaced.total, rp.unplaced.truncated, rp.unplaced.items.map((r) => {
    const u = unplacedText(r);
    return `${r.method ?? "?"} ${u.text} (${u.marker}) at ${where(r.file, r.line)}`;
  })));
  out.push(...listLines("Patterns built at run time", rp.dynamic.total, rp.dynamic.truncated, rp.dynamic.items.map((d) => `${d.expression ?? d.message} at ${where(d.file, d.line)}`)));
  out.push(...listLines("Router forms outside the route contract", rp.router_forms.total, rp.router_forms.truncated, rp.router_forms.items.map((r) => `${r.file}: ${r.message}`)));
  out.push(...listLines("Mounts whose router is not known (no path claimed)", rp.mounts_unresolved.total, rp.mounts_unresolved.truncated, rp.mounts_unresolved.items.map((d) => `${d.expression ?? d.message} at ${where(d.file, d.line)}`)));
  out.push(...listLines(ROUTES_WITHHELD_TITLE, rp.routes_withheld.total, rp.routes_withheld.truncated, rp.routes_withheld.items.map((d) => `${d.expression ?? d.message} at ${where(d.file, d.line)}`)));
  out.push(...listLines("Go route recognition failed", rp.failed.total, rp.failed.truncated, rp.failed.items.map((d) => d.message)));
  const cc = p.client_calls;
  out.push(...listLines("Client calls with a URL or method not known statically", cc.dynamic.total, cc.dynamic.truncated, cc.dynamic.items.map((c) => `${c.name} at ${where(c.file, c.line)}`)));
  out.push(...listLines("Client calls through a wrapper", cc.wrapper.total, cc.wrapper.truncated, cc.wrapper.items.map((c) => `${c.name} at ${where(c.file, c.line)}`)));
  const sh = p.shapes;
  out.push(...listLines("Routes without a stored shape", sh.without_shape.total, sh.without_shape.truncated, sh.without_shape.items.map((r) => `${r.method ?? "?"} ${r.label} at ${where(r.file, r.line)}`)));
  out.push(...listLines("Routes with shape sections not read", sh.sections_not_read.total, sh.sections_not_read.truncated, sh.sections_not_read.items.map((r) => `${r.method ?? "?"} ${r.label} (${r.sections.join(", ")}) at ${where(r.file, r.line)}`)));
  out.push(...listLines("Route shapes cut at the depth cap", p.depth_caps.routes.total, p.depth_caps.routes.truncated, p.depth_caps.routes.items.map((r) => `${r.method ?? "?"} ${r.label} at ${where(r.file, r.line)}`)));
  const fi = p.files;
  out.push(...listLines("Files not read", fi.not_read.total, fi.not_read.truncated, fi.not_read.items.map((f) => `${f.file}: ${f.code}`)));
  out.push(...listLines("Files read with syntax errors", fi.syntax_errors.total, fi.syntax_errors.truncated, fi.syntax_errors.items.map((f) => f.file)));
  out.push(...listLines("Records dropped as invalid", fi.records_dropped.total, fi.records_dropped.truncated, fi.records_dropped.items.map((f) => (f.file === "" ? f.message : `${f.file}: ${f.message}`))));
  out.push(...listLines("TypeScript structure outside the contract", fi.structure_outside_contract.total, fi.structure_outside_contract.truncated, fi.structure_outside_contract.items.map((f) => `${f.file}: ${f.message}`)));
  out.push(...listLines("Paths the scanner did not index", fi.scanner_gaps.total, fi.scanner_gaps.truncated, fi.scanner_gaps.items.map((g) => `${g.path}: ${g.code}`)));
  const wf = fi.without_facts;
  if (wf.available) {
    out.push(...listLines("Files with no fact stored (by extension)", wf.files.total, false, wf.by_extension.map((e) => `${extensionLabel(e.extension)}: ${formatCount(e.count)}`)));
  } else {
    out.push(`Files with no fact stored: ${reasonText(wf.reason, wf.error)}`);
  }
  return out;
}

/** projectText is the project scope as plain text. */
export function projectText(p: UnreadProject): string[] {
  const out = [`Project ${p.project}, scope ${p.scope} (project totals are the members' sums)`];
  for (const c of cards(p.totals, p.not_available, p.scope)) {
    out.push(`- ${c.value === null ? `? ${c.title} (${c.missing ?? "not available"})` : `${formatCount(c.value)} ${c.title}`}`);
  }
  for (const m of p.members) {
    out.push("");
    out.push(...pageText(m));
  }
  return out;
}

/** routePathsComplete says whether the page may say that every production
 * route it holds has a full path: no route path gap of any kind is listed
 * (rows CR-6, CR-7, CR-9, CR-10, CR-40..CR-42, CR-44; T040 review).
 * Files not read are apart: see codeFilesNotRead. */
export function routePathsComplete(p: UnreadPage): boolean {
  const rp = p.route_paths;
  if (!rp.document.available) return false;
  return rp.unplaced.total + rp.mounts.total + rp.dynamic.total + rp.router_forms.total + rp.failed.total + rp.mounts_unresolved.total + rp.routes_withheld.total + p.files.records_dropped.total === 0;
}

/** codeFilesNotRead is the number of files in a language Graphvein reads
 * that were not read (row CR-19: file_not_utf8, file_too_large and the
 * like; category code_with_reader, CR-36), from the list's per-category
 * count. A route in such a file is not known, so the full-path sentence
 * may speak only of the files read (JX-R2 B3). */
export function codeFilesNotRead(p: UnreadPage): number {
  const nr = p.files.not_read;
  const counted = nr.by_category.find((b) => b.category === "code_with_reader");
  return counted !== undefined ? counted.count : nr.items.filter((f) => f.category === "code_with_reader").length;
}

/** PATHS_COMPLETE is the sentence shown when routePathsComplete holds,
 * every code file in a language Graphvein reads was read and no file was
 * read with syntax errors. */
export const PATHS_COMPLETE = "Every production route has a full path, and no route pattern, router form, mount or Go route recognition is recorded as not read or failed.";

/** PATHS_COMPLETE_IN_FILES_READ opens the sentence shown instead when
 * routePathsComplete holds but code files were not read; filesNotReadText
 * ends it, linking to the files-not-read list. */
export const PATHS_COMPLETE_IN_FILES_READ = "Every production route in the files read has a full path";

/** filesNotReadText is "N files were not read" (n > 0). */
export function filesNotReadText(n: number): string {
  return `${plural(n, "file was", "files were")} not read`;
}

/** PATHS_COMPLETE_IN_FILES_READ_WITHOUT_ERRORS opens the sentence when
 * routePathsComplete holds but files were read with syntax errors: a
 * route in a part tree-sitter recovered may be missed, so the sentence
 * speaks only of the files read without errors (row CR-20). */
export const PATHS_COMPLETE_IN_FILES_READ_WITHOUT_ERRORS = "Every production route in the files read without errors has a full path";

/** syntaxErrorsText is "N files had syntax errors" (n > 0). */
export function syntaxErrorsText(n: number): string {
  return `${plural(n, "file", "files")} had syntax errors`;
}

/** PathsGap is one reason the full-path sentence is limited: its text
 * ("3 files were not read") and the files list it links to. */
export interface PathsGap {
  list: "files-not-read" | "syntax-errors";
  text: string;
}

/** pathsSentence is the full-path sentence when routePathsComplete holds:
 * PATHS_COMPLETE with no file gap, else the opening that names the files
 * it speaks of and the gaps that limit it, each linking to its list
 * (files not read, JX-R2 B3; files read with syntax errors). */
export function pathsSentence(p: UnreadPage): { opening: string; gaps: PathsGap[] } {
  const notRead = codeFilesNotRead(p);
  const syntax = p.files.syntax_errors.total;
  const gaps: PathsGap[] = [];
  if (notRead > 0) gaps.push({ list: "files-not-read", text: filesNotReadText(notRead) });
  if (syntax > 0) gaps.push({ list: "syntax-errors", text: syntaxErrorsText(syntax) });
  if (gaps.length === 0) return { opening: PATHS_COMPLETE, gaps };
  return { opening: syntax > 0 ? PATHS_COMPLETE_IN_FILES_READ_WITHOUT_ERRORS : PATHS_COMPLETE_IN_FILES_READ, gaps };
}

/** COPY_HEADER opens the copied text. */
export const COPY_HEADER = "What Graphvein couldn't read. Facts behind these items are shown as unknown in the other views, never as absent.";
