// The wrong-fact report (T040, ADR 0089): the public-repository switch
// and the prefilled issue. Nothing renders while the server's report_url
// is null; the prefill goes into a public URL, so the flag always shows a
// preview first (developer decision 2026-11-24, variant B).

import { createContext, createElement, useContext, useEffect, useState, type ReactNode } from "react";

import { fetchUiDefaults, type UiFetcher } from "@/api/ui";

/** ReportState is what a flag needs: the issue form (null: no flag) and
 * the version line of the report. */
export interface ReportState {
  url: string | null;
  version: string;
}

const ReportContext = createContext<ReportState>({ url: null, version: "" });

/** useReport reads the switch; outside a provider it is off. */
export function useReport(): ReportState {
  return useContext(ReportContext);
}

/** ReportProvider asks GET /api/v0/ui for the switch once. version is the
 * shell's version line (server, extractors, contract). A failed request
 * leaves the switch off. */
export function ReportProvider({ fetcher = fetchUiDefaults, version, children }: { fetcher?: UiFetcher; version: string; children: ReactNode }) {
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    fetcher({ signal: controller.signal }).then(
      (d) => {
        if (!controller.signal.aborted) setUrl(d.report_url ?? null);
      },
      () => {
        // No switch when the request fails.
      },
    );
    return () => {
      controller.abort();
    };
  }, [fetcher]);
  return createElement(ReportContext.Provider, { value: { url, version } }, children);
}

/** WrongFact is the fact a flag reports. */
export interface WrongFact {
  /** The fact as the UI states it ("GET /api/items is served by ..."). */
  fact: string;
  /** file:line of the fact's evidence, and of the other side when there
   * is one. */
  where: string[];
  /** The rule or precision it rests on, in the UI's words. */
  rule: string | null;
  /** How the UI labelled it, when it has a label ("Inferred across
   * repositories"). */
  shown: string | null;
}

/** HOST is what replaces the scheme and host of a URL in the prefill. */
export const HOST = "<host>";

/** LOCAL_PATH is what replaces an absolute local path in the prefill. */
export const LOCAL_PATH = "<local path>";

// An absolute URL's scheme and authority ("https://billing.corp:8443"), a
// scheme-relative authority ("//cdn.example.com") at the start of the text
// or after a quote, bracket or space, or the authority after a scheme
// built in code: "//host" or "://host" right after a template
// substitution's "}" ("${location.protocol}//host", "${proto}://host",
// "http${s}://host"). The path after it is kept. A template's "${host}"
// stays inside the authority.
const URL_AUTHORITY = /\b[A-Za-z][A-Za-z0-9+.-]*:\/\/[^/\s"'`)\]]*|(^|[\s"'`([=])\/\/[A-Za-z0-9$][^/\s"'`)\]]*|(\}):?\/\/[A-Za-z0-9$[][^/\s"'`)\]]*/g;
// A host joined to a "//", "://" or "https://" string by "+"
// ('location.protocol + "//" + host', "proto + '://' + 'api.corp'"): the
// operand after the "+", an
// identifier or member chain, or a string literal up to its first "/".
const JOINED_HOST = /(["'`])((?:[A-Za-z][A-Za-z0-9+.-]*:|:)?\/\/)\1(\s*\+\s*)(?:(["'`])[^"'`/\s]*|[A-Za-z_$][\w$]*(?:\??\.[A-Za-z_$][\w$]*)*(?:\(\))?)/g;
// A file URL or a Windows drive path ("file:///home/me/x", "C:\\Users\\me").
const LOCAL_ABSOLUTE = /\bfile:\/\/\/?[^\s"'`)\]}>]*|\b[A-Za-z]:[\\/][^\s"'`)\]}>]*/g;
// The origin of an origin assumption ("origin <origin> assumed to be"):
// a stored literal, possibly without a scheme.
const ASSUMED_ORIGIN = /\borigin (\S+) assumed to be\b/g;

/** redactText removes what can name a private machine or network from a
 * text of the prefill: local file URLs and drive paths become
 * LOCAL_PATH, the scheme and host of absolute URLs and the host of an
 * assumed origin become HOST; URL paths are kept, because the fact is
 * about them (T040 review). */
export function redactText(text: string): string {
  return text
    .replace(LOCAL_ABSOLUTE, LOCAL_PATH)
    .replace(JOINED_HOST, (_m: string, q: string, slashes: string, plus: string, quote: string | undefined) => `${q}${slashes}${q}${plus}${quote ?? ""}${HOST}`)
    .replace(URL_AUTHORITY, (m: string, lead: string | undefined, brace: string | undefined) => {
      if (brace !== undefined) return brace + HOST;
      return lead !== undefined && m.startsWith(lead) && !m.includes("://") ? lead + HOST : HOST;
    })
    .replace(ASSUMED_ORIGIN, (m: string, origin: string) => {
      if (origin.startsWith(HOST) || origin.startsWith("(")) return m;
      const slash = origin.indexOf("/");
      return `origin ${HOST}${slash >= 0 ? origin.slice(slash) : ""} assumed to be`;
    });
}

/** redactWhere is redactText for a file:line: a stored path is relative
 * to its repository, so an absolute one ("/home/me/repo/x.ts:3") can only
 * be a local path and is replaced, keeping the line. */
export function redactWhere(where: string): string {
  // "<member> <path>" in project mode: the member name is kept.
  const space = where.lastIndexOf(" ");
  const lead = space >= 0 ? where.slice(0, space + 1) : "";
  const at = space >= 0 ? where.slice(space + 1) : where;
  if (at.startsWith("/") || at.startsWith("\\") || at.startsWith("~")) {
    const line = /:\d+$/.exec(at);
    return redactText(lead) + LOCAL_PATH + (line !== null ? line[0] : "");
  }
  return redactText(where);
}

/** shownText is the issue's "What Graphvein shows" field: the form has
 * no rule or location field, so fact, file:line and rule all go here,
 * redacted (redactText). Copy text copies it whole. */
export function shownText(f: WrongFact, version: string): string {
  const lines = [`Fact: ${redactText(f.fact)}`];
  if (f.shown !== null) lines.push(`Shown as: ${redactText(f.shown)}`);
  if (f.rule !== null) lines.push(`Rule: ${redactText(f.rule)}`);
  f.where.forEach((w, i) => {
    lines.push(`${i === 0 ? "Where: " : "  and: "}${redactWhere(w)}`);
  });
  if (version !== "") lines.push(`Version: ${version}`);
  return lines.join("\n");
}

/** ISSUE_TEMPLATE is the issue form's file name in the public repository. */
export const ISSUE_TEMPLATE = "wrong-fact.yml";

/** MAX_ISSUE_URL is the longest issue link built, in characters. GitHub
 * refuses or cuts longer links (about 8 kB); this stays under 8000. */
export const MAX_ISSUE_URL = 7900;

/** SHORTENED_LINE ends a prefill that was cut to fit the link. */
export const SHORTENED_LINE = "(Shortened to fit the link. Copy text in Graphvein has all of it.)";

/** IssueLink is the prefilled form link and whether its text was cut. */
export interface IssueLink {
  url: string;
  shortened: boolean;
}

function buildURL(base: string, version: string, shown: string): string {
  const q = new URLSearchParams();
  q.set("template", ISSUE_TEMPLATE);
  if (version !== "") q.set("version", version);
  q.set("shown", shown);
  return `${base}?${q.toString()}`;
}

/** MIN_LINE is the shortest a cut line gets: its field name and the
 * start of its text. */
const MIN_LINE = 48;

/** FIELD_START marks the first line of each field of shownText; those are
 * always kept when the text is cut, other lines ("  and: ...") may go. */
const FIELD_START = /^(Fact|Shown as|Rule|Where|Version): /;

/** cutLine keeps the start of a line longer than max, ending it with "…";
 * a surrogate pair is never split. */
function cutLine(line: string, max: number): string {
  if (line.length <= max) return line;
  let end = max - 1;
  const last = line.charCodeAt(end - 1);
  if (last >= 0xd800 && last <= 0xdbff) end--;
  return `${line.slice(0, end)}…`;
}

/** LONG_LINE is the length a line may be cut to before whole lines are
 * dropped: an overlong line (a rule of hundreds of edges, a fact of
 * thousands of characters) is cut first. */
const LONG_LINE = 300;

/** issueLink prefills the public issue form: the version field and "What
 * Graphvein shows" (design D8; the kind dropdown is not prefilled). A text
 * too long for the link (MAX_ISSUE_URL) is cut and ends with
 * SHORTENED_LINE (T040 review, JX-R2 B2):
 *
 *  1. every line longer than the longest length that fits keeps its start
 *     plus "…", when that length is at least LONG_LINE;
 *  2. else lines that are not a field's first line ("  and: ...") go from
 *     the end until the lines cut at LONG_LINE fit, and then step 1 runs
 *     on the lines kept;
 *  3. the first line of every field (Fact, Shown as, Rule, Where, Version)
 *     is always kept, cut down to MIN_LINE at most. */
export function issueLink(base: string, f: WrongFact, version: string): IssueLink {
  const full = shownText(f, version);
  const url = buildURL(base, version, full);
  if (url.length <= MAX_ISSUE_URL) return { url, shortened: false };
  const build = (kept: string[], max: number): string => buildURL(base, version, [...kept.map((l) => cutLine(l, max)), SHORTENED_LINE].join("\n"));
  const fits = (kept: string[], max: number): boolean => build(kept, max).length <= MAX_ISSUE_URL;
  // The longest line length in [floor, longest line] that fits, or null.
  const longest = (kept: string[], floor: number): number | null => {
    const top = Math.max(floor, ...kept.map((l) => l.length));
    // Whole lines first: a cut adds "…", which can make a line longer
    // once encoded.
    if (fits(kept, top)) return top;
    if (!fits(kept, floor)) return null;
    let lo = floor;
    let hi = top;
    while (lo < hi) {
      const mid = Math.ceil((lo + hi) / 2);
      if (fits(kept, mid)) lo = mid;
      else hi = mid - 1;
    }
    return lo;
  };
  const kept = full.split("\n");
  const all = longest(kept, LONG_LINE);
  if (all !== null) return { url: build(kept, all), shortened: true };
  while (!fits(kept, LONG_LINE)) {
    let drop = -1;
    for (let i = kept.length - 1; i >= 0; i--) {
      if (!FIELD_START.test(kept[i] ?? "")) {
        drop = i;
        break;
      }
    }
    if (drop < 0) break;
    kept.splice(drop, 1);
  }
  const cut = longest(kept, MIN_LINE);
  if (cut !== null) return { url: build(kept, cut), shortened: true };
  // Only reached for a base or version so long that the fields' first
  // lines cannot fit even cut.
  return { url: buildURL(base, "", SHORTENED_LINE), shortened: true };
}

/** issueURL is issueLink's link. */
export function issueURL(base: string, f: WrongFact, version: string): string {
  return issueLink(base, f, version).url;
}

/** reportHost is the host the preview names ("github.com/owner/repo"). */
export function reportHost(base: string): string {
  try {
    const u = new URL(base);
    const parts = u.pathname.split("/").filter((s) => s !== "");
    return [u.host, ...parts.slice(0, 2)].join("/");
  } catch {
    return base;
  }
}
