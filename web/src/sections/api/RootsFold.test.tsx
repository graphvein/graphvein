// The roots fold (T016i D1, DD1-DD3 A) on both surfaces, with the
// measurement injected: jsdom has no layout, so every chip measures 100 px,
// the "+N more roots" chip 90 px, the gap 6 px, and the line `room` px.
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CATEGORY_LABEL, CATEGORY_TONE, linkedPercent } from "@/api/categories";
import type { RootCounts, TransportSummary } from "@/api/endpoints";
import { cn } from "@/lib/utils";
import { CoverageCards } from "@/sections/api/CoverageCards";
import { manifestTitle, rootCategoryCounts, rootLabel, type CoverageActions } from "@/sections/api/coverage";
import { BAR_CLASS } from "@/sections/api/parts";
import { ROOTS_EXPANDED_KEY, RootsMeasureContext, resetRootsChoice, type RootsMeasurer } from "@/sections/api/RootsFold";
import { SummaryStrip } from "@/sections/api/SummaryStrip";
import { endpointsGolden } from "@/test/apiFixtures";

function root(name: string, total: number, byStatus: Record<string, number> = { linked: total }): RootCounts {
  return { root: name, manifest: null, total, by_status: byStatus, repository_by_status: null };
}

// Server order ("." first, then byte order); by calls: ".", b, d, a, c, e.
const ROOTS = [root(".", 30), root("a", 5, { linked: 3, unmatched: 2 }), root("b", 12), root("c", 5), root("d", 12, { linked: 10, dynamic: 2 }), root("e", 1)];

function transports(roots: RootCounts[] = ROOTS): TransportSummary[] {
  const base = endpointsGolden("api-endpoints-all").transports;
  return base.map((t) => ({ ...t, roots: t.transport === "http" ? roots : roots.slice(0, 5).map((r) => ({ ...r })) }));
}

function measurer(room: number): RootsMeasurer {
  return (_line, row) => {
    const widths = new Map<string, number>();
    for (const el of row.querySelectorAll("[data-measure-root]")) widths.set(el.getAttribute("data-measure-root") ?? "", 100);
    return { available: room, widths, more: 90, gap: 6 };
  };
}

function actions(over: Partial<CoverageActions> = {}): CoverageActions {
  return { openGap: vi.fn(), setRoot: vi.fn(), activeGap: null, root: null, ...over };
}

type Surface = "strip" | "cards";

function view(surface: Surface, room: number, a: CoverageActions = actions(), ts: TransportSummary[] = transports()) {
  const body =
    surface === "strip" ? <SummaryStrip transports={ts} scope="all" actions={a} /> : <CoverageCards transports={ts} repository="fixture" scope="all" actions={a} />;
  return render(<RootsMeasureContext.Provider value={measurer(room)}>{body}</RootsMeasureContext.Provider>);
}

function list(surface: Surface, transport = "http"): HTMLElement {
  return screen.getByTestId(surface === "strip" ? `strip-roots-${transport}` : `roots-${transport}`);
}

/** The roots a list shows, in order (the measuring row has no data-root). */
function shown(el: HTMLElement): string[] {
  return [...el.querySelectorAll("[data-root]")].map((e) => e.getAttribute("data-root") ?? "");
}

beforeEach(() => {
  window.localStorage.clear();
  resetRootsChoice();
});

afterEach(() => {
  vi.restoreAllMocks();
  resetRootsChoice();
});

// The strip's Roots row as it was before T016i (SummaryStrip.tsx at
// 7fad72a), for the "fits: unchanged" check.
function TodayStripChips({ t, actions: a }: { t: TransportSummary; actions: CoverageActions }) {
  return (
    <div data-testid="today">
      {t.roots.map((r) => {
        const on = a.root === r.root;
        const pct = linkedPercent(r.by_status.linked ?? 0, r.total);
        return (
          <span key={r.root} data-root={r.root} className={cn("inline-flex h-[24px] items-center gap-1.5 rounded-md border pr-1 text-xs", on && "border-brand bg-brand-soft")}>
            <button type="button" aria-pressed={on} title={`${manifestTitle(r)}. ${on ? "Show all roots" : "Filter by this root"}`} className="h-full rounded-l-md px-1.5 font-mono hover:bg-accent">
              {rootLabel(r.root)}
            </button>
            <span className="tabular-nums text-muted-foreground">
              {r.total} · <b className="font-semibold text-foreground">{pct === null ? "-" : `${String(pct)}%`}</b>
            </span>
            {rootCategoryCounts(t.transport, r)
              .filter((c) => c.category !== "linked")
              .map((c) => (
                <button key={c.category} type="button" title={`${CATEGORY_LABEL[c.category]} in ${r.root}: show these calls`} className="inline-flex items-center gap-1 rounded px-1 hover:bg-accent">
                  <i aria-hidden="true" className={cn("size-[7px] rounded-[2px]", BAR_CLASS[CATEGORY_TONE[c.category]])} />
                  <b className="font-semibold tabular-nums">{c.count}</b>
                  <span className="sr-only">{CATEGORY_LABEL[c.category]}</span>
                </button>
              ))}
          </span>
        );
      })}
    </div>
  );
}

describe("roots fold: fits", () => {
  it("strip: the list is today's when every chip fits, whatever the stored choice", () => {
    window.localStorage.setItem(ROOTS_EXPANDED_KEY, "1");
    const a = actions({ root: "d" });
    const ts = transports();
    view("strip", 630, a, ts);
    const el = list("strip");
    expect(el.getAttribute("data-fold")).toBe("fits");
    expect(within(el).queryByTestId("roots-toggle")).toBeNull();
    expect(shown(el)).toEqual(ROOTS.map((r) => r.root));
    // Same elements: the label, then every chip as before, nothing else
    // (the measuring row aside).
    const kids = [...el.children].filter((c) => c.getAttribute("data-testid") !== "roots-measure");
    expect(kids[0]?.textContent).toBe("Roots");
    const http = ts.find((t) => t.transport === "http");
    if (http === undefined) throw new Error("no http");
    const today = render(<TodayStripChips t={http} actions={a} />);
    const want = [...today.getByTestId("today").children].map((c) => c.outerHTML);
    expect(kids.slice(1).map((c) => c.outerHTML)).toEqual(want);
    expect(el.getAttribute("aria-label")).toBe("Source roots, HTTP");
  });

  it("cards: today's rows when every chip fits", () => {
    view("cards", 630);
    const el = list("cards");
    expect(el.getAttribute("data-fold")).toBe("fits");
    expect(el.className).toContain("grid gap-0.5");
    expect(within(el).queryByTestId("roots-toggle")).toBeNull();
    expect(within(el).getByText("By source root of the calling site")).toBeTruthy();
    expect(shown(el)).toEqual(ROOTS.map((r) => r.root));
    expect(el.querySelectorAll('[data-root][class="contents"]')).toHaveLength(ROOTS.length);
  });

  it("is today's list when nothing can be measured (a hidden surface)", () => {
    render(
      <RootsMeasureContext.Provider value={() => null}>
        <SummaryStrip transports={transports()} scope="all" actions={actions()} />
      </RootsMeasureContext.Provider>,
    );
    expect(list("strip").getAttribute("data-fold")).toBe("fits");
    expect(shown(list("strip"))).toEqual(ROOTS.map((r) => r.root));
  });
});

for (const surface of ["strip", "cards"] as const) {
  describe(`roots fold: ${surface}`, () => {
    it("folds to the largest roots and a +N chip, then unfolds to every root in server order", () => {
      view(surface, 420);
      const el = list(surface);
      expect(el.getAttribute("data-fold")).toBe("folded");
      expect(shown(el)).toEqual([".", "b", "d"]);
      expect(el.getAttribute("role")).toBe("group");
      expect(el.getAttribute("aria-label")).toBe("Source roots, HTTP, 3 of 6 shown");
      const toggle = within(el).getByTestId("roots-toggle");
      expect(toggle.textContent).toBe("+3 more roots");
      expect(toggle.getAttribute("aria-expanded")).toBe("false");
      expect(toggle.getAttribute("aria-controls")).toBe(el.id);
      expect(toggle.getAttribute("title")).toBe("Show all 6 roots");
      toggle.focus();
      fireEvent.click(toggle);
      expect(list(surface).getAttribute("data-fold")).toBe("expanded");
      expect(shown(list(surface))).toEqual(ROOTS.map((r) => r.root));
      // One button whose text changed: the focus stayed on it.
      const again = within(list(surface)).getByTestId("roots-toggle");
      expect(again).toBe(toggle);
      expect(document.activeElement).toBe(toggle);
      expect(again.textContent).toBe("Show fewer");
      expect(again.getAttribute("aria-expanded")).toBe("true");
      expect(again.getAttribute("title")).toBe("Show only the largest roots");
      expect(list(surface).getAttribute("aria-label")).toBe("Source roots, HTTP");
      // The toggle is last in the list.
      expect(list(surface).lastElementChild).toBe(again);
      expect(window.localStorage.getItem(ROOTS_EXPANDED_KEY)).toBe("1");
      fireEvent.click(again);
      expect(shown(list(surface))).toEqual([".", "b", "d"]);
      expect(document.activeElement).toBe(toggle);
      expect(window.localStorage.getItem(ROOTS_EXPANDED_KEY)).toBeNull();
    });

    it("is the +N chip alone when no root fits, with the singular for one", () => {
      view(surface, 150);
      expect(shown(list(surface))).toEqual([]);
      expect(within(list(surface)).getByTestId("roots-toggle").textContent).toBe("+6 more roots");
    });

    it('says "+1 more root" for one hidden root', () => {
      // 5 x 100 + 90 + 5 gaps = 620 fits; all six (630) does not.
      view(surface, 625);
      expect(shown(list(surface))).toEqual([".", "b", "d", "a", "c"]);
      expect(within(list(surface)).getByTestId("roots-toggle").textContent).toBe("+1 more root");
    });

    it("pins the active root's chip last in the folded line (DD3 A)", () => {
      view(surface, 420, actions({ root: "e" }));
      expect(shown(list(surface))).toEqual([".", "b", "e"]);
      expect(list(surface).querySelector('[data-root="e"] button[aria-pressed="true"]')).not.toBeNull();
    });

    it("reads the stored choice: exactly 1 is unfolded, anything else folded", () => {
      window.localStorage.setItem(ROOTS_EXPANDED_KEY, "1");
      const a = view(surface, 420);
      expect(list(surface).getAttribute("data-fold")).toBe("expanded");
      a.unmount();
      window.localStorage.setItem(ROOTS_EXPANDED_KEY, "true");
      const b = view(surface, 420);
      expect(list(surface).getAttribute("data-fold")).toBe("folded");
      b.unmount();
      window.localStorage.removeItem(ROOTS_EXPANDED_KEY);
      view(surface, 420);
      expect(list(surface).getAttribute("data-fold")).toBe("folded");
    });

    it("works with throwing storage, in memory, without an error or a log", () => {
      const error = vi.spyOn(console, "error");
      const warn = vi.spyOn(console, "warn");
      for (const m of ["getItem", "setItem", "removeItem"] as const) {
        vi.spyOn(Storage.prototype, m).mockImplementation(() => {
          throw new Error("blocked");
        });
      }
      view(surface, 420);
      expect(list(surface).getAttribute("data-fold")).toBe("folded");
      fireEvent.click(within(list(surface)).getByTestId("roots-toggle"));
      expect(list(surface).getAttribute("data-fold")).toBe("expanded");
      fireEvent.click(within(list(surface)).getByTestId("roots-toggle"));
      expect(list(surface).getAttribute("data-fold")).toBe("folded");
      expect(error).not.toHaveBeenCalled();
      expect(warn).not.toHaveBeenCalled();
    });

    it("root and category chips keep their actions, folded and unfolded", () => {
      const a = actions();
      view(surface, 420, a);
      const el = list(surface);
      fireEvent.click(within(el.querySelector('[data-root="d"]') as HTMLElement).getByRole("button", { name: "d" }));
      expect(a.setRoot).toHaveBeenLastCalledWith("d");
      const dyn = within(el.querySelector('[data-root="d"]') as HTMLElement).getByRole("button", { name: /Dynamic/ });
      fireEvent.click(dyn);
      expect(a.openGap).toHaveBeenLastCalledWith("http", "dynamic", "d");
      fireEvent.click(within(el).getByTestId("roots-toggle"));
      const rowA = list(surface).querySelector('[data-root="a"]') as HTMLElement;
      // The cards' unfolded rows are display: contents; their buttons are
      // the row's children.
      const nameA = surface === "strip" ? within(rowA).getByRole("button", { name: "a" }) : (rowA.querySelector("button") as HTMLElement);
      fireEvent.click(nameA);
      expect(a.setRoot).toHaveBeenLastCalledWith("a");
      const unmatched = [...rowA.querySelectorAll("button")].find((b) => /unmatched/i.test(b.textContent) || /Unmatched/.test(b.title));
      if (unmatched === undefined) throw new Error("no unmatched button");
      fireEvent.click(unmatched);
      expect(a.openGap).toHaveBeenLastCalledWith("http", "unmatched", "a");
    });

    it("the measuring row is hidden, inert, without data-root and without a tab stop", () => {
      view(surface, 420);
      const row = within(list(surface)).getByTestId("roots-measure");
      expect(row.getAttribute("aria-hidden")).toBe("true");
      expect(row.hasAttribute("inert")).toBe(true);
      expect(row.className).toContain("invisible");
      expect(row.className).toContain("absolute");
      expect(row.className).toContain("h-0");
      expect(row.querySelectorAll("[data-root]")).toHaveLength(0);
      expect(row.querySelectorAll("[data-measure-root]")).toHaveLength(ROOTS.length);
      expect(row.querySelector("[data-measure-more]")?.textContent).toBe("+6 more roots");
      for (const b of row.querySelectorAll("button")) expect(b.tabIndex).toBe(-1);
    });

    it("one choice unfolds every transport's list (DD2 A)", () => {
      view(surface, 420);
      expect(list(surface, "tauri_ipc").getAttribute("data-fold")).toBe("folded");
      fireEvent.click(within(list(surface, "http")).getByTestId("roots-toggle"));
      expect(list(surface, "http").getAttribute("data-fold")).toBe("expanded");
      expect(list(surface, "tauri_ipc").getAttribute("data-fold")).toBe("expanded");
    });
  });
}

describe("roots fold: other rules", () => {
  it("re-measures when the room changes (folded <-> fits)", () => {
    const a = actions();
    const ts = transports();
    const r = view("strip", 420, a, ts);
    expect(list("strip").getAttribute("data-fold")).toBe("folded");
    r.rerender(
      <RootsMeasureContext.Provider value={measurer(2000)}>
        <SummaryStrip transports={ts} scope="all" actions={a} />
      </RootsMeasureContext.Provider>,
    );
    expect(list("strip").getAttribute("data-fold")).toBe("fits");
  });

  it("cards: a line that mounts after its roots arrive still re-folds on resize", () => {
    // A recording observer: jsdom never fires one, so the test fires the
    // callbacks of whatever is observed at the time of the "resize".
    const observers = new Set<{ cb: ResizeObserverCallback; targets: Set<Element> }>();
    class RecordingObserver {
      private entry: { cb: ResizeObserverCallback; targets: Set<Element> };
      constructor(cb: ResizeObserverCallback) {
        this.entry = { cb, targets: new Set() };
        observers.add(this.entry);
      }
      observe(el: Element): void {
        this.entry.targets.add(el);
      }
      unobserve(el: Element): void {
        this.entry.targets.delete(el);
      }
      disconnect(): void {
        this.entry.targets.clear();
        observers.delete(this.entry);
      }
    }
    vi.stubGlobal("ResizeObserver", RecordingObserver);
    try {
      let room = 420;
      const live: RootsMeasurer = (line, row, label) => measurer(room)(line, row, label);
      const a = actions();
      const cards = (ts: TransportSummary[]) => (
        <RootsMeasureContext.Provider value={live}>
          <CoverageCards transports={ts} repository="fixture" scope="all" actions={a} />
        </RootsMeasureContext.Provider>
      );
      const empty = transports().map((t) => (t.transport === "http" ? { ...t, roots: [] } : t));
      const r = render(cards(empty));
      expect(screen.queryByTestId("roots-http")).toBeNull();
      // Include tests on: the roots arrive and the line mounts, folded.
      r.rerender(cards(transports()));
      expect(list("cards").getAttribute("data-fold")).toBe("folded");
      // The window widens: the observer on the late line must re-measure.
      room = 2000;
      const el = list("cards");
      act(() => {
        for (const o of [...observers]) if (o.targets.has(el)) o.cb([], {} as ResizeObserver);
      });
      expect(list("cards").getAttribute("data-fold")).toBe("fits");
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("the strip with the peek open still has no Roots row", () => {
    render(
      <RootsMeasureContext.Provider value={measurer(420)}>
        <SummaryStrip transports={transports()} scope="all" actions={actions()} shown />
      </RootsMeasureContext.Provider>,
    );
    expect(screen.queryAllByTestId(/^strip-roots-/)).toHaveLength(0);
  });

  it("the cards' folded line uses the strip's root chips; unfolded, today's rows", () => {
    view("cards", 420);
    const el = list("cards");
    expect(el.querySelector('[data-root="."]')?.className).toContain("h-[24px]");
    fireEvent.click(within(el).getByTestId("roots-toggle"));
    expect(list("cards").querySelectorAll('[data-root][class="contents"]')).toHaveLength(ROOTS.length);
    expect(within(list("cards")).getByText("By source root of the calling site")).toBeTruthy();
  });
});
