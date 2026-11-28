// The label style controls (T025b): the browser's "Label style" choice,
// shown in Settings since T039 (it was in the sidebar footer; the stored
// key graphvein.labels.v1 is unchanged), and the one-time question, a card
// on the Overview since T064 (it was a banner above every view). It never
// blocks the page: it stays until a style is chosen or it is closed (×,
// which keeps the current style, plain unless graphvein.yaml says
// otherwise), once per browser.

import { X } from "lucide-react";

import { useLabels, type LabelState } from "@/hooks/useLabels";
import type { LabelStyle } from "@/lib/labelChoice";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";

/** STYLES are the two options in display order, with a one-line example
 * from the label table (T025 rows 1 and 11). */
const STYLES: readonly { style: LabelStyle; name: string; example: string }[] = [
  { style: "plain", name: "Plain", example: label("plain", "edge", { kind: "calls_route", provenance: "inferred", confidence: 0.9, match: "exact" }).text },
  {
    style: "technical",
    name: "Technical",
    example: `${label("technical", "edge", { kind: "calls_route", provenance: "inferred", confidence: 0.9, match: "exact" }).text}, ${label("technical", "impactRule", "I4").text}`,
  },
];

/** sourceWords says where the applied style comes from. */
export function sourceWords(state: Pick<LabelState, "source" | "saved">): string {
  if (state.source === "browser") return state.saved ? "your choice in this browser" : "your choice for this page (browser storage is blocked)";
  if (state.source === "repository") return "the repository default from graphvein.yaml";
  return "Graphvein's default";
}

/** BROWSER_OPTIONS are the browser's choices: nothing chosen (the
 * repository default or plain applies) or one of the styles. */
const BROWSER_OPTIONS: readonly { style: LabelStyle | null; name: string; id: string }[] = [
  { style: null, name: "Not set", id: "unset" },
  { style: "plain", name: "Plain", id: "plain" },
  { style: "technical", name: "Technical", id: "technical" },
];

/** LabelSetting is this browser's label choice: a three-option switch
 * (Not set, Plain, Technical) whose checked option is the stored choice;
 * its tooltip says which style applies and where it comes from. */
export function LabelSetting({ labelledBy }: { labelledBy?: string }) {
  const state = useLabels();
  const name = STYLES.find((s) => s.style === state.style)?.name ?? "Plain";
  const chosen = state.source === "browser" ? state.style : null;
  return (
    <div className="grid gap-1" data-testid="labels-setting">
      <div
        role="radiogroup"
        aria-label={labelledBy === undefined ? "Label style in this browser" : undefined}
        aria-labelledby={labelledBy}
        title={`Labels: ${name}, ${sourceWords(state)}`}
        className="inline-grid w-fit grid-cols-3 gap-0.5 rounded-md border border-input bg-background p-0.5"
        data-testid="labels-control"
      >
        {BROWSER_OPTIONS.map((o) => {
          const on = o.style === chosen;
          const example = STYLES.find((s) => s.style === o.style)?.example;
          return (
            <button
              key={o.id}
              type="button"
              role="radio"
              aria-checked={on}
              title={example === undefined ? "Nothing chosen in this browser: the repository default applies, else plain" : `${o.name}: e.g. ${example}`}
              data-testid={`labels-${o.id}`}
              className={cn(
                "h-7 rounded-[5px] px-2.5 text-[12.5px] whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-ring",
                on ? "bg-brand-soft font-semibold text-brand-text" : "text-muted-foreground hover:bg-accent hover:text-foreground",
              )}
              onClick={() => {
                state.setStyle(o.style);
              }}
            >
              {o.name}
            </button>
          );
        })}
      </div>
      {!state.saved && state.source === "browser" && <div className="text-[11px] text-stale">Not saved: lasts until the page reloads</div>}
    </div>
  );
}

/** LabelBanner is the one-time label-style question. Since T064 it is a
 * card on the Overview only (it was a banner above every section): a page
 * opened directly by a link never shows it. */
export function LabelBanner() {
  const state = useLabels();
  if (!state.banner) return null;
  return (
    <section aria-labelledby="labels-banner-title" data-testid="labels-banner" className="grid gap-2.5 rounded-xl border bg-card p-4 text-[13px] shadow-[0_1px_2px_rgba(0,0,0,.04)]">
      <div className="flex items-start gap-2">
        <h2 id="labels-banner-title" className="m-0 flex-1 text-[15px] font-semibold">
          How should Graphvein label facts?
        </h2>
        <button
          type="button"
          aria-label="Close: keep plain labels"
          title="Close: keep plain labels"
          data-testid="labels-banner-close"
          className="-mt-1 -mr-1 grid size-7 flex-none place-items-center rounded-md text-muted-foreground outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
          onClick={state.dismissBanner}
        >
          <X aria-hidden="true" className="size-4" />
        </button>
      </div>
      <div className="grid grid-cols-2 gap-2">
        {STYLES.map((s) => (
          <button
            key={s.style}
            type="button"
            data-testid={`labels-banner-${s.style}`}
            className="grid min-w-0 content-start gap-0.5 rounded-md border border-input bg-background px-3 py-2 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => {
              state.setStyle(s.style);
            }}
          >
            <b className="font-semibold">{s.name}</b>
            <span className="text-xs text-muted-foreground">e.g. {s.example}</span>
          </button>
        ))}
      </div>
      <p className="m-0 text-xs text-muted-foreground">Asked once, here only; “Label style” in Settings changes it any time.</p>
    </section>
  );
}
