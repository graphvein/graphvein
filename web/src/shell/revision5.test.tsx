// T019 Revision 5 (T019b) through the shell: markers
// describe the current index only (R5-1); an answer from another index run
// than the summary is withheld while the summary reloads (R5-2); request
// keys follow the resolved comparison, so a re-run branch comparison or a
// re-pointed pin reloads the markers (R5-3); the API footer and marker
// placement, the compare menu's focus and the merge base note (R5-5, R5-6).
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { ChangeItem, ChangesBases, ChangesClient, ChangesHeader, ChangesSummary } from "@/api/changes";
import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { COMPARE_KEY, MARKERS_OFF_HEAD } from "@/shell/compare";
import { endpointsGolden, fakeClient } from "@/test/apiFixtures";
import { basesFor, fakeChangesClient, itemsOf, summaryOf, type ChangeCase } from "@/test/changesFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, summaryGolden } from "@/test/schemaFixtures";

const rows = endpointsGolden("api-endpoints-production").transports.flatMap((t) => t.endpoints);
const goldenItems = itemsOf("previous-rev1-rev2", { base: "previous", tests: true, limit: 500 }).items;
const changedRoute = goldenItems.find((i) => i.kind === "route" && i.change === "changed");
const removedRoute = goldenItems.find((i) => i.kind === "route" && i.change === "removed");

/** A changed route on rows[0] with the given fields, and the golden's
 * removed route (no row). */
function routeItems(fields = changedRoute?.fields ?? [], removed: Partial<ChangeItem> = {}): ChangeItem[] {
  if (changedRoute === undefined || removedRoute === undefined || rows[0] === undefined) throw new Error("golden changed");
  return [{ ...changedRoute, id: rows[0].id, identity: rows[0].id, fields }, { ...removedRoute, ...removed }];
}

function indexAt(finished: string): NonNullable<ChangesHeader["index"]> {
  return { finished_at: finished, indexer: { name: "graphvein", version: "extractors-34" }, contract_version: 1 };
}

/** withHead is a golden summary whose head side has another kind or run. */
function withHead(c: ChangeCase, head: Partial<ChangesSummary["summary"]["head"]>, base?: string): ChangesSummary {
  const s = summaryOf(c);
  return { ...s, base: base ?? s.base, summary: { ...s.summary, head: { ...s.summary.head, ...head } } };
}

interface Opts {
  case?: ChangeCase;
  summary?: (base: string, n: number) => ChangesSummary;
  bases?: (n: number) => ChangesBases;
  routes?: (n: number) => ChangeItem[];
  itemsIndex?: () => string | null;
  calls?: string[];
}

function client(o: Opts): ChangesClient {
  const calls = o.calls ?? [];
  const base = fakeChangesClient({ ...(o.case !== undefined ? { case: o.case } : {}), calls });
  let summaries = 0;
  let bases = 0;
  let routeLoads = 0;
  return {
    ...base,
    bases: (opts) => (o.bases === undefined ? base.bases(opts) : Promise.resolve(o.bases(bases++))),
    summary: (b, opts) => {
      calls.push(`summary:${b}`);
      return o.summary === undefined ? base.summary(b, opts) : Promise.resolve(o.summary(b, summaries++));
    },
    items: async (p, opts) => {
      const page = p.group === "routes" && o.routes !== undefined ? { ...itemsOf(o.case ?? "previous-rev1-rev2", p), items: o.routes(routeLoads++), total: 2, next_cursor: null } : await base.items(p, opts);
      if (p.group === "routes" && o.routes !== undefined) calls.push(`items:routes`);
      const at = o.itemsIndex?.() ?? null;
      return at === null ? page : { ...page, index: indexAt(at) };
    },
  };
}

function show(hash: string, c: ChangesClient, fetcher: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"))) {
  window.history.replaceState(null, "", hash);
  return render(<AppShell fetcher={fetcher} client={fakeClient()} schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })} changesClient={c} />);
}

beforeEach(() => {
  window.localStorage.clear();
});

describe("R5-1: markers describe the current index only", () => {
  it("a branch comparison whose head is another commit: no markers, no footer, the chip says why", async () => {
    window.localStorage.setItem(COMPARE_KEY, JSON.stringify({ base: "ref:rev1..rev3", on: true }));
    const calls: string[] = [];
    show("#/api", client({ case: "ref-rev1-rev3", routes: () => routeItems(), calls }));
    await screen.findByRole("listbox", { name: "Endpoints" });
    const chip = screen.getByTestId("compare-chip");
    await waitFor(() => {
      expect(screen.getByTestId("compare-markers-off").textContent).toBe("markers off");
    });
    expect(chip.getAttribute("title")).toContain(MARKERS_OFF_HEAD);
    expect(screen.queryAllByTestId("change-marker")).toEqual([]);
    expect(screen.queryByTestId("endpoints-gone")).toBeNull();
    expect(calls.filter((c) => c.startsWith("items"))).toEqual([]);
    fireEvent.click(chip);
    const menu = await screen.findByRole("dialog", { name: "Compare against" });
    expect(within(menu).getByTestId("markers-off-note").textContent).toBe("Markers off: this comparison's head is not the current index.");
  });

  it("a branch base against the current index keeps its markers", async () => {
    window.localStorage.setItem(COMPARE_KEY, JSON.stringify({ base: "ref:rev1", on: true }));
    show("#/api", client({ case: "ref-rev1-rev3", summary: (b) => withHead("ref-rev1-rev3", { kind: "current" }, b), routes: () => routeItems() }));
    const list = await screen.findByRole("listbox", { name: "Endpoints" });
    await waitFor(() => {
      expect(within(list).getAllByTestId("change-marker").length).toBe(1);
    });
    expect(screen.queryByTestId("compare-markers-off")).toBeNull();
    expect(screen.getByTestId("compare-text").textContent).toBe("Since rev1..current index");
  });
});

describe("R5-2: answers are checked against the summary", () => {
  it("a list answer from a newer index run shows loading, checks the status and reloads the summary", async () => {
    let statusCalls = 0;
    const fetcher: StatusFetcher = () => {
      statusCalls++;
      return Promise.resolve<Status>(goldenStatus("fresh"));
    };
    // The summary was read at run 3; the list answers from run 4 until the
    // summary is read again (then both are at run 4).
    const calls: string[] = [];
    const RUN3 = "2026-11-14T10:00:00Z";
    const RUN4 = "2026-11-14T10:05:00Z";
    const c = client({ summary: (b, n) => ({ ...summaryOf("previous-rev1-rev2"), base: b, index: indexAt(n === 0 ? RUN3 : RUN4) }), itemsIndex: () => RUN4, calls });
    show("#/changes?group=code", c, fetcher);
    await screen.findByTestId("changes-summary");
    await waitFor(() => {
      expect(calls.filter((x) => x.startsWith("summary:")).length).toBe(2);
    });
    expect(statusCalls).toBeGreaterThanOrEqual(2);
    // Once the summary is at run 4 the list shows its rows.
    await waitFor(() => {
      expect(screen.getAllByTestId("change-row").length).toBeGreaterThan(0);
    });
  });

  it("never shows rows of another run while the summary is older", async () => {
    // The summary keeps answering run 3: the list stays loading (withheld),
    // and the summary is requested again only once for that pair.
    const calls: string[] = [];
    const c = client({ summary: (b) => ({ ...summaryOf("previous-rev1-rev2"), base: b, index: indexAt("2026-11-14T10:00:00Z") }), itemsIndex: () => "2026-11-14T10:05:00Z", calls });
    show("#/changes?group=code", c);
    await screen.findByTestId("changes-summary");
    await waitFor(() => {
      expect(calls.filter((x) => x.startsWith("summary:")).length).toBe(2);
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    expect(screen.queryAllByTestId("change-row")).toEqual([]);
    expect(calls.filter((x) => x.startsWith("summary:")).length).toBe(2);
  });

  it("marker pages from another run are withheld", async () => {
    const calls: string[] = [];
    show("#/api", client({ routes: () => routeItems(), itemsIndex: () => "2026-11-14T10:05:00Z", calls }));
    await screen.findByRole("listbox", { name: "Endpoints" });
    await waitFor(() => {
      expect(calls.filter((x) => x.startsWith("summary:")).length).toBe(2);
    });
    expect(screen.queryAllByTestId("change-marker")).toEqual([]);
  });
});

describe("R5-3: request keys follow the resolved comparison", () => {
  it("re-running a branch comparison reloads the markers with the new head", async () => {
    window.localStorage.setItem(COMPARE_KEY, JSON.stringify({ base: "ref:rev1", on: true }));
    const committed = { id: "J", state: "committed", started_at: "t", ended_at: "t", base: "ref:rev1", commits: ["0d8c4f7e5b1a2c3d"], indexed: [], error: null };
    const calls: string[] = [];
    const base = client({
      case: "ref-rev1-rev3",
      summary: (b, n) => withHead("ref-rev1-rev3", { kind: "current", run: n === 0 ? "RUN-A" : "RUN-B" }, b),
      // The first load marks the path change, the second the handler too.
      routes: (n) => routeItems(n === 0 ? (changedRoute?.fields ?? []) : []).map((i, k) => (k === 0 && n > 0 ? { ...i, labels: ["handler_changed"] } : i)),
      calls,
    });
    const c: ChangesClient = { ...base, startRefs: () => Promise.resolve(committed) };
    show("#/api", c);
    const list = await screen.findByRole("listbox", { name: "Endpoints" });
    await waitFor(() => {
      expect(within(list).getByTestId("change-marker").textContent).toBe("paths changed");
    });
    fireEvent.click(screen.getByTestId("compare-chip"));
    const menu = await screen.findByRole("dialog", { name: "Compare against" });
    fireEvent.change(within(menu).getByRole("textbox", { name: "Base ref" }), { target: { value: "rev1" } });
    fireEvent.click(within(menu).getByRole("button", { name: "Index and compare" }));
    await waitFor(() => {
      expect(within(list).getByTestId("change-marker").textContent).toBe("handler changed");
    });
    expect(calls.filter((x) => x === "items:routes").length).toBe(2);
  });

  it("a pin deleted and pinned again from the CLI reloads with the next status answer", async () => {
    window.localStorage.setItem(COMPARE_KEY, JSON.stringify({ base: "pin:release-1", on: true }));
    const golden = summaryOf("pin-rev1-rev3");
    const repinned = (n: number): ChangesBases => {
      const b = basesFor("pin-rev1-rev3");
      if (n === 0) return b;
      const pin = b.pins[0];
      if (pin === undefined) throw new Error("no pin");
      return { ...b, pins: [{ ...pin, id: "PIN-NEW", run: { ...pin.run, run_id: "RUN-NEW" } }] };
    };
    const calls: string[] = [];
    let summaries = 0;
    const c = client({
      case: "pin-rev1-rev3",
      bases: repinned,
      // The same base parameter both times: only the resolved pin differs.
      summary: () => {
        summaries++;
        return summaries === 1 ? golden : { ...golden, summary: { ...golden.summary, base: { ...golden.summary.base, run: "RUN-NEW" } } };
      },
      routes: (n) => (n === 0 ? routeItems() : []),
      calls,
    });
    show("#/api", c);
    const list = await screen.findByRole("listbox", { name: "Endpoints" });
    await waitFor(() => {
      expect(within(list).getAllByTestId("change-marker").length).toBe(1);
    });
    expect(calls.filter((x) => x.startsWith("summary:")).length).toBe(1);
    // The next status answer (here: Check) reloads the bases; the pin's new
    // id and run make it another comparison.
    fireEvent.click(screen.getByRole("button", { name: /Check/ }));
    await waitFor(() => {
      expect(calls.filter((x) => x.startsWith("summary:")).length).toBe(2);
    });
    await waitFor(() => {
      expect(within(list).queryAllByTestId("change-marker")).toEqual([]);
    });
    expect(calls.filter((x) => x === "items:routes").length).toBe(2);
  });
});

describe("R5-5, R5-6: the API list, the menu and the V3 notes", () => {
  it("the marker follows the path in its cell; the footer wraps and honours Include tests", async () => {
    const c = client({ routes: () => routeItems(changedRoute?.fields ?? [], { test: true }) });
    show("#/api", c);
    const list = await screen.findByRole("listbox", { name: "Endpoints" });
    const marker = await within(list).findByTestId("change-marker");
    const cell = marker.parentElement;
    expect(cell?.firstElementChild?.getAttribute("data-testid")).toBe("endpoint-path");
    expect(marker.className).toContain("max-w-[45%]");
    // The removed test-scope route is not counted without Include tests:
    // the golden's removed Tauri command is.
    await waitFor(() => {
      expect(screen.getByTestId("endpoints-gone").textContent).toBe("1 endpoint no longer in the index since previous run");
    });
    expect(screen.getByTestId("endpoints-footer").className).toContain("flex-wrap");
  });

  it("with Include tests the test-scope removal is counted", async () => {
    const c = client({ routes: () => routeItems(changedRoute?.fields ?? [], { test: true }) });
    show("#/api?tests=1", c);
    await screen.findByRole("listbox", { name: "Endpoints" });
    await waitFor(() => {
      expect(screen.getByTestId("endpoints-gone").textContent).toBe("2 endpoints no longer in the index since previous run");
    });
  });

  it("the compare menu closes when focus leaves it", async () => {
    show("#/changes", client({}));
    await screen.findByTestId("compare-count");
    fireEvent.click(screen.getByTestId("compare-chip"));
    const menu = await screen.findByRole("dialog", { name: "Compare against" });
    const inside = within(menu).getByRole("textbox", { name: "Base ref" });
    // Moving inside keeps it; a blur with no new target (a click on text) too.
    fireEvent.blur(inside, { relatedTarget: within(menu).getByRole("textbox", { name: "Pin name" }) });
    fireEvent.blur(inside, { relatedTarget: null });
    expect(screen.getByRole("dialog", { name: "Compare against" })).toBeTruthy();
    const outside = screen.getByRole("button", { name: /Check/ });
    fireEvent.blur(inside, { relatedTarget: outside });
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Compare against" })).toBeNull();
    });
  });

  it("a branch comparison names its merge base in the summary and the menu", async () => {
    window.localStorage.setItem(COMPARE_KEY, JSON.stringify({ base: "ref:rev1..rev3", on: true }));
    show("#/changes", client({ case: "ref-rev1-rev3" }));
    const summary = await screen.findByTestId("changes-summary");
    const base = summaryOf("ref-rev1-rev3").summary.base.commit ?? "";
    expect(within(summary).getAllByTestId("side-ref")[0]?.textContent).toContain(`merge base ${base.slice(0, 7)}`);
    fireEvent.click(screen.getByTestId("compare-chip"));
    const menu = await screen.findByRole("dialog", { name: "Compare against" });
    expect(within(menu).getByTestId("merge-base-note").textContent).toBe(`The comparison shown is from the merge base ${base.slice(0, 7)}.`);
  });

  it("migration rows are named by file with edited (history rewritten)", async () => {
    show("#/changes?group=migrations", client({}));
    const rowsShown = await screen.findAllByTestId("change-row");
    const titles = rowsShown.map((r) => r.querySelector("[title]")?.textContent);
    expect(titles).toEqual(expect.arrayContaining(["0002_tags.sql", "0003_item_price.sql"]));
    const edited = rowsShown.find((r) => r.textContent.includes("0002_tags.sql"));
    expect(edited === undefined ? null : within(edited).getByTestId("label-chip").textContent).toBe("edited (history rewritten)");
  });
});
