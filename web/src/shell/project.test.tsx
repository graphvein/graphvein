import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { requestRepository, setRequestRepository } from "@/api/client";
import { parseProject, type ProjectFetcher, type ProjectMember } from "@/api/project";
import { MemberListView, MemberRailButton, memberSettingsWords, NoCheckoutNote } from "@/shell/ProjectMembers";
import { ProjectRoot, REPOSITORY_KEY, useProject } from "@/shell/project";
import { project, projectBody } from "@/test/projectFixtures";

afterEach(() => {
  setRequestRepository(null);
  window.localStorage.clear();
});

const single: ProjectFetcher = () => Promise.resolve({ kind: "single" });
const shop: ProjectFetcher = () => Promise.resolve({ kind: "project", project: project() });

/** Shell stands in for AppShell: it records the repository its requests
 * name when it renders. */
function Shell({ repository }: { repository: string | null }) {
  const state = useProject();
  return (
    <div data-testid="shell" data-repository={repository ?? ""} data-request={requestRepository() ?? ""} data-explicit={String(state?.explicit ?? "")}>
      {state !== null && <MemberListView state={state} />}
    </div>
  );
}

describe("project mode in the shell (T022 B9)", () => {
  it("a single-repository server renders the shell as before: no member list, no repository parameter", async () => {
    render(<ProjectRoot fetcher={single} shell={(r) => <Shell repository={r} />} />);
    const shell = await screen.findByTestId("shell");
    expect(shell.getAttribute("data-repository")).toBe("");
    expect(shell.getAttribute("data-request")).toBe("");
    expect(screen.queryByTestId("project-members")).toBeNull();
  });

  it("a failed probe falls back to the single-repository shell, whose own status request reports the failure", async () => {
    render(<ProjectRoot fetcher={() => Promise.reject(new Error("down"))} shell={(r) => <Shell repository={r} />} />);
    expect((await screen.findByTestId("shell")).getAttribute("data-repository")).toBe("");
  });

  it("shows the first member only as a visible default, then the chosen member, remembered per project", async () => {
    render(<ProjectRoot fetcher={shop} shell={(r) => <Shell key={r} repository={r} />} />);
    const shell = await screen.findByTestId("shell");
    expect(shell.getAttribute("data-repository")).toBe("react");
    expect(shell.getAttribute("data-request")).toBe("react");
    expect(shell.getAttribute("data-explicit")).toBe("false");
    expect(screen.getByTestId("default-member").textContent).toBe("shown by default: choose a repository");
    // The note sits beside the highlighted row.
    expect(screen.getByTestId("default-member").parentElement?.querySelector('[aria-checked="true"]')?.getAttribute("data-repository")).toBe("react");
    const group = screen.getByRole("radiogroup", { name: "Repository" });
    expect(within(group).getByRole("radio", { name: /react/ }).getAttribute("aria-checked")).toBe("true");
    act(() => {
      fireEvent.click(within(group).getByRole("radio", { name: /express/ }));
    });
    await waitFor(() => {
      expect(screen.getByTestId("shell").getAttribute("data-repository")).toBe("express");
    });
    expect(screen.getByTestId("shell").getAttribute("data-request")).toBe("express");
    expect(screen.queryByTestId("default-member")).toBeNull();
    expect(window.localStorage.getItem(`${REPOSITORY_KEY}.shop`)).toBe("express");
  });

  it("restores a stored member, and ignores one that is no longer a member", async () => {
    window.localStorage.setItem(`${REPOSITORY_KEY}.shop`, "express");
    const { unmount } = render(<ProjectRoot fetcher={shop} shell={(r) => <Shell repository={r} />} />);
    expect((await screen.findByTestId("shell")).getAttribute("data-repository")).toBe("express");
    expect(screen.queryByTestId("default-member")).toBeNull();
    unmount();
    window.localStorage.setItem(`${REPOSITORY_KEY}.shop`, "gone");
    render(<ProjectRoot fetcher={shop} shell={(r) => <Shell repository={r} />} />);
    expect((await screen.findByTestId("shell")).getAttribute("data-repository")).toBe("react");
    expect(screen.getByTestId("default-member")).toBeTruthy();
  });

  it("the member list shows the project, members with freshness dots, the link status; the no-checkout note sits above the section", () => {
    const p = project({ link_pass: { state: "stale", pass: 3, links: 27, stale_links: 27, outcomes: {}, stale_reasons: ["react was indexed again after the link pass"] } });
    const state = { project: p, repository: "axum-ddd", explicit: true, select: () => undefined, refresh: () => undefined };
    render(<MemberListView state={state} />);
    expect(screen.getByTestId("project-name").textContent).toBe("shop");
    expect(screen.getByTestId("project-name").getAttribute("href")).toBe("#/project");
    const buttons = screen.getAllByTestId("member-button");
    expect(buttons.map((b) => b.getAttribute("data-repository"))).toEqual(["react", "express", "axum-ddd"]);
    expect(buttons.map((b) => b.getAttribute("aria-checked"))).toEqual(["false", "false", "true"]);
    expect(screen.getAllByTestId("member-dot").map((d) => d.getAttribute("data-state"))).toEqual(["fresh", "fresh", "none"]);
    expect(buttons[2]?.textContent).toContain("no local checkout");
    expect(buttons[2]?.getAttribute("title")).toContain("code peek, editor links and freshness are unavailable");
    const pass = screen.getByTestId("link-pass");
    expect(pass.getAttribute("href")).toBe("#/project");
    expect(pass.textContent).toBe("links stale · pass 3: re-index the project");
    expect(pass.getAttribute("title")).toContain("react was indexed again");
    expect(screen.queryByTestId("default-member")).toBeNull();
  });

  it("the link status reads links fresh · pass N · M links", () => {
    render(<MemberListView state={{ project: project(), repository: "react", explicit: true, select: () => undefined, refresh: () => undefined }} />);
    expect(screen.getByTestId("link-pass").textContent).toBe("links fresh · pass 2 · 27 links");
  });

  it("the no-checkout note shows for a member without a checkout", async () => {
    window.localStorage.setItem(`${REPOSITORY_KEY}.shop`, "axum-ddd");
    render(<ProjectRoot fetcher={shop} shell={() => <NoCheckoutNote />} />);
    expect((await screen.findByTestId("no-checkout")).textContent).toContain("axum-ddd has no local checkout here");
  });

  it("the rail opens a popover with the same member list, and a click switches", () => {
    const picked: string[] = [];
    const state = { project: project(), repository: "react", explicit: false, select: (r: string) => picked.push(r), refresh: () => undefined };
    render(<MemberRailButton state={state} />);
    const trigger = screen.getByTestId("members-rail");
    expect(trigger.getAttribute("aria-label")).toBe("Repository react in project shop");
    act(() => {
      fireEvent.click(trigger);
    });
    const pop = screen.getByTestId("members-popover");
    expect(within(pop).getAllByTestId("member-button")).toHaveLength(3);
    expect(within(pop).getByTestId("link-pass").textContent).toBe("links fresh · pass 2 · 27 links");
    expect(within(pop).getByTestId("default-member")).toBeTruthy();
    act(() => {
      fireEvent.click(within(pop).getByRole("radio", { name: /express/ }));
    });
    expect(picked).toEqual(["express"]);
  });

  it("a member whose index settings changed gets an amber dot with the keys in its tooltip; unknown is not unchanged", () => {
    const changed = { state: "changed", changed_keys: ["index.shape_depth"], unknown_keys: [], reason: null };
    const unknown = { state: "unknown", changed_keys: [], unknown_keys: [], reason: "no record" };
    const fresh = (settings: unknown) => ({ state: "fresh", checked_at: "x", changed_count: 0, changed_files: [], changed_files_truncated: false, settings });
    const body = projectBody();
    const members = body.members as Record<string, unknown>[];
    members[0] = { ...members[0], freshness: fresh(changed) };
    members[1] = { ...members[1], freshness: fresh(unknown) };
    const p = parseProject(body);
    render(<MemberListView state={{ project: p, repository: "react", explicit: true, select: () => undefined, refresh: () => undefined }} />);
    expect(screen.getAllByTestId("member-dot").map((d) => d.getAttribute("data-state"))).toEqual(["settings", "fresh", "none"]);
    const buttons = screen.getAllByTestId("member-button");
    expect(buttons[0]?.getAttribute("title")).toContain("settings changed: index.shape_depth. Re-index to apply");
    expect(buttons[0]?.textContent).toContain("settings changed");
    expect(buttons[1]?.getAttribute("title")).toContain("whether the index settings changed is unknown: no record");
    expect(memberSettingsWords(p.members[2] as ProjectMember)).toBeNull();
  });
});
