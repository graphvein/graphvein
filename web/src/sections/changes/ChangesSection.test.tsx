// The Changes section and the compare chip through the shell, on the
// T019a goldens (changesFixtures.ts): counts in the UI equal the API's,
// every refusal state, the base picker, the branch progress card, and the
// principle (unknown now never removed, not extracted never zero).
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ChangesClient } from "@/api/changes";
import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { LABELS_KEY } from "@/lib/labelChoice";
import { COMPARE_KEY } from "@/shell/compare";
import { fakeClient } from "@/test/apiFixtures";
import { apiError, basesFor, fakeChangesClient, summaryOf, type FakeChanges } from "@/test/changesFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, summaryGolden } from "@/test/schemaFixtures";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));

function show(hash: string, changes: ChangesClient) {
  window.history.replaceState(null, "", hash);
  return render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })} changesClient={changes} />);
}

function fake(o: FakeChanges = {}) {
  const calls: string[] = [];
  return { client: fakeChangesClient({ calls, ...o }), calls };
}

const nav = () => screen.getByRole("navigation", { name: "Sections" });

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("Changes: summary, groups and counts equal the API", () => {
  it("shows both sides, the total, moves and not extracted groups (never zero)", async () => {
    const { client } = fake();
    show("#/changes", client);
    const summary = await screen.findByTestId("changes-summary");
    const s = summaryOf("previous-rev1-rev2").summary;
    expect(within(summary).getByTestId("summary-total").textContent).toBe(`${String(s.total)} changes`);
    const moves = s.groups.reduce((n, g) => n + g.moves, 0);
    expect(summary.textContent).toContain(`${String(moves)} moves counted`);
    expect(summary.textContent).toContain("Architecture: not extracted");
    expect(within(summary).getByTestId("partial").textContent).toContain("web/broken.ts");
    // The chip and the sidebar badge show the same total.
    expect(screen.getByTestId("compare-count").textContent).toBe(String(s.total));
    expect(within(nav()).getByRole("link", { name: `Changes, ${String(s.total)}` })).toBeTruthy();
  });

  it("lists every group in the sidebar with the summary's item count; not extracted has no count", async () => {
    const { client } = fake();
    show("#/changes", client);
    const sub = await screen.findByTestId("sidebar-sub");
    const s = summaryOf("previous-rev1-rev2").summary;
    const links = within(sub).getAllByRole("link");
    expect(links.length).toBe(s.groups.length);
    for (const g of s.groups) {
      const link = links.find((a) => a.getAttribute("href") === `#/changes?group=${g.group}`);
      expect(link?.textContent, g.group).toMatch(g.group === "architecture" ? /not extracted$/ : new RegExp(`${String(g.items)}$`));
    }
    // The first group with items is current.
    expect(within(sub).getByRole("link", { current: true }).textContent).toContain("Routes");
  });

  it("collapses the group list when another section is active", async () => {
    const { client } = fake();
    show("#/changes", client);
    await screen.findByTestId("sidebar-sub");
    act(() => {
      window.location.hash = "#/api";
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    await waitFor(() => {
      expect(screen.queryByTestId("sidebar-sub")).toBeNull();
    });
  });

  it("the list shows the group's items and the change chips with the summary's counts", async () => {
    const { client, calls } = fake();
    show("#/changes?group=client_calls", client);
    const list = await screen.findByRole("listbox", { name: "Client calls and links changes" });
    const g = summaryOf("previous-rev1-rev2").summary.groups.find((x) => x.group === "client_calls");
    expect(within(list).getAllByTestId("change-row").length).toBe(g?.items);
    const filters = screen.getByRole("group", { name: "Filter by change" });
    for (const n of Object.values(g?.changes ?? {})) expect(filters.textContent).toContain(String(n));
    fireEvent.click(within(filters).getByRole("button", { name: /^link lost/ }));
    await waitFor(() => {
      expect(calls.some((c) => c.includes('"change":"link_lost"'))).toBe(true);
    });
    expect(screen.getByTestId("list-footer").textContent).toContain("of 3");
  });
});

describe("Changes: item pane", () => {
  it("a changed route: base and head lines, changed fields, cause relink", async () => {
    const { client } = fake();
    show("#/changes?group=routes", client);
    const pane = await screen.findByTestId("item-pane");
    await within(pane).findByTestId("changed-fields");
    expect(within(pane).getByTestId("cause-chip").textContent).toBe("relink");
    expect(within(pane).getByTestId("loc-base").textContent).toContain("server/router.ts:5");
    expect(within(pane).getByTestId("loc-base").textContent).toContain("baseline location");
    expect(within(pane).getByTestId("changed-fields").textContent).toContain("/api/v2/items/{id}");
  });

  it("unknown now reads as unknown, never removed", async () => {
    const { client } = fake();
    show(`#/changes?group=code&sel=${encodeURIComponent("function:web/broken.ts#helperTwo")}`, client);
    const pane = await screen.findByTestId("item-pane");
    const banner = await within(pane).findByTestId("unknown-now");
    expect(banner.textContent).toContain("It is not removed");
    expect(within(pane).getByTestId("loc-head").textContent).toContain("state unknown in the head");
    expect(within(pane).getAllByTestId("change-chip")[0]?.textContent).toContain("unknown now");
  });

  it("a removed function shows its move hint as inferred and opens the other side", async () => {
    const { client } = fake();
    show(`#/changes?group=code&sel=${encodeURIComponent("function:server/slug.ts#slugify")}`, client);
    const hint = await screen.findByTestId("hint");
    expect(hint.textContent).toContain("Inferred");
    expect(hint.textContent).toContain("Both items stay removed");
    fireEvent.click(within(hint).getByRole("button", { name: "slugify" }));
    await waitFor(() => {
      expect(window.location.hash).toContain(encodeURIComponent("function:server/text.ts#slugify"));
    });
  });

  it("a lost link names the proven reason", async () => {
    const { client } = fake();
    const id = 'edge:["calls_route","client_call:web/api.ts#removeLegacy.DELETE%20%27%2Fapi%2Flegacy%27","route:server/app.ts#DELETE%20%2Fapi%2Flegacy"]';
    show(`#/changes?group=client_calls&sel=${encodeURIComponent(id)}`, client);
    const reason = await screen.findByTestId("reason");
    expect(reason.textContent).toContain("target removed");
    fireEvent.click(screen.getByRole("tab", { name: "Impact" }));
    expect((await screen.findByTestId("not-seed")).textContent).toContain("An edge item starts no impact traversal");
  });

  it("an edited migration is labelled", async () => {
    const { client } = fake();
    show(`#/changes?group=migrations&sel=${encodeURIComponent("migration:db/migrations/0002_tags.sql#migration")}`, client);
    const pane = await screen.findByTestId("item-pane");
    await waitFor(() => {
      expect(within(pane).getByTestId("label-chip").textContent).toBe("edited (history rewritten)");
    });
  });

  it("rows and tabs are keyboard reachable", async () => {
    const { client } = fake();
    show("#/changes?group=client_calls", client);
    const rows = await screen.findAllByTestId("change-row");
    rows[0]?.focus();
    fireEvent.keyDown(rows[0] as HTMLElement, { key: "ArrowDown" });
    await waitFor(() => {
      expect(rows[1]?.getAttribute("aria-selected")).toBe("true");
    });
    expect(document.activeElement).toBe(rows[1]);
    const change = screen.getByRole("tab", { name: "Change" });
    change.focus();
    fireEvent.keyDown(change, { key: "ArrowRight" });
    await waitFor(() => {
      expect(screen.getByRole("tab", { name: "Impact" }).getAttribute("aria-selected")).toBe("true");
    });
  });

  it("the raw tab requests the stored payloads", async () => {
    const { client, calls } = fake();
    show(`#/changes?group=routes&tab=raw`, client);
    await screen.findByTestId("raw-json");
    await waitFor(() => {
      expect(calls.some((c) => c.includes('"raw":true'))).toBe(true);
    });
  });
});

describe("Changes: impact panel", () => {
  it("I8 from a model column: wording, verification, assumptions, column use and the lower bound", async () => {
    const { client } = fake();
    show(`#/changes?group=model_tables&tab=impact&sel=${encodeURIComponent("column:models/app/models.py#Hero.age")}`, client);
    const body = await screen.findByTestId("impact-body");
    expect(body.textContent).toContain("may read this table");
    expect(body.textContent).toContain("reads this table");
    expect(body.textContent).toContain("may depend on");
    expect(within(body).getAllByTestId("verification").map((v) => v.textContent)).toEqual(expect.arrayContaining(["mapped, not verified", "verified by an oracle"]));
    // R5-4: the documented tooltips; an oracle never checked a real database.
    for (const v of within(body).getAllByTestId("verification")) {
      expect(v.getAttribute("title")).toBe(v.textContent === "verified by an oracle" ? "compared by an oracle on fixtures" : "mapped, not verified");
    }
    expect(within(body).getByTestId("assumption").textContent).toContain("TA-V3c");
    // T019b part 2: the id's text from the served catalogue, and the
    // Schema view's popover on the id.
    await waitFor(() => {
      // T025: the chip says the title (the id is its tooltip), the text follows.
      expect(within(body).getByTestId("assumption-chip").textContent).toBe("SQLAlchemy receiver");
      expect(within(body).getByTestId("assumption-chip").getAttribute("data-id")).toBe("TA-V3c");
      expect(within(body).getByTestId("assumption-text").textContent).toBe("A SQLAlchemy session or connection receiver is what its name says.");
    });
    fireEvent.click(within(body).getByTestId("assumption-chip"));
    const pop = await screen.findByTestId("why-popover");
    expect(within(pop).getByRole("heading").textContent).toBe("TA-V3c · SQLAlchemy receiver");
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByTestId("why-popover")).toBeNull();
    });
    expect(body.textContent).toContain("column use not extracted");
    expect(within(body).getByTestId("uses-found").textContent).toContain("uses found (a lower bound)");
    expect(within(body).getByTestId("may-also").textContent).toContain("largest_team");
    expect(body.textContent).not.toMatch(/(?<!\u201c)no users/);
  });

  it("blind spots, untraversed kinds and the depth cap of formatRow", async () => {
    const { client, calls } = fake();
    show(`#/changes?group=code&tab=impact&sel=${encodeURIComponent("function:server/items.ts#formatRow")}`, client);
    const unknowns = await screen.findByTestId("impact-unknowns");
    expect(unknowns.textContent).toContain("blind spot");
    expect(unknowns.textContent).toContain("server/blind.ts:4");
    // T025b: plain words; the rule id is in the tooltip.
    expect(unknowns.textContent).toContain("Architecture components: not extracted yet");
    expect(unknowns.textContent).not.toMatch(/\bI11\b/);
    expect(unknowns.querySelector("[data-relation=I11]")?.getAttribute("title")).toBe("impact rule I11");
    // Untraversed edge kinds come from the label table (row 20).
    expect(unknowns.textContent).toContain("Edge kinds no relation follows: contains 4");
    expect(within(unknowns).getByText("contains").getAttribute("title")).toBe("contains");
    expect(screen.getByTestId("impact-caps").textContent).toContain("Depth cap reached");
    fireEvent.click(screen.getByRole("radio", { name: "6" }));
    await waitFor(() => {
      expect(calls).toContain("impact:function:server/items.ts#formatRow:6");
    });
  });
});

describe("Changes: impact depth defaults (T042)", () => {
  const sel = encodeURIComponent("function:server/items.ts#formatRow");
  const showWith = (hash: string, changes: ChangesClient, impactDepth: number | null) => {
    window.history.replaceState(null, "", hash);
    return render(
      <AppShell
        fetcher={fresh}
        client={fakeClient()}
        schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })}
        changesClient={changes}
        uiFetcher={() => Promise.resolve({ api_version: 0, labels: "plain", impact_depth: impactDepth, chain_depth: null, error: null })}
      />,
    );
  };
  const depthCalls = (calls: string[]) => calls.filter((c) => c.startsWith("impact:function:server/items.ts#formatRow:"));

  it("offers 1 to 10; with nothing chosen the repository default applies, else 3", async () => {
    const { client, calls } = fake();
    showWith(`#/changes?group=code&tab=impact&sel=${sel}`, client, 5);
    const group = await screen.findByRole("radiogroup", { name: "Depth" });
    expect(within(group).getAllByRole("radio").map((r) => r.textContent)).toEqual(["1", "2", "3", "4", "5", "6", "7", "8", "9", "10"]);
    await waitFor(() => {
      expect(within(group).getByRole("radio", { name: "5" }).getAttribute("aria-checked")).toBe("true");
    });
    expect(screen.getByText("Depth").getAttribute("title")).toContain("repository default (graphvein.yaml ui.impact_depth)");
    // The walk waits for the repository default: one request, at 5.
    await waitFor(() => {
      expect(depthCalls(calls)).toEqual(["impact:function:server/items.ts#formatRow:5"]);
    });
  });

  it("the built-in 3 without a repository default", async () => {
    const { client, calls } = fake();
    showWith(`#/changes?group=code&tab=impact&sel=${sel}`, client, null);
    await waitFor(() => {
      expect(depthCalls(calls)).toEqual(["impact:function:server/items.ts#formatRow:3"]);
    });
    expect(screen.getByText("Depth").getAttribute("title")).toContain("built-in default (3)");
  });

  it("a choice is remembered in this browser and wins over the repository default", async () => {
    const { client, calls } = fake();
    const first = showWith(`#/changes?group=code&tab=impact&sel=${sel}`, client, 5);
    fireEvent.click(await screen.findByRole("radio", { name: "9" }));
    await waitFor(() => {
      expect(calls).toContain("impact:function:server/items.ts#formatRow:9");
    });
    expect(window.localStorage.getItem("graphvein.impact-depth.v1")).toBe("9");
    expect(window.location.hash).toContain("depth=9");
    first.unmount();
    calls.length = 0;
    showWith(`#/changes?group=code&tab=impact&sel=${sel}`, client, 5);
    await waitFor(() => {
      expect(depthCalls(calls)).toEqual(["impact:function:server/items.ts#formatRow:9"]);
    });
    expect(screen.getByText("Depth").getAttribute("title")).toContain("your last choice in this browser");
  });

  it("a link's depth wins for the page and is not stored", async () => {
    window.localStorage.setItem("graphvein.impact-depth.v1", "7");
    const { client, calls } = fake();
    showWith(`#/changes?group=code&tab=impact&depth=2&sel=${sel}`, client, 5);
    await waitFor(() => {
      expect(depthCalls(calls)).toEqual(["impact:function:server/items.ts#formatRow:2"]);
    });
    expect(screen.getByText("Depth").getAttribute("title")).toContain("set by this link");
    expect(window.localStorage.getItem("graphvein.impact-depth.v1")).toBe("7");
  });

  it("an invalid stored value is no choice", async () => {
    window.localStorage.setItem("graphvein.impact-depth.v1", "12");
    const { client, calls } = fake();
    showWith(`#/changes?group=code&tab=impact&sel=${sel}`, client, null);
    await waitFor(() => {
      expect(depthCalls(calls)).toEqual(["impact:function:server/items.ts#formatRow:3"]);
    });
  });
});

describe("Changes: refusals and empty states", () => {
  it("first run: no comparison yet", async () => {
    const { client } = fake({ summary: () => apiError(404, "base_unknown"), bases: basesFor("previous-rev1-rev2", { runs: 1 }) });
    show("#/changes", client);
    const page = await screen.findByTestId("changes-refusal");
    expect(page.textContent).toContain("No comparison yet");
    expect(screen.getByTestId("compare-chip").textContent).toContain("No base yet");
  });

  it.each([
    ["base_expired", 410, "Base run no longer covered", "(expired)"],
    ["reindex_needed", 409, "Re-index before comparing branches", "(re-index needed)"],
    ["git_unavailable", 503, "git not found", "(git not found)"],
    ["ref_objects_missing", 422, "Objects for the ref are missing", "(objects missing)"],
  ])("%s shows the refusal on the page and the chip", async (code, status, title, chip) => {
    const { client } = fake({ summary: () => apiError(status, code) });
    show("#/changes", client);
    const page = await screen.findByTestId("changes-refusal");
    expect(page.textContent).toContain(title);
    expect(screen.getByTestId("compare-chip").textContent).toContain(chip);
    expect(screen.getByTestId("compare-chip").getAttribute("data-tone")).toBe("refused");
    expect(screen.queryByTestId("compare-count")).toBeNull();
    expect(within(nav()).getByRole("link", { name: "Changes" })).toBeTruthy();
  });

  it("nothing changed: says so, with moves counted and not extracted groups not compared", async () => {
    const base = summaryOf("previous-rev1-rev2");
    // crafted from previous-rev1-rev2: no item in any group.
    const empty = { ...base, summary: { ...base.summary, total: 0, partial: false, groups: base.summary.groups.map((g) => ({ ...g, items: 0, changes: {}, causes: {}, kinds: {} })) } };
    const { client } = fake({ summary: () => empty });
    show("#/changes", client);
    const page = await screen.findByTestId("changes-nothing");
    expect(page.textContent).toContain("Nothing changed since previous run");
    expect(page.textContent).toContain("Architecture");
    expect(screen.getByTestId("compare-count").textContent).toBe("0");
  });
});

describe("Compare chip", () => {
  it("one click turns compare off for every view; the choice is remembered", async () => {
    const { client } = fake();
    show("#/changes", client);
    await screen.findByTestId("compare-count");
    fireEvent.click(screen.getByTestId("compare-off"));
    expect(await screen.findByTestId("changes-off")).toBeTruthy();
    expect(screen.getByTestId("compare-chip").textContent).toContain("Compare off");
    expect(within(nav()).getByRole("link", { name: "Changes" })).toBeTruthy();
    expect(JSON.parse(window.localStorage.getItem(COMPARE_KEY) ?? "{}")).toEqual({ base: "previous", on: false });
  });

  it("picks a pin from the menu and requests that base", async () => {
    const { client, calls } = fake();
    show("#/changes", client);
    await screen.findByTestId("compare-count");
    fireEvent.click(screen.getByTestId("compare-chip"));
    const menu = await screen.findByRole("dialog", { name: "Compare against" });
    fireEvent.click(within(menu).getByRole("button", { name: "release-1" }));
    fireEvent.click(within(menu).getByRole("button", { name: "Apply" }));
    await waitFor(() => {
      expect(calls).toContain("summary:pin:release-1");
    });
    expect(JSON.parse(window.localStorage.getItem(COMPARE_KEY) ?? "{}")).toEqual({ base: "pin:release-1", on: true });
    await waitFor(() => {
      expect(screen.getByTestId("compare-chip").textContent).toContain("Since pin release-1");
    });
  });

  it("pins and unpins from the menu", async () => {
    const { client, calls } = fake();
    show("#/changes", client);
    await screen.findByTestId("compare-count");
    fireEvent.click(screen.getByTestId("compare-chip"));
    const menu = await screen.findByRole("dialog", { name: "Compare against" });
    fireEvent.change(within(menu).getByRole("textbox", { name: "Pin name" }), { target: { value: "before-refactor" } });
    fireEvent.click(within(menu).getByRole("button", { name: "Pin run 2" }));
    await waitFor(() => {
      expect(calls).toContain(`pin:before-refactor:${summaryOf("previous-rev1-rev2").summary.head.run}`);
    });
    fireEvent.click(await within(menu).findByRole("button", { name: "Unpin release-1" }));
    await waitFor(() => {
      expect(calls).toContain("unpin:release-1");
    });
  });

  it("a stored base is used at load; Escape closes the menu and returns focus", async () => {
    window.localStorage.setItem(COMPARE_KEY, JSON.stringify({ base: "pin:release-1", on: true }));
    const { client, calls } = fake();
    show("#/changes", client);
    await waitFor(() => {
      expect(calls).toContain("summary:pin:release-1");
    });
    const chip = screen.getByTestId("compare-chip");
    fireEvent.click(chip);
    const menu = await screen.findByRole("dialog", { name: "Compare against" });
    fireEvent.keyDown(menu, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Compare against" })).toBeNull();
    });
    expect(document.activeElement).toBe(chip);
  });

  it("a branch comparison shows the progress card over the summary, then switches the base", async () => {
    const running = { id: "J", state: "running", started_at: "t", ended_at: null, base: "ref:main", commits: ["0d8c4f7e5b1a2c3d"], indexed: [], error: null };
    const committed = { ...running, state: "committed", ended_at: "t", indexed: ["0d8c4f7e5b1a2c3d"] };
    const { client, calls } = fake({ startRefs: running, refs: [null, committed] });
    show("#/changes", client);
    await screen.findByTestId("compare-count");
    const box = screen.getByTestId("summary-box");
    fireEvent.click(screen.getByTestId("compare-chip"));
    const menu = await screen.findByRole("dialog", { name: "Compare against" });
    fireEvent.change(within(menu).getByRole("textbox", { name: "Base ref" }), { target: { value: "main" } });
    fireEvent.click(within(menu).getByRole("button", { name: "Index and compare" }));
    const card = await within(box).findByTestId("ref-progress");
    expect(card.textContent).toContain("Branch main");
    expect(screen.getByTestId("compare-chip").textContent).toContain("indexing");
    // The panes stay while the job runs.
    expect(screen.getByTestId("changes-panes")).toBeTruthy();
    await waitFor(
      () => {
        expect(calls).toContain("summary:ref:main");
      },
      { timeout: 3000 },
    );
    expect(calls).toContain("startRefs:main:");
  });

  it("a failed branch job keeps its code", async () => {
    const failed = { id: "J", state: "failed", started_at: "t", ended_at: "t", base: "ref:main", commits: ["0d8c"], indexed: [], error: { code: "ref_objects_missing", message: "objects missing" } };
    const { client } = fake({ startRefs: failed });
    show("#/changes", client);
    await screen.findByTestId("compare-count");
    fireEvent.click(screen.getByTestId("compare-chip"));
    const menu = await screen.findByRole("dialog", { name: "Compare against" });
    fireEvent.change(within(menu).getByRole("textbox", { name: "Base ref" }), { target: { value: "main" } });
    fireEvent.click(within(menu).getByRole("button", { name: "Index and compare" }));
    const card = await screen.findByTestId("ref-progress");
    expect(card.textContent).toContain("ref_objects_missing");
    expect(screen.getByTestId("compare-chip").getAttribute("data-tone")).toBe("refused");
  });
});

describe("Changes: other goldens", () => {
  it("extractor upgrade shows the banner and the extractor-or-relink cause", async () => {
    const { client } = fake({ case: "extractor-upgrade" });
    show("#/changes?group=routes", client);
    expect((await screen.findByTestId("extractor-changed")).textContent).toContain("Extractor changed");
    await waitFor(() => {
      expect(screen.getByTestId("cause-chip").textContent).toBe("extractor or relink");
    });
  });

  it("a ref comparison names both commits' sides", async () => {
    window.localStorage.setItem(COMPARE_KEY, JSON.stringify({ base: "ref:rev1..rev3", on: true }));
    const { client } = fake({ case: "ref-rev1-rev3" });
    show("#/changes", client);
    const summary = await screen.findByTestId("changes-summary");
    expect(within(summary).getAllByTestId("side-ref").length).toBe(2);
    expect(screen.getByTestId("compare-chip").textContent).toContain("rev1..rev3");
  });
});

describe("Changes: a route's full path (T025)", () => {
  const ROUTE = "route:server/router.ts#GET%20%2Fitems%2F%3Aid";

  it("the list and the item header show the composed path with the router prefix, not the path as written", async () => {
    const { client } = fake();
    show(`#/changes?group=routes&sel=${encodeURIComponent(ROUTE)}`, client);
    const header = await screen.findByRole("heading", { level: 2, name: /api\/v2\/items/ });
    expect(within(header).getByTestId("route-path").textContent).toBe("GET /api/v2/items/{id}");
    expect(within(header).queryByTestId("route-marker")).toBeNull();
    // The path as written stays in the tooltip.
    expect(header.getAttribute("title")).toContain("as written in its file: GET /items/:id");
    const row = screen.getAllByTestId("change-row").find((r) => r.getAttribute("data-id") === ROUTE);
    expect(row?.textContent).toContain("GET /api/v2/items/{id}");
    expect(row?.textContent).not.toContain("GET /items/:id");
  });

  it("an impact path to a route shows its full path from route_paths", async () => {
    const { client } = fake();
    show(`#/changes?group=code&tab=impact&sel=${encodeURIComponent("function:server/handlers.ts#listItems")}`, client);
    const body = await screen.findByTestId("impact-body");
    const route = within(body)
      .getAllByTestId("impact-path")
      .find((p) => p.textContent.includes("GET /api/items"));
    expect(route).toBeDefined();
    expect(within(route as HTMLElement).queryByTestId("route-marker")).toBeNull();
    // Plain words with the confidence beside them (T041); the rule id in
    // a tooltip.
    expect(within(route as HTMLElement).getByTestId("prov").textContent).toBe("Fact · 1.0");
    expect(within(route as HTMLElement).getByTestId("prov").getAttribute("title")).toBe("provenance fact, confidence 1.0 (the weakest step). The number is a fixed rank per match rule, not a probability.");
    expect(route?.querySelector("[data-relation]")?.getAttribute("title")).toBe("impact rule I4");
    expect(route?.textContent).not.toMatch(/\bI4\b/);
  });
});

describe("Changes: technical labels (T025b)", () => {
  const ROUTE = "route:server/router.ts#GET%20%2Fitems%2F%3Aid";

  it("an impact path shows the rule id, raw kinds and provenance with confidence; plain words in tooltips", async () => {
    window.localStorage.setItem(LABELS_KEY, "technical");
    const { client } = fake();
    show(`#/changes?group=code&tab=impact&sel=${encodeURIComponent("function:server/handlers.ts#listItems")}`, client);
    const body = await screen.findByTestId("impact-body");
    const route = within(body)
      .getAllByTestId("impact-path")
      .find((p) => p.textContent.includes("GET /api/items")) as HTMLElement;
    expect(within(route).getByTestId("prov").textContent).toBe("fact 1.0");
    expect(within(route).getByTestId("prov").getAttribute("title")).toContain("Fact");
    expect(within(route).getByText("I4").getAttribute("title")).toBe("impact rule I4");
    expect(route.textContent).toContain("exposes_route");
    expect(route.textContent).not.toContain("exposes route");
    expect(body.textContent).toContain("confidence is the weakest step.");
    expect(within(body).getByTestId("impact-unknowns").textContent).toContain("Architecture components (I11): not extracted yet");
    // The route's full path is a fact, the same in both styles.
    expect(route.textContent).toContain("GET /api/items");
    expect(screen.queryByTestId("labels-banner")).toBeNull();
  });

  it("the item header and group heading show their contract rows", async () => {
    window.localStorage.setItem(LABELS_KEY, "technical");
    const { client } = fake();
    show(`#/changes?group=routes&sel=${encodeURIComponent(ROUTE)}`, client);
    const header = await screen.findByRole("heading", { level: 2, name: /api\/v2\/items/ });
    const head = header.parentElement as HTMLElement;
    expect(within(head).getByText("C3").getAttribute("title")).toBe("contract row C3");
    const group = screen.getByRole("heading", { level: 2, name: "Routes" }).parentElement as HTMLElement;
    expect(within(group).getByText("C3").getAttribute("title")).toBe("contract rows C3");
  });

  it("the I8 assumes line shows the id, then the title and text", async () => {
    window.localStorage.setItem(LABELS_KEY, "technical");
    const { client } = fake();
    show(`#/changes?group=model_tables&tab=impact&sel=${encodeURIComponent("column:models/app/models.py#Hero.age")}`, client);
    const body = await screen.findByTestId("impact-body");
    await waitFor(() => {
      expect(within(body).getByTestId("assumption-chip").textContent).toBe("TA-V3c");
      expect(within(body).getByTestId("assumption-text").textContent).toBe("SQLAlchemy receiver. A SQLAlchemy session or connection receiver is what its name says.");
    });
    expect(within(body).getByTestId("assumption-chip").getAttribute("title")).toBe("assumption TA-V3c: SQLAlchemy receiver");
  });
});

describe("label style onboarding (T025b)", () => {
  it("first launch: the banner sits above the content, the setting in Settings, not the sidebar", async () => {
    const { client } = fake();
    window.history.replaceState(null, "", "#/changes");
    render(
      <AppShell
        fetcher={fresh}
        client={fakeClient()}
        schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })}
        changesClient={client}
        uiFetcher={() => Promise.resolve({ api_version: 0, labels: null, error: null })}
      />,
    );
    const banner = await screen.findByTestId("labels-banner");
    expect(banner.closest("main")).not.toBeNull();
    expect(banner.closest("main")?.firstElementChild).toBe(banner);
    // The label choice moved from the sidebar footer to Settings (T039).
    expect(within(nav()).queryByTestId("labels-control")).toBeNull();
    expect(within(nav()).getByTestId("sidebar-settings").textContent).toContain("Settings");
    fireEvent.click(within(banner).getByTestId("labels-banner-technical"));
    expect(screen.queryByTestId("labels-banner")).toBeNull();
    expect(window.localStorage.getItem(LABELS_KEY)).toBe("technical");
  });
});
