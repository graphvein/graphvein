import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { NO_LINKED_CALLER_HINT } from "@/api/categories";
import { ApiError } from "@/api/client";
import type { ApiClient, EndpointsParams } from "@/api/endpoints";
import type { StatusView } from "@/hooks/useStatus";
import { ApiSection } from "@/sections/api/ApiSection";
import { COVERAGE_OPEN_KEY } from "@/sections/api/SummaryStrip";
import { endpointsGolden, fakeClient, type Request } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";

function setHash(hash: string) {
  window.history.replaceState(null, "", hash);
}

function ok(name: "fresh" | "stale" = "fresh", finishedAt?: string): StatusView {
  const status = goldenStatus(name);
  if (finishedAt !== undefined && status.index.last_run !== null) status.index.last_run.finished_at = finishedAt;
  return { kind: "ok", status };
}

function endpointRequests(requests: Request[]): EndpointsParams[] {
  return requests.filter((r) => r.kind === "endpoints").map((r) => r.params as EndpointsParams);
}

beforeEach(() => {
  setHash("#/api");
  // Most tests read the coverage cards: open the disclosure (closed by
  // default; the T065 tests below clear it).
  window.localStorage.clear();
  window.localStorage.setItem(COVERAGE_OPEN_KEY, "open");
});

describe("ApiSection", () => {
  it("renders every root of each transport in the card, summing to the total", async () => {
    render(<ApiSection status={ok()} client={fakeClient()} />);
    const data = endpointsGolden("api-endpoints-production");
    for (const t of data.transports) {
      const card = await screen.findByTestId(`roots-${t.transport}`);
      const want = t.roots.map((r) => r.root);
      expect([...card.querySelectorAll("[data-root]")].map((e) => e.getAttribute("data-root"))).toEqual(want);
      expect(t.roots.reduce((n, r) => n + r.total, 0)).toBe(t.calls.total);
      for (const r of t.roots) expect(card.textContent).toContain(`${String(r.total)} total`);
    }
  });

  it("sets the root filter on every request and in the hash", async () => {
    const requests: Request[] = [];
    const id = "route:server/app.ts#GET%20%2Ft016d-items";
    setHash(`#/api?sel=${encodeURIComponent(id)}&gap=http:dynamic`);
    render(<ApiSection status={ok()} client={fakeClient(requests)} />);
    const card = await screen.findByTestId("roots-http");
    await screen.findByRole("dialog");
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Close" }));
    fireEvent.click(within(card).getByRole("button", { name: "web" }));
    await waitFor(() => {
      expect(window.location.hash).toContain("root=web");
    });
    await waitFor(() => {
      expect(endpointRequests(requests).at(-1)).toEqual({ scope: "production", root: "web" });
      expect(requests.filter((r) => r.kind === "endpoint").at(-1)?.params).toMatchObject({ id, root: "web" });
    });
    // A root count opens the gap sheet with that root.
    const card2 = await screen.findByTestId("roots-http");
    const webLine = [...card2.querySelectorAll("[data-root]")].find((e) => e.getAttribute("data-root") === "web");
    fireEvent.click(within(webLine as HTMLElement).getByRole("button", { name: /^3\s*dynamic$/ }));
    await waitFor(() => {
      expect(requests.filter((r) => r.kind === "calls").at(-1)?.params).toMatchObject({ transport: "http", statuses: ["dynamic"], root: "web" });
    });
    expect(window.location.hash).toContain("gap=http%3Adynamic");
  });

  it("switches production and all with Include tests", async () => {
    const requests: Request[] = [];
    render(<ApiSection status={ok()} client={fakeClient(requests)} />);
    const box = await screen.findByRole("checkbox", { name: "Include tests" });
    expect(endpointRequests(requests).at(-1)?.scope).toBe("production");
    fireEvent.click(box);
    await waitFor(() => {
      expect(endpointRequests(requests).at(-1)?.scope).toBe("all");
    });
    expect(window.location.hash).toBe("#/api?tests=1");
  });

  it("refetches when the status names another last run", async () => {
    const requests: Request[] = [];
    const client = fakeClient(requests);
    const { rerender } = render(<ApiSection status={ok()} client={client} />);
    await screen.findByTestId("card-http");
    expect(endpointRequests(requests)).toHaveLength(1);
    rerender(<ApiSection status={ok("fresh", "2026-11-03T15:00:00Z")} client={client} />);
    await waitFor(() => {
      expect(endpointRequests(requests)).toHaveLength(2);
    });
    // The data still names the old run; it is requested once per new run.
    await screen.findByTestId("card-http");
    rerender(<ApiSection status={ok("fresh", "2026-11-03T15:00:00Z")} client={client} />);
    expect(endpointRequests(requests)).toHaveLength(2);
  });

  it("does not refetch when the status poll names an older run than the data", async () => {
    const requests: Request[] = [];
    const client = fakeClient(requests);
    const { rerender } = render(<ApiSection status={ok("fresh", "2026-11-03T14:00:00Z")} client={client} />);
    await screen.findByTestId("card-http");
    rerender(<ApiSection status={ok("fresh", "2026-11-03T14:00:00Z")} client={client} />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(endpointRequests(requests)).toHaveLength(1);
  });

  it("never mixes an endpoint detail from another index run with the list", async () => {
    const requests: Request[] = [];
    const base = fakeClient(requests);
    // The detail always comes from a newer run than the list.
    const client: ApiClient = {
      ...base,
      endpoint: async (p, o) => {
        const d = await base.endpoint(p, o);
        if (d.index !== null) d.index.finished_at = "2026-11-03T15:00:00Z";
        return d;
      },
    };
    setHash(`#/api?sel=${encodeURIComponent("route:server/app.ts#GET%20%2Ft016d-items")}`);
    render(<ApiSection status={ok()} client={client} />);
    const note = await screen.findByTestId("run-mismatch");
    expect(note.textContent).toContain("another index run");
    // The list was requested again once for the newer run.
    expect(endpointRequests(requests)).toHaveLength(2);
    expect(requests.filter((r) => r.kind === "endpoint")).toHaveLength(2);
    expect(screen.queryByTestId("endpoint-detail")).toBeNull();
  });

  it("shows the detail once its run matches the list again", async () => {
    const requests: Request[] = [];
    const base = fakeClient(requests);
    let n = 0;
    const client: ApiClient = {
      ...base,
      endpoint: async (p, o) => {
        const d = await base.endpoint(p, o);
        n++;
        if (n === 1 && d.index !== null) d.index.finished_at = "2026-11-03T15:00:00Z";
        return d;
      },
    };
    setHash(`#/api?sel=${encodeURIComponent("route:server/app.ts#GET%20%2Ft016d-items")}`);
    render(<ApiSection status={ok()} client={client} />);
    expect(await screen.findByTestId("endpoint-detail")).toBeTruthy();
    expect(endpointRequests(requests)).toHaveLength(2);
    expect(screen.queryByTestId("run-mismatch")).toBeNull();
  });

  it("never mixes gap-sheet calls from another index run with the list counts", async () => {
    const requests: Request[] = [];
    const base = fakeClient(requests);
    const client: ApiClient = {
      ...base,
      calls: async (p, o) => {
        const r = await base.calls(p, o);
        if (r.index !== null) r.index.finished_at = "2026-11-03T15:00:00Z";
        return r;
      },
    };
    setHash("#/api?gap=http:dynamic");
    render(<ApiSection status={ok()} client={client} />);
    // The list is requested again once for the newer run; the sheet then
    // shows the note, not the calls.
    await waitFor(() => {
      expect(endpointRequests(requests)).toHaveLength(2);
    });
    const note = await screen.findByTestId("run-mismatch");
    expect(note.textContent).toContain("another index run");
    const dialog = screen.getByRole("dialog");
    expect(dialog.contains(note)).toBe(true);
    expect(endpointRequests(requests)).toHaveLength(2);
    expect(dialog.querySelectorAll("[data-item]")).toHaveLength(0);
  });

  it("does not request a raw status that contains a comma and says so in the sheet", async () => {
    const requests: Request[] = [];
    const base = fakeClient(requests);
    const client: ApiClient = {
      ...base,
      endpoints: async (p, o) => {
        const r = await base.endpoints(p, o);
        const http = r.transports.find((t) => t.transport === "http");
        if (http !== undefined) {
          http.calls.by_status = { ...http.calls.by_status, "a,b": 1 };
          http.calls.total += 1;
        }
        return r;
      },
    };
    setHash("#/api?gap=http:other");
    render(<ApiSection status={ok()} client={client} />);
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByTestId("unlisted-statuses").textContent).toContain("a,b");
    await act(async () => {
      await Promise.resolve();
    });
    for (const r of requests.filter((x) => x.kind === "calls")) {
      expect((r.params as { statuses: string[] }).statuses).not.toContain("a,b");
    }
  });

  it("clears a selection that left the index, with a notice", async () => {
    setHash(`#/api?sel=${encodeURIComponent("route:gone.ts#GET%20%2Fx")}`);
    render(<ApiSection status={ok()} client={fakeClient()} />);
    const notice = await screen.findByTestId("notice");
    expect(notice.textContent).toContain("no longer in the index");
    expect(window.location.hash).toBe("#/api");
    expect(screen.getByText("Select a route or command to see its handler and callers.")).toBeTruthy();
  });

  it("reports an index that kept changing, with Retry", async () => {
    let n = 0;
    const base = fakeClient();
    const client: ApiClient = {
      ...base,
      endpoints: (p, o) => {
        n++;
        return n === 1 ? Promise.reject(new ApiError("http", "index_changed: retry", 503, "index_changed")) : base.endpoints(p, o);
      },
    };
    render(<ApiSection status={ok()} client={client} />);
    expect((await screen.findByRole("alert")).textContent).toContain("The index changed twice while this view was loading");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      await Promise.resolve();
    });
    expect(await screen.findByTestId("card-http")).toBeTruthy();
  });

  it("keeps the data visible and adds a banner when stale", async () => {
    render(<ApiSection status={ok("stale")} client={fakeClient()} />);
    expect(await screen.findByTestId("card-http")).toBeTruthy();
    expect(screen.getByTestId("stale-banner").textContent).toContain("The index is stale");
  });

  it("never shows a non-linked status as linked in the coverage", async () => {
    const base = fakeClient();
    const client: ApiClient = {
      ...base,
      endpoints: () => Promise.resolve(endpointsGolden("api-statuses-endpoints")),
    };
    render(<ApiSection status={ok()} client={client} />);
    const card = await screen.findByTestId("card-http");
    // by_status: linked 3, LINKED 1, "" 1, weird 1 ... only the 3 are linked.
    const linked = card.querySelector('[data-cell="linked"]');
    expect(linked?.textContent).toMatch(/^3Linked/);
    const other = card.querySelector('[data-cell="other"]');
    expect(other?.textContent).toMatch(/^3Other/);
    expect(other?.getAttribute("title")).toBe("Raw statuses: (no status): 1, LINKED: 1, weird: 1");
    expect(screen.getAllByTestId("inconsistency")[0]?.textContent).toContain("marked linked but has no stored link");
  });

  it("says no linked caller in production everywhere in production scope, no linked caller with Include tests", async () => {
    render(<ApiSection status={ok()} client={fakeClient()} />);
    await screen.findByTestId("card-http");
    const texts = () => {
      const labels = [...document.querySelectorAll("[aria-label]")].map((e) => e.getAttribute("aria-label") ?? "");
      return [document.body.textContent, ...labels].join("\n");
    };
    // Graphvein cannot prove an absence of callers (unknown is never zero).
    expect(texts()).not.toMatch(/never called|not called/i);
    expect(within(screen.getByTestId("card-http")).getByText("No linked caller in production")).toBeTruthy();
    expect(screen.getByTestId("strip-http").textContent).toContain("no linked caller in production");
    expect(screen.getByTestId("strip-http").querySelector('[data-cell="never"]')?.getAttribute("title")).toMatch(new RegExp(`^\\d+ no linked caller in production\\. ${NO_LINKED_CALLER_HINT.replace(/[.;]/g, "\\$&")}$`));
    // The list's group header counts them; a row says "0 linked" with the
    // sentence in its tooltip (T073).
    expect(screen.getAllByTestId("group-no-caller")[0]?.textContent).toMatch(/^\d+ no linked caller in production$/);
    const row = screen.getAllByTestId("row-no-caller")[0];
    expect(row?.textContent).toBe("0 linked");
    expect(row?.getAttribute("title")).toBe(`No linked caller in production. ${NO_LINKED_CALLER_HINT}`);
    fireEvent.click(screen.getByRole("checkbox", { name: "Include tests" }));
    await waitFor(() => {
      expect(within(screen.getByTestId("card-http")).queryByText("No linked caller")).toBeTruthy();
    });
    expect(texts()).not.toMatch(/no linked caller in production|never called|not called/i);
    expect(screen.getByTestId("strip-http").textContent).toContain("no linked caller");
  });
});

// T065: compact transport strips, "none found" tabs and the disclosure.
describe("ApiSection strips (T065)", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  /** A client whose answers have no Tauri IPC and no server actions at
   * all, as on owncast or pdfcraft. */
  function httpOnly(requests?: Request[]): ApiClient {
    const base = fakeClient(requests);
    return {
      ...base,
      endpoints: async (p, o) => {
        const r = await base.endpoints(p, o);
        for (const t of r.transports) {
          if (t.transport === "http") continue;
          t.endpoint_count = 0;
          t.endpoints = [];
          t.never_called = 0;
          t.calls = { ...t.calls, total: 0, by_status: {} };
          t.roots = [];
        }
        return r;
      },
    };
  }

  it("shows one strip per transport with the disclosure closed by default, and remembers it", async () => {
    render(<ApiSection status={ok()} client={fakeClient()} />);
    const strip = await screen.findByTestId("strip-http");
    expect(screen.getByTestId("strip-tauri_ipc")).toBeTruthy();
    expect(screen.queryByTestId("coverage-cards")).toBeNull();
    const toggle = screen.getByRole("button", { name: "Coverage and roots" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(strip.textContent).toContain("express");
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const cards = screen.getByTestId("coverage-cards");
    expect(toggle.getAttribute("aria-controls")).toBe(cards.id);
    expect(within(cards).getByTestId("roots-http")).toBeTruthy();
    expect(window.localStorage.getItem(COVERAGE_OPEN_KEY)).toBe("open");
    fireEvent.click(toggle);
    expect(screen.queryByTestId("coverage-cards")).toBeNull();
    expect(window.localStorage.getItem(COVERAGE_OPEN_KEY)).toBeNull();
  });

  it("works with throwing storage", async () => {
    for (const m of ["getItem", "setItem", "removeItem"] as const) {
      vi.spyOn(Storage.prototype, m).mockImplementation(() => {
        throw new Error("blocked");
      });
    }
    try {
      render(<ApiSection status={ok()} client={fakeClient()} />);
      const toggle = await screen.findByRole("button", { name: "Coverage and roots" });
      fireEvent.click(toggle);
      expect(screen.getByTestId("coverage-cards")).toBeTruthy();
    } finally {
      vi.restoreAllMocks();
    }
  });

  it("turns empty transports into dashed none-found tabs with no strip and no doubled word", async () => {
    render(<ApiSection status={ok()} client={httpOnly()} />);
    await screen.findByTestId("strip-http");
    expect(screen.queryByTestId("strip-tauri_ipc")).toBeNull();
    expect(screen.queryByTestId("strip-server_action")).toBeNull();
    const group = screen.getByRole("group", { name: "Transport" });
    const ipc = within(group).getByRole("button", { name: /Tauri IPC/ });
    expect(ipc.getAttribute("data-none-found")).toBe("true");
    expect(ipc.textContent).toBe("Tauri IPCnone found");
    expect(ipc.getAttribute("title")).toBe(
      "Tauri IPC: none found in the indexed production files; Include tests also counts test files. Graphvein looks for #[tauri::command] functions and invoke calls.",
    );
    const actionsTab = within(group).getByRole("button", { name: /Server actions/ });
    expect(actionsTab.textContent).toBe("Server actionsnone found");
    expect(within(group).getByRole("button", { name: /HTTP/ }).hasAttribute("data-none-found")).toBe(false);
    // Label defect 10: "No Server actions actions or action uses".
    expect(document.body.textContent).not.toMatch(/actions actions|in this repository/);
    fireEvent.click(screen.getByRole("checkbox", { name: "Include tests" }));
    await waitFor(() => {
      expect(within(screen.getByRole("group", { name: "Transport" })).getByRole("button", { name: /Tauri IPC/ }).getAttribute("title")).toMatch(
        /^Tauri IPC: none found in the indexed files\. /,
      );
    });
    // A link to an empty transport says so once, in place of the strips,
    // and the list says the same, with no doubled word.
    fireEvent.click(within(screen.getByRole("group", { name: "Transport" })).getByRole("button", { name: /Server actions/ }));
    expect((await screen.findByTestId("none-found")).textContent).toBe("No server actions or action uses found in the indexed files.");
    expect(screen.getByText("Server actions: no actions found in the indexed files")).toBeTruthy();
    expect(screen.queryByTestId("summary-strip")).toBeNull();
    expect(document.body.textContent).not.toMatch(/actions actions|in this repository/);
  });

  it("keeps a transport with no endpoints but calls in another root as a strip, not none found", async () => {
    const base = httpOnly();
    const client: ApiClient = {
      ...base,
      endpoints: async (p, o) => {
        const r = await base.endpoints(p, o);
        const ipc = r.transports.find((t) => t.transport === "tauri_ipc");
        if (ipc !== undefined) ipc.roots = [{ root: "web", manifest: null, total: 2, by_status: { dynamic: 2 }, repository_by_status: null }];
        return r;
      },
    };
    render(<ApiSection status={ok()} client={client} />);
    await screen.findByTestId("strip-http");
    expect(within(screen.getByRole("group", { name: "Transport" })).getByRole("button", { name: /Tauri IPC/ }).hasAttribute("data-none-found")).toBe(false);
  });

  it("every count on the page follows Include tests (label defect 11)", async () => {
    const prod = endpointsGolden("api-endpoints-production");
    const all = endpointsGolden("api-endpoints-all");
    const counts = () => {
      const group = screen.getByRole("group", { name: "Transport" });
      return {
        tabs: within(group)
          .getAllByRole("button")
          .map((b) => b.textContent),
        strips: ["http", "tauri_ipc", "server_action"].map((t) => [
          screen.getByTestId(`strip-count-${t}`).textContent,
          screen.getByTestId(`strip-total-${t}`).textContent,
          ...[...screen.getByTestId(`strip-${t}`).querySelectorAll("[data-cell]")].map((c) => `${c.getAttribute("data-cell") ?? ""}:${c.querySelector("b")?.textContent ?? ""}`),
        ]),
        roots: [...screen.getByRole("combobox", { name: "Source root" }).querySelectorAll("option")].map((o) => o.textContent),
      };
    };
    const want = (data: typeof prod) => {
      const n = (t: string) => data.transports.find((x) => x.transport === t);
      const total = data.transports.reduce((s, t) => s + t.endpoint_count, 0);
      return { total, http: n("http"), ipc: n("tauri_ipc") };
    };
    render(<ApiSection status={ok()} client={fakeClient()} />);
    await screen.findByTestId("strip-http");
    const p = want(prod);
    const before = counts();
    expect(before.tabs[0]).toBe(`All${String(p.total)}`);
    expect(before.strips[0]?.slice(0, 2)).toEqual([`${String(p.http?.endpoint_count)} routes`, ` of ${String(p.http?.calls.total)} client calls`]);
    expect(before.strips[0]).toContain(`never:${String(p.http?.never_called)}`);
    expect(before.strips[1]?.[1]).toBe(` of ${String(p.ipc?.calls.total)} invoke sites`);
    fireEvent.click(screen.getByRole("checkbox", { name: "Include tests" }));
    const a = want(all);
    await waitFor(() => {
      expect(counts().tabs[0]).toBe(`All${String(a.total)}`);
    });
    const after = counts();
    expect(after.strips[0]?.slice(0, 2)).toEqual([`${String(a.http?.endpoint_count)} routes`, ` of ${String(a.http?.calls.total)} client calls`]);
    expect(after.strips[0]).toContain(`never:${String(a.http?.never_called)}`);
    expect(after.strips[1]?.[1]).toBe(` of ${String(a.ipc?.calls.total)} invoke sites`);
    // The two answers differ, so no count could have stayed from the
    // production answer.
    expect(after.strips).not.toEqual(before.strips);
    expect(after.tabs).not.toEqual(before.tabs);
    // A call whose path matches only test-scope routes is a production
    // call: its chip says what it is, not "test only".
    expect(screen.getByTestId("strip-http").querySelector('[data-cell="test_only"]')?.textContent).toContain("only test endpoints match");
  });

  it("drops the chip words below 1300 px but keeps them for screen readers and in the tooltip", async () => {
    render(<ApiSection status={ok()} client={fakeClient()} />);
    const strip = await screen.findByTestId("strip-http");
    const dyn = strip.querySelector('[data-cell="dynamic"]') as HTMLElement;
    expect(dyn.querySelector("span")?.className).toContain("max-[1299px]:sr-only");
    expect(dyn.getAttribute("title")).toMatch(/^3 dynamic\. dynamic: 3$/);
    expect(dyn.textContent).toContain("dynamic");
  });

  it("puts the index time at the right end of the title row", async () => {
    render(<ApiSection status={ok()} client={fakeClient()} />);
    const time = await screen.findByTestId("index-time");
    // The long words above 1300 px, "Index of" below (CSS decides).
    expect(time.textContent).toMatch(/^Data from the index ofIndex of /);
    expect(within(time).getByText("Data from the index of").className).toContain("max-[1299px]:hidden");
    expect(time.className).toContain("ml-auto");
  });

  it("says once that a repository without any API has none, in production scope (pdfcraft)", async () => {
    const base = httpOnly();
    const client: ApiClient = {
      ...base,
      endpoints: async (p, o) => {
        const r = await base.endpoints(p, o);
        for (const t of r.transports) {
          t.endpoint_count = 0;
          t.endpoints = [];
          t.never_called = 0;
          t.calls = { ...t.calls, total: 0, by_status: {} };
          t.roots = [];
        }
        return r;
      },
    };
    render(<ApiSection status={ok()} client={client} />);
    expect((await screen.findByTestId("none-found")).textContent).toBe(
      "No HTTP routes or client calls, Tauri IPC commands or invoke sites or server actions or action uses found in the indexed production files; Include tests also counts test files.",
    );
    expect(screen.getByText("No routes or commands found in the indexed production files")).toBeTruthy();
    expect(screen.queryByTestId("summary-strip")).toBeNull();
    expect(document.body.textContent).not.toMatch(/in this repository/);
  });
});
