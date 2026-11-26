// The run switch notice (T016j decision D4): when a status answer names a
// newer index run than the previous answer this tab saw, the data under the
// user's eyes changes, so the page says so once, until dismissed. Both times
// come from status answers; nothing is assumed from a click or a job.

import { Info, X } from "lucide-react";
import { useState } from "react";

import type { StatusView } from "@/hooks/useStatus";
import { runOf } from "@/hooks/useSchemaSummary";
import { formatShortTime, formatTime } from "@/lib/time";
import { compareRuns } from "@/sections/api/ApiSection";

export interface RunSwitch {
  from: string;
  to: string;
}

/** useRunSwitchNotice reports the latest switch to a newer run seen in
 * status answers; the first answer is not a switch. */
export function useRunSwitchNotice(view: StatusView): { notice: RunSwitch | null; dismiss: () => void } {
  const current = view.kind === "ok" ? runOf(view.status.index.last_run) : null;
  const [seen, setSeen] = useState<string | null>(current);
  const [notice, setNotice] = useState<RunSwitch | null>(null);
  if (current !== null && current !== seen) {
    // Adjusting state while rendering (React's documented pattern for
    // "information from previous renders").
    setSeen(current);
    if (seen !== null && compareRuns(current, seen) > 0) setNotice({ from: seen, to: current });
  }
  return {
    notice,
    dismiss: () => {
      setNotice(null);
    },
  };
}

/** times formats both runs without seconds, unless that makes them look
 * the same (two runs in one minute). */
function times(n: RunSwitch): { from: string; to: string } {
  const fmt = (r: string, f: (v: string) => string) => (r === "none" ? "none" : f(r));
  const short = { from: fmt(n.from, formatShortTime), to: fmt(n.to, formatShortTime) };
  return short.from === short.to ? { from: fmt(n.from, formatTime), to: fmt(n.to, formatTime) } : short;
}

/** SwitchNotice is the dismissible note above the content. */
export function SwitchNotice({ notice, onDismiss }: { notice: RunSwitch; onDismiss: () => void }) {
  return (
    <div
      role="status"
      data-testid="switch-notice"
      className="flex items-center gap-3 rounded-lg border border-brand/40 bg-brand-soft px-3 py-2 text-[13px]"
    >
      <Info aria-hidden="true" className="size-4 flex-none text-brand-text" />
      <p className="min-w-0 flex-1">
        Now showing the index of {times(notice).to} (was {notice.from === "none" ? "not indexed" : times(notice).from}).
      </p>
      <button
        type="button"
        onClick={onDismiss}
        className="inline-flex h-7 items-center gap-1 rounded-md px-2 font-medium hover:bg-background/60"
      >
        <X aria-hidden="true" className="size-3.5" />
        Dismiss
      </button>
    </div>
  );
}
