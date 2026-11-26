// The "changed since" markers in the API section through the shell: a
// marker on each row the API names, none while comparing is off or the
// base is refused (every view drops them together), and the item link.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { ChangeItem, ChangesClient } from "@/api/changes";
import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { COMPARE_KEY } from "@/shell/compare";
import { itemHash } from "@/shell/ChangeMarker";
import { endpointsGolden, fakeClient } from "@/test/apiFixtures";
import { apiError, fakeChangesClient, itemsOf } from "@/test/changesFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, summaryGolden } from "@/test/schemaFixtures";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));

const rows = endpointsGolden("api-endpoints-production").transports.flatMap((t) => t.endpoints);
const goldenItems = itemsOf("previous-rev1-rev2", { base: "previous", tests: true, limit: 500 }).items;

/** The golden's route items, renamed onto endpoints of the API golden:
 * a changed route on rows[0], an unknown-now route on rows[1], and the
 * removed route kept (no row). */
function routeItems(): ChangeItem[] {
  const changed = goldenItems.find((i) => i.kind === "route" && i.change === "changed");
  const removed = goldenItems.find((i) => i.kind === "route" && i.change === "removed");
  if (changed === undefined || removed === undefined || rows[0] === undefined || rows[1] === undefined) throw new Error("golden changed");
  return [
    { ...changed, id: rows[0].id, identity: rows[0].id },
    { ...changed, id: rows[1].id, identity: rows[1].id, change: "unknown_now", fields: [] },
    removed,
  ];
}

function client(o: { summaryError?: Error; calls?: string[] } = {}): ChangesClient {
  const base = fakeChangesClient({ ...(o.summaryError !== undefined ? { summary: () => o.summaryError as Error } : {}) });
  return {
    ...base,
    items: (p, opts) => {
      o.calls?.push(`items:${p.group ?? ""}`);
      if (p.group === "routes") return Promise.resolve({ ...itemsOf("previous-rev1-rev2", p), items: routeItems(), total: 3, next_cursor: null });
      return base.items(p, opts);
    },
  };
}

function show(c: ChangesClient) {
  window.history.replaceState(null, "", "#/api");
  return render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })} changesClient={c} />);
}

beforeEach(() => {
  window.localStorage.clear();
});

describe("markers in the API section", () => {
  it("marks the rows the API names, in their own words, and counts the removed ones apart", async () => {
    const calls: string[] = [];
    show(client({ calls }));
    const list = await screen.findByRole("listbox", { name: "Endpoints" });
    await waitFor(() => {
      expect(within(list).getAllByTestId("change-marker").length).toBe(2);
    });
    const markers = within(list).getAllByTestId("change-marker");
    const byItem = new Map(markers.map((m) => [m.getAttribute("data-item"), m]));
    expect(byItem.get(rows[0]?.id ?? "")?.textContent).toBe("paths changed");
    const unknown = byItem.get(rows[1]?.id ?? "");
    expect(unknown?.textContent).toBe("unknown now");
    expect(unknown?.getAttribute("data-tone")).toBe("knowledge");
    expect(unknown?.getAttribute("title")).toBe("unknown now since previous run: open in Changes");
    // The removed route and the golden's removed Tauri command are no rows:
    // they are counted in the footer.
    expect(screen.getByTestId("endpoints-gone").textContent).toBe("2 endpoints no longer in the index since previous run");
    // Only the view's groups were requested.
    expect(calls.sort()).toEqual(["items:ipc", "items:routes"]);
    // A click opens the item in Changes.
    fireEvent.click(byItem.get(rows[0]?.id ?? "") as HTMLElement);
    expect(window.location.hash).toBe(itemHash(routeItems()[0] as ChangeItem));
  });

  it("no markers while comparing is off", async () => {
    window.localStorage.setItem(COMPARE_KEY, JSON.stringify({ base: "previous", on: false }));
    const calls: string[] = [];
    show(client({ calls }));
    await screen.findByRole("listbox", { name: "Endpoints" });
    await screen.findByText(/Compare off/);
    expect(screen.queryAllByTestId("change-marker")).toEqual([]);
    expect(calls).toEqual([]);
  });

  it("an expired base drops every marker and requests no items", async () => {
    const calls: string[] = [];
    show(client({ calls, summaryError: apiError(410, "base_expired") }));
    await screen.findByRole("listbox", { name: "Endpoints" });
    await waitFor(() => {
      expect(screen.getByTestId("compare-chip").getAttribute("data-tone")).toBe("refused");
    });
    expect(screen.queryAllByTestId("change-marker")).toEqual([]);
    expect(screen.queryByTestId("endpoints-gone")).toBeNull();
    expect(calls).toEqual([]);
  });
});
