// The workspace's notices in the content area (T038): a link naming an
// index this server does not have (S13, claim row WS-47), and the line
// while a switched-to index opens (A5, WS-48). The opening line floats over
// the content's skeletons, so nothing moves when the index answers.

import { LoaderCircle, Plus, TriangleAlert, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useWorkspace } from "@/shell/workspace/WorkspaceRoot";

/** UnknownLinkNotice says that the opened link names an entry this server
 * does not have, and what is shown instead. */
export function UnknownLinkNotice() {
  const state = useWorkspace();
  if (state === null || state.unknownLink === null) return null;
  const shown = state.entry;
  return (
    <div role="status" className="flex flex-none items-center gap-3 rounded-lg border border-dynamic/50 bg-dynamic-soft px-3 py-2 text-[13px]" data-testid="unknown-link">
      <TriangleAlert aria-hidden="true" className="size-4 flex-none text-dynamic" />
      <p className="m-0 min-w-0 flex-1">
        This link opens <b className="font-semibold font-mono">{state.unknownLink}</b>, which this server does not have.{" "}
        {shown !== null ? (
          <>
            Showing <b className="font-semibold">{shown.name}</b> instead.
          </>
        ) : (
          "Nothing is shown in its place."
        )}{" "}
        Add the folder to open it, or ask whoever sent the link which folder it is.
      </p>
      <Button
        size="sm"
        variant="outline"
        onClick={() => {
          state.openAdd();
        }}
      >
        <Plus aria-hidden="true" />
        Add a folder
      </Button>
      <Button size="sm" variant="ghost" onClick={state.dismissUnknown} aria-label="Dismiss">
        <X aria-hidden="true" />
        Dismiss
      </Button>
    </div>
  );
}

/** OpeningNote floats over the content while the viewed index's first
 * answer is awaited: nothing of the previous index is shown meanwhile. */
export function OpeningNote() {
  const state = useWorkspace();
  if (state === null || state.entry === null) return null;
  return (
    <div className="pointer-events-none sticky top-0 z-20 -mb-3.5 h-0" data-testid="opening-note">
      <p role="status" className="m-0 ml-auto flex w-fit items-center gap-2 rounded-full border bg-background/95 px-3 py-1 text-[12.5px] text-muted-foreground shadow-sm">
        <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" />
        <span>
          Opening the index of <b className="font-semibold text-foreground">{state.entry.name}</b>; nothing of the previous index is shown meanwhile.
        </span>
      </p>
    </div>
  );
}
