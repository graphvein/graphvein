// The "Rests on" lines (T018f R-2, 2.1): on the group in the set strip
// ("Facts in <group> rest on", the union, O2) and on the table in the
// detail header ("Rests on", the intersection: true of every fact). Each id
// is a chip that opens its popover; "What these mean" opens the
// Assumptions tab. A line the index did not record (pre-bump) says so and
// shows no chip, never an empty line.

import { useSchemaWhy } from "@/sections/schema/catalogue";
import { AssumptionChip } from "@/sections/schema/WhyPopover";

/** RestsOn renders one line; group names the group for its wording, or
 * is undefined for the table line. */
export function RestsOn({ ids, source, group, testId }: { ids: string[] | null; source: string; group?: string; testId: string }) {
  const { openEntry } = useSchemaWhy();
  const label = group === undefined ? "Rests on" : (
    <>
      Facts in <b className="font-mono font-semibold text-foreground">{group}</b> rest on
    </>
  );
  if (ids === null)
    return (
      <span className="text-[12.5px] text-muted-foreground italic" data-testid={testId} data-state="not_recorded">
        {group === undefined ? "Rests on: " : <>{label}: </>}row not recorded for this index; re-index.
      </span>
    );
  return (
    <span className="inline-flex flex-wrap items-center gap-1 text-[12.5px] text-muted-foreground" data-testid={testId} data-state="ids">
      {label}
      {ids.length === 0 ? (
        <span className="text-foreground">none of the catalogue&apos;s assumptions</span>
      ) : (
        ids.map((id) => <AssumptionChip key={id} id={id} source={source} />)
      )}
      <button type="button" className="ml-1 text-brand-text underline underline-offset-[3px]" onClick={() => { openEntry(null); }} data-testid="what-these-mean">
        What these mean
      </button>
    </span>
  );
}
