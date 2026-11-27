// The precision chips of the API section (T020, Variant A): the label of
// a transport's endpoint family (routes, IPC commands) and of its calls
// (client calls, IPC calls), as /endpoints, /endpoint and /flow serve them.

import type { ScopeLabel, Transport } from "@/api/endpoints";
import { PrecisionChip } from "@/components/precision/PrecisionChip";
import { scopeChip } from "@/components/precision/words";
import { useLabelStyle } from "@/hooks/useLabels";
import { label as labelText } from "@/lib/labels";

/** The two families of a transport: names on the chips and the server's
 * family keys (the popover's column when the label is row D). */
export const FAMILIES: Record<Transport, { endpoints: [string, string]; calls: [string, string] }> = {
  http: { endpoints: ["Routes", "routes"], calls: ["Client calls", "client_calls"] },
  tauri_ipc: { endpoints: ["IPC commands", "ipc_commands"], calls: ["IPC calls", "ipc_calls"] },
  server_action: { endpoints: ["Server actions", "server_actions"], calls: ["Action uses", "action_calls"] },
};

/** column is the coverage column a label belongs to: its row, or for row
 * D the family. */
export function column(label: ScopeLabel | null, family: string): string {
  return label !== null && label.row !== null && label.row !== "D" ? label.row : family;
}

/** unverifiedFacts are the facts a precise label does not cover (empty
 * for any other label). */
export function unverifiedFacts(label: ScopeLabel | null): readonly string[] {
  return label !== null && label.label === "precise" ? (label.unverified ?? []) : [];
}

/** FACT_PREFIX_UNKNOWN is the unverified fact the server adds to a
 * precise route label whose routes show unknown-prefix paths (T033 row
 * UP-37: best effort). */
export const FACT_PREFIX_UNKNOWN = "prefix-unknown paths";

/** UNVERIFIED_TAG is the dashed tag of a fact outside the chip's scope. */
const UNVERIFIED_TAG = "rounded-[3px] border border-dashed border-dynamic/70 px-0.5 font-sans text-[10px] leading-[13px] font-semibold text-dynamic";

/** UnverifiedMark marks one fact unverified when the label is precise but
 * its row does not cover the fact (T023 GR10). */
export function UnverifiedMark({ label, fact }: { label: ScopeLabel | null; fact: string }) {
  if (!unverifiedFacts(label).includes(fact)) return null;
  return (
    <span className={UNVERIFIED_TAG} title={`Unverified: no oracle compares the ${fact}; the Precise label does not cover it.`} data-testid={`unverified-${fact}`}>
      unverified
    </span>
  );
}

/** FamilyChip is one family's chip, or nothing without a label. A precise
 * chip whose row leaves facts out names them beside it. */
export function FamilyChip({ transport, which, label, testId, bare = false, countUnverified = false }: { transport: Transport; which: "endpoints" | "calls"; label: ScopeLabel | null; testId?: string; bare?: boolean; countUnverified?: boolean }) {
  const style = useLabelStyle();
  const [name, family] = FAMILIES[transport][which];
  const out = unverifiedFacts(label);
  const chip = <PrecisionChip state={scopeChip(label)} scope={name} focus={{ column: column(label, family) }} bare={bare} testId={testId ?? `precision-${which}`} />;
  if (out.length === 0) return chip;
  // countUnverified (the route header, T073): "4 parts unverified" with
  // the facts in the tooltip (label row 72); the technical style lists
  // them as before.
  const tag = countUnverified ? labelText(style, "unverifiedCount", out) : { text: `unverified: ${out.join(", ")}`, title: "Outside the Precise scope (no oracle compares them): these facts stay unverified." };
  return (
    <span className="inline-flex items-center gap-1">
      {chip}
      <span className={UNVERIFIED_TAG} title={tag.title} data-testid={`${testId ?? `precision-${which}`}-unverified`}>
        {tag.text}
        {countUnverified && style === "plain" && <span className="sr-only">: {out.join(", ")}</span>}
      </span>
    </span>
  );
}
