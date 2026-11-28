// Settings' Storage section (T068 part 3; T063 A4-A8): the viewed
// index's row, Move… and its server checks, "New indexes are kept in"
// Change…, team mode, and Settings without an index.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { formatBytes, storePlaceWords, type StoreClient } from "@/api/store";
import type { Workspace } from "@/api/workspace";
import { FirstRunPage } from "@/shell/AppShell";
import { MachineSettings, StorageSection } from "@/sections/settings/StorageSection";
import { WorkspaceProvider, type WorkspaceState } from "@/shell/workspace/WorkspaceRoot";
import { fakeStoreClient, fakeWorkspaceClient, rawEntry, rawStoreDoc, rawStoreEntry, readyMove, refusedStore, STORES, storeSettings, workspaceDoc } from "@/test/workspaceFixtures";

function stateOf(ws: Workspace, storeClient: StoreClient, patch: Partial<WorkspaceState> = {}): WorkspaceState {
  return {
    workspace: ws,
    entry: ws.entries[0] ?? null,
    unknownLink: null,
    dismissUnknown: vi.fn(),
    switchTo: vi.fn(),
    refresh: vi.fn(),
    watch: vi.fn(),
    client: fakeWorkspaceClient(ws),
    storeClient,
    addRequest: null,
    openAdd: vi.fn(),
    closeAdd: vi.fn(),
    ...patch,
  };
}

const WS = workspaceDoc([rawEntry({ id: "miniflux~1", name: "miniflux", current: true, state: "open" })], "miniflux~1");

function section(client: StoreClient, state = stateOf(WS, client)) {
  render(
    <WorkspaceProvider value={state}>
      <StorageSection entryId="miniflux~1" />
    </WorkspaceProvider>,
  );
  return state;
}

beforeEach(() => {
  window.history.replaceState(null, "", "/#/settings");
});

describe("words", () => {
  it("words each place and keeps an unknown one", () => {
    const base = { kind: "sqlite", path: "/x", env: null, exists: true, size: null, stores_dir: STORES, error_code: null, error: null };
    expect(storePlaceWords({ ...base, origin: "default", place: "outside", dir_source: "platform" })).toBe("outside the folder · default");
    expect(storePlaceWords({ ...base, origin: "default", place: "outside", dir_source: "env" })).toBe("outside the folder · GRAPHVEIN_STORE_DIR");
    expect(storePlaceWords({ ...base, origin: "stores_file", place: "outside", dir_source: "platform" })).toBe("outside the folder · chosen");
    expect(storePlaceWords({ ...base, origin: "in_folder", place: "inside", dir_source: "platform" })).toBe("inside the folder");
    expect(storePlaceWords({ ...base, origin: "sqlite_path_env", place: "environment", dir_source: null })).toBe("environment · this run only");
    expect(storePlaceWords({ ...base, origin: null, place: "orbit", dir_source: null })).toBe("orbit");
    expect(formatBytes(50544640)).toBe("48.2 MB");
  });
});

describe("Storage section (T063 A5)", () => {
  it("shows the viewed index's file, place and size, the folder for new indexes, team mode and the workspace list", async () => {
    section(fakeStoreClient(storeSettings()));
    const row = await screen.findByTestId("storage-index");
    expect(row.textContent).toContain("Index of miniflux");
    expect(screen.getByTestId("storage-index-path").textContent).toBe(`${STORES}/miniflux-3f9a1c27/index.db`);
    expect(within(row).getByTestId("store-place").textContent).toBe("outside the folder · default");
    expect(row.textContent).toContain("48.2 MB");
    expect(screen.getByTestId("storage-move").textContent).toBe("Move…");
    expect(screen.getByTestId("storage-default-path").textContent).toBe(STORES);
    expect(screen.getByTestId("storage-default").textContent).toContain("Built-in default");
    expect(screen.getByTestId("storage-team").textContent).toContain("not set");
    expect(screen.getByTestId("storage-team").textContent).toContain("Indexes are SQLite files on this machine.");
    expect(screen.getByTestId("storage-registry").textContent).toContain("/home/dev/.config/graphvein/workspace.yaml");
  });

  it("offers moving an in-folder index out of the folder", async () => {
    const inside = storeSettings({ entries: [rawStoreEntry({ store: rawStoreDoc({ path: "/src/miniflux/.graphvein/index.db", origin: "in_folder", place: "inside", exists: true, size: 1024 }) })] });
    section(fakeStoreClient(inside));
    expect((await screen.findByTestId("storage-move")).textContent).toBe("Move out of the folder…");
    expect(screen.getByTestId("storage-index").textContent).toContain("Git lists .graphvein/ as untracked");
  });

  it("disables Move for GRAPHVEIN_SQLITE_PATH's file with the server's reason and names the entries that open from their own stores (A8)", async () => {
    const env = storeSettings({
      sqlite_path: { env: "GRAPHVEIN_SQLITE_PATH", set: true, entry: "miniflux~1" },
      entries: [
        rawStoreEntry({ store: rawStoreDoc({ path: "/scratch/miniflux.db", origin: "sqlite_path_env", place: "environment", env: "GRAPHVEIN_SQLITE_PATH", exists: true, size: 2048 }), movable: false, reason: "GRAPHVEIN_SQLITE_PATH names this file; unset it to move the index here, or move the file yourself" }),
        rawStoreEntry({ id: "gotify~2", name: "gotify", current: false }),
      ],
    });
    section(fakeStoreClient(env));
    const move = await screen.findByTestId("storage-move");
    expect((move as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByTestId("storage-move-reason").textContent).toContain("GRAPHVEIN_SQLITE_PATH names this file");
    expect(within(screen.getByTestId("storage-index")).getByTestId("store-place").textContent).toBe("environment · this run only");
    expect(screen.getByTestId("storage-others").textContent).toBe("gotify open from their own indexes while GRAPHVEIN_SQLITE_PATH is set.");
  });

  it("says team mode without a URL and offers no Move", async () => {
    const team = storeSettings({
      team_mode: { env: "GRAPHVEIN_POSTGRES_URL", set: true },
      entries: [rawStoreEntry({ store: rawStoreDoc({ kind: "postgres", path: null, env: "GRAPHVEIN_POSTGRES_URL", origin: "postgres", place: "postgres", exists: false, stores_dir: null, dir_source: null }), movable: false, reason: "team mode" })],
    });
    section(fakeStoreClient(team));
    const row = await screen.findByTestId("storage-index");
    expect(row.textContent).toContain("Postgres from GRAPHVEIN_POSTGRES_URL");
    expect(screen.queryByTestId("storage-move")).toBeNull();
    expect(screen.getByTestId("storage-team").textContent).toContain("set");
    expect(screen.getByTestId("storage-default").textContent).toContain("Not used in team mode");
  });
});

describe("Move… (T063 A6)", () => {
  it("checks the typed folder, then moves with the check's confirm value and reloads", async () => {
    const client = fakeStoreClient(storeSettings());
    const state = section(client);
    fireEvent.click(await screen.findByTestId("storage-move"));
    const dialog = await screen.findByTestId("storage-move-dialog");
    expect(dialog.textContent).toContain("Move the index of miniflux?");
    expect(screen.getByTestId<HTMLButtonElement>("storage-move-confirm").disabled).toBe(true);
    // An index already in Graphvein's folder is not checked before a
    // folder is typed.
    expect(client.calls.filter((c) => c.startsWith("checkMove"))).toEqual([]);
    fireEvent.change(screen.getByTestId("storage-move-dir"), { target: { value: "/data/graphvein" } });
    expect((await screen.findByTestId("storage-move-to")).textContent).toBe("/data/graphvein/miniflux-3f9a1c27/index.db");
    expect(client.calls).toContain("checkMove miniflux~1 /data/graphvein TOKEN");
    expect(dialog.textContent).toContain("48.2 MB");
    expect(screen.getByTestId("storage-copy-command").textContent).toBe("Copy the command instead");
    fireEvent.click(screen.getByTestId("storage-move-confirm"));
    const moved = await screen.findByTestId("storage-moved");
    expect(client.calls).toContain("move miniflux~1 /data/graphvein MOVE TOKEN");
    expect(moved.textContent).toContain("/data/graphvein/miniflux-3f9a1c27/index.db");
    expect(moved.textContent).toContain("3 runs");
    fireEvent.click(within(screen.getByTestId("storage-move-dialog")).getByText("Close"));
    await waitFor(() => {
      expect(client.calls.filter((c) => c === "get")).toHaveLength(2);
    });
    expect(state.refresh).toHaveBeenCalled();
  });

  it("checks the move back into Graphvein's folder at once for an in-folder index", async () => {
    const inside = storeSettings({ entries: [rawStoreEntry({ store: rawStoreDoc({ path: "/src/miniflux/.graphvein/index.db", origin: "in_folder", place: "inside", exists: true, size: 1024 }) })] });
    const client = fakeStoreClient(inside, { checkMove: () => Promise.resolve(readyMove(`${STORES}/miniflux-3f9a1c27/index.db`)) });
    section(client);
    fireEvent.click(await screen.findByTestId("storage-move"));
    expect((await screen.findByTestId("storage-move-to")).textContent).toBe(`${STORES}/miniflux-3f9a1c27/index.db`);
    expect(client.calls).toContain("checkMove miniflux~1  TOKEN");
  });

  it("shows the server's refusal and never enables Move index", async () => {
    const client = fakeStoreClient(storeSettings(), { checkMove: () => Promise.resolve(refusedStore("network_path", "a network path cannot hold an index")) });
    section(client);
    fireEvent.click(await screen.findByTestId("storage-move"));
    fireEvent.change(await screen.findByTestId("storage-move-dir"), { target: { value: "//server/share" } });
    const refused = await screen.findByTestId("storage-refused");
    expect(refused.getAttribute("data-code")).toBe("network_path");
    expect(refused.textContent).toContain("a network path cannot hold an index");
    expect(screen.getByTestId<HTMLButtonElement>("storage-move-confirm").disabled).toBe(true);
  });

  it("says the original is kept when the move fails", async () => {
    const { ApiError } = await import("@/api/client");
    const client = fakeStoreClient(storeSettings(), { move: () => Promise.reject(new ApiError("http", "move_failed: the copy's row counts differ", 500, "move_failed")) });
    section(client);
    fireEvent.click(await screen.findByTestId("storage-move"));
    fireEvent.change(await screen.findByTestId("storage-move-dir"), { target: { value: "/data/graphvein" } });
    await screen.findByTestId("storage-move-to");
    fireEvent.click(screen.getByTestId("storage-move-confirm"));
    const failed = await screen.findByTestId("storage-move-failed");
    expect(failed.getAttribute("data-code")).toBe("move_failed");
    expect(failed.textContent).toContain("The original index is kept.");
  });
});

describe("New indexes are kept in: Change…", () => {
  it("checks the folder and sets it with the check's confirm value", async () => {
    const client = fakeStoreClient(storeSettings());
    section(client);
    fireEvent.click(await screen.findByTestId("storage-default-change"));
    await screen.findByTestId("storage-default-dialog");
    fireEvent.change(screen.getByTestId("storage-default-dir"), { target: { value: "/data/gv" } });
    expect((await screen.findByTestId("storage-default-to")).textContent).toBe("/data/gv");
    fireEvent.click(screen.getByTestId("storage-default-confirm"));
    await waitFor(() => {
      expect(client.calls).toContain("setDefault /data/gv DEF TOKEN");
    });
    await waitFor(() => {
      expect(screen.queryByTestId("storage-default-dialog")).toBeNull();
    });
  });

  it("is read-only with the reason when GRAPHVEIN_STORE_DIR decides the folder", async () => {
    const env = storeSettings({ default_dir: { path: "/big/gv", source: "env", env: "GRAPHVEIN_STORE_DIR", editable: false, reason: "GRAPHVEIN_STORE_DIR is set in the environment of graphvein serve; it decides the folder until it is unset", error: null } });
    section(fakeStoreClient(env));
    const change = await screen.findByTestId("storage-default-change");
    expect((change as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByTestId("storage-default").textContent).toContain("it decides the folder until it is unset");
    expect(screen.getByTestId("storage-default").textContent).toContain("environment");
  });
});

describe("Settings without an index (T063 A4)", () => {
  it("shows this machine's Storage and where repository settings appear", async () => {
    const ws = workspaceDoc([]);
    const state = stateOf(ws, fakeStoreClient(storeSettings({ entries: [] })), { entry: null });
    render(
      <WorkspaceProvider value={state}>
        <MachineSettings />
      </WorkspaceProvider>,
    );
    expect(screen.getByTestId("settings-machine").textContent).toContain("No folder is indexed yet");
    expect(await screen.findByTestId("storage-default")).toBeTruthy();
    expect(screen.queryByTestId("storage-index")).toBeNull();
    fireEvent.click(within(screen.getByTestId("settings-repository-later")).getByText("Add a folder"));
    expect(state.openAdd).toHaveBeenCalled();
  });

  it("is what the first-run page shows on #/settings", async () => {
    window.history.replaceState(null, "", "/#/settings");
    const ws = workspaceDoc([]);
    render(
      <WorkspaceProvider value={stateOf(ws, fakeStoreClient(storeSettings({ entries: [] })), { entry: null })}>
        <FirstRunPage />
      </WorkspaceProvider>,
    );
    expect(await screen.findByTestId("settings-machine")).toBeTruthy();
    expect(screen.getByTestId("top-bar").textContent).toBe("Settings");
    expect(screen.queryByTestId("first-run")).toBeNull();
  });
});
