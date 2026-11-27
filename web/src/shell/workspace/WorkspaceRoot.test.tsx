// The workspace root (T038 WS-U): the viewed entry in the page URL,
// switching without a reload, the view memory restored on switching back,
// links to an entry this server does not have, and one re-index state.
import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, setRequestWorkspace, withRepository } from "@/api/client";
import type { IndexRunClient } from "@/api/indexRun";
import type { Status } from "@/api/status";
import type { WorkspaceEntry } from "@/api/workspace";
import type { StatusFetcher } from "@/hooks/useStatus";
import { useHash } from "@/router";
import { AppShell } from "@/shell/AppShell";
import { readLocation } from "@/shell/workspace/location";
import { readView, saveView } from "@/shell/workspace/memory";
import { UnknownLinkNotice } from "@/shell/workspace/Notices";
import { WorkspaceProvider, WorkspaceRoot, pickEntry, useWorkspace, type WorkspaceState } from "@/shell/workspace/WorkspaceRoot";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { runDoc } from "@/test/indexRunFixtures";
import { fakeSchemaClient, summaryGolden } from "@/test/schemaFixtures";
import { fakeWorkspaceClient, rawEntry, rawJob, sample, workspaceDoc } from "@/test/workspaceFixtures";

/** Shell shows what the page below the root would: the entry and hash. */
function Shell({ entry }: { entry: WorkspaceEntry | null }) {
  const hash = useHash();
  const ws = useWorkspace();
  return (
    <div>
      <UnknownLinkNotice />
      <p data-testid="shown">{entry === null ? "legacy" : `${entry.name} ${hash}`}</p>
      <button
        type="button"
        onClick={() => {
          ws?.switchTo("gotify~3");
        }}
      >
        to gotify
      </button>
      <button
        type="button"
        onClick={() => {
          ws?.switchTo("express~2");
        }}
      >
        to express
      </button>
    </div>
  );
}

function root(client: ReturnType<typeof fakeWorkspaceClient>) {
  return render(<WorkspaceRoot client={client} shell={(e) => <Shell key={e?.id ?? ""} entry={e} />} empty={() => <p data-testid="empty">first run</p>} />);
}

beforeEach(() => {
  window.localStorage.clear();
  window.history.replaceState(null, "", "/#/api");
  setRequestWorkspace(null);
});

describe("WorkspaceRoot", () => {
  it("names the current entry in the URL and on every per-index request", async () => {
    root(fakeWorkspaceClient(sample()));
    expect((await screen.findByTestId("shown")).textContent).toBe("express #/api");
    expect(readLocation().ws).toBe("express~2");
    expect(withRepository("/api/v0/status")).toBe("/api/v0/status?ws=express~2");
    expect(withRepository("/api/v0/endpoints?limit=5")).toBe("/api/v0/endpoints?limit=5&ws=express~2");
    // The workspace's own endpoints name no entry.
    expect(withRepository("/api/v0/workspace")).toBe("/api/v0/workspace");
  });

  it("opens the entry a link names, with its section and selection (URL restore)", async () => {
    window.history.replaceState(null, "", "/?ws=gotify~3#/schema?table=users");
    root(fakeWorkspaceClient(sample()));
    expect((await screen.findByTestId("shown")).textContent).toBe("gotify #/schema?table=users");
  });

  it("switches without a reload, remembers the view and restores it on switching back (WS-49)", async () => {
    window.history.replaceState(null, "", "/?ws=express~2#/api?sel=GET%20%2Fusers&tab=callers");
    root(fakeWorkspaceClient(sample()));
    await screen.findByText(/express #\/api\?sel=/);
    const before = window.history.length;
    act(() => {
      screen.getByRole("button", { name: "to gotify" }).click();
    });
    // A target never seen opens its default section, with one history entry.
    expect(screen.getByTestId("shown").textContent).toBe("gotify #/overview");
    expect(window.history.length).toBe(before + 1);
    expect(readView("express~2")?.hash).toBe("#/api?sel=GET%20%2Fusers&tab=callers");
    act(() => {
      window.location.hash = "#/schema?table=orders";
    });
    await waitFor(() => {
      expect(screen.getByTestId("shown").textContent).toBe("gotify #/schema?table=orders");
    });
    act(() => {
      screen.getByRole("button", { name: "to express" }).click();
    });
    expect(screen.getByTestId("shown").textContent).toBe("express #/api?sel=GET%20%2Fusers&tab=callers");
    act(() => {
      screen.getByRole("button", { name: "to gotify" }).click();
    });
    expect(screen.getByTestId("shown").textContent).toBe("gotify #/schema?table=orders");
  });

  it("switches between two entries on the same section (only the page query changes)", async () => {
    window.history.replaceState(null, "", "/?ws=express~2#/api");
    root(fakeWorkspaceClient(sample()));
    await screen.findByText("express #/api");
    act(() => {
      screen.getByRole("button", { name: "to gotify" }).click();
    });
    expect(readLocation().ws).toBe("gotify~3");
    // A target never seen opens the home page (T064).
    expect(screen.getByTestId("shown").textContent).toBe("gotify #/overview");
  });

  it("restores a remembered project member with its view", async () => {
    saveView("shop~1", { hash: "#/schema", member: "react" });
    window.history.replaceState(null, "", "/?ws=express~2#/api");
    const client = fakeWorkspaceClient(sample());
    render(
      <WorkspaceRoot
        client={client}
        shell={(e) => <Shell key={e?.id ?? ""} entry={e} />}
        empty={() => null}
        overlay={<SwitchButton id="shop~1" />}
      />,
    );
    await screen.findByText("express #/api");
    act(() => {
      screen.getByRole("button", { name: "to shop~1" }).click();
    });
    expect(readLocation()).toEqual({ ws: "shop~1", member: "react" });
    expect(window.location.hash).toBe("#/schema");
  });

  it("says a link names an entry this server does not have, shows the current one and drops the selection (WS-47)", async () => {
    window.history.replaceState(null, "", "/?ws=lost~9#/api?sel=GET%20%2Fx");
    root(fakeWorkspaceClient(sample()));
    expect((await screen.findByTestId("unknown-link")).textContent).toContain("lost~9");
    expect(screen.getByTestId("unknown-link").textContent).toContain("Showing express instead.");
    await waitFor(() => {
      expect(readLocation().ws).toBe("express~2");
    });
    expect(window.location.hash).toBe("#/api");
    expect(pickEntry(sample(null), "lost~9")).toEqual({ entry: null, unknown: "lost~9" });
  });

  it("shows the first-run state without an entry and the shell as before without a workspace endpoint", async () => {
    const { unmount } = root(fakeWorkspaceClient(workspaceDoc([])));
    expect(await screen.findByTestId("empty")).toBeTruthy();
    unmount();
    const legacy = fakeWorkspaceClient(sample());
    legacy.list = () => Promise.reject(new ApiError("http", "not_found", 404, "not_found"));
    root(legacy);
    expect((await screen.findByTestId("shown")).textContent).toBe("legacy");
    expect(withRepository("/api/v0/status")).toBe("/api/v0/status");
  });
});

function SwitchButton({ id }: { id: string }) {
  const ws = useWorkspace();
  return (
    <button
      type="button"
      onClick={() => {
        ws?.switchTo(id);
      }}
    >
      to {id}
    </button>
  );
}

describe("one re-index state (WS-52)", () => {
  it("asks for the run at once when the workspace list sees the viewed index's job change", async () => {
    const fetch = vi.fn<IndexRunClient["fetch"]>(() => Promise.resolve(runDoc("free")));
    const runClient: IndexRunClient = { fetch, start: () => Promise.reject(new Error("not expected")) };
    const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
    const idle = workspaceDoc([rawEntry({ id: "express~2", name: "express", current: true, state: "open" })], "express~2");
    const busy = workspaceDoc([rawEntry({ id: "express~2", name: "express", current: true, state: "open", job: rawJob({ id: "J7" }) })], "express~2");
    const state = (ws: typeof idle): WorkspaceState => ({
      workspace: ws,
      entry: ws.entries[0] ?? null,
      unknownLink: null,
      dismissUnknown: vi.fn(),
      switchTo: vi.fn(),
      refresh: vi.fn(),
      watch: vi.fn(),
      client: fakeWorkspaceClient(ws),
      addRequest: null,
      openAdd: vi.fn(),
      closeAdd: vi.fn(),
    });
    const shell = <AppShell fetcher={fresh} client={fakeClient()} schemaClient={fakeSchemaClient({ summary: summaryGolden("fixture") })} runClient={runClient} />;
    const view = render(<WorkspaceProvider value={state(idle)}>{shell}</WorkspaceProvider>);
    await waitFor(() => {
      expect(fetch).toHaveBeenCalledTimes(1);
    });
    view.rerender(<WorkspaceProvider value={state(busy)}>{shell}</WorkspaceProvider>);
    await waitFor(() => {
      expect(fetch).toHaveBeenCalledTimes(2);
    });
  });
});
