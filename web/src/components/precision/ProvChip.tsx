// The one provenance chip of the web UI (T041, ADR 0078 Revision 1): every
// visible label that says how sure a stored fact or link is ("Fact · 1.0",
// "Inferred · 0.9", "Exact path match · 0.9", a link tier, a shape item's
// kind) renders through TierChip, so a provenance reads the same in every
// section: a fact on a solid green tint, an inferred link in a dashed amber
// border, anything else on a neutral tint. The look is the Changes
// section's provenance tag (T025 row 10), which became the standard.
//
// The components below take the label table's inputs, not finished
// strings: they call the table themselves, so a component cannot print a
// provenance label as bare text (provenanceChipSource.test.ts enforces it).
// Tooltips are the table's titles, unchanged.

import type { ReactNode } from "react";

import { useLabelStyle } from "@/hooks/useLabels";
import { edgeLabel, factLabel, label, type EdgeFields, type Label } from "@/lib/labels";
import { cn } from "@/lib/utils";

/** ProvTier is what the chip's look says: read as a fact, inferred (not a
 * fact), or neither of the two (a kind the chip has no tier for). */
export type ProvTier = "fact" | "inferred" | "other";

/** provTier maps a stored provenance to the chip's look; any provenance
 * this UI does not know is "other", never a fact. */
export function provTier(provenance: string): ProvTier {
  return provenance === "fact" ? "fact" : provenance === "inferred" ? "inferred" : "other";
}

/** linkTierOf maps a link tier as the server writes it ("fact",
 * "inferred 0.9") to the chip's look. */
export function linkTierOf(tier: string): ProvTier {
  if (tier === "fact") return "fact";
  return tier === "inferred" || tier.startsWith("inferred ") ? "inferred" : "other";
}

/** shapeTier maps a shape item's kind (claim rows SM-6..SM-10) to the
 * chip's look: only kind fact is a fact; a mapped item takes an inferred
 * link's confidence (SM-10), so it reads inferred; a name read in the
 * handler, an intended or a returned type is neither, so it reads neutral
 * rather than claim the fact look. */
export function shapeTier(kind: string): ProvTier {
  return kind === "fact" ? "fact" : kind === "mapped" ? "inferred" : "other";
}

const TONE: Record<ProvTier, string> = {
  fact: "bg-linked-soft text-linked",
  inferred: "border border-dashed border-dynamic-bar text-dynamic",
  other: "bg-muted text-muted-foreground",
};

/** TierChipProps: size md is the standard (18 px, a row's own chip); sm
 * (16 px) fits inside another 20 px chip or a line of 12 px text without
 * making it taller. */
export interface TierChipProps {
  tier: ProvTier;
  label: Label;
  size?: "md" | "sm" | undefined;
  testId?: string;
  className?: string | undefined;
  /** Extra data attributes for tests and screenshots. */
  data?: Readonly<Record<`data-${string}`, string | number | undefined>>;
}

/** TierChip is the chip's one look. Components normally use the keyed
 * chips below; TierChip itself is for those and for tests. */
export function TierChip({ tier, label: l, size = "md", testId = "prov-chip", className, data }: TierChipProps) {
  return (
    <span
      className={cn(
        "inline-flex flex-none items-center rounded align-middle leading-none font-medium whitespace-nowrap",
        size === "md" ? "h-[18px] px-1.5 text-[11px]" : "h-4 px-1 text-[10.5px]",
        TONE[tier],
        className,
      )}
      title={l.title === "" ? undefined : l.title}
      aria-label={l.aria}
      data-testid={testId}
      data-tier={tier}
      {...data}
    >
      {l.text}
    </span>
  );
}

interface Common {
  size?: "md" | "sm" | undefined;
  testId?: string;
  className?: string | undefined;
}

/** ProvChip is a provenance with its confidence (the weakest step of an
 * impact path, a changed item; T025 row 10). No number where the API
 * gives none (a move hint, a path inferred only by its rule). */
export function ProvChip({ provenance, confidence, size, testId = "prov", className }: Common & { provenance: string; confidence: number | null }) {
  const l = label(useLabelStyle(), "provenance", { provenance, confidence });
  return <TierChip tier={provTier(provenance)} label={l} size={size} testId={testId} className={className} data={{ "data-confidence": confidence ?? undefined }} />;
}

/** EdgeChip is a stored edge's provenance in the label table's words
 * (rows 1-4: Callers, the endpoint's Link, an OpenAPI operation). */
export function EdgeChip({ edge, size, testId = "edge-chip", className }: Common & { edge: EdgeFields }) {
  return <TierChip tier={provTier(edge.provenance)} label={edgeLabel(useLabelStyle(), edge)} size={size} testId={testId} className={className} />;
}

/** FactChip is "Fact · 1.0" where the API sends no number beside a fact
 * (an endpoint header, the route side of a request difference). */
export function FactChip({ size, testId = "fact-badge", className }: Common) {
  return <TierChip tier="fact" label={factLabel(useLabelStyle())} size={size} testId={testId} className={className} />;
}

/** ProjectLinkChip is a cross-repository link's provenance (row 22). */
export function ProjectLinkChip({ link, size, testId = "project-link-label", className }: Common & { link: { kind: string; provenance: string; confidence: number } }) {
  const l = label(useLabelStyle(), "projectLink", { kind: link.kind, provenance: link.provenance, confidence: link.confidence });
  return <TierChip tier={provTier(link.provenance)} label={l} size={size} testId={testId} className={className} />;
}

/** useProjectLinkText is ProjectLinkChip's visible text, for a report
 * that quotes the label the reader saw (T040's wrong-fact flag). */
export function useProjectLinkText(link: { kind: string; provenance: string; confidence: number }): string {
  return label(useLabelStyle(), "projectLink", { kind: link.kind, provenance: link.provenance, confidence: link.confidence }).text;
}

/** LinkTierChip is an architecture link tier as the server writes it
 * (row 35). */
export function LinkTierChip({ tier, size, testId = "link-tier", className }: Common & { tier: string }) {
  return <TierChip tier={linkTierOf(tier)} label={label(useLabelStyle(), "linkTier", tier)} size={size} testId={testId} className={className} />;
}

/** ShapeKindChip is a shape item's kind with its confidence (row 59). */
export function ShapeKindChip({ p, size, testId = "shape-kind", className }: Common & { p: { kind: string; rule: string; confidence: number } }) {
  const l = label(useLabelStyle(), "shapeKind", { kind: p.kind, rule: p.rule, confidence: p.confidence });
  // The tooltip belongs to the chip that holds this one (it adds file,
  // line and chain), so the inner chip carries none of its own.
  return <TierChip tier={shapeTier(p.kind)} label={{ ...l, title: "" }} size={size} testId={testId} className={className} />;
}

/** useShapeKindTitle is ShapeKindChip's tooltip, for the chip around it. */
export function useShapeKindTitle(p: { kind: string; rule: string; confidence: number }): string {
  return label(useLabelStyle(), "shapeKind", { kind: p.kind, rule: p.rule, confidence: p.confidence }).title;
}

/** ChainEdgeLabel is a followed chain edge (row 43): the server's wording,
 * then its tier as a chip, the row id after it in the technical style.
 * The tooltip is the table's. */
export function ChainEdgeLabel({ edge, after, className }: { edge: { wording: string; row: string; tier: string; dashed: boolean }; after?: ReactNode; className?: string }) {
  const style = useLabelStyle();
  const l = label(style, "chainEdge", edge);
  return (
    <span title={l.title} className={cn("inline-flex flex-wrap items-center gap-1", className)} data-testid="chain-edge">
      <span className={cn(edge.dashed && "italic")}>{edge.wording}</span>
      <TierChip tier={linkTierOf(edge.tier)} label={{ ...label(style, "linkTier", edge.tier), title: "" }} size="sm" testId="link-tier" />
      {style === "technical" && <span>({edge.row})</span>}
      {after}
    </span>
  );
}

/** GuideExampleChip is a label of the Help page's examples (T045): the
 * guide's fixed illustration and the "In your index:" line, worded as the
 * guide documents the plain style ("Fact · 1.0", "Inferred · 0.9",
 * "Unknown"). The words come from the guide and the example picker, not
 * from a stored fact, so this chip takes them as given; the look is the
 * one every provenance label has. Unknown is neither tier. */
export function GuideExampleChip({ kind, text, testId = "help-example-chip" }: { kind: "fact" | "inferred" | "unknown"; text: string; testId?: string }) {
  return <TierChip tier={kind === "unknown" ? "other" : kind} label={{ text, title: "" }} testId={testId} />;
}
