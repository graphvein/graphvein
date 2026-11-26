import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { NO_LINKED_CALLER_HINT } from "@/api/categories";
import { ApiError } from "@/api/client";
import type { ApiClient, EndpointsParams } from "@/api/endpoints";
import type { StatusView } from "@/hooks/useStatus";
import { ApiSection } from "@/sections/api/ApiSection";
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
});

describe("ApiSection", () => {
  it("renders every root of each transport in the card and the strip, summing to the total", async () => {
    render(<ApiSection status={ok()} client={fakeClient()} />);
    const data = endpointsGolden("api-endpoints-production");
    for (const t of data.transports) {
      const card = await screen.findByTestId(`roots-${t.transport}`);
      const strip = screen.getByTestId(`strip-roots-${t.transport}`);
      const want = t.roots.map((r) => r.root);
      expect([...card.querySelectorAll("[data-root]")].map((e) => e.getAttribute("data-root"))).toEqual(want);
      expect([...strip.querySelectorAll("[data-root]")].map((e) => e.getAttribute("data-root"))).toEqual(want);
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
    expect(screen.getByTestId("strip-http").querySelector('[data-cell="never"]')?.getAttribute("title")).toBe(NO_LINKED_CALLER_HINT);
    expect(texts()).toMatch(/\d+ with no linked caller in production/);
    fireEvent.click(screen.getByRole("checkbox", { name: "Include tests" }));
    await waitFor(() => {
      expect(within(screen.getByTestId("card-http")).queryByText("No linked caller")).toBeTruthy();
    });
    expect(texts()).not.toMatch(/no linked caller in production|never called|not called/i);
    expect(screen.getByTestId("strip-http").textContent).toContain("no linked caller");
  });
});
