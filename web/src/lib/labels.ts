// The label table (T025b): every string T025 made plainer, in both styles.
// T025 (the T025 task record (internal), "Plainer default wording")
// moved internal ids, tiers with confidence numbers and raw kinds out of
// the primary text; the "Labels: Plain / Technical" setting brings them
// back for people who read them faster. This table is the single source of
// both variants: each entry lists the T025 rows it covers, and the UI
// reads its text only through `label` (and the small wrappers below), so a
// style never drifts from the other.
//
// Tooltips stay in both styles: a plain label carries the technical detail
// in its tooltip, a technical label the plain words.
//
// Rows 20 and 21 are not in T025's table: they are the two T025 leftovers
// T025b added (the impact panel's Unknowns line), numbered after it. Rows
// 22 to 25 are project mode's wording (T022 B9): a cross-repository link,
// a link pass outcome, its claim row and an impact path crossing
// repositories. Rows 26 and 27 are manifest editing's (T022 B10): an
// operation's name and a selector field. Rows 28 to 30 are the sidebar's
// project member list (T022 MR-UI): the link status line, a member's
// freshness and the default-selection note. Rows 59 to 64 are the
// Explorer's (T029): the provenance chip, a section's state, an
// operation's path marker, how a name was read, a value set by you and the
// claim row that keeps an item out of the standard fields. Rows 65 to 68
// are the Explorer's Try it out (T029 part B): a base URL suggestion's
// source, where an environment's auth comes from, why a request asks for a
// confirmation, and why a response was cut. Rows 69 to 73 are T073's
// quieter API pages: a list row's "0 linked", the group header's path
// assumption count, the lead of an assumption every caller shares, the
// route header's unverified count and the Flow legend strip's count of
// kinds with no line.
//
// Row 9 reads the same in both styles: it is the route's full path, a
// correctness fix rather than a wording choice. Row 4 has no checked rule
// word (T025 kept the stored fields); since T041 its plain style shows the
// provenance word and the number ("Inferred · 0.9") when it has no match
// field, and the stored fields otherwise.

import type { LabelStyle } from "@/lib/labelChoice";

/** Label is one rendered string: text is the primary text (empty for a
 * tag the style hides), title its tooltip, aria an accessible name where
 * the element needs its own. */
export interface Label {
  text: string;
  title: string;
  aria?: string;
}

/** Entry is one table entry: the T025 rows it covers with a sample input
 * per row (the test renders each row in both styles), and the two
 * variants. same lists rows whose two variants are deliberately equal. */
interface Entry<A> {
  rows: Readonly<Record<number, A>>;
  same?: readonly number[];
  plain: (a: A) => Label;
  technical: (a: A) => Label;
}

/** EdgeFields are the stored fields of an edge that a label reads. */
export interface EdgeFields {
  kind: string;
  provenance: string;
  confidence: number;
  match: string | null;
}

/** confidenceNumber writes a stored confidence in its shortest exact form,
 * the one number format of the UI (T041): an integer with one decimal (1 as
 * "1.0"), every other value as stored (0.9, 0.6; an external indexer's 0.85
 * stays 0.85 and a stored 0.875 is never rounded to 0.88). */
export function confidenceNumber(c: number): string {
  return Number.isInteger(c) ? c.toFixed(1) : String(c);
}

/** RANK_NOTE says once, in a tooltip of a plain label that shows a
 * confidence, what the number is (T041). */
export const RANK_NOTE = "The number is a fixed rank per match rule, not a probability.";

/** SHAPE_NOTE is RANK_NOTE for an Explorer provenance chip (row 59): its
 * number is fixed per kind (claim row SM-11), not per match rule. */
export const SHAPE_NOTE = "The number is fixed per kind (1.0 for anything read from code; a mapped item takes its link's rank), not a probability.";

/** withNumber puts a stored confidence beside a plain word (T041): "Exact
 * path match · 0.9", the one "rule · number" form of the plain style. */
export function withNumber(words: string, c: number): string {
  return `${words} · ${confidenceNumber(c)}`;
}

/** factLabel is the chip of something read as a fact where the API sends
 * no number beside it (an endpoint's header badge, the route side of a
 * request difference): every fact has confidence exactly 1 (the indexer
 * contract; claim row SM-11 for shape items), so it reads "Fact · 1.0"
 * like every other fact chip, "fact 1.0" in the technical style. */
export function factLabel(style: LabelStyle): Label {
  return style === "technical"
    ? { text: `fact ${confidenceNumber(1)}`, title: "Fact: read from the code; every fact has confidence 1.0" }
    : { text: withNumber("Fact", 1), title: `fact ${confidenceNumber(1)}: read from the code; every fact has confidence 1.0. ${RANK_NOTE}` };
}

/** storedEdge writes an edge as stored: "inferred 0.9 exact", "fact 1.0". */
export function storedEdge(e: EdgeFields): string {
  return [e.provenance, confidenceNumber(e.confidence), e.match].filter((s) => s !== null && s !== "").join(" ");
}

/** edgePlainWords is the plain word of an edge, or null when no word was
 * checked for its form (T025 row 4: the stored fields stay). */
function edgePlainWords(e: EdgeFields): string | null {
  if (e.provenance === "fact" && e.confidence === 1) return "Fact";
  if (e.provenance === "inferred" && e.match === "exact") return "Exact path match";
  if (e.provenance === "inferred" && e.match === "suffix") return "Path suffix match";
  return null;
}

/** A flow tier T025 worded (rows 5-8). */
export type WordedTier = "fact" | "exact" | "suffix";

const TIER_WORD: Record<WordedTier, string> = { fact: "Fact", exact: "Exact path match", suffix: "Path suffix match" };

/** The legend's explanation of a worded tier. Both styles show the
 * numbers in the chip (T041), so the explanation carries none. */
const TIER_DETAIL: Record<WordedTier, { technical: string }> = {
  fact: { technical: "exposure or invoke" },
  exact: { technical: "URL matches the route path" },
  suffix: { technical: "base URL or route prefix unresolved" },
};

/** The fixed confidences of a worded tier's match rules. */
const TIER_NUMBERS: Record<WordedTier, string> = { fact: "1.0", exact: "0.9 / 0.7", suffix: "0.6" };

/** The plain chip shows the first rank only, kept short (T041: about 24
 * characters per visible plain label); a second rank is in its tooltip. */
const TIER_NUMBERS_SHORT: Record<WordedTier, string> = { fact: "1.0", exact: "0.9", suffix: "0.6" };

/** The second rank of a tier, said in the plain chip's tooltip. */
const TIER_SECOND_RANK: Partial<Record<WordedTier, string>> = { exact: "0.7 when the call's origin is assumed, not proven" };

const TIER_TECHNICAL: Record<WordedTier, string> = { fact: `fact ${TIER_NUMBERS.fact}`, exact: `exact ${TIER_NUMBERS.exact}`, suffix: `suffix ${TIER_NUMBERS.suffix}` };

/** kindWords says a stored kind ("client_call") as words ("client call"). */
export function kindWords(kind: string): string {
  return kind.replace(/_/g, " ");
}

/** PROVENANCE_WORD says a provenance in plain words; one this UI does not
 * know is shown raw. */
const PROVENANCE_WORD: Record<string, string> = { fact: "Fact", inferred: "Inferred" };

/** Step is the part of an impact path step the edge-kinds line reads. */
export interface KindStep {
  kind: string;
  removed: boolean;
}

/** Assumption is an assumption id with its catalogue title, when the
 * catalogue has it. */
export interface Assumption {
  id: string;
  title?: string | undefined;
}

/** RouteText is a route's name as Changes words it (routeTitle). */
export interface RouteText {
  text: string;
  title: string;
}

const FOOTNOTE_START = "Fact paths read “depends on”, inferred paths “may depend on”; ";

function entry<A>(e: Entry<A>): Entry<A> {
  return e;
}

/** LABELS is the table. Keys name what is labelled; rows are T025's. */
export const LABELS = {
  /** API section: Callers "Confidence", endpoint Overview "Link" and the
   * OpenAPI operation edge (rows 1-4). */
  edge: entry<EdgeFields>({
    rows: {
      1: { kind: "calls_route", provenance: "inferred", confidence: 0.9, match: "exact" },
      2: { kind: "calls_route", provenance: "inferred", confidence: 0.6, match: "suffix" },
      3: { kind: "exposes_route", provenance: "fact", confidence: 1, match: null },
      4: { kind: "documents_route", provenance: "inferred", confidence: 0.9, match: null },
    },
    plain: (e) => {
      const words = edgePlainWords(e);
      if (words !== null) return { text: withNumber(words, e.confidence), title: `Stored ${e.kind} edge: ${storedEdge(e)}. ${RANK_NOTE}` };
      // Row 4: an inferred edge with no match field (an OpenAPI operation's
      // documents_route) has no checked rule word; the provenance word and
      // the number stand in its place. Any other form keeps its stored fields.
      if (e.provenance === "inferred" && (e.match === null || e.match === "")) return { text: withNumber("Inferred", e.confidence), title: `Stored ${e.kind} edge: ${storedEdge(e)}. ${RANK_NOTE}` };
      return { text: storedEdge(e), title: `Stored ${e.kind} edge: ${storedEdge(e)}` };
    },
    technical: (e) => {
      const words = edgePlainWords(e);
      return { text: storedEdge(e), title: `Stored ${e.kind} edge${words !== null ? `: ${words}` : ""}` };
    },
  }),
  /** API Flow: the text drawn on an edge of a worded tier (rows 5-7). The
   * technical text is the tier with the edge's stored confidence. */
  flowTier: entry<{ tier: WordedTier; confidence: number }>({
    rows: { 5: { tier: "fact", confidence: 1 }, 6: { tier: "exact", confidence: 0.9 }, 7: { tier: "suffix", confidence: 0.6 } },
    plain: (a) => ({ text: withNumber(TIER_WORD[a.tier], a.confidence), title: `${a.tier} ${confidenceNumber(a.confidence)}. ${RANK_NOTE}` }),
    technical: (a) => ({ text: `${a.tier} ${confidenceNumber(a.confidence)}`, title: TIER_WORD[a.tier] }),
  }),
  /** API Flow legend chips (row 8): text is the chip's label, title its
   * explanation. */
  legendTier: entry<WordedTier>({
    rows: { 8: "exact" },
    plain: (t) => ({ text: `${TIER_WORD[t]} · ${TIER_NUMBERS_SHORT[t]}`, title: `${TIER_DETAIL[t].technical}${TIER_SECOND_RANK[t] !== undefined ? `; ${TIER_SECOND_RANK[t]}` : ""}. ${RANK_NOTE}` }),
    technical: (t) => ({ text: TIER_TECHNICAL[t], title: `${TIER_DETAIL[t].technical} (${TIER_WORD[t]})` }),
  }),
  /** Changes: a route's full path (row 9), the same in both styles. */
  route: entry<RouteText>({
    rows: { 9: { text: "GET /api/v1/items", title: "as written in its file: GET /" } },
    same: [9],
    plain: (r) => ({ text: r.text, title: r.title }),
    technical: (r) => ({ text: r.text, title: r.title }),
  }),
  /** Changes: the provenance tag of impact paths, items and hints (row
   * 10). confidence is null where none is shown (move hints). */
  provenance: entry<{ provenance: string; confidence: number | null }>({
    rows: { 10: { provenance: "inferred", confidence: 0.9 } },
    plain: (a) => {
      const word = PROVENANCE_WORD[a.provenance] ?? a.provenance;
      return a.confidence !== null
        ? { text: withNumber(word, a.confidence), title: `provenance ${a.provenance}, confidence ${confidenceNumber(a.confidence)} (the weakest step). ${RANK_NOTE}` }
        : { text: word, title: `provenance ${a.provenance}` };
    },
    technical: (a) => ({
      text: a.confidence !== null ? `${a.provenance} ${confidenceNumber(a.confidence)}` : a.provenance,
      title: `${PROVENANCE_WORD[a.provenance] ?? a.provenance}: provenance ${a.provenance}${a.confidence !== null ? `, confidence ${confidenceNumber(a.confidence)} (the weakest step)` : ""}`,
    }),
  }),
  /** Changes: the impact rule tag of a path or group (row 11). Plain hides
   * the tag (text ""); its id stays in the wording's tooltip. */
  impactRule: entry<string>({
    rows: { 11: "I4" },
    plain: (rule) => ({ text: "", title: `impact rule ${rule}` }),
    technical: (rule) => ({ text: rule, title: `impact rule ${rule}` }),
  }),
  /** Changes: the contract row tag of an item header (row 12). */
  contractRow: entry<string>({
    rows: { 12: "C3" },
    plain: (row) => ({ text: "", title: `contract row ${row}` }),
    technical: (row) => ({ text: row, title: `contract row ${row}` }),
  }),
  /** Changes: the contract rows tag of a group list header (row 13). */
  contractRows: entry<string>({
    rows: { 13: "C4 C5" },
    plain: (rows) => ({ text: "", title: `contract rows ${rows}` }),
    technical: (rows) => ({ text: rows, title: `contract rows ${rows}` }),
  }),
  /** Changes: the edge kinds of an impact path, from the seed (row 14). */
  edgeKinds: entry<readonly KindStep[]>({
    rows: {
      14: [
        { kind: "exposes_route", removed: false },
        { kind: "calls_route", removed: false },
      ],
    },
    plain: (steps) => ({
      text: steps.map((s) => (s.removed ? `${kindWords(s.kind)} (removed, base)` : kindWords(s.kind))).join(" → "),
      title: `edge kinds of the path, from the seed: ${steps.map((s) => s.kind).join(" → ")}`,
    }),
    technical: (steps) => ({
      text: steps.map((s) => (s.removed ? `${s.kind} (removed, base)` : s.kind)).join(" → "),
      title: `edge kinds of the path, from the seed: ${steps.map((s) => kindWords(s.kind)).join(" → ")}`,
    }),
  }),
  /** Changes: a node kind on list rows, item headers and impact paths
   * (row 15). */
  nodeKind: entry<string>({
    rows: { 15: "client_call" },
    plain: (kind) => ({ text: kindWords(kind), title: kind }),
    technical: (kind) => ({ text: kind, title: kindWords(kind) }),
  }),
  /** Changes: the impact panel's footnote (row 16). */
  impactFootnote: entry<null>({
    rows: { 16: null },
    plain: () => ({
      text: `${FOOTNOTE_START}a path’s confidence, beside its Fact or Inferred tag, is its weakest step. Never “breaks”.`,
      title: "",
    }),
    technical: () => ({ text: `${FOOTNOTE_START}confidence is the weakest step. Never “breaks”.`, title: "" }),
  }),
  /** Schema "Rests on" chips and the Changes "assumes" chip (row 17). An
   * id the catalogue lacks stays raw in both styles. */
  assumptionChip: entry<Assumption>({
    rows: { 17: { id: "V1", title: "Library versions" } },
    plain: (a) => ({
      text: a.title ?? a.id,
      title: a.title !== undefined ? `assumption ${a.id}: ${a.title}` : `assumption ${a.id}`,
      aria: `${a.title !== undefined ? `${a.title} (${a.id})` : a.id}: what it means`,
    }),
    technical: (a) => ({
      text: a.id,
      title: a.title !== undefined ? `assumption ${a.id}: ${a.title}` : `assumption ${a.id}`,
      aria: `${a.id}${a.title !== undefined ? `, ${a.title}` : ""}: what it means`,
    }),
  }),
  /** Schema column row chip (row 18): the assumptions a row rests on. */
  assumptionCount: entry<readonly Assumption[]>({
    rows: { 18: [{ id: "E2", title: "Engine dialect" }] },
    plain: (all) => ({ text: `+${String(all.length)} ${all.length === 1 ? "assumption" : "assumptions"}`, title: restsTitle(all) }),
    technical: (all) => ({ text: all.map((a) => a.id).join(" "), title: restsTitle(all) }),
  }),
  /** Changes I8 "assumes" line (row 19): the catalogue title before the
   * text. Plain does not repeat it (the chip shows it). */
  assumptionLead: entry<Assumption>({
    rows: { 19: { id: "TA-V3c", title: "SQLAlchemy receiver" } },
    plain: () => ({ text: "", title: "" }),
    technical: (a) => ({ text: a.title !== undefined ? `${a.title}.` : "", title: "" }),
  }),
  /** Changes Unknowns, "Edge kinds no relation follows" (row 20, T025b):
   * one edge kind of the line. */
  untraversedKind: entry<string>({
    rows: { 20: "exposes_route" },
    plain: (kind) => ({ text: kindWords(kind), title: kind }),
    technical: (kind) => ({ text: kind, title: kindWords(kind) }),
  }),
  /** Changes Unknowns, a relation not extracted (row 21, T025b). names
   * are the short names of the nodes it concerns. The plain style keeps
   * the rule id in the tooltip; an unknown relation reads raw in both. */
  notExtracted: entry<{ relation: string; names: readonly string[] }>({
    rows: { 21: { relation: "I11", names: [] } },
    plain: (a) => ({ text: notExtractedText(a.relation, a.names, false), title: `impact rule ${a.relation}` }),
    technical: (a) => ({ text: notExtractedText(a.relation, a.names, true), title: `impact rule ${a.relation}` }),
  }),
  /** T022 B9 (project mode): a cross-repository link's provenance and
   * confidence (row 22). Plain shows the provenance word with the number
   * (T041, kept short: the repository badge beside it already says the
   * link crosses repositories; the tooltip says it in words). */
  projectLink: entry<{ kind: string; provenance: string; confidence: number }>({
    rows: { 22: { kind: "calls_route", provenance: "inferred", confidence: 0.7 } },
    plain: (l) => ({
      text: withNumber(PROVENANCE_WORD[l.provenance] ?? l.provenance, l.confidence),
      title: `${PROVENANCE_WORD[l.provenance] ?? l.provenance} across repositories: ${l.kind} project link, ${l.provenance} ${confidenceNumber(l.confidence)}. ${RANK_NOTE}`,
    }),
    technical: (l) => ({ text: `${l.provenance} ${confidenceNumber(l.confidence)}`, title: `${l.kind} project link, across repositories` }),
  }),
  /** T022 B9: a project link outcome's status (row 23). A status this UI
   * does not know reads raw in both styles. */
  outcomeStatus: entry<string>({
    rows: { 23: "ambiguous" },
    plain: (status) => ({ text: OUTCOME_WORD[status] ?? status, title: `link pass outcome ${status}` }),
    technical: (status) => ({ text: status, title: OUTCOME_WORD[status] ?? `link pass outcome ${status}` }),
  }),
  /** T022 B9: the claim row of a project link outcome (row 24). Plain hides
   * the tag; its id stays in the tooltip. */
  claimRow: entry<string>({
    rows: { 24: "PL-R11" },
    plain: (row) => ({ text: "", title: `project link claim row ${row}` }),
    technical: (row) => ({ text: row, title: `project link claim row ${row}` }),
  }),
  /** T022 B9: an impact path that crosses repositories through a project
   * link (row 25). */
  crossRepository: entry<{ from: string; to: string }>({
    rows: { 25: { from: "express", to: "react" } },
    plain: (a) => ({ text: `crosses from ${a.from} into ${a.to}`, title: `project link ${a.from} → ${a.to}` }),
    technical: (a) => ({ text: `${a.from} → ${a.to}`, title: `crosses from ${a.from} into ${a.to} through a project link` }),
  }),
  /** Project view, manifest editing: an operation's name (row 26) and a
   * selector field (row 27), T022 B10. */
  manifestOp: entry<string>({
    rows: { 26: "accept_suggestion" },
    plain: (op) => ({ text: OP_WORD[op] ?? op, title: `manifest operation ${op}` }),
    technical: (op) => ({ text: op, title: OP_WORD[op] ?? op }),
  }),
  selectorField: entry<string>({
    rows: { 27: "path_prefixes" },
    plain: (k) => ({ text: FIELD_WORD[k] ?? k, title: `select: ${k}` }),
    technical: (k) => ({ text: k, title: FIELD_WORD[k] ?? k }),
  }),
  /** Sidebar member list, T022 MR-UI: the link status line (row 28).
   * Plain says the state, the pass and the count, or that the links are
   * stale; technical gives the stored fields. Both tooltips carry the
   * stale reasons. */
  linkStatus: entry<{ state: string; pass: number; links: number; stale_links: number; stale_reasons: readonly string[] }>({
    rows: { 28: { state: "fresh", pass: 4, links: 46, stale_links: 0, stale_reasons: [] } },
    plain: (p) => ({ text: linkStatusPlain(p), title: linkStatusTitle(p, `link_pass ${p.state}, pass ${String(p.pass)}`) }),
    technical: (p) => ({
      text: p.state === "none" ? "link_pass none" : `link_pass ${p.state} · pass ${String(p.pass)} · ${String(p.links)} links, ${String(p.stale_links)} stale`,
      title: linkStatusTitle(p, linkStatusPlain(p)),
    }),
  }),
  /** Sidebar member list: a member's freshness (row 29). Plain words it
   * (the Project view's words); technical gives the stored state. */
  memberFreshness: entry<{ checkout: boolean; state: string; changed_count: number | null }>({
    rows: { 29: { checkout: true, state: "stale", changed_count: 3 } },
    plain: (f) => ({ text: freshnessText(f), title: `freshness ${freshnessStored(f)}` }),
    technical: (f) => ({ text: freshnessStored(f), title: freshnessText(f) }),
  }),
  /** Sidebar member list: the note beside the member shown by default
   * until the user chooses one (row 30, ADR 0075 "UI view"). */
  defaultMember: entry<string>({
    rows: { 30: "react" },
    plain: (r) => ({ text: "shown by default: choose a repository", title: `${r} is the first member, shown until a repository is chosen` }),
    technical: (r) => ({ text: "default: first member", title: `${r}: shown by default, choose a repository` }),
  }),
  /** Architecture (T026 AG-C1, rows 31-41): a component kind on cards and
   * in the inspector. framework names the routes' framework of a server. */
  archKind: entry<ArchKind>({
    rows: { 31: { kind: "server", framework: "fastapi", row: "CK1" } },
    plain: (k) => ({ text: kindPlain(k), title: `kind ${k.kind}${k.framework !== null ? ` (${k.framework})` : ""}${k.row !== null ? `, row ${k.row}` : ""}` }),
    technical: (k) => ({ text: `${k.kind}${k.framework !== null ? ` · ${k.framework}` : ""}`, title: `${kindPlain(k)}${k.row !== null ? `, row ${k.row}` : ""}` }),
  }),
  /** Architecture: a link family on chips, the inspector and the Links tab
   * (row 32). */
  linkFamily: entry<{ family: string; row: string }>({
    rows: { 32: { family: "in_process", row: "CE4" } },
    plain: (f) => ({ text: FAMILY_WORD[f.family] ?? kindWords(f.family), title: `family ${f.family}, row ${f.row}` }),
    technical: (f) => ({ text: `${f.family} ${f.row}`, title: FAMILY_WORD[f.family] ?? kindWords(f.family) }),
  }),
  /** Architecture: where an annotation cell's value comes from (row 33),
   * as the server words it; technical adds the contract rows. */
  cellSource: entry<{ source: string; state: string; rows: readonly string[] }>({
    rows: { 33: { source: "proven: route and client-call facts", state: "proven", rows: ["AP1"] } },
    plain: (c) => ({ text: c.source, title: `${c.state}${c.rows.length > 0 ? `, rows ${c.rows.join(" ")}` : ""}` }),
    technical: (c) => ({ text: c.rows.length > 0 ? `${c.source} [${c.rows.join(" ")}]` : `${c.source} [no row]`, title: STATE_WORD[c.state] ?? c.state }),
  }),
  /** Architecture: an annotation cell's state (row 34). */
  cellState: entry<string>({
    rows: { 34: "as_written" },
    plain: (s) => ({ text: STATE_WORD[s] ?? kindWords(s), title: `state ${s}` }),
    technical: (s) => ({ text: s, title: STATE_WORD[s] ?? kindWords(s) }),
  }),
  /** Architecture: a link tier as the server writes it ("fact",
   * "inferred 0.9"; row 35). */
  linkTier: entry<string>({
    rows: { 35: "inferred 0.9" },
    plain: (t) => ({ text: tierPlain(t), title: tierNumber(t) !== null ? `${t}. ${RANK_NOTE}` : t }),
    technical: (t) => ({ text: t, title: tierPlain(t) }),
  }),
  /** Architecture: the level tag of the breadcrumb and of expanded rows
   * (row 36), the same in both styles (the mockups' L1-L4). */
  levelTag: entry<number>({
    rows: { 36: 2 },
    same: [36],
    plain: (n) => ({ text: `L${String(n)}`, title: LEVEL_WORD[n] ?? `level ${String(n)}` }),
    technical: (n) => ({ text: `L${String(n)}`, title: LEVEL_WORD[n] ?? `level ${String(n)}` }),
  }),
  /** Architecture: the Hidden note above the map (row 37). */
  hiddenNote: entry<HiddenParts>({
    rows: { 37: { tests: 0, docs: 1, configured: 0, libraries: 27, testEdges: 17, sdk: 0 } },
    plain: (h) => ({ text: hiddenText(h, false), title: "Hidden by default; the Show toggles bring them back" }),
    technical: (h) => ({ text: hiddenText(h, true), title: "CV1 tests, CV2 docs, CV3 libraries, CV4 configured; test-scope edges left out of the production scope" }),
  }),
  /** Architecture: calls of a component with no stored link (row 38), a
   * chip on the card, never a link. */
  ghost: entry<{ total: number; row: string; by_status: Readonly<Record<string, number>> }>({
    rows: { 38: { total: 19, row: "CE1", by_status: { dynamic: 13, unmatched: 6 } } },
    plain: (g) => ({ text: `${String(g.total)} ${g.total === 1 ? "call" : "calls"} not linked`, title: statusList(g.by_status) }),
    technical: (g) => ({ text: `not linked ×${String(g.total)} (${g.row})`, title: statusList(g.by_status) }),
  }),
  /** Architecture: an entry of the Not proven list (row 39). */
  notProven: entry<{ text: string; row: string }>({
    rows: { 39: { text: "documented, no route: 3 operations", row: "CE5" } },
    plain: (n) => ({ text: n.text, title: n.row !== "" ? `row ${n.row}` : "not proven" }),
    technical: (n) => ({ text: n.row !== "" ? `${n.row}: ${n.text}` : n.text, title: "not proven" }),
  }),
  /** Architecture: a deploy marker (row 40); a marker never makes a link. */
  marker: entry<{ label: string; row: string | null }>({
    rows: { 40: { label: "Dockerfile backend/Dockerfile", row: "CD1" } },
    plain: (m) => ({ text: m.label, title: `deploy marker${m.row !== null ? `, row ${m.row}` : ""}; a marker never makes a link` }),
    technical: (m) => ({ text: `${m.label} (${m.row ?? "no row"})`, title: "deploy marker; a marker never makes a link" }),
  }),
  /** Architecture: the SDK stub of a component (row 41). */
  stub: entry<{ text: string; row: string }>({
    rows: { 41: { text: "imports 12 external packages", row: "CE6" } },
    plain: (s) => ({ text: s.text, title: `row ${s.row}: imports only, never a protocol` }),
    technical: (s) => ({ text: `${s.text} (${s.row})`, title: "imports only, never a protocol" }),
  }),
  /** Architecture chain (T026 AG-C2, rows 42-50). The header of a chain
   * drawn in place (row 42): its start, steps, members and folds. */
  chainSummary: entry<{ start: string; steps: number; members: number; folded: number; direction: string }>({
    rows: { 42: { start: "GET /api/v1/items/", steps: 7, members: 7, folded: 1, direction: "both" } },
    plain: (c) => ({ text: `Chain of ${c.start} · ${plural(c.steps, "step")}${c.folded > 0 ? ` · ${plural(c.folded, "member")} folded` : ""} · rest dimmed`, title: `${plural(c.members, "member")} ${DIRECTION_WORD[c.direction] ?? c.direction}; the inspector lists every member within the caps` }),
    technical: (c) => ({ text: `chain ${c.start} · direction ${c.direction} · ${String(c.steps)} steps · ${String(c.members)} members · map folds ${String(c.folded)} (AG16)`, title: "map budget: 3 steps each way, 5 per step, 40 in total" }),
  }),
  /** A followed edge of a chain step (row 43): the server's wording and
   * the edge's tier. */
  chainEdge: entry<{ wording: string; row: string; tier: string; dashed: boolean }>({
    rows: { 43: { wording: "called by client call", row: "CH3", tier: "inferred 0.9", dashed: false } },
    plain: (e) => ({ text: `${e.wording} · ${tierPlain(e.tier)}`, title: `${e.tier}, row ${e.row}${e.dashed ? ", structure, not a call" : ""}${tierNumber(e.tier) !== null ? `. ${RANK_NOTE}` : ""}` }),
    technical: (e) => ({ text: `${e.wording} · ${e.tier} (${e.row})`, title: `${tierPlain(e.tier)}${e.dashed ? "; structure, not a call" : ""}` }),
  }),
  /** A chain end line (row 44, CH11): what is not modelled past a member. */
  chainEnd: entry<{ text: string; row: string }>({
    rows: { 44: { text: "? not modelled: no stored caller; FastAPI dependency injection into 1 route(s) as declared in security_dependencies (GET /items in api/items.py); the injection is not a stored call", row: "CH11" } },
    plain: (e) => ({ text: e.text, title: `row ${e.row}: the chain ends here; this is not "no callers"` }),
    technical: (e) => ({ text: `${e.row}: ${e.text}`, title: 'the chain ends here; this is not "no callers"' }),
  }),
  /** A cap that bound a chain step (row 45, CH12). */
  chainBound: entry<{ text: string; cap: string }>({
    rows: { 45: { text: "+ 21 beyond step 6", cap: "depth" } },
    plain: (b) => ({ text: b.text, title: `the ${b.cap} cap stopped the walk here; the members beyond are not listed` }),
    technical: (b) => ({ text: `${b.text} (cap ${b.cap}, CH12)`, title: "caps: the chosen depth (at most 10), 2,000 members, fan-out 200 per member" }),
  }),
  /** A step's members the map folds (row 46). */
  chainFold: entry<{ count: number; number: number; hidden: number }>({
    rows: { 46: { count: 6, number: 2, hidden: 0 } },
    plain: (f) => ({ text: `+ ${plural(f.count, "more")} at step ${String(f.number)}${f.hidden > 0 ? ` (${String(f.hidden)} in hidden components)` : ""}`, title: "over the map budget; the inspector lists every member, the Flow tab draws them" }),
    technical: (f) => ({ text: `step ${String(f.number)}: ${String(f.count)} folded${f.hidden > 0 ? `, ${String(f.hidden)} hidden by Show` : ""} (map budget)`, title: "folded over the map budget (3 steps each way, 5 per step, 40 in total)" }),
  }),
  /** The Not proven group of compose services mapped to no component
   * (row 47, CD3). */
  deployGroup: entry<{ count: number }>({
    rows: { 47: { count: 10 } },
    plain: (g) => ({ text: `Deploy services not tied to a component (${String(g.count)})`, title: "compose services no component directory or Dockerfile maps to; each reason inside" }),
    technical: (g) => ({ text: `CD3 service_not_mapped ×${String(g.count)}`, title: "Deploy services not tied to a component" }),
  }),
  /** An AT2 ingress of a component (row 48): TLS as configured at its
   * router, never the link's TLS. */
  ingress: entry<{ text: string; row: string; basis: string | null }>({
    rows: { 48: { text: "ingress TLS at router backend-https as configured in compose.deploy.yml (service backend)", row: "AT2", basis: null } },
    plain: (g) => ({ text: g.text, title: `row ${g.row}: as configured, never the TLS of a link${g.basis !== null ? `; ${g.basis}` : ""}` }),
    technical: (g) => ({ text: `${g.text} (${g.row}, as_configured)`, title: "as configured, never the TLS of a link" }),
  }),
  /** A component's table-access sites bound to no table (row 49, CE3). */
  mayAlsoUse: entry<{ database: string; card: string; component: string | null; count: number; row: string }>({
    rows: { 49: { database: "database:models/sqlmodel/.", card: "SQLModel schema", component: "backend", count: 24, row: "CE3" } },
    plain: (m) => ({ text: `${m.component !== null ? `${m.component} ` : ""}may also use ${m.card}: ${plural(m.count, "code site")} not bound to a table`, title: `row ${m.row}; database card ${m.database}; never a link` }),
    technical: (m) => ({ text: `${m.row}: ${m.component !== null ? `${m.component} ` : ""}may also use ${m.database}: ${plural(m.count, "site")} not bound to a table`, title: `${m.card}; never a link` }),
  }),
  /** Where a Not proven entry of a hidden component comes from (row 50). */
  hiddenSource: entry<{ hiddenBy: string; name: string }>({
    rows: { 50: { hiddenBy: "docs", name: "docs" } },
    plain: (h) => ({ text: `in hidden ${HIDDEN_WORD[h.hiddenBy] ?? h.hiddenBy} component ${h.name}`, title: "its component is hidden by default; the Show toggles bring it back" }),
    technical: (h) => ({ text: `component ${h.name} (hidden_by ${h.hiddenBy}, ${HIDDEN_ROW[h.hiddenBy] ?? "CV4"})`, title: "hidden by default; the Show toggles bring it back" }),
  }),
  /** A chain step's members test scope kept out (row 51, CH10). */
  chainHidden: entry<{ count: number }>({
    rows: { 51: { count: 1 } },
    plain: (h) => ({ text: `${plural(h.count, "member")} hidden by test scope`, title: "not followed in the production scope; the Tests toggle follows them" }),
    technical: (h) => ({ text: `CH10 test scope ×${String(h.count)}`, title: "hidden by test scope, not followed" }),
  }),
  /** Edges of a kind the chain does not follow (row 52, CHD). */
  chainNotFollowed: entry<{ kind: string; count: number }>({
    rows: { 52: { kind: "contains", count: 4 } },
    plain: (n) => ({ text: `${kindWords(n.kind)} not followed ×${String(n.count)}`, title: `edge kind ${n.kind}: no chain row follows it (CHD)` }),
    technical: (n) => ({ text: `CHD ${n.kind} ×${String(n.count)}`, title: `${kindWords(n.kind)}: not followed` }),
  }),
  /** The Not proven group's note on database services left out (row 53,
   * AE2): their image is on a database card. */
  deployTied: entry<{ services: readonly string[]; followers: readonly string[]; cards: readonly string[] }>({
    rows: { 53: { services: ["db (compose.yml)"], followers: ["db (compose.override.yml)"], cards: ["SQLModel schema"] } },
    plain: (t) => ({
      text: `Not listed: ${t.services.join(", ")}, the database ${t.services.length === 1 ? "service" : "services"} whose image is on the ${t.cards.join(", ")} card${t.followers.length > 0 ? `; ${t.followers.join(", ")}, same-name ${t.followers.length === 1 ? "service" : "services"} without an image of ${t.followers.length === 1 ? "its" : "their"} own` : ""}`,
      title: "AE2 ties the image to that database card; same-name entries without an image of their own follow it",
    }),
    technical: (t) => ({ text: `AE2 tied, not listed: ${t.services.join(", ")} → ${t.cards.join(", ")}${t.followers.length > 0 ? `; no own image: ${t.followers.join(", ")}` : ""}`, title: "service_not_mapped entries whose database image is on a card, and same-name entries without an image" }),
  }),
  /** API Flow: a table card's access chip (row 54, T026 AG-C3 and AG-F)
   * from the lines drawn into it: reads and writes with their tiers ("fact"
   * or the stored confidence). detail lists the drawn lines' wording. */
  tableAccess: entry<{ reads: readonly string[]; writes: readonly string[]; builds?: readonly string[]; detail: string }>({
    rows: { 54: { reads: ["fact"], writes: ["0.9"], builds: [], detail: "reads (fact 1.0), may write (inferred 0.9)" } },
    plain: (a) => ({ text: accessPlain(a.reads, a.writes, a.builds ?? []), title: `${accessTechnical(a.reads, a.writes, a.builds ?? [])}; stored table access drawn here: ${a.detail}. ${RANK_NOTE}` }),
    technical: (a) => ({ text: accessTechnical(a.reads, a.writes, a.builds ?? []), title: `${accessPlain(a.reads, a.writes, a.builds ?? [])}; stored table access drawn here: ${a.detail}` }),
  }),
  /** Architecture: a database card accessor's counts (row 56):
   * a query builder's entry reads "builds a query on", as on the chain
   * (CH6) and the Flow tab. */
  dbAccess: entry<{ reads: number; builds: number; writes: number; mayAlsoUse: number }>({
    rows: { 56: { reads: 3, builds: 12, writes: 2, mayAlsoUse: 1 } },
    plain: (a) => ({
      text: [`reads ${String(a.reads)}`, ...(a.builds > 0 ? [`builds a query on ${String(a.builds)}`] : []), `writes ${String(a.writes)}`, ...(a.mayAlsoUse > 0 ? [`may also use ${String(a.mayAlsoUse)}`] : [])].join(" · "),
      title: "stored reads_table and writes_table edges; builds a query on: a query builder's entry (table_access role builds); may also use: unbound sites",
    }),
    technical: (a) => ({ text: `R ${String(a.reads)} · B ${String(a.builds)} · W ${String(a.writes)}${a.mayAlsoUse > 0 ? ` · ? ${String(a.mayAlsoUse)}` : ""}`, title: "reads, builds a query on, writes, may also use" }),
  }),
  /** Architecture: the overview's links line (row 57, TOP-2 of 2026-11-18
   * R1): counts with their singular or plural noun. */
  archLinksSummary: entry<{ links: number; components: number }>({
    rows: { 57: { links: 1, components: 1 } },
    plain: (a) => ({ text: `${String(a.links)} ${a.links === 1 ? "link" : "links"} between ${String(a.components)} ${a.components === 1 ? "component" : "components"}`, title: "component links of this repository's answer (rows CE)" }),
    technical: (a) => ({ text: `links ×${String(a.links)} · components ×${String(a.components)}`, title: `${String(a.links)} ${a.links === 1 ? "link" : "links"} between ${String(a.components)} ${a.components === 1 ? "component" : "components"}` }),
  }),
  /** Architecture: the overview's project block heading (row 58, TOP-2
   * R1 of 2026-11-18, serve --project). */
  archProjectHeading: entry<{ project: string }>({
    rows: { 58: { project: "shop" } },
    plain: (a) => ({ text: "Linked across repositories", title: `stored project links of project ${a.project} (row CE8)` }),
    technical: (a) => ({ text: "Project links (CE8)", title: `Linked across repositories in project ${a.project}` }),
  }),
  // T029 Explorer (rows 59-64): the provenance chip on every field of an
  // operation, a section's state, an operation's path marker, how a name
  // was read, a value set by you and the claim row that keeps an item out
  // of the document's standard fields.
  /** Explorer: the provenance chip of an item (row 59, rows SM-6..SM-10).
   * Plain names the kind in words; technical the stored kind, its
   * confidence and the extractor's rule. */
  shapeKind: entry<{ kind: string; rule: string; confidence: number }>({
    rows: { 59: { kind: "read_in_handler", rule: "SH-E3", confidence: 1 } },
    plain: (a) => {
      const word = SHAPE_KIND_WORD[a.kind];
      return word !== undefined
        ? { text: withNumber(word, a.confidence), title: `${a.kind} ${confidenceNumber(a.confidence)}, rule ${a.rule}: ${SHAPE_KIND_MEANING[a.kind] ?? ""}. ${SHAPE_NOTE}` }
        : { text: a.kind, title: `${a.kind} ${confidenceNumber(a.confidence)}, rule ${a.rule}: a kind this UI does not know` };
    },
    technical: (a) => ({ text: `${a.kind} ${confidenceNumber(a.confidence)} ${a.rule}`, title: `${SHAPE_KIND_WORD[a.kind] ?? a.kind}: ${SHAPE_KIND_MEANING[a.kind] ?? "a kind this UI does not know"}` }),
  }),
  /** Explorer: a section's state line (row 60, SM-2..SM-5, OG-41). Plain
   * never says "none" for a section that was not read. */
  sectionState: entry<{ state: string; section: string }>({
    rows: { 60: { state: "not_read", section: "query" } },
    plain: (a) => ({ text: SECTION_STATE_WORD[a.state] ?? a.state, title: `${a.section}: state ${a.state}` }),
    technical: (a) => ({ text: a.state, title: `${a.section}: ${SECTION_STATE_WORD[a.state] ?? a.state}` }),
  }),
  /** Explorer: where an operation's path comes from when it is not a
   * literal read from code (row 61, OG-43, OG-92). */
  resolution: entry<string>({
    rows: { 61: "settings_default" },
    plain: (r) => ({ text: RESOLUTION_WORD[r] ?? kindWords(r), title: `resolution ${r}` }),
    technical: (r) => ({ text: r, title: RESOLUTION_WORD[r] ?? kindWords(r) }),
  }),
  /** Explorer: how a parameter name was read (row 62, SM-12, SM-13, SM-16). */
  shapeAccess: entry<string>({
    rows: { 62: "read_by_dependency" },
    plain: (a) => ({ text: ACCESS_WORD[a] ?? kindWords(a), title: `access ${a}` }),
    technical: (a) => ({ text: a, title: ACCESS_WORD[a] ?? kindWords(a) }),
  }),
  /** Explorer: a value set by you and its use (row 63, OG-93). */
  userValueStatus: entry<string>({
    rows: { 63: "unused" },
    plain: (s) => ({ text: s === "used" ? "set by you, used" : s === "unused" ? "set by you, not used" : `set by you, ${s}`, title: `explorer.path_values status ${s}` }),
    technical: (s) => ({ text: `set_by_you ${s}`, title: s === "used" ? "set by you, used" : "set by you, not used" }),
  }),
  /** Explorer: the claim row that keeps an item out of the standard
   * fields (row 64, OG-73..OG-87). Plain hides the id; it stays in the
   * tooltip with the reason. */
  documentRow: entry<{ row: string; why: string }>({
    rows: { 64: { row: "OG-76", why: "a name read in code: its type and whether it must be sent are not read" } },
    plain: (a) => ({ text: "", title: `not a standard OpenAPI field (${a.row}): ${a.why}` }),
    technical: (a) => ({ text: a.row, title: `not a standard OpenAPI field: ${a.why}` }),
  }),
  // T029 Explorer, Try it out (rows 65-68, part B): a base URL
  // suggestion's source, where an environment's auth comes from, why a
  // request asks for a confirmation, and why a response was cut.
  /** Try it: a base URL suggestion's source (row 65, TI-31..TI-41). Plain
   * names the source in words; technical the stored kind and its row. */
  suggestionSource: entry<{ kind: string; row: string }>({
    rows: { 65: { kind: "compose_published_port", row: "TI-31" } },
    plain: (a) => ({ text: SUGGESTION_WORD[a.kind] ?? kindWords(a.kind), title: `${a.kind}, claim row ${a.row}` }),
    technical: (a) => ({ text: `${a.kind} ${a.row}`, title: SUGGESTION_WORD[a.kind] ?? kindWords(a.kind) }),
  }),
  /** Try it: where an environment's auth comes from (row 66, TI-11,
   * TI-12). Never a value: the variable's name and whether it is set, or
   * that a value was typed for the session. */
  tryAuth: entry<{ header: string | null; env: string | null; set: boolean; session: boolean; prefix: string | null }>({
    rows: { 66: { header: "Authorization", env: "API_TOKEN", set: true, session: false, prefix: "Bearer " } },
    plain: (a) => ({ text: tryAuthPlain(a), title: `${a.header ?? "no header"}${a.env !== null ? ` from the variable ${a.env}` : ""}; the value is never shown` }),
    technical: (a) => ({ text: a.session ? `${a.header ?? "Authorization"}: session value` : a.env === null ? "auth null" : `${a.header ?? ""}: env ${a.env} ${a.set ? "set" : "unset"}`, title: tryAuthPlain(a) }),
  }),
  /** Try it: why a request asks before it is sent (row 67, TI-9, TI-26,
   * TI-27). */
  confirmationReason: entry<string>({
    rows: { 67: "method_override" },
    plain: (r) => ({ text: REASON_WORD[r] ?? kindWords(r), title: `confirmation reason ${r}` }),
    technical: (r) => ({ text: r, title: REASON_WORD[r] ?? kindWords(r) }),
  }),
  /** Try it: why a response body was cut (row 68, TI-15, TI-44). */
  truncation: entry<{ reason: string; limit: string | null }>({
    rows: { 68: { reason: "size", limit: "response_bytes" } },
    plain: (a) => ({ text: TRUNCATION_WORD[a.reason] ?? `cut: ${a.reason}`, title: `truncated_reason ${a.reason}${a.limit !== null ? `, limit ${a.limit}` : ""}` }),
    technical: (a) => ({ text: `truncated ${a.reason}${a.limit !== null ? ` ${a.limit}` : ""}`, title: TRUNCATION_WORD[a.reason] ?? a.reason }),
  }),
  /** API Flow: the text on a line into a table (row 55): the server's
   * wording (or reads / may read / writes / may write), the stored kind and
   * tier in the technical style. */
  tableLine: entry<{ kind: string; provenance: string; confidence: number; wording: string | null }>({
    rows: { 55: { kind: "writes_table", provenance: "inferred", confidence: 0.9, wording: null } },
    plain: (a) => ({ text: withNumber(a.wording ?? accessVerb(a.kind, a.provenance === "fact"), a.confidence), title: `${a.kind} ${a.provenance} ${confidenceNumber(a.confidence)}. ${RANK_NOTE}` }),
    technical: (a) => ({ text: `${a.kind} ${a.provenance} ${confidenceNumber(a.confidence)}`, title: a.wording ?? accessVerb(a.kind, a.provenance === "fact") }),
  }),
  // T073 (rows 69-73, T071 questions 8-10): the quieter API list, route
  // header and callers list, and the Flow tab's legend strip.
  /** API list: the caller cell of a row with no linked caller (row 69).
   * full is the headline's sentence ("No linked caller in production",
   * neverCalledLabel); it stays in the tooltip in both styles. Technical
   * shows the API key the row is counted under. */
  rowNoCaller: entry<{ full: string; hint: string }>({
    rows: { 69: { full: "No linked caller in production", hint: "Graphvein found no call it could link; unresolved calls may still reach it." } },
    plain: (a) => ({ text: "0 linked", title: `${a.full}. ${a.hint}` }),
    technical: (a) => ({ text: "never_called", title: `${a.full}. ${a.hint}` }),
  }),
  /** API list: the group header's count of rows whose composed path rests
   * on an assumption, when the row icon is left out because at least half
   * the group has it (row 70). */
  groupAssumed: entry<{ count: number; of: number; noun: string }>({
    rows: { 70: { count: 266, of: 273, noun: "routes" } },
    plain: (a) => ({ text: `${String(a.count)} path assumed`, title: `Path assumption on ${String(a.count)} of ${String(a.of)} ${a.noun}: named on the route, and marked on the row under the pointer or selected (flag path_assumed)` }),
    technical: (a) => ({ text: `${String(a.count)} path_assumed`, title: `A composed path rests on an assumption on ${String(a.count)} of ${String(a.of)} ${a.noun}; marked on the row under the pointer or selected` }),
  }),
  /** Callers: the lead of an assumption every caller of the list carries
   * with the same text, said once above the table (row 71). */
  sharedAssumption: entry<{ count: number; code: string }>({
    rows: { 71: { count: 2, code: "path_assumed" } },
    plain: (a) => ({ text: `${a.count === 2 ? "Both callers rest" : `All ${String(a.count)} callers rest`} on one assumption.`, title: `Said once: every caller below carries this reason (${a.code}) with the same text` }),
    technical: (a) => ({ text: `${a.code} on ${String(a.count)} callers:`, title: `Every caller below rests on the same assumption, said once` }),
  }),
  /** Route header: the facts a Precise label does not cover, as a count
   * with the facts in the tooltip (row 72); technical lists them. */
  unverifiedCount: entry<readonly string[]>({
    rows: { 72: ["handler", "middleware", "conditional", "scope"] },
    plain: (f) => ({ text: `${String(f.length)} ${f.length === 1 ? "part" : "parts"} unverified`, title: `Unverified: ${f.join(", ")}. Outside the Precise scope (no oracle compares them): these facts stay unverified.` }),
    technical: (f) => ({ text: `unverified: ${f.join(", ")}`, title: "Outside the Precise scope (no oracle compares them): these facts stay unverified." }),
  }),
  /** Flow tab: the legend strip's count of the kinds the tab can draw
   * that have no line on this screen; they are in the full legend
   * (row 73). */
  legendHidden: entry<number>({
    rows: { 73: 5 },
    plain: (n) => ({ text: `+ ${String(n)} ${n === 1 ? "kind" : "kinds"} with no line here`, title: "Kinds this tab can draw with no line on this screen (their count is 0): open the Legend for every kind, its sample and the gap sheets" }),
    technical: (n) => ({ text: `+${String(n)} at 0`, title: `${String(n)} ${n === 1 ? "kind" : "kinds"} with no line here; every kind is in the Legend` }),
  }),
};

/** linkStatusPlain is row 28's plain text. */
function linkStatusPlain(p: { state: string; pass: number; links: number }): string {
  if (p.state === "none") return "no link pass yet";
  if (p.state === "stale") return `links stale · pass ${String(p.pass)}: re-index the project`;
  return `links ${p.state} · pass ${String(p.pass)} · ${String(p.links)} ${p.links === 1 ? "link" : "links"}`;
}

function linkStatusTitle(p: { state: string; stale_reasons: readonly string[] }, other: string): string {
  return p.state === "stale" && p.stale_reasons.length > 0 ? `${other}; stale: ${p.stale_reasons.join("; ")}` : other;
}

/** freshnessText words a member's freshness; anything but "fresh" warns. */
function freshnessText(f: { checkout: boolean; state: string; changed_count: number | null }): string {
  if (!f.checkout) return "no local checkout";
  switch (f.state) {
    case "fresh":
      return "fresh";
    case "stale":
      return f.changed_count === null ? "stale" : `stale: ${String(f.changed_count)} ${f.changed_count === 1 ? "file" : "files"} changed`;
    case "not_indexed":
      return "not indexed";
    default:
      return "freshness unknown";
  }
}

function freshnessStored(f: { checkout: boolean; state: string; changed_count: number | null }): string {
  if (!f.checkout) return "checkout none";
  return f.state === "stale" && f.changed_count !== null ? `stale ${String(f.changed_count)}` : f.state;
}

/** OP_WORD and FIELD_WORD say manifest operations and selector fields in
 * plain words (docs/cli/serve.md, "Project manifest endpoints"). */
const OP_WORD: Record<string, string> = {
  add_consume: "Add entry",
  remove_consume: "Remove",
  set_select: "Selector",
  set_default_scope: "Default scope only",
  add_scope: "Add scope",
  remove_scope: "Remove scope",
  rename_repository: "Rename",
  accept_suggestion: "Accept",
};
const FIELD_WORD: Record<string, string> = { bases: "base names", origins: "literal origins", path_prefixes: "path prefixes" };

/** OUTCOME_WORD says a project link outcome status in plain words
 * (docs/extract/project-links.md, claim contract). */
const OUTCOME_WORD: Record<string, string> = {
  linked: "Linked across repositories",
  in_repository: "Decided inside its repository",
  dynamic: "Path not known",
  unproven: "Base not stored",
  unmatched: "No matching route in the project",
  incomplete: "A candidate repository is not indexed",
  test_only: "Only test routes match",
  ambiguous: "Several possible routes",
  unknown: "Not understood",
};

/** accessVerb is a table line's plain verb: a fact reads or writes, an
 * inferred line may. */
function accessVerb(kind: string, fact: boolean): string {
  const verb = kind === "reads_table" ? "read" : "write";
  return fact ? `${verb}s` : `may ${verb}`;
}

/** accessPlain words a table's access chip with its numbers (T041):
 * "Reads and writes · 1.0", "Reads · 1.0, may write · 0.9", "May write ·
 * 0.9"; a kind drawn both as fact and inferred reads as the fact with both
 * numbers ("Reads · 1.0/0.9"). A tier is "fact" (1.0) or a stored
 * confidence as tableAccess writes it; one that is neither is kept raw. */
function accessPlain(reads: readonly string[], writes: readonly string[], builds: readonly string[] = []): string {
  const numbers = (tiers: readonly string[]) => {
    const n = tiers.map((t) => (t === "fact" ? 1 : Number(t)));
    return n.every(Number.isFinite) ? n.sort((a, b) => b - a).map(confidenceNumber).join("/") : tiers.join("/");
  };
  const part = (verb: string, tiers: readonly string[]) => (tiers.length === 0 ? "" : `${verb} · ${numbers(tiers)}`);
  const onlyFact = (tiers: readonly string[]) => tiers.length > 0 && tiers.every((t) => t === "fact");
  const both = onlyFact(reads) && onlyFact(writes) && builds.length === 0
    ? `reads and writes · ${confidenceNumber(1)}`
    : [
        part(accessVerb("reads_table", reads.includes("fact")), reads),
        // A query builder's entry (CH6 "builds a query on").
        part("builds a query on", builds),
        part(accessVerb("writes_table", writes.includes("fact")), writes),
      ]
        .filter((x) => x !== "")
        .join(", ");
  return both.charAt(0).toUpperCase() + both.slice(1);
}

/** accessTechnical is the chip as stored tiers: "R·W fact", "R fact · W
 * 0.9", "W 0.9". */
function accessTechnical(reads: readonly string[], writes: readonly string[], builds: readonly string[] = []): string {
  const r = reads.join("/");
  const w = writes.join("/");
  const rw = r !== "" && w !== "" ? (r === w ? `R·W ${r}` : `R ${r} · W ${w}`) : r !== "" ? `R ${r}` : w !== "" ? `W ${w}` : "";
  const b = builds.length > 0 ? `B ${builds.join("/")}` : "";
  return [b, rw].filter((x) => x !== "").join(" · ");
}

const DIRECTION_WORD: Record<string, string> = { both: "both ways", up: "upstream", down: "downstream" };
const HIDDEN_WORD: Record<string, string> = { tests: "test", docs: "docs", configured: "configured" };
const HIDDEN_ROW: Record<string, string> = { tests: "CV1", docs: "CV2", configured: "CV4" };

function plural(n: number, one: string): string {
  const many = one === "more" ? "more" : one.endsWith("s") ? one : `${one}s`;
  return `${n.toLocaleString("en-US")} ${n === 1 ? one : many}`;
}

/** ArchKind is a component kind with the framework of its routes. */
export interface ArchKind {
  kind: string;
  framework: string | null;
  row: string | null;
}

const KIND_WORD: Record<string, string> = {
  server: "Server",
  tauri_core: "Tauri core",
  client: "Client",
  schema_source: "Schema source",
  binary: "Binary",
  docs_site: "Docs site",
  package: "Package",
  unassigned_files: "Unassigned files",
  unknown: "Kind unknown",
};

const FRAMEWORK_WORD: Record<string, string> = { fastapi: "FastAPI", express: "Express", axum: "Axum", flask: "Flask", django: "Django", nest: "Nest", nestjs: "Nest", spring: "Spring", gin: "Gin", actix: "Actix", nextjs: "Next.js" };

function kindPlain(k: ArchKind): string {
  const word = KIND_WORD[k.kind] ?? kindWords(k.kind);
  if (k.kind === "server" && k.framework !== null) return `${FRAMEWORK_WORD[k.framework] ?? k.framework} server`;
  return word;
}

const FAMILY_WORD: Record<string, string> = { http: "HTTP", tauri_ipc: "Tauri IPC", sql: "SQL", in_process: "In-process", server_action: "Server action" };

const STATE_WORD: Record<string, string> = {
  proven: "Proven",
  as_written: "As written",
  documented: "Documented",
  as_configured: "As configured",
  not_applicable: "Not applicable",
  unknown: "Unknown",
  mixed: "Mixed",
};

const LEVEL_WORD: Record<number, string> = { 1: "level 1: the repository's components", 2: "level 2: a component", 3: "level 3: a file", 4: "level 4: a route, call, command or table" };

/** tierNumber is the confidence of a link tier as the server writes it:
 * 1 for "fact", the number of "inferred <confidence>"; null for any other
 * form. */
function tierNumber(t: string): number | null {
  if (t === "fact") return 1;
  if (!t.startsWith("inferred ")) return null;
  const n = Number(t.slice("inferred ".length));
  return t.length > "inferred ".length && Number.isFinite(n) ? n : null;
}

/** tierPlain words a link tier with its number (T041): "Fact · 1.0",
 * "Inferred · 0.9"; any other form stays as the server wrote it. */
function tierPlain(t: string): string {
  const n = tierNumber(t);
  if (n === null) return t;
  return withNumber(t === "fact" ? "Fact" : "Inferred", n);
}

/** HiddenParts are the counts of the Hidden note (0 leaves a part out). */
export interface HiddenParts {
  tests: number;
  docs: number;
  configured: number;
  libraries: number;
  testEdges: number;
  sdk: number;
}

function hiddenText(h: HiddenParts, ids: boolean): string {
  const parts: string[] = [];
  const n = (v: number) => v.toLocaleString("en-US");
  const tag = (row: string) => (ids ? ` (${row})` : "");
  if (h.tests > 0) parts.push(`tests ${n(h.tests)} ${h.tests === 1 ? "component" : "components"}${tag("CV1")}`);
  if (h.testEdges > 0) parts.push(`${n(h.testEdges)} test-scope ${h.testEdges === 1 ? "edge" : "edges"}${ids ? " (scope production)" : ""}`);
  if (h.docs > 0) parts.push(`docs ${n(h.docs)} ${h.docs === 1 ? "component" : "components"}${tag("CV2")}`);
  if (h.libraries > 0) parts.push(`libraries ${n(h.libraries)}${tag("CV3")}`);
  if (h.configured > 0) parts.push(`configured ${n(h.configured)}${tag("CV4")}`);
  if (h.sdk > 0) parts.push(`SDK stubs ${n(h.sdk)}${tag("CE6")}`);
  return parts.length === 0 ? "Nothing hidden" : `Hidden: ${parts.join(" · ")}`;
}

function statusList(by: Readonly<Record<string, number>>): string {
  const parts = Object.entries(by).map(([k, v]) => `${kindWords(k)} ${String(v)}`);
  return parts.length > 0 ? `by status: ${parts.join(", ")}` : "no status counts sent";
}

/** SHAPE_KIND_WORD says a shape item's kind (SM-6..SM-10) in words. */
const SHAPE_KIND_WORD: Record<string, string> = { fact: "Fact", read_in_handler: "Read in handler", intended: "Intended", returned: "Returned", mapped: "Mapped" };

/** SHAPE_KIND_MEANING is the one-line meaning of a kind, for tooltips. */
const SHAPE_KIND_MEANING: Record<string, string> = {
  fact: "a typed declaration the framework checks at runtime",
  read_in_handler: "a name read in the handler or a function it calls; its type and whether it must be sent are not read",
  intended: "a static type nothing checks at runtime",
  returned: "the declared type of a returned value, not validated by the framework",
  mapped: "from a committed spec or a typed client linked to the route, at that link's confidence",
};

/** SUGGESTION_WORD says a suggestion's source kind (TI-31..TI-41). */
const SUGGESTION_WORD: Record<string, string> = {
  compose_published_port: "Compose published port",
  traefik_router: "Traefik router",
  openapi_server: "Committed spec server",
  frontend_env: "Another member's address",
  listen_call: "listen() call",
  uvicorn_run: "uvicorn.run port",
  go_listen: "Go listen address",
  package_json_script: "package.json script port",
  procfile: "Procfile port",
  dockerfile_expose: "Dockerfile EXPOSE",
  framework_default: "Framework default, not read from your code",
};

function tryAuthPlain(a: { header: string | null; env: string | null; set: boolean; session: boolean; prefix: string | null }): string {
  if (a.session) return `${a.header ?? "Authorization"}: a value typed for this session`;
  if (a.env === null) return "none configured";
  const prefix = (a.prefix ?? "").trim();
  return `${prefix !== "" ? `${prefix} ` : ""}from $${a.env} · ${a.set ? "set" : "not set"}`;
}

/** REASON_WORD says a confirmation reason (TI-9, TI-26, TI-27). */
const REASON_WORD: Record<string, string> = {
  method: "the method may change data",
  method_override: "a method override turns this GET or HEAD into another method",
  body_on_get_or_head: "a GET or HEAD with a body is treated as changing data",
  free_form_path: "a free-form path does not match the operation",
};

/** TRUNCATION_WORD says why a response body was cut (TI-15). */
const TRUNCATION_WORD: Record<string, string> = {
  size: "cut at the response size limit",
  timeout: "cut by the time limit after the status arrived",
  read_error: "cut by a read error",
};

/** SECTION_STATE_WORD says a section state (SM-2..SM-5) in words. */
const SECTION_STATE_WORD: Record<string, string> = {
  not_read: "Not read by Graphvein",
  provably_none: "None, proven",
  // A proven-empty header, query or cookie section of an operation whose
  // security scheme reads (or may read) there: the scheme's credential is
  // not "none" (T029 part B).
  none_besides_scheme: "None besides the security scheme",
  partial: "Partial: more may exist",
  complete: "Complete",
};

const RESOLUTION_WORD: Record<string, string> = {
  literal: "path as written",
  constant: "path from a constant",
  settings_default: "path from a settings default",
  set_by_you: "path set by you",
};

const ACCESS_WORD: Record<string, string> = { declared: "declared", read_by_handler: "read by the handler", read_by_dependency: "read by a dependency" };

/** notExtractedText words an impact unknown of `not_extracted`, with the
 * rule id after the subject when withId is set. */
function notExtractedText(relation: string, names: readonly string[], withId: boolean): string {
  const id = withId ? ` (${relation})` : "";
  if (relation === "I11") return `Architecture components${id}: not extracted yet, so no component edge is traversed.`;
  if (relation === "I8") {
    const what = names.length === 0 ? "these tables" : names.join(", ");
    return `Code using ${what}${id}: not extracted for this source; this is not “no users”.`;
  }
  return `${relation}: not extracted${names.length > 0 ? ` for ${names.join(", ")}` : ""}.`;
}

function restsTitle(all: readonly Assumption[]): string {
  return `rests on more than its table: ${all.map((a) => (a.title !== undefined ? `${a.id} (${a.title})` : a.id)).join(", ")}`;
}

type Labels = typeof LABELS;

/** LabelKey names a table entry. */
export type LabelKey = keyof Labels;

/** InputOf is the input type of an entry. */
export type InputOf<K extends LabelKey> = Labels[K] extends Entry<infer A> ? A : never;

/** label renders entry key for input in a style. This is the one way the
 * UI reads a T025 string. */
export function label<K extends LabelKey>(style: LabelStyle, key: K, input: InputOf<K>): Label {
  const e = LABELS[key] as unknown as Entry<InputOf<K>>;
  return style === "technical" ? e.technical(input) : e.plain(input);
}

/** edgeLabel is label(style, "edge", e) for any stored edge; it returns
 * the stored fields for a form without a checked word. */
export function edgeLabel(style: LabelStyle, e: EdgeFields): Label {
  return label(style, "edge", { kind: e.kind, provenance: e.provenance, confidence: e.confidence, match: e.match });
}

/** RowSample is one T025 row rendered in both styles from its sample. */
export interface RowSample {
  row: number;
  key: LabelKey;
  same: boolean;
  plain: Label;
  technical: Label;
}

/** rowSamples renders every row of every entry in both styles, for the
 * table test and the docs. */
export function rowSamples(): RowSample[] {
  const out: RowSample[] = [];
  for (const key of Object.keys(LABELS) as LabelKey[]) {
    const e = LABELS[key] as unknown as Entry<unknown>;
    for (const [row, input] of Object.entries(e.rows)) {
      const n = Number(row);
      out.push({ row: n, key, same: e.same?.includes(n) === true, plain: e.plain(input), technical: e.technical(input) });
    }
  }
  return out.sort((a, b) => a.row - b.row);
}
