// The workspace UI's parts (T038 WS-U): words and dots, the list's
// grouping and indentation, the add flow's states, "Copy link to this
// view", the first-run page, the view memory and the page URL.
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import { parseIndexRun } from "@/api/indexRun";
import type { Workspace } from "@/api/workspace";
import { TooltipProvider } from "@/components/ui/tooltip";
import { FirstRunPage, ShellPlaceholder } from "@/shell/AppShell";
import { ProjectProvider } from "@/shell/project";
import { AddFolderDialog, FirstRunCard, indexWords, refusalHeading } from "@/shell/workspace/AddFolder";
import { COPY_LINK_LABEL, CopyLinkButton, viewDescription } from "@/shell/workspace/CopyLink";
import { locationSearch, readLocation, viewLink, writeLocation } from "@/shell/workspace/location";
import { MAX_ENTRIES, VIEW_MEMORY_KEY, applyScroll, readView, saveView } from "@/shell/workspace/memory";
import { UnknownLinkNotice } from "@/shell/workspace/Notices";
import { FILTER_FROM, WorkspaceTree, resetListState } from "@/shell/workspace/WorkspaceList";
import { WorkspaceProvider, type AddRequest, type WorkspaceState } from "@/shell/workspace/WorkspaceRoot";
import { checkAge, dotState, entryTitle, freshnessText, jobProgress, phaseWord } from "@/shell/workspace/words";
import { formatShortTime } from "@/lib/time";
import { project } from "@/test/projectFixtures";
import { addNotStarted, addResult, entry, fakeWorkspaceClient, rawEntry, rawJob, rawProject, readyCheck, refusedCheck, sample, workspaceDoc } from "@/test/workspaceFixtures";

function stateOf(ws: Workspace, id: string | null, patch: Partial<WorkspaceState> = {}): WorkspaceState {
  return {
    workspace: ws,
    entry: ws.entries.find((e) => e.id === id) ?? null,
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

function tree(state: WorkspaceState) {
  return render(
    <WorkspaceProvider value={state}>
      <WorkspaceTree state={state} opening={false} />
    </WorkspaceProvider>,
  );
}

beforeEach(() => {
  resetListState();
  window.localStorage.clear();
  window.history.replaceState(null, "", "/#/api");
});

describe("words and dots (WS-41, WS-44, WS-69)", () => {
  it("draws a hollow dot until a check finished and never green from anything else", () => {
    expect(dotState(null)).toBe("unchecked");
    expect(freshnessText(null)).toBe("not checked yet");
    expect(dotState({ state: "fresh", checked_at: "2026-11-24T10:00:00Z", changed_count: 0, error: null })).toBe("fresh");
    expect(dotState({ state: "stale", checked_at: "2026-11-24T10:00:00Z", changed_count: 1, error: null })).toBe("warn");
    // Amber only for stale: not indexed, unknown and a word the UI does
    // not know are the neutral hollow dot of "not checked yet".
    for (const s of ["not_indexed", "unknown", "brand_new_state"]) {
      expect(dotState({ state: s, checked_at: "2026-11-24T10:00:00Z", changed_count: null, error: null }), s).toBe("unchecked");
    }
  });

  it("keeps a state the UI does not know as the server's word", () => {
    expect(freshnessText({ state: "brand_new_state", checked_at: "2026-11-24T10:00:00Z", changed_count: null, error: null })).toMatch(/^brand_new_state, checked /);
    const e = entry(rawEntry({ id: "x~1", name: "x", kind: "monorepo", state: "sleeping" }));
    expect(entryTitle(e)).toContain("x (monorepo)");
    expect(entryTitle(e)).toContain("state: sleeping");
    expect(phaseWord("defragmenting")).toBe("defragmenting");
  });

  it("shows counts only when the run counted both numbers", () => {
    const counted = entry(rawEntry({ id: "a~1", name: "a", job: rawJob() })).job;
    const uncounted = entry(rawEntry({ id: "a~1", name: "a", job: rawJob({ phase: "scanning", progress: null }) })).job;
    if (counted === null || uncounted === null) throw new Error("fixture");
    expect(jobProgress(counted)).toEqual({ text: "212/412", fraction: 212 / 412 });
    expect(jobProgress(uncounted)).toEqual({ text: "scanning", fraction: null });
  });

  it("accepts a project-mode run document whose repository is empty (WS-40)", () => {
    const doc = parseIndexRun({ api_version: 0, repository: "", project: "shop", lock: { state: "free", error: null }, job: null });
    expect(doc.repository).toBe("");
  });
});

describe("Workspace list (grouping and indentation)", () => {
  it("lists projects first, the viewed project expanded with its members inside the guide, repositories aligned with project rows", () => {
    const ws = sample("shop~1");
    const state = stateOf(ws, "shop~1");
    render(
      <WorkspaceProvider value={state}>
        <ProjectProvider initial={project()} fetcher={() => Promise.resolve({ kind: "single" })}>
          {() => <WorkspaceTree state={state} opening={false} />}
        </ProjectProvider>
      </WorkspaceProvider>,
    );
    const list = screen.getByTestId("ws-list");
    const top = [...list.children].map((li) => li.getAttribute("data-testid"));
    expect(top).toEqual(["ws-project", "ws-repository", "ws-repository"]);
    const guide = screen.getByTestId("ws-guide");
    // Members one step in, behind a faint guide line, with the link status
    // inside the guide (WS-43).
    expect(guide.className).toContain("border-l");
    expect(guide.className).toContain("ml-[22px]");
    expect(within(guide).getAllByTestId("member-button").length).toBe(project().members.length);
    expect(within(guide).getByTestId("link-pass")).toBeTruthy();
    // A top-level repository's icon lines up with the project's (after
    // its chevron), not with the members.
    const repoButton = within(list.children[1] as HTMLElement).getByTestId("ws-entry");
    expect(repoButton.className).toContain("pl-6");
    expect(screen.getByRole("button", { name: /^Collapse shop/ }).getAttribute("aria-expanded")).toBe("true");
  });

  it("keeps other projects collapsed with their member count, and shows no link state for them (WS-43)", () => {
    const ws = sample("express~2");
    const state = stateOf(ws, "express~2");
    tree(state);
    const proj = screen.getAllByTestId("ws-project")[0];
    if (proj === undefined) throw new Error("no project row");
    expect(within(proj).queryByTestId("ws-guide")).toBeNull();
    expect(within(proj).getByTestId("ws-entry").textContent).toContain("2");
    fireEvent.click(screen.getByRole("button", { name: "Expand shop" }));
    expect(within(proj).getByTestId("ws-guide")).toBeTruthy();
    expect(screen.queryByTestId("link-pass")).toBeNull();
    // A member of another project switches to that project and member.
    const express = within(proj).getAllByTestId("member-button").find((b) => b.getAttribute("data-repository") === "express");
    if (express === undefined) throw new Error("no member");
    fireEvent.click(express);
    expect(state.switchTo).toHaveBeenCalledWith("shop~1", "express");
  });

  it("switches on a click on another entry and draws the dots from recorded checks only (WS-41)", () => {
    const state = stateOf(sample(), "express~2");
    tree(state);
    const dots = screen.getAllByTestId("ws-dot").map((d) => d.getAttribute("data-state"));
    // shop stale, express fresh, gotify never checked.
    expect(dots).toEqual(["warn", "fresh", "unchecked"]);
    const gotify = screen.getAllByTestId("ws-entry").find((b) => b.getAttribute("data-id") === "gotify~3");
    if (gotify === undefined) throw new Error("no row");
    expect(gotify.textContent).toContain("not checked yet");
    fireEvent.click(gotify);
    expect(state.switchTo).toHaveBeenCalledWith("gotify~3");
  });

  it("strikes through a folder that is gone and never opens it (WS-42)", () => {
    const ws = workspaceDoc([rawEntry({ id: "old~1", name: "old", state: "folder_gone", freshness: { state: "fresh", checked_at: "2026-11-24T10:00:00Z", changed_count: 0 } })]);
    const state = stateOf(ws, null);
    tree(state);
    const row = screen.getByTestId("ws-entry");
    expect((row as HTMLButtonElement).disabled).toBe(true);
    expect(row.querySelector(".line-through")).not.toBeNull();
    expect(row.textContent).toContain("folder not found at /src/old");
    expect(screen.getByTestId("ws-dot").getAttribute("data-state")).toBe("gone");
  });

  it("shows a running job's phase and run-counted progress on its row, never an estimate (WS-44)", () => {
    const ws = workspaceDoc([rawEntry({ id: "a~1", name: "alpha", job: rawJob() }), rawEntry({ id: "b~2", name: "beta", job: rawJob({ phase: "scanning", progress: null }) }), rawEntry({ id: "c~3", name: "gamma", job: rawJob({ state: "failed", ended_at: "2026-11-24T10:02:00Z", error: { code: "store_error", message: "disk full" } }) })]);
    tree(stateOf(ws, null));
    const progress = screen.getAllByTestId("ws-progress").map((p) => p.textContent);
    expect(progress).toEqual(["212/412", "scanning"]);
    expect(screen.getByTestId("ws-failed").textContent).toBe("store_error");
  });

  it(`offers a filter from ${String(FILTER_FROM)} entries and matches members too`, () => {
    const few = workspaceDoc(Array.from({ length: FILTER_FROM - 1 }, (_, i) => rawEntry({ id: `r~${String(i)}`, name: `repo${String(i)}` })));
    const { unmount } = tree(stateOf(few, null));
    expect(screen.queryByTestId("ws-filter")).toBeNull();
    unmount();
    const many = workspaceDoc([rawProject("p~1", "proj", ["needle"]), ...Array.from({ length: FILTER_FROM - 1 }, (_, i) => rawEntry({ id: `r~${String(i)}`, name: `repo${String(i)}` }))]);
    tree(stateOf(many, null));
    fireEvent.change(screen.getByTestId("ws-filter"), { target: { value: "needle" } });
    expect(screen.getAllByTestId("ws-project")).toHaveLength(1);
    expect(screen.queryAllByTestId("ws-repository")).toHaveLength(0);
    expect(screen.getByText("1 of 9")).toBeTruthy();
    expect(screen.getByTestId("ws-guide").textContent).toContain("needle");
  });

  it("says when the workspace has no entry", () => {
    tree(stateOf(workspaceDoc([]), null));
    expect(screen.getByTestId("ws-empty").textContent).toBe("No folder indexed yet.");
  });
});

function dialog(state: WorkspaceState) {
  return render(
    <WorkspaceProvider value={state}>
      <AddFolderDialog />
    </WorkspaceProvider>,
  );
}

describe("Add a folder (WS-44, WS-45, WS-46)", () => {
  it("checks the typed path, shows what would happen, and reads nothing before Index", async () => {
    const ws = sample();
    const client = fakeWorkspaceClient(ws);
    const req: AddRequest = {};
    const state = stateOf(ws, "express~2", { client, addRequest: req });
    dialog(state);
    fireEvent.change(screen.getByTestId("add-path"), { target: { value: "  /src/miniflux " } });
    expect(screen.getByTestId("index-command").textContent).toContain("graphvein index '/src/miniflux'");
    fireEvent.click(screen.getByTestId("add-continue"));
    const confirm = await screen.findByTestId("add-confirm");
    expect(client.calls).toEqual(["check /src/miniflux TOKEN"]);
    const d = screen.getByTestId("add-dialog");
    expect(d.textContent).toContain("Index /src/miniflux?");
    expect(confirm.textContent).toContain("Graphvein will read every file in it.");
    expect(confirm.textContent).toContain("/src/miniflux/.graphvein/index.db");
    expect(confirm.textContent).toContain("no index yet");
    expect(confirm.textContent).toContain("counted by the scan when it starts; no estimate before");
    expect(screen.getByTestId("add-copy-command").textContent).toBe("Copy the command instead");
    fireEvent.click(screen.getByTestId("add-index"));
    expect(await screen.findByTestId("add-progress")).toBeTruthy();
    expect(client.calls[1]).toBe("add /src/miniflux CONFIRM TOKEN");
    expect(state.watch).toHaveBeenCalledWith("miniflux~9");
  });

  it("follows the entry's job to the end and offers Open without switching by itself", async () => {
    const running = workspaceDoc([rawEntry({ id: "miniflux~9", name: "miniflux", job: rawJob() })]);
    const client = fakeWorkspaceClient(running, { add: () => Promise.resolve(addResult("miniflux~9", "miniflux")) });
    let state = stateOf(running, null, { client, addRequest: {} });
    const view = dialog(state);
    fireEvent.change(screen.getByTestId("add-path"), { target: { value: "/src/miniflux" } });
    fireEvent.click(screen.getByTestId("add-continue"));
    fireEvent.click(await screen.findByTestId("add-index"));
    await screen.findByTestId("add-progress");
    expect(screen.getByTestId("add-count").textContent).toBe("212 of 412 files");
    expect(screen.queryByTestId("add-open")).toBeNull();
    const done = workspaceDoc([rawEntry({ id: "miniflux~9", name: "miniflux", job: rawJob({ state: "committed", ended_at: "2026-11-24T10:05:00Z", phase: "finishing", run: { finished_at: "2026-11-24T10:05:00Z" } }) })]);
    state = { ...state, workspace: done };
    view.rerender(
      <WorkspaceProvider value={state}>
        <AddFolderDialog />
      </WorkspaceProvider>,
    );
    expect(screen.getByTestId("add-done")).toBeTruthy();
    expect(state.switchTo).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("add-open"));
    expect(state.switchTo).toHaveBeenCalledWith("miniflux~9");
    expect(state.closeAdd).toHaveBeenCalled();
  });

  it("names a project's members and a stored index's last run, and an unreadable store", async () => {
    const check = readyCheck({ kind: "project", name: "shop", members: [{ repository: "api", path: "/src/api", checkout: true }, { repository: "web", path: "/src/web", checkout: false }], index: { state: "unknown", last_run: null, error: "locked" } });
    const state = stateOf(sample(), null, { client: fakeWorkspaceClient(sample(), { check: () => Promise.resolve(check) }), addRequest: { path: "/src/shop" } });
    dialog(state);
    const members = await screen.findByTestId("add-members");
    expect(members.textContent).toContain("api");
    expect(members.textContent).toContain("no checkout at this path: not indexed");
    expect(screen.getByTestId("add-confirm").textContent).toContain("could not be read: locked");
    expect(indexWords({ state: "present", last_run: "2026-11-20T09:00:00Z", error: null })).toMatch(/^an index from .* is stored; this run replaces it$/);
  });

  it("words every refusal under a plain heading, an unknown code with the server's code", async () => {
    for (const code of ["path_not_found", "home_directory", "inside_registered", "a_new_code"]) {
      const state = stateOf(sample(), null, { client: fakeWorkspaceClient(sample(), { check: () => Promise.resolve(refusedCheck(code, `msg ${code}`, code === "inside_registered" ? "gotify~3" : null)) }), addRequest: { path: "/x" } });
      const { unmount } = dialog(state);
      const refused = await screen.findByTestId("add-refused");
      expect(refused.textContent).toContain(refusalHeading(code));
      expect(refused.textContent).toContain(`msg ${code}`);
      expect(refused.textContent).toContain("Nothing was read or indexed.");
      if (code === "inside_registered") expect(screen.getByRole("button", { name: "Open gotify" })).toBeTruthy();
      unmount();
    }
    expect(refusalHeading("a_new_code")).toBe("Refused: a_new_code");
  });

  it("says nothing was read when the guard refuses, and nothing indexed when the confirmation expired", async () => {
    const guard = new ApiError("http", "token_required: the session token is missing", 403, "token_required");
    let state = stateOf(sample(), null, { client: fakeWorkspaceClient(sample(), { check: () => Promise.reject(guard) }), addRequest: { path: "/x" } });
    const first = dialog(state);
    expect((await screen.findByTestId("add-error")).textContent).toContain("Nothing was read.");
    first.unmount();
    const expired = new ApiError("http", "confirm_required: check again", 409, "confirm_required");
    state = stateOf(sample(), null, { client: fakeWorkspaceClient(sample(), { add: () => Promise.reject(expired) }), addRequest: { path: "/src/miniflux" } });
    dialog(state);
    fireEvent.click(await screen.findByTestId("add-index"));
    const err = await screen.findByTestId("add-error");
    expect(err.getAttribute("data-code")).toBe("confirm_required");
    expect(err.textContent).toContain("nothing was indexed");
  });

  it("offers Open and Re-index for a folder already in the workspace", async () => {
    const reg = readyCheck({ outcome: "registered", entry: "gotify~3", confirm: null });
    const state = stateOf(sample(), null, { client: fakeWorkspaceClient(sample(), { check: () => Promise.resolve(reg) }), addRequest: { path: "/src/gotify" } });
    dialog(state);
    expect((await screen.findByTestId("add-registered")).textContent).toContain("gotify");
    fireEvent.click(screen.getByRole("button", { name: "Open it" }));
    expect(state.switchTo).toHaveBeenCalledWith("gotify~3");
  });
});

describe("entry rows: dots, check times and Index it (R1, R2, R5, R7, R8, R9)", () => {
  it("sizes every dot as a box, also on member rows outside a flex parent (R1)", () => {
    const ws = sample("shop~1");
    const state = stateOf(ws, "shop~1");
    render(
      <WorkspaceProvider value={state}>
        <ProjectProvider initial={project()} fetcher={() => Promise.resolve({ kind: "single" })}>
          {() => <WorkspaceTree state={state} opening={false} />}
        </ProjectProvider>
      </WorkspaceProvider>,
    );
    // jsdom does no layout; the browser measurement is in the task
    // record. An inline span ignores width and height, so every dot must
    // be a block-level box.
    const dots = screen.getAllByTestId("ws-dot");
    expect(dots.length).toBeGreaterThan(3);
    for (const d of dots) expect(d.className).toContain("inline-block");
  });

  it("words a viewed member's check with the server's checked_at, never the time of the render (R2)", () => {
    const ws = sample("shop~1");
    const state = stateOf(ws, "shop~1");
    const p = project();
    const at = "2026-11-03T14:40:00Z";
    p.members = p.members.map((m) => ({ ...m, freshness: { ...m.freshness, checked_at: at } }));
    render(
      <WorkspaceProvider value={state}>
        <ProjectProvider initial={p} fetcher={() => Promise.resolve({ kind: "single" })}>
          {() => <WorkspaceTree state={state} opening={false} />}
        </ProjectProvider>
      </WorkspaceProvider>,
    );
    const react = screen.getAllByTestId("member-button").find((b) => b.getAttribute("data-repository") === "react");
    expect(react?.getAttribute("title")).toContain(`checked ${formatShortTime(at)}`);
    expect(react?.getAttribute("title")).not.toContain(formatShortTime(new Date().toISOString()));
  });

  it("says how old a check is in the tooltip (R5)", () => {
    const now = Date.parse("2026-11-24T10:04:30Z");
    expect(checkAge("2026-11-24T10:00:00Z", now)).toBe("4 min ago");
    expect(checkAge("2026-11-24T10:04:10Z", now)).toBe("less than a minute ago");
    expect(checkAge("2026-11-24T07:00:00Z", now)).toBe("3 h ago");
    expect(checkAge("2026-11-24T10:05:00Z", now)).toBe("");
    expect(freshnessText({ state: "fresh", checked_at: "2026-11-24T10:00:00Z", changed_count: 0, error: null }, now)).toMatch(/^fresh, checked .* \(4 min ago\)$/);
  });

  it("offers Index it on an entry whose check found no index, and shows a refused run on its row", async () => {
    const raw = rawEntry({ id: "plain~7f3f56d5", name: "plain", freshness: { state: "not_indexed", checked_at: "2026-11-24T10:00:00Z", changed_count: null } });
    const ws = workspaceDoc([raw]);
    const client = fakeWorkspaceClient(ws);
    const state = stateOf(ws, null, { client });
    const view = tree(state);
    // One line that never wraps, under the row; the row keeps the name and
    // the neutral dot (amber only for stale).
    const line = screen.getByTestId("ws-not-indexed");
    expect(line.textContent).toBe("not indexed yet·Index it");
    expect(line.className).toContain("whitespace-nowrap");
    expect(line.className).not.toContain("flex-wrap");
    expect(within(screen.getByTestId("ws-entry")).getByTestId("ws-dot").getAttribute("data-state")).toBe("unchecked");
    expect(screen.getByTestId("ws-entry").querySelector(".truncate")).not.toBeNull();
    // The list's column may shrink below its rows' unwrapped width, so the
    // name truncates and the row end stays inside the sidebar.
    expect(screen.getByTestId("ws-list").className).toContain("grid-cols-[minmax(0,1fr)]");
    fireEvent.click(screen.getByRole("button", { name: "Index plain" }));
    await waitFor(() => {
      expect(state.watch).toHaveBeenCalledWith("plain~7f3f56d5");
    });
    expect(client.calls).toContain("run plain~7f3f56d5");
    view.unmount();
    // A run that is running or committed hides the action.
    const running = tree(stateOf(workspaceDoc([{ ...raw, job: rawJob() }]), null));
    expect(screen.queryByTestId("ws-not-indexed")).toBeNull();
    running.unmount();
    const refused = new ApiError("http", "index_locked: another process holds the index lock", 409, "index_locked");
    tree(stateOf(ws, null, { client: fakeWorkspaceClient(ws, { run: () => Promise.reject(refused) }) }));
    fireEvent.click(screen.getByTestId("ws-index-it"));
    expect((await screen.findByTestId("ws-index-error")).textContent).toBe("index_locked: another process holds the index lock");
  });

  it("says which entry holds a name shared in one store and where to rename one, with no freshness or Index it (WS-58)", () => {
    const message = "app and app (/b/app) both use the repository name \"app\" in the one database GRAPHVEIN_POSTGRES_URL names, from different folders (/a/app and /b/app), so either could show the other's facts; set another repository: in one folder's graphvein.yaml and index it again, or remove app from the workspace";
    const conflict = { code: "shared_store_conflict", message, kind: "repository", name: "app", entry: "app~b", entry_name: "app-b", entry_path: "/b/app" };
    const ws = workspaceDoc([rawEntry({ id: "app~a", name: "app", path: "/a/app", freshness: { state: "not_indexed", checked_at: "2026-11-24T10:00:00Z", changed_count: null }, conflict }), rawProject("shop~1", "shop", ["api"], { conflict: { ...conflict, kind: "project", name: "shop" } })]);
    tree(stateOf(ws, null));
    const lines = screen.getAllByTestId("ws-conflict");
    expect(lines).toHaveLength(2);
    const repo = lines.find((l) => l.closest("[data-id]")?.getAttribute("data-id") === "app~a");
    const proj = lines.find((l) => l.closest("[data-id]")?.getAttribute("data-id") === "shop~1");
    if (repo === undefined || proj === undefined) throw new Error("no conflict line");
    expect(repo.textContent).toContain("name app shared with app-b");
    expect(repo.textContent).toContain("rename in graphvein.yaml");
    expect(repo.getAttribute("title")).toBe(message);
    expect(proj.textContent).toContain("rename in graphvein-project.yaml");
    // No freshness word, no Index it, the neutral dot.
    expect(screen.queryByTestId("ws-not-indexed")).toBeNull();
    const row = screen.getAllByTestId("ws-entry").find((b) => b.getAttribute("data-id") === "app~a");
    if (row === undefined) throw new Error("no row");
    expect(row.textContent).not.toContain("not indexed");
    expect(row.textContent).toContain("GRAPHVEIN_POSTGRES_URL");
    expect(within(row).getByTestId("ws-dot").getAttribute("data-state")).toBe("unchecked");
  });

  it("offers Index it, not add again, for a registered folder without an index", async () => {
    const ws = workspaceDoc([rawEntry({ id: "plain~1", name: "plain", freshness: { state: "not_indexed", checked_at: "2026-11-24T10:00:00Z", changed_count: null } })]);
    const reg = readyCheck({ outcome: "registered", entry: "plain~1", confirm: null, resolved: "/src/plain", path: "/src/plain" });
    const client = fakeWorkspaceClient(ws, { check: () => Promise.resolve(reg) });
    const state = stateOf(ws, null, { client, addRequest: { path: "/src/plain" } });
    dialog(state);
    expect((await screen.findByTestId("add-registered")).textContent).toContain("not indexed yet");
    expect(screen.getByTestId("add-reindex").textContent).toBe("Index it");
    fireEvent.click(screen.getByTestId("add-reindex"));
    expect(await screen.findByTestId("add-progress")).toBeTruthy();
    expect(client.calls).toContain("run plain~1");
  });

  it("says the folder was added and not indexed when the run did not start, with the reason and a retry", async () => {
    const ws = sample();
    const client = fakeWorkspaceClient(ws, { add: () => Promise.resolve(addNotStarted("plain3~1", "plain3")) });
    const state = stateOf(ws, null, { client, addRequest: { path: "/src/plain3" } });
    dialog(state);
    fireEvent.click(await screen.findByTestId("add-index"));
    const box = await screen.findByTestId("add-not-started");
    const d = screen.getByTestId("add-dialog");
    expect(d.textContent).toContain("Added plain3 to the workspace; indexing did not start");
    expect(d.textContent).not.toContain("was not added");
    expect(box.getAttribute("data-code")).toBe("index_locked");
    expect(box.textContent).toContain("another process holds the index lock");
    fireEvent.click(screen.getByTestId("add-retry"));
    expect(await screen.findByTestId("add-progress")).toBeTruthy();
    expect(client.calls).toContain("run plain3~1");
  });

  it("names the registered entries a folder holds before it is indexed", async () => {
    const check = readyCheck({
      path: "/src/sc",
      resolved: "/src/sc",
      name: "sc",
      contains: [
        { id: "r1~1", kind: "repository", name: "r1", path: "/src/sc/r1" },
        { id: "shop~2", kind: "project", name: "shop", path: "/src/sc/proj" },
      ],
    });
    const state = stateOf(sample(), null, { client: fakeWorkspaceClient(sample(), { check: () => Promise.resolve(check) }), addRequest: { path: "/src/sc" } });
    dialog(state);
    const notice = await screen.findByTestId("add-contains");
    expect(notice.textContent).toContain("This folder holds 2 entries already in the workspace");
    expect(notice.textContent).toContain("r1 (repository) /src/sc/r1");
    expect(notice.textContent).toContain("shop (project) /src/sc/proj");
    expect(readyCheck().contains).toEqual([]);
  });
});

describe("Copy link to this view (WS-50)", () => {
  const writeText = vi.fn<(t: string) => Promise<void>>();
  beforeEach(() => {
    writeText.mockReset();
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("is an icon-only button with the app's tooltip on keyboard focus and the same name", async () => {
    render(
      <TooltipProvider>
        <CopyLinkButton entry="shop" section="API" />
      </TooltipProvider>,
    );
    const button = screen.getByRole("button", { name: COPY_LINK_LABEL });
    expect(button.textContent).toBe("");
    expect(button.getAttribute("title")).toBeNull();
    act(() => {
      button.focus();
    });
    expect((await screen.findAllByText(COPY_LINK_LABEL)).length).toBeGreaterThan(0);
  });

  it("copies the entry, member and the section's whole query, and says what it opens", async () => {
    writeText.mockResolvedValue(undefined);
    window.history.replaceState(null, "", "/?ws=shop~1&repository=express#/api?sel=GET%20%2Fx&tab=callers");
    render(
      <TooltipProvider>
        <CopyLinkButton entry="shop" section="API" />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: COPY_LINK_LABEL }));
    const toast = await screen.findByTestId("copy-toast");
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/?ws=shop%7E1&repository=express#/api?sel=GET%20%2Fx&tab=callers`);
    expect(toast.textContent).toBe("Link copied. It opens shop › express, API, with the selection, tab and filters.");
  });

  it("shows the link to copy by hand when the clipboard refuses", async () => {
    writeText.mockRejectedValue(new Error("denied"));
    window.history.replaceState(null, "", "/?ws=gotify~3#/schema");
    render(
      <TooltipProvider>
        <CopyLinkButton entry="gotify" section="Schema" />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: COPY_LINK_LABEL }));
    const toast = await screen.findByTestId("copy-toast");
    expect(toast.textContent).toContain("The clipboard is not available.");
    expect(toast.textContent).toContain("?ws=gotify%7E3#/schema");
  });

  it("describes a view without a query plainly", () => {
    expect(viewDescription("gotify", null, "Schema", "#/schema")).toBe("gotify, Schema");
    expect(viewDescription(null, null, "API", "#/api?filter=get")).toBe("API, with the tab and filters");
    expect(viewDescription("gotify", null, "Help", "#/help?at=start-here")).toBe("gotify, Help, at that heading");
  });
});

describe("first run and notices (WS-47, WS-48, WS-51)", () => {
  it("greys every section, claims no version and offers Add a folder with the command", () => {
    const state = stateOf(workspaceDoc([]), null);
    render(
      <WorkspaceProvider value={state}>
        <FirstRunPage />
      </WorkspaceProvider>,
    );
    const nav = screen.getByRole("navigation", { name: "Sections" });
    // Only Help is a link: it needs no index (T045); Settings is greyed.
    expect([...nav.querySelectorAll("a[href^='#/']")].map((a) => a.getAttribute("href"))).toEqual(["#/help"]);
    expect(screen.getByTestId("sidebar-versions").textContent).not.toMatch(/Extractors|Server/);
    expect(screen.getByTestId("first-run").textContent).toContain("Add a folder");
    expect(screen.getByTestId("index-command").textContent).toContain("graphvein index <folder>");
    expect(screen.getByTestId("first-run-help").getAttribute("href")).toBe("#/help?at=start-here");
    expect(screen.getByTestId("first-run-help").textContent).toBe("New to Graphvein? Start here");
  });

  it("shows Help on the first-run page, with no live example", () => {
    window.location.hash = "#/help?at=start-here";
    const state = stateOf(workspaceDoc([]), null);
    render(
      <WorkspaceProvider value={state}>
        <FirstRunPage />
      </WorkspaceProvider>,
    );
    expect(screen.getByTestId("help-section")).toBeTruthy();
    expect(screen.queryByTestId("first-run")).toBeNull();
    expect(screen.queryByTestId("help-example-pending")).toBeNull();
    expect(screen.queryByTestId("help-example-index")).toBeNull();
    expect(screen.queryByTestId("help-example-empty")).toBeNull();
    window.location.hash = "";
  });

  it("hands a typed path to the dialog, and links Help only when given", () => {
    const state = stateOf(workspaceDoc([]), null);
    render(
      <WorkspaceProvider value={state}>
        <FirstRunCard helpHref="#/help" />
      </WorkspaceProvider>,
    );
    expect(screen.getByTestId("first-run-help").textContent).toBe("New to Graphvein? Start here");
    fireEvent.change(screen.getByTestId("add-path"), { target: { value: "/src/app" } });
    fireEvent.click(screen.getByTestId("add-continue"));
    expect(state.openAdd).toHaveBeenCalledWith({ path: "/src/app" });
  });

  it("says a link names an entry this server does not have and what is shown instead", () => {
    const state = stateOf(sample(), "express~2", { unknownLink: "lost~9" });
    render(
      <WorkspaceProvider value={state}>
        <UnknownLinkNotice />
      </WorkspaceProvider>,
    );
    const notice = screen.getByTestId("unknown-link");
    expect(notice.textContent).toContain("lost~9");
    expect(notice.textContent).toContain("Showing express instead.");
  });

  it("shows skeletons and the opening line, nothing of another index, while an index opens", () => {
    const state = stateOf(sample(), "gotify~3");
    render(
      <WorkspaceProvider value={state}>
        <ShellPlaceholder />
      </WorkspaceProvider>,
    );
    expect(screen.getByTestId("shell-placeholder").getAttribute("aria-busy")).toBe("true");
    expect(screen.getByTestId("opening-note").textContent).toContain("Opening the index of gotify");
    expect(screen.getByTestId("sidebar-versions").textContent).not.toContain("express");
  });
});

describe("view memory (WS-49)", () => {
  it("remembers a view per entry id and keeps scroll positions not given", () => {
    saveView("a~1", { hash: "#/api?sel=x", member: "web", scroll: { "main#0": 400 } });
    saveView("a~1", { hash: "#/schema", member: "web" });
    expect(readView("a~1")).toMatchObject({ hash: "#/schema", member: "web", scroll: { "main#0": 400 } });
    expect(readView("b~2")).toBeNull();
  });

  it("keeps at most MAX_ENTRIES views, dropping the oldest", () => {
    const now = vi.spyOn(Date, "now");
    for (let i = 0; i <= MAX_ENTRIES; i++) {
      now.mockReturnValue(1000 + i);
      saveView(`e~${String(i)}`, { hash: "#/api", member: null });
    }
    now.mockRestore();
    expect(readView("e~0")).toBeNull();
    expect(readView(`e~${String(MAX_ENTRIES)}`)).not.toBeNull();
  });

  it("ignores a malformed store and remembers nothing when storage is blocked", () => {
    window.localStorage.setItem(VIEW_MEMORY_KEY, "{not json");
    expect(readView("a~1")).toBeNull();
    const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(() => {
      saveView("a~1", { hash: "#/api", member: null });
    }).not.toThrow();
    expect(readView("a~1")).toBeNull();
    get.mockRestore();
    set.mockRestore();
  });

  it("restores a scroll position only once the area is tall enough", () => {
    const el = document.createElement("div");
    el.setAttribute("data-scroll-memory", "main");
    document.body.append(el);
    Object.defineProperty(el, "scrollHeight", { value: 300, configurable: true });
    Object.defineProperty(el, "clientHeight", { value: 100, configurable: true });
    expect(applyScroll({ "main#0": 500 })).toEqual(["main#0"]);
    expect(applyScroll({ "main#0": 150 })).toEqual([]);
    expect(el.scrollTop).toBe(150);
    el.remove();
  });
});

describe("page URL (URL restore)", () => {
  it("reads and writes the entry and member beside the section's hash", () => {
    window.history.replaceState(null, "", "/?other=1#/api?sel=a");
    expect(readLocation()).toEqual({ ws: null, member: null });
    const before = window.history.length;
    writeLocation({ ws: "shop~1", member: "web" }, "#/schema?table=users", "push");
    expect(window.location.search).toBe("?other=1&ws=shop%7E1&repository=web");
    expect(window.location.hash).toBe("#/schema?table=users");
    expect(window.history.length).toBe(before + 1);
    expect(readLocation()).toEqual({ ws: "shop~1", member: "web" });
    expect(locationSearch(null, null, "?ws=a&repository=b")).toBe("");
    expect(viewLink({ ws: "a~1", member: null }, "#/api")).toBe(`${window.location.origin}/?ws=a%7E1#/api`);
  });

  it("waits for the dialog's add answer before following progress", async () => {
    // A slow add keeps the confirmation with a disabled "Starting…".
    let resolve: (v: ReturnType<typeof addResult>) => void = () => undefined;
    const add = () =>
      new Promise<ReturnType<typeof addResult>>((r) => {
        resolve = r;
      });
    const state = stateOf(sample(), null, { client: fakeWorkspaceClient(sample(), { add }), addRequest: { path: "/src/miniflux" } });
    dialog(state);
    fireEvent.click(await screen.findByTestId("add-index"));
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Starting…" }).disabled).toBe(true);
    act(() => {
      resolve(addResult("miniflux~9", "miniflux"));
    });
    await waitFor(() => {
      expect(screen.getByTestId("add-progress")).toBeTruthy();
    });
  });
});
