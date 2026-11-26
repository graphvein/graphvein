// The row chip (T018f R-3, 2.1): on a grid row or an index row, the
// assumptions its claims' rows add to its table's "Rests on" line (the set
// difference). Nothing when there are none; "Row not recorded for this
// index; re-index" on a pre-bump index (never an empty chip); nothing
// while the catalogue loads or after it failed (the detail header says so
// once). A claim row the catalogue lacks is shown raw.

import type { Claim } from "@/api/schema";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { rowChip, rowsOfClaims, useSchemaWhy } from "@/sections/schema/catalogue";
import { WhyButton, type WhySource } from "@/sections/schema/WhyPopover";

/** claimSources lists where each claim's statement is (the IDE link from
 * /table, else the file from migration_ref, else a model claim's file,
 * else the migration id). */
export function claimSources(claims: readonly Claim[]): WhySource[] {
  const seen = new Set<string>();
  const out: WhySource[] = [];
  for (const c of claims) {
    const ref = c.migration_ref;
    const file = ref !== null && typeof ref.file === "string" ? ref.file : (c.file ?? c.migration ?? "migration not recorded");
    const line = c.span === null ? "" : `:${String(c.span.start_line)}`;
    const label = `${file}${line}`;
    if (seen.has(label)) continue;
    seen.add(label);
    out.push({ label, link: c.link, migration: c.link === null ? c.migration : null });
  }
  return out;
}

export function RowChip({ claims, source, restsOn, name }: { claims: readonly Claim[] | null; source: string; restsOn: readonly string[] | null; name: string }) {
  const { catalogue } = useSchemaWhy();
  const chip = rowChip(claims, source, restsOn, catalogue);
  const style = useLabelStyle();
  if (chip.kind === "not_recorded")
    return (
      <span className="ml-1 text-[11px] text-muted-foreground italic" data-testid="row-chip-not-recorded" title="The claims of this record were not stored by the index that built it">
        row not recorded; re-index
      </span>
    );
  if (chip.kind === "unavailable" || (chip.ids.length === 0 && chip.unknownRows.length === 0)) return null;
  const rows = claims === null ? [] : rowsOfClaims(claims);
  const all = [...chip.ids, ...chip.unknownRows];
  // T025 row 18 (lib/labels.ts): a count in the plain style, the ids in
  // the technical style; ids and titles in the tooltip either way.
  const count = label(
    style,
    "assumptionCount",
    all.map((id) => ({ id, title: catalogue.kind === "ok" ? catalogue.byId.get(id)?.title : undefined })),
  );
  return (
    <WhyButton
      testId="row-chip"
      label={`${name} rests on more: ${all.join(", ")}`}
      title={count.title}
      dataId={all.join(" ")}
      className="ml-1 inline-flex h-[18px] items-center gap-0.5 rounded-[4px] border border-dashed border-dynamic/70 bg-dynamic-soft px-1 font-mono text-[11px] font-semibold text-dynamic hover:brightness-95"
      why={() => ({
        kind: "row",
        title: `${name} rests on more than its table`,
        source,
        lead: (
          <>
            Its facts were set by {rows.length === 1 ? "a row" : "rows"} that also {rows.length === 1 ? "rests" : "rest"} on {chip.ids.join(", ") || "assumptions not in this build's catalogue"}
            {chip.unknownRows.length > 0 && <> (rows not in this build&apos;s catalogue: {chip.unknownRows.join(", ")})</>}.
          </>
        ),
        rows,
        assumptions: { heading: "It also rests on", ids: chip.ids },
        sources: claims === null ? [] : claimSources(claims),
        entry: chip.ids[0] ?? null,
      })}
    >
      {count.text}
    </WhyButton>
  );
}

