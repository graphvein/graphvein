// Edge tiers of the Flow tab (T016f D3): how a drawn line looks, decided
// from the stored fields only. The order of the checks is the table's: a
// tier is never guessed from one field alone, so an `inferred` edge with
// confidence 1 is not a fact, and a `fact` edge of a kind this table does
// not name is "other" (neutral, with its raw fields as the label).

import type { LabelStyle } from "@/lib/labelChoice";
import { confidenceNumber, label, type Label } from "@/lib/labels";

/** The fields of a stored edge that decide its tier. */
export interface TierFields {
  kind: string;
  provenance: string;
  confidence: number;
  match: string | null;
  /** The server's wording of a table edge (T026 AG-C3); absent or null
   * otherwise. */
  wording?: string | null;
}

export type Tier = "fact" | "exact" | "suffix" | "call" | "access" | "contains" | "other" | "candidate";

/** Tiers in legend order. */
export const TIERS: readonly Tier[] = ["fact", "exact", "suffix", "call", "access", "contains", "candidate", "other"];

/** The tiers the confidence filter can switch off. "defined in" is
 * structure, not confidence, so it has no chip. */
export type FilterTier = Exclude<Tier, "contains">;

export const FILTER_TIERS: readonly FilterTier[] = ["fact", "exact", "suffix", "call", "access", "candidate", "other"];

/** TABLE_KINDS are the stored table-access edge kinds (chain row CH6). */
export const TABLE_KINDS = ["reads_table", "writes_table"];

const FACT_KINDS = ["exposes_route", "exposes_command", "invokes_command"];

/** edgeTier is D3's table, in its order. */
export function edgeTier(e: TierFields): Tier {
  if (FACT_KINDS.includes(e.kind) && e.provenance === "fact" && e.confidence === 1) return "fact";
  if (e.kind === "calls_route" && e.provenance === "inferred" && e.match === "exact") return "exact";
  if (e.kind === "calls_route" && e.provenance === "inferred" && e.match === "suffix") return "suffix";
  if (e.kind === "calls" && e.provenance === "fact") return "call";
  // T026 AG-C3: a table read or write, fact or inferred; the label says
  // which ("reads", "may write").
  if (TABLE_KINDS.includes(e.kind)) return "access";
  if (e.kind === "contains") return "contains";
  return "other";
}

/** tierLabel is the text drawn on an edge of a tier, or null for none (a
 * plain in-code call). The known tiers come from the label table (T025,
 * T025b): plain words by default, the tier with its stored confidence in
 * the technical style; the stored confidence is also in the line's facts
 * (edgeFacts) and the legend. An edge of no known tier keeps its raw
 * fields. */
export function tierLabel(e: TierFields, tier: Tier = edgeTier(e), style: LabelStyle = "plain"): string | null {
  return tierText(e, tier, style)?.text ?? null;
}

/** tierText is tierLabel with its tooltip (the other style's text), or
 * null for no label. */
export function tierText(e: TierFields, tier: Tier = edgeTier(e), style: LabelStyle = "plain"): Label | null {
  const c = confidenceNumber(e.confidence);
  switch (tier) {
    case "fact":
    case "exact":
    case "suffix":
      return label(style, "flowTier", { tier, confidence: e.confidence });
    case "call":
      return null;
    case "access":
      return label(style, "tableLine", { kind: e.kind, provenance: e.provenance, confidence: e.confidence, wording: e.wording ?? null });
    case "contains":
      return { text: "defined in", title: "" };
    default:
      return { text: [e.kind, e.provenance, c, e.match].filter((s) => s !== null && s !== "").join(" "), title: "" };
  }
}

const RELATION_WORD: Record<string, string> = {
  ambiguous_candidate: "ambiguous",
  test_only_candidate: "test-only",
};

/** candidateLabel names a near miss's recorded candidate: "ambiguous · 1
 * of 2", "test-only · 1 of at least 10" when the count is a lower bound.
 * An unknown relation keeps its raw name. */
export function candidateLabel(relation: string, count: number, exact: boolean): string {
  const word = Object.hasOwn(RELATION_WORD, relation) ? (RELATION_WORD[relation] ?? relation) : relation;
  return `${word} · 1 of ${exact ? "" : "at least "}${String(count)}`;
}

export const TIER_LABEL: Record<Tier, string> = {
  fact: "Fact",
  exact: "Exact",
  suffix: "Suffix",
  call: "Call",
  access: "Reads / writes",
  contains: "Defined in",
  candidate: "Near miss",
  other: "Other",
};
