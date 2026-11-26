// This browser's depth choice for one walk, as the Settings page shows it
// (T042 keys ui.impact_depth / ui.chain_depth in the T039 form): "Not set"
// (the repository default applies, else the built-in depth) or a depth 1
// to 10. It writes the same stored key as the Impact tab's and the
// chain's depth switches, so a choice here applies at once.

import { useStoredDepth } from "@/hooks/useDepth";
import { BUILT_IN_DEPTH, DEPTHS, clearDepth, writeDepth, type DepthKind } from "@/lib/depthChoice";

/** DepthSetting is a select of "Not set" and the depths 1 to 10 whose
 * value is the stored choice. */
export function DepthSetting({ kind, labelledBy }: { kind: DepthKind; labelledBy?: string }) {
  const [stored, setStored] = useStoredDepth(kind);
  return (
    <div className="grid gap-1" data-testid={`depth-setting-${kind}`}>
      <select
        aria-labelledby={labelledBy}
        aria-label={labelledBy === undefined ? `${kind === "impact" ? "Impact" : "Chain"} depth in this browser` : undefined}
        className="h-7 w-fit rounded-md border border-input bg-background px-2 text-[12.5px] outline-none focus-visible:ring-2 focus-visible:ring-ring"
        value={stored.depth === null ? "" : String(stored.depth)}
        onChange={(e) => {
          const value = e.target.value;
          if (value === "") {
            setStored({ depth: null, saved: clearDepth(kind) });
            return;
          }
          const depth = Number(value);
          setStored({ depth, saved: writeDepth(kind, depth) });
        }}
      >
        <option value="">Not set</option>
        {DEPTHS.map((d) => (
          <option key={d} value={String(d)}>
            {d}
            {d === BUILT_IN_DEPTH[kind] ? " (built-in)" : ""}
          </option>
        ))}
      </select>
      {!stored.saved && stored.depth !== null && <div className="text-[11px] text-stale">Not saved: lasts until the page reloads</div>}
    </div>
  );
}
