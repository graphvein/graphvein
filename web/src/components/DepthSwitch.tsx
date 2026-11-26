// The depth switch of a walk (T042): a radio group of the depths 1 to 10,
// shared by the Impact tab and the architecture chain so both read and
// work the same. The label's tooltip names where the applied depth comes
// from (this link, this browser, the repository, built-in).

import { useId } from "react";

import { DEPTHS } from "@/lib/depthChoice";
import { cn } from "@/lib/utils";

export interface DepthSwitchProps {
  /** The applied depth; null while it is not known yet (nothing checked). */
  depth: number | null;
  onDepth: (depth: number) => void;
  /** Where the applied depth comes from, as a sentence. */
  title: string;
  testId?: string;
}

/** DepthSwitch renders "Depth" and the choices 1 to 10. */
export function DepthSwitch({ depth, onDepth, title, testId }: DepthSwitchProps) {
  const labelId = useId();
  return (
    <span className="inline-flex flex-wrap items-center gap-2" data-testid={testId}>
      <span className="cursor-help text-muted-foreground" id={labelId} title={title}>
        Depth
      </span>
      <span role="radiogroup" aria-labelledby={labelId} aria-description={title} className="inline-flex rounded-md bg-muted p-0.5">
        {DEPTHS.map((d) => (
          <button
            key={d}
            type="button"
            role="radio"
            aria-checked={d === depth}
            className={cn("h-6 min-w-6 rounded px-1.5 text-[12px]", d === depth ? "bg-background font-semibold shadow-xs" : "text-muted-foreground hover:text-foreground")}
            onClick={() => {
              onDepth(d);
            }}
          >
            {d}
          </button>
        ))}
      </span>
    </span>
  );
}
