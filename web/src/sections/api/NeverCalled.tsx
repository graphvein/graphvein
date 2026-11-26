// The never-called state of an endpoint (M5 .empty). It says what is known
// and nothing more: no stored caller, possible reasons, and that dynamic
// calls exist which are not attached to any endpoint. It never suggests
// one of them as the caller.

import { Ban } from "lucide-react";

import { callNoun, endpointNoun, neverCalledLabel } from "@/api/categories";
import type { Transport } from "@/api/endpoints";

export interface NeverCalledProps {
  transport: Transport;
  repository: string;
  /** The scope the callers were read in. In production scope test calls
   * are not callers (docs/cli/serve.md), so the text says so. */
  scope: string;
  /** The source-root filter, when set. */
  root: string | null;
  /** Dynamic calls of the transport in the current scope and root. */
  dynamicCount: number;
  /** Never-called endpoints of the transport in the current scope and root. */
  neverCount: number;
  /** serve --project (T022 B9 R1): no other member's call is linked to
   * the endpoint either, and the counts are the project's. */
  project?: boolean;
  onReviewDynamic: () => void;
  onAllNever: () => void;
}

/** NeverCalled renders the empty callers state. */
export function NeverCalled(props: NeverCalledProps) {
  const noun = endpointNoun(props.transport, 1);
  // Tauri keeps its M5 wording; a server action's callers are action uses.
  const caller = props.transport === "server_action" ? callNoun(props.transport, 1) : "client call";
  return (
    <div className="grid justify-items-start gap-2 rounded-lg border border-dashed p-[18px] text-[13px]" data-testid="never-called">
      <div aria-hidden="true" className="grid size-8 place-items-center rounded-lg bg-never-soft text-never">
        <Ban className="size-4" />
      </div>
      <b className="text-sm">
        {props.scope === "production" ? `No production ${caller}` : `No ${caller}`} in {props.repository}
        {props.root !== null ? ` (source root ${props.root})` : ""} is linked to this {noun}
        {props.scope === "production" ? " (test calls are excluded; turn on Include tests to see them)" : ""}
        {props.project === true ? ", and no call of another repository of the project is linked to it" : ""}.
      </b>
      <span className="text-muted-foreground">
        Unresolved calls may still reach it. It may also be used by external clients, scripts or tests that are not
        indexed, or it may be dead code. Graphvein does not guess.
      </span>
      {props.dynamicCount > 0 && (
        <span className="text-muted-foreground">
          {props.dynamicCount} dynamic {props.dynamicCount === 1 ? "call is" : "calls are"} not linked to any {noun}; one
          of them might be the caller.
        </span>
      )}
      <div className="mt-1 flex flex-wrap gap-2">
        {props.dynamicCount > 0 && (
          <button
            type="button"
            className="inline-flex h-7 items-center rounded-md border border-input bg-background px-2.5 text-[12.5px] font-medium shadow-xs hover:bg-accent"
            onClick={props.onReviewDynamic}
          >
            Review {props.dynamicCount} dynamic {props.dynamicCount === 1 ? "call" : "calls"}
          </button>
        )}
        <button
          type="button"
          className="inline-flex h-7 items-center rounded-md px-2.5 text-[12.5px] font-medium hover:bg-accent"
          onClick={props.onAllNever}
        >
          All {neverCalledLabel(props.scope, props.neverCount, props.project === true)}
        </button>
      </div>
    </div>
  );
}
