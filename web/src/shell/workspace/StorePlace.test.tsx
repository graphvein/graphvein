// Where a new index is kept (T068 part 3): the check's store and places,
// the confirmation's "Index kept in" row, Change… and its server checks,
// and the first-run line.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { parseStoreSettings } from "@/api/store";
import { checkBody, type Workspace } from "@/api/workspace";
import { AddFolderDialog, FirstRunCard } from "@/shell/workspace/AddFolder";
import { firstRunWords } from "@/shell/workspace/StorePlace";
import { WorkspaceProvider, type WorkspaceState } from "@/shell/workspace/WorkspaceRoot";
import { fakeStoreClient, fakeWorkspaceClient, rawStoreDoc, refusedCheck, STORES, storeCheck, storeSettings, workspaceDoc } from "@/test/workspaceFixtures";

function stateOf(ws: Workspace, patch: Partial<WorkspaceState> = {}): WorkspaceState {
  return {
    workspace: ws,
    entry: null,
    unknownLink: null,
    dismissUnknown: vi.fn(),
    switchTo: vi.fn(),
    refresh: vi.fn(),
    watch: vi.fn(),
    client: fakeWorkspaceClient(ws),
    addRequest: null,
    openAdd: vi.fn(),
    closeAdd: vi.fn(),
    ...patch,
  };
}

beforeEach(() => {
  window.history.replaceState(null, "", "/#/api");
});

describe("check body and parsing (WS-73, WS-76)", () => {
  it("sends a place only when one was chosen, and store_dir only with a typed folder", () => {
    expect(checkBody("/src/a")).toEqual({ path: "/src/a" });
    expect(checkBody("/src/a", { place: "inside" })).toEqual({ path: "/src/a", store_place: "inside" });
    expect(checkBody("/src/a", { place: "folder", dir: "/data/gv" })).toEqual({ path: "/src/a", store_place: "folder", store_dir: "/data/gv" });
  });

  it("reads the store, its place and the places of a ready check", () => {
    const c = storeCheck();
    expect(c.store?.place).toBe("outside");
    expect(c.store?.origin).toBe("default");
    expect(c.places?.default).toBe(`${STORES}/miniflux-3f9a1c27/index.db`);
    expect(c.places?.inside).toBe("/src/miniflux/.graphvein/index.db");
  });

  it("says where indexes go only from the server's answer", () => {
    expect(firstRunWords(storeSettings())).toEqual({ text: "Indexes are kept in Graphvein's own folder on this machine, not in your repositories:", path: STORES });
    const env = storeSettings({ default_dir: { path: "/big/gv", source: "env", env: "GRAPHVEIN_STORE_DIR", editable: false, reason: "set by the environment", error: null } });
    expect(firstRunWords(env)?.text).toContain("GRAPHVEIN_STORE_DIR");
    const team = storeSettings({ team_mode: { env: "GRAPHVEIN_POSTGRES_URL", set: true } });
    expect(firstRunWords(team)).toEqual({ text: "Indexes are kept in the Postgres database named by GRAPHVEIN_POSTGRES_URL in the environment of graphvein serve.", path: null });
    const unknown = storeSettings({ default_dir: { path: null, source: null, env: "GRAPHVEIN_STORE_DIR", editable: false, reason: null, error: "no home folder" } });
    expect(firstRunWords(unknown)).toBeNull();
  });
});

describe("first run (T063 A1)", () => {
  it("says where indexes are kept and links Settings' Storage", async () => {
    const ws = workspaceDoc([]);
    const state = stateOf(ws, { storeClient: fakeStoreClient(storeSettings()) });
    render(
      <WorkspaceProvider value={state}>
        <FirstRunCard />
      </WorkspaceProvider>,
    );
    const line = await screen.findByTestId("first-run-store");
    expect(line.textContent).toContain(`not in your repositories: ${STORES}.`);
    expect(screen.getByTestId("first-run-store-settings").getAttribute("href")).toBe("#/settings?at=storage");
  });

  it("shows no line when the server did not answer", async () => {
    const ws = workspaceDoc([]);
    const client = fakeStoreClient(storeSettings());
    client.get = () => Promise.reject(new Error("offline"));
    render(
      <WorkspaceProvider value={stateOf(ws, { storeClient: client })}>
        <FirstRunCard />
      </WorkspaceProvider>,
    );
    await waitFor(() => {
      expect(screen.getByTestId("first-run")).toBeTruthy();
    });
    expect(screen.queryByTestId("first-run-store")).toBeNull();
  });
});

function openConfirm(state: WorkspaceState) {
  render(
    <WorkspaceProvider value={state}>
      <AddFolderDialog />
    </WorkspaceProvider>,
  );
  fireEvent.change(screen.getByTestId("add-path"), { target: { value: "/src/miniflux" } });
  fireEvent.click(screen.getByTestId("add-continue"));
  return screen.findByTestId("add-confirm");
}

describe("Add confirm and Change… (T063 A2, A3)", () => {
  it("names the index file outside the folder, its place, and that nothing is written into the folder", async () => {
    const ws = workspaceDoc([]);
    const client = fakeWorkspaceClient(ws, { check: () => Promise.resolve(storeCheck()) });
    const confirm = await openConfirm(stateOf(ws, { client, addRequest: {} }));
    const kept = within(confirm).getByTestId("add-kept-in");
    expect(kept.textContent).toContain(`${STORES}/miniflux-3f9a1c27/index.db`);
    expect(within(kept).getByTestId("store-place").textContent).toBe("outside the folder · default");
    expect(screen.getByTestId("add-writes").textContent).toContain("writes nothing into this folder");
    expect(screen.getByTestId("add-change-place").textContent).toBe("Change…");
  });

  it("hides Change… when the folder's index exists already and says why", async () => {
    const ws = workspaceDoc([]);
    const check = storeCheck({ store: rawStoreDoc({ path: "/src/miniflux/.graphvein/index.db", origin: "in_folder", place: "inside", exists: true }), places: { choosable: false, reason: "an index exists already; it stays where it is", chosen: null, default: null, inside: null, folder: null } });
    const client = fakeWorkspaceClient(ws, { check: () => Promise.resolve(check) });
    const confirm = await openConfirm(stateOf(ws, { client, addRequest: {} }));
    expect(screen.queryByTestId("add-change-place")).toBeNull();
    expect(confirm.textContent).toContain("an index exists already; it stays where it is");
    expect(screen.getByTestId("add-writes").textContent).toContain("it only adds its .graphvein folder");
    expect(confirm.textContent).toContain("Git lists .graphvein/ as untracked");
  });

  it("checks the chosen place with the server and indexes with that check's confirm value", async () => {
    const ws = workspaceDoc([]);
    const inside = storeCheck({ confirm: "INSIDE", store: rawStoreDoc({ path: "/src/miniflux/.graphvein/index.db", origin: "in_folder", place: "inside" }), places: { choosable: true, reason: null, chosen: "inside", default: `${STORES}/miniflux-3f9a1c27/index.db`, inside: "/src/miniflux/.graphvein/index.db", folder: null } });
    const client = fakeWorkspaceClient(ws, { check: (_path, place) => Promise.resolve(place?.place === "inside" ? inside : storeCheck()) });
    const state = stateOf(ws, { client, addRequest: {} });
    await openConfirm(state);
    fireEvent.click(screen.getByTestId("add-change-place"));
    const chooser = await screen.findByTestId("add-place");
    expect(screen.getByTestId("add-dialog").textContent).toContain("Where should the index of miniflux be kept?");
    expect(within(chooser).getByTestId("place-default").textContent).toContain(`${STORES}/miniflux-3f9a1c27/index.db`);
    expect(within(chooser).getByTestId("place-inside").textContent).toContain("Git lists .graphvein/ as untracked");
    fireEvent.click(within(screen.getByTestId("place-inside")).getByRole("radio"));
    await waitFor(() => {
      expect(client.calls).toContain("check /src/miniflux TOKEN inside");
    });
    const index = screen.getByTestId("add-index");
    await waitFor(() => {
      expect((index as HTMLButtonElement).disabled).toBe(false);
    });
    fireEvent.click(index);
    await screen.findByTestId("add-progress");
    expect(client.calls.at(-1)).toBe("add /src/miniflux INSIDE TOKEN");
  });

  it("keeps Index disabled for a typed folder until the server accepts it, and shows its refusal", async () => {
    const ws = workspaceDoc([]);
    const refused = refusedCheck("inside_git", "the folder is inside a Git repository's .git folder");
    const client = fakeWorkspaceClient(ws, { check: (_path, place) => Promise.resolve(place?.place === "folder" ? refused : storeCheck()) });
    await openConfirm(stateOf(ws, { client, addRequest: {} }));
    fireEvent.click(screen.getByTestId("add-change-place"));
    await screen.findByTestId("add-place");
    fireEvent.change(screen.getByTestId("place-dir"), { target: { value: "/src/other/.git/x" } });
    expect(screen.getByTestId<HTMLButtonElement>("add-index").disabled).toBe(true);
    const refusal = await screen.findByTestId("place-refused");
    expect(refusal.getAttribute("data-code")).toBe("inside_git");
    expect(refusal.textContent).toContain("inside a Git repository's .git folder");
    expect(client.calls).toContain("check /src/miniflux TOKEN folder=/src/other/.git/x");
    expect(screen.getByTestId<HTMLButtonElement>("add-index").disabled).toBe(true);
    // Back returns to the confirmation with the place it showed.
    fireEvent.click(screen.getByTestId("add-place-back"));
    expect(within(await screen.findByTestId("add-confirm")).getByTestId("store-place").textContent).toBe("outside the folder · default");
  });
});

describe("store settings parsing", () => {
  it("keeps a place word the UI does not know", () => {
    const s = parseStoreSettings({
      api_version: 0,
      team_mode: { env: "GRAPHVEIN_POSTGRES_URL", set: false },
      sqlite_path: { env: "GRAPHVEIN_SQLITE_PATH", set: false, entry: null },
      default_dir: { path: STORES, source: "platform", env: "GRAPHVEIN_STORE_DIR", editable: true, reason: null, error: null },
      stores_file: { path: null, error: null },
      registry: { path: null, error: null },
      entries: [{ id: "a~1", kind: "repository", name: "a", folder: "/a", folder_found: false, current: false, store: null, movable: false, reason: "the folder is not at /a any more" }],
    });
    expect(s.entries[0]?.store).toBeNull();
    expect(s.entries[0]?.reason).toBe("the folder is not at /a any more");
  });
});
