// The Flow tab's controls in the detail pane's tab row (as the PNG): the
// hop setting and the near-miss toggle. Outside the lazy chunk, so they
// show while the graph loads; both only filter the loaded answer.

import { cn } from "@/lib/utils";
import type { Hops } from "@/sections/api/flow/graph";

export interface FlowControlsProps {
  hops: Hops;
  nearMisses: boolean;
  onHops: (hops: Hops) => void;
  onNearMisses: (on: boolean) => void;
}

/** FlowControls renders the segmented hop control and the checkbox. */
export function FlowControls({ hops, nearMisses, onHops, onNearMisses }: FlowControlsProps) {
  const seg = "h-7 px-2.5 text-[12.5px] font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring";
  return (
    <div className="flex items-center gap-3" data-testid="flow-controls">
      <div className="inline-flex overflow-hidden rounded-md border" role="group" aria-label="Hops each way">
        {([1, 2] as const).map((h) => (
          <button
            key={h}
            type="button"
            aria-pressed={hops === h}
            aria-keyshortcuts={String(h)}
            title={`${String(h)} ${h === 1 ? "hop" : "hops"} each way (${String(h)})`}
            className={cn(seg, h === 2 && "border-l", hops === h ? "bg-brand-soft text-brand-text" : "bg-card hover:bg-accent")}
            onClick={() => {
              onHops(h);
            }}
          >
            {h === 1 ? "1 hop" : "2 hops"}
          </button>
        ))}
      </div>
      <label className="inline-flex cursor-pointer items-center gap-1.5 text-[13px]">
        <input
          type="checkbox"
          className="size-4 accent-[hsl(var(--brand))]"
          checked={nearMisses}
          onChange={(e) => {
            onNearMisses(e.target.checked);
          }}
        />
        Near misses
      </label>
    </div>
  );
}
