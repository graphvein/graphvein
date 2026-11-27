// T022 B9: the project server's goldens (internal/server/testdata
// project-users.golden.json and project-monorepo.golden.json) parsed by
// the UI's guards and rendered by its views, so the UI reads what
// `serve --project` really sends.
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import monorepo from "../../../internal/server/testdata/project-monorepo.golden.json";
import users from "../../../internal/server/testdata/project-users.golden.json";

import { parseImpact } from "@/api/changes";
import { callsPath, parseCalls, parseEndpointDetail, parseEndpoints } from "@/api/endpoints";
import { parseFlow } from "@/api/flow";
import { parseProject } from "@/api/project";
import { isNeverRow, neverCalledLabel, neverScoped } from "@/api/categories";
import { CoverageCards } from "@/sections/api/CoverageCards";
import { EndpointDetailView } from "@/sections/api/EndpointDetail";
import { rowAriaLabel } from "@/sections/api/EndpointList";
import { SummaryStrip } from "@/sections/api/SummaryStrip";
import { itemsFromCalls, itemsFromNever } from "@/sections/api/GapSheet";
import { ImpactBody, crossings } from "@/sections/changes/ImpactTab";
import { ProjectView } from "@/sections/project/ProjectSection";

const ASSUME = "matched across project repositories; base URL not proven";

function raw<T>(v: T): T {
  return structuredClone(v);
}

describe("project goldens in the UI (T022 B9)", () => {
  it("the backend route's endpoint lists the frontend caller from another repository, and is not never called", () => {
    const d = parseEndpointDetail(raw(users["3-endpoint-backend-route"]));
    expect(d.callers).toHaveLength(0);
    expect(d.project?.callers[0]?.repository).toBe("frontend");
    const summary = parseEndpoints(raw(users["2-endpoints-backend"])).transports.find((t) => t.transport === "http");
    expect(summary?.endpoints.some((e) => (e.project_caller_count ?? 0) > 0)).toBe(true);
    expect([summary?.never_called, summary?.never_called_in_project]).toEqual([2, 0]);
    render(<EndpointDetailView detail={d} summary={summary} openGap={() => undefined} tab="callers" />);
    expect(screen.queryByTestId("never-called")).toBeNull();
    expect(screen.getByTestId("no-repo-callers").textContent).toContain("No caller inside backend; 1 call of other repositories is linked");
    const row = screen.getAllByTestId("project-caller")[0] as HTMLElement;
    expect(screen.getAllByTestId("project-caller")).toHaveLength(2);
    expect(within(row).getByTestId("repo-badge").textContent).toBe("frontend");
    expect(within(row).getByTestId("project-assumption").textContent).toBe(`Assumption: ${ASSUME}`);
    expect(screen.getByRole("heading", { name: /Documented in other repositories/ })).toBeTruthy();
  });

  it("the Callers tab's +N chip has the count chip's height and padding and never wraps (T038)", () => {
    const d = parseEndpointDetail(raw(users["3-endpoint-backend-route"]));
    render(<EndpointDetailView detail={d} summary={undefined} openGap={() => undefined} tab="callers" onTab={() => undefined} />);
    const count = screen.getByTestId("caller-count");
    const plus = screen.getByTestId("project-caller-count");
    expect(plus.textContent).toBe("+1");
    for (const cls of ["inline-flex", "h-5", "px-1.5", "leading-none", "whitespace-nowrap", "flex-none", "border"]) {
      expect(count.classList.contains(cls), `count ${cls}`).toBe(true);
      expect(plus.classList.contains(cls), `+N ${cls}`).toBe(true);
    }
    expect(screen.getByTestId("tab-callers").classList.contains("whitespace-nowrap")).toBe(true);
  });

  it("a stale pass marks the endpoint's project links", () => {
    const d = parseEndpointDetail(raw(users["10-endpoint-stale"]));
    render(<EndpointDetailView detail={d} summary={undefined} openGap={() => undefined} tab="callers" />);
    expect(screen.getByTestId("project-pass-stale")).toBeTruthy();
    for (const row of screen.getAllByTestId("project-caller")) expect(row.getAttribute("data-stale")).toBe("true");
  });

  it("the frontend's endpoints count its calls linked across repositories", () => {
    const http = parseEndpoints(raw(users["2a-endpoints-frontend"])).transports.find((t) => t.transport === "http");
    expect(http?.project_linked_calls).toBe(2);
    // T022 MR-UI: the counts every view reads are the project's; the
    // repository's own are kept apart.
    expect(http?.calls.by_status).toEqual({ linked_across: 2 });
    expect(http?.calls.repository_by_status).toEqual({ unmatched: 2 });
    expect(http?.roots.map((r) => [r.by_status, r.repository_by_status])).toEqual([[{ linked_across: 2 }, { unmatched: 2 }]]);
  });

  it("MR-UI R1: the frontend's card headlines the project's counts, the repository's as secondary text", () => {
    const res = parseEndpoints(raw(users["2a-endpoints-frontend"]));
    render(<CoverageCards transports={res.transports} repository="frontend" scope={res.scope} actions={{ openGap: () => undefined, setRoot: () => undefined, activeGap: null, root: null }} />);
    expect(screen.getByTestId("pct-http").textContent).toBe("100%");
    expect(screen.getByTestId("card-http").textContent).toContain("linked in the project · 2 client calls");
    const across = document.querySelector('[data-cell="linked_across"]');
    expect(across?.textContent).toContain("2");
    expect(across?.textContent).toContain("Linked across repositories");
    const unmatched = document.querySelector('[data-cell="unmatched"]');
    expect(unmatched?.textContent).toContain("0");
    expect(unmatched?.textContent).toContain("Unmatched in the project");
    expect(screen.getByTestId("repository-counts-http").textContent).toContain("Inside frontend alone: 0% linked · 2 unmatched.");
    render(<SummaryStrip transports={res.transports} scope={res.scope} actions={{ openGap: () => undefined, setRoot: () => undefined, activeGap: null, root: null }} open={false} onOpen={() => undefined} />);
    expect(screen.getByTestId("strip-pct-http").textContent).toBe("100% linked");
    expect(screen.getByTestId("strip-pct-http").getAttribute("title")).toContain("inside this repository alone 0%");
  });

  it("MR-UI R1: gap sheets request the project-scope statuses", () => {
    expect(callsPath({ transport: "http", statuses: ["unmatched"], scope: "all", root: null, statusScope: "project" })).toBe("/api/v0/api/calls?transport=http&scope=all&status_scope=project&status=unmatched");
    expect(callsPath({ transport: "http", statuses: ["unmatched"], scope: "all", root: null })).toBe("/api/v0/api/calls?transport=http&scope=all&status=unmatched");
    const calls = parseCalls(raw(users["6c-calls-frontend-project-scope"]));
    const notes = itemsFromCalls(calls).map((i) => i.project);
    expect(notes).toHaveLength(2);
    for (const n of notes) expect(n?.startsWith("Linked across repositories to backend")).toBe(true);
  });

  it("flow and calls carry the project links", () => {
    const flow = parseFlow(raw(users["6a-flow-backend-route"]));
    expect(flow.project?.callers).toHaveLength(1);
    const calls = parseCalls(raw(users["6b-calls-frontend"]));
    const notes = itemsFromCalls(calls).map((i) => i.project);
    expect(notes.filter((n) => n !== null && n.startsWith("Linked across repositories to backend"))).not.toHaveLength(0);
  });

  it("project change impact labels the paths that cross repositories", () => {
    const data = parseImpact(raw(users["7-project-change-impact"]));
    const crossing = data.paths.filter((p) => crossings(p).length > 0);
    expect(crossing.length).toBeGreaterThan(0);
    const first = crossing[0];
    if (first === undefined) throw new Error("no crossing path");
    expect(crossings(first)[0]).toEqual({ from: "backend", to: "frontend", assumption: ASSUME });
    render(<ImpactBody data={data} paths={data.paths} more={null} />);
    expect(screen.getAllByTestId("impact-crossing")[0]?.textContent).toContain("crosses from backend into frontend");
    const badges = screen.getAllByTestId("repo-badge").map((b) => b.textContent);
    expect(badges).toContain("frontend");
    expect(badges).not.toContain("backend");
  });

  it("the project documents parse and render: no declarations, then the monorepo's scopes, ambiguity and conflict", () => {
    const fresh = parseProject(raw(users["1-project"]));
    expect(fresh.declarations.consumes).toHaveLength(0);
    const { unmount } = render(<ProjectView project={fresh} repository="frontend" />);
    expect(screen.getByTestId("no-declarations")).toBeTruthy();
    unmount();
    const stale = parseProject(raw(users["9-project-stale"]));
    expect(stale.link_pass.state).toBe("stale");
    const mono = parseProject(raw(monorepo.project));
    render(<ProjectView project={mono} repository="monorepo" />);
    expect(screen.getAllByTestId("declaration")).toHaveLength(6);
    expect(screen.getByTestId("ambiguity-group").getAttribute("data-row")).toBe("PL-S3");
    expect(screen.getByTestId("conflict").textContent).toContain("scope monorepo/users");
  });

  it("R1: never-called headlines are the project's in project mode, the repository's count secondary", () => {
    const res = parseEndpoints(raw(users["2-endpoints-backend"]));
    const http = res.transports.find((t) => t.transport === "http");
    if (http === undefined) throw new Error("no http transport");
    // The backend's two routes have no caller inside backend; the frontend calls both.
    expect(neverScoped(http)).toEqual({ count: 0, project: true, repositoryCount: 2 });
    expect(http.endpoints.filter(isNeverRow)).toHaveLength(0);
    expect(itemsFromNever(http.endpoints)).toHaveLength(0);
    for (const row of http.endpoints) expect(rowAriaLabel(row, res.scope)).not.toContain("no linked caller");
    render(<CoverageCards transports={res.transports} repository="backend" scope={res.scope} actions={{ openGap: () => undefined, setRoot: () => undefined, activeGap: null, root: null }} />);
    const cell = document.querySelector('[data-cell="never"]');
    expect(cell?.textContent).toContain("0");
    expect(cell?.textContent).toContain("No linked caller in the project");
    expect(cell?.textContent).not.toContain("production");
    expect(screen.getByTestId("project-never-http").textContent).toBe("2 without a caller inside backend (2 of them called from other repositories of the project).");
  });

  it("R1: a single-repository answer keeps its wording", () => {
    expect(neverScoped({ never_called: 3, never_called_in_project: null })).toEqual({ count: 3, project: false, repositoryCount: 3 });
    expect(neverCalledLabel("production", 3)).toBe("3 with no linked caller in production");
    expect(neverCalledLabel("production", 5, true)).toBe("5 with no linked caller in the project");
    expect(isNeverRow({ caller_count: 0, project_caller_count: null })).toBe(true);
    expect(isNeverRow({ caller_count: 0, project_caller_count: 2 })).toBe(false);
  });
});
