// The editor buttons of provenance chips (T029, ADR 0081 "Editor links on
// shape chips"): a chip whose file:line has a link in the route's
// shape_links (GET /api/v0/api/endpoint) ends in a small icon-only button
// with the editor's mark. The links and the ide status come from the
// server, as for every IDE button (presentIde, IdeMark): nothing here
// builds or rewrites a link. Without a link, or with editor links off in
// this browser, the chip shows no button.

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

import type { IdeLink, IdeStatus } from "@/api/endpoints";
import type { LinkedCallDifferences } from "@/api/shapeDifferences";
import { EditorLink } from "@/components/EditorLink";
import { IdeMark } from "@/components/IdeParts";
import { useEditor } from "@/hooks/useEditor";
import { GENERIC_IDE_LABEL, presentIde } from "@/lib/ide";

/** ChipLinks are the server's editor links of one route's chips, with the
 * ide status of the response that built them. */
export interface ChipLinks {
  status: IdeStatus;
  links: Record<string, IdeLink>;
  /** The route's linked calls whose request differs from what the route
   * reads (T029 W4), from the same endpoint detail; absent or empty when
   * none. */
  callers?: LinkedCallDifferences[];
}

const ChipLinksContext = createContext<ChipLinks | null>(null);

/** ChipLinksProvider gives the chips below it their route's links; null
 * (or no provider) shows no editor button. */
export function ChipLinksProvider({ value, children }: { value: ChipLinks | null; children: ReactNode }) {
  return <ChipLinksContext.Provider value={value}>{children}</ChipLinksContext.Provider>;
}

/** chipLinkKey is the shape_links key of a position: "file:line". */
export function chipLinkKey(file: string, line: number): string {
  return `${file}:${String(line)}`;
}

/** chipAction is the full action of a chip's button, "Open
 * app/items.py:75 in VS Code", from the IDE button's label ("Open in VS
 * Code"; the generic "Open in editor" reads "in the editor"). */
export function chipAction(file: string, line: number, label: string): string {
  const editor = label === GENERIC_IDE_LABEL ? "the editor" : label.replace(/^Open in /, "");
  return `Open ${file}:${String(line)} in ${editor}`;
}

/** ChipIdeButton is the icon-only editor button at the end of a chip;
 * nothing when the position has no server link or links are off. Its
 * title and accessible name are the full action, followed by what the IDE
 * button's name adds (the editor's source, a caveat). */
export function ChipIdeButton({ file, line }: { file: string; line: number }) {
  const ctx = useContext(ChipLinksContext);
  const editor = useEditor();
  const link = ctx?.links[chipLinkKey(file, line)];
  if (ctx === null || link === undefined) return null;
  const view = presentIde(ctx.status, link, editor);
  if (view.href === null) return null;
  const name = `${chipAction(file, line, view.text)}${view.name.slice(view.text.length)}`;
  return (
    <EditorLink
      href={view.href}
      place={chipLinkKey(file, line)}
      label={view.text}
      detail={name}
      aria-label={name}
      data-testid="chip-ide-button"
      onClick={(e) => {
        e.stopPropagation();
      }}
      className="-mr-1 inline-grid size-4 flex-none place-items-center rounded-[3px] opacity-55 outline-none hover:bg-background/70 hover:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring [&_svg]:size-3"
    >
      <IdeMark view={view} size="xs" />
    </EditorLink>
  );
}

/** RouteLinksSource loads a route's chip links (the Explorer: the
 * document itself carries no editor link). */
export type RouteLinksSource = (route: string, editor: string | null, signal: AbortSignal) => Promise<ChipLinks>;

/** routeLinksCache wraps load so each route and editor is requested once
 * per generation (the index run, reload and member it was made for); a
 * failed request is forgotten, so the next opening asks again. */
export function routeLinksCache(load: RouteLinksSource, generation: string): RouteLinksSource {
  const held = new Map<string, Promise<ChipLinks>>();
  return (route, editor, signal) => {
    const key = JSON.stringify([generation, route, editor]);
    let p = held.get(key);
    if (p === undefined) {
      // The shared request is not tied to the first caller's signal, so
      // closing one operation does not cancel it for another.
      p = load(route, editor, new AbortController().signal);
      held.set(key, p);
      p.catch(() => held.delete(key));
    }
    return signal.aborted ? Promise.reject(new Error("aborted")) : p;
  };
}

const RouteLinksContext = createContext<RouteLinksSource | null>(null);

/** RouteLinksProvider gives operations the Explorer's cached source. */
export function RouteLinksProvider({ source, children }: { source: RouteLinksSource | null; children: ReactNode }) {
  return <RouteLinksContext.Provider value={source}>{children}</RouteLinksContext.Provider>;
}

/** useRouteChipLinks loads the chip links of route while mounted (an open
 * operation): null until they arrive, and when the request fails (the
 * chips then show no button). */
export function useRouteChipLinks(route: string): ChipLinks | null {
  const source = useContext(RouteLinksContext);
  const { requestEditor } = useEditor();
  const editor = requestEditor ?? null;
  // The editor choice is not known yet: wait, as the API section does.
  const waiting = requestEditor === undefined;
  const key = JSON.stringify([route, editor]);
  const [held, setHeld] = useState<{ key: string; value: ChipLinks | null } | null>(null);
  useEffect(() => {
    if (source === null || waiting) return;
    const controller = new AbortController();
    source(route, editor, controller.signal).then(
      (value) => {
        if (!controller.signal.aborted) setHeld({ key, value });
      },
      () => {
        if (!controller.signal.aborted) setHeld({ key, value: null });
      },
    );
    return () => {
      controller.abort();
    };
  }, [source, route, editor, waiting, key]);
  return held !== null && held.key === key ? held.value : null;
}
