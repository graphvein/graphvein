// "Copy link to this view" (T038 S12, decisions of 2026-11-24): an
// icon-only button after the breadcrumb (lucide Link, the standard chain)
// with the app's own tooltip, shown at once on hover and on keyboard
// focus, and the same accessible name. The link holds the entry id, the
// project member and the section's whole URL query, so it opens the same
// view (claim row WS-50); the toast says what it opens.

import { Link } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { parseHashQuery } from "@/router";
import { readLocation, viewLink } from "@/shell/workspace/location";

export const COPY_LINK_LABEL = "Copy link to this view";

/** TOAST_MS is how long the toast stays. */
export const TOAST_MS = 4_000;

/** viewDescription says what a link opens: the entry (and member), the
 * section, and whether a selection or filters come with it. */
export function viewDescription(entry: string | null, member: string | null, section: string, hash: string): string {
  const where = [entry, member].filter((x): x is string => x !== null && x !== "").join(" › ");
  const q = parseHashQuery(hash);
  const keys = [...q.keys()];
  const sel = keys.some((k) => k === "sel" || k === "table" || k === "node" || k === "id" || k === "op" || k === "item");
  // Help's ?at= names a heading of the guide (T045), not a tab or filter.
  const heading = keys.length > 0 && keys.every((k) => k === "at");
  const extra = keys.length === 0 ? "" : heading ? ", at that heading" : sel ? ", with the selection, tab and filters" : ", with the tab and filters";
  return `${where === "" ? "" : `${where}, `}${section}${extra}`;
}

/** CopyLinkButton copies the link to the current view. entry and section
 * name it in the toast. */
export function CopyLinkButton({ entry, section }: { entry: string | null; section: string }) {
  const [toast, setToast] = useState<{ ok: boolean; text: string; link: string } | null>(null);
  const timer = useRef(0);
  useEffect(
    () => () => {
      window.clearTimeout(timer.current);
    },
    [],
  );
  const copy = () => {
    const loc = readLocation();
    const hash = window.location.hash;
    const link = viewLink(loc, hash);
    const text = viewDescription(entry, loc.member, section, hash);
    const show = (ok: boolean) => {
      setToast({ ok, text, link });
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => {
        setToast(null);
      }, ok ? TOAST_MS : TOAST_MS * 3);
    };
    try {
      navigator.clipboard.writeText(link).then(
        () => {
          show(true);
        },
        () => {
          show(false);
        },
      );
    } catch {
      // No clipboard (an insecure context): the toast shows the link.
      show(false);
    }
  };
  return (
    <>
      <Tooltip delayDuration={0}>
        <TooltipTrigger asChild>
          <button
            type="button"
            aria-label={COPY_LINK_LABEL}
            onClick={copy}
            data-testid="copy-link"
            className="grid size-7 flex-none place-items-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
          >
            <Link aria-hidden="true" className="size-4" />
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom" sideOffset={6}>
          {COPY_LINK_LABEL}
        </TooltipContent>
      </Tooltip>
      {toast !== null && (
        <div role="status" className="fixed bottom-5 left-1/2 z-50 grid max-w-[min(560px,calc(100vw-32px))] -translate-x-1/2 gap-1 rounded-lg bg-foreground px-4 py-2.5 text-[13px] text-background shadow-lg" data-testid="copy-toast">
          {toast.ok ? (
            <span>
              <b className="font-semibold">Link copied.</b> It opens {toast.text}.
            </span>
          ) : (
            <>
              <span>
                <b className="font-semibold">The clipboard is not available.</b> Copy the link to {toast.text}:
              </span>
              <code className="font-mono text-[12px] [overflow-wrap:anywhere] select-all">{toast.link}</code>
            </>
          )}
        </div>
      )}
    </>
  );
}
