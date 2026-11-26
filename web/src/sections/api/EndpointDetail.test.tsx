import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { EndpointDetail } from "@/api/endpoints";
import { EndpointDetailView } from "@/sections/api/EndpointDetail";
import type { DetailTab } from "@/sections/api/params";
import { detailGolden, endpointsGolden } from "@/test/apiFixtures";

const all = endpointsGolden("api-endpoints-all");

function view(d: EndpointDetail, openGap = vi.fn(), tab: DetailTab = "callers") {
  render(<EndpointDetailView detail={d} summary={all.transports.find((t) => t.transport === d.transport)} openGap={openGap} tab={tab} />);
  return openGap;
}

describe("EndpointDetailView", () => {
  it("shows a route whose prefix is unknown with the prefix marked, its reason and its UP callers' assumptions (T033)", () => {
    const d = detailGolden("unknown-prefix-endpoint");
    view(d, vi.fn(), "callers");
    expect(screen.getByRole("heading", { level: 2 }).textContent).toBe("…/upb/orders/{id}");
    expect(screen.getByTestId("prefix-unknown-badge").textContent).toBe("prefix unknown");
    expect(screen.queryByText("not mounted")).toBeNull();
    const list = screen.getByTestId("prefix-unknown-paths").textContent;
    expect(list).toContain("prefix unknown: a prefix on the way to the route is not a constant");
    expect(list).toContain("mount:server/api.go#API%20r.Route%28base%29");
    const reasons = [...document.querySelectorAll("[data-reason='prefix_assumed']")].map((e) => e.textContent);
    expect(reasons.some((t) => t.includes("UP-V1"))).toBe(true);
    expect(reasons.some((t) => t.includes("UP-V2"))).toBe(true);
    // Row UP-37: the Precise route label does not
    // cover unknown-prefix paths; the "…" block marks them unverified.
    expect(d.precision?.unverified).toContain("prefix-unknown paths");
    expect(screen.getByTestId("prefix-unknown-paths").querySelector("[data-testid='unverified-prefix-unknown paths']")).not.toBeNull();
  });

  it("shows a route whose every path an earlier mount takes as shadowed, never not mounted", () => {
    const d = structuredClone(detailGolden("unknown-prefix-endpoint"));
    d.endpoint.attributes = { ...d.endpoint.attributes, paths: [], unresolved_prefix_paths: undefined, shadowed_paths: [{ path: "/sh/a", by: 'app.mount("/sh",shadow_sub) (api/app/main.py:228)' }] };
    view(d, vi.fn(), "callers");
    expect(screen.getByTestId("shadowed-badge").textContent).toBe("shadowed");
    expect(screen.queryByText("not mounted")).toBeNull();
    expect(screen.getByTestId("shadowed-paths").textContent).toContain('taken first byapp.mount("/sh",shadow_sub) (api/app/main.py:228)');
  });

  // JX-F4: a route with no path of any kind is "path withheld" when the
  // server marks it (row CR-44, owncast's ANY /* on the root router), else
  // "path not known"; never "not mounted".
  it("shows a route with no path as path withheld or path not known, never not mounted", () => {
    const d = structuredClone(detailGolden("unknown-prefix-endpoint"));
    d.endpoint.attributes = { ...d.endpoint.attributes, paths: [], unresolved_prefix_paths: undefined };
    const { unmount } = render(<EndpointDetailView detail={d} summary={undefined} openGap={vi.fn()} tab="callers" />);
    expect(screen.getByTestId("no-path-badge").textContent).toBe("path not known");
    expect(screen.getByTestId("no-path-badge").title).toContain("not proof that it is unreachable");
    expect(screen.queryByText("not mounted")).toBeNull();
    unmount();
    render(<EndpointDetailView detail={{ ...d, path_withheld: true }} summary={undefined} openGap={vi.fn()} tab="callers" />);
    expect(screen.getByTestId("no-path-badge").textContent).toBe("path withheld");
    expect(screen.getByTestId("no-path-badge").title).toContain("another pattern of the ServeMux");
    expect(screen.queryByText("not mounted")).toBeNull();
  });

  it("names the assumption a composed path rests on (RO-V2)", () => {
    const d = structuredClone(detailGolden("unknown-prefix-endpoint"));
    d.endpoint.attributes = {
      ...d.endpoint.attributes,
      unresolved_prefix_paths: undefined,
      paths: [{ template: "/z/b", conditional: true, resolution: "literal", assumptions: ["RO-V2"], may_answer_first: ["catchSub (web/app.js:109)"] }],
    };
    view(d, vi.fn(), "callers");
    const chip = screen.getByTestId("path-assumption");
    expect(chip.textContent).toBe("assumes RO-V2");
    expect(chip.getAttribute("title")).toContain("a mounted app's catch-all may answer first");
    expect(chip.getAttribute("title")).toContain("catchSub (web/app.js:109)");
  });

  it("shows a Next.js route whose basePath is not read as prefix unknown (T033 U6)", () => {
    const d = detailGolden("unknown-prefix-next-endpoint");
    view(d, vi.fn(), "callers");
    expect(screen.getByRole("heading", { level: 2 }).textContent).toBe("…/api/search");
    expect(screen.getByTestId("prefix-unknown-paths").textContent).toContain("the Next.js basePath is not read as a literal");
  });

  it("shows the state without a linked caller with its two buttons and no dynamic call attached", () => {
    const d = detailGolden("api-endpoint-route-never");
    const openGap = view(d);
    const box = screen.getByTestId("never-called");
    // The golden is production scope: test calls are not callers there.
    expect(box.textContent).toContain(
      "No production client call in fixture is linked to this route (test calls are excluded; turn on Include tests to see them).",
    );
    expect(screen.getByText("No linked caller in production")).toBeTruthy();
    expect(box.textContent).toContain("Unresolved calls may still reach it.");
    expect(box.textContent).toContain("Graphvein does not guess.");
    expect(box.textContent).toContain("3 dynamic calls are not linked to any route; one of them might be the caller.");
    expect(document.querySelectorAll("tr[data-caller]")).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Review 3 dynamic calls" }));
    expect(openGap).toHaveBeenLastCalledWith("dynamic");
    expect(screen.queryByRole("button", { name: /never called|not called/i })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^All \d+ with no linked caller in production$/ }));
    expect(openGap).toHaveBeenLastCalledWith("never");
  });

  it("keeps the plain wording without a linked caller with Include tests (scope all)", () => {
    const d = detailGolden("api-endpoint-route-never");
    d.scope = "all";
    view(d);
    expect(screen.getByTestId("never-called").textContent).toContain("No client call in fixture is linked to this route.");
    expect(screen.getByText("No linked caller")).toBeTruthy();
    expect(screen.queryByText("No linked caller in production")).toBeNull();
    expect(screen.getByRole("button", { name: /^All \d+ with no linked caller$/ })).toBeTruthy();
  });

  it("marks the facts a precise Go route label does not cover unverified (T023 GR10)", () => {
    const d = detailGolden("api-endpoint-route");
    d.precision = { label: "precise", row: "PL-F10", reason: null, unverified: ["handler", "middleware", "conditional", "scope"] };
    view(d, vi.fn(), "overview");
    expect(screen.getByTestId("detail-precision-unverified").textContent).toBe("unverified: handler, middleware, conditional, scope");
    expect(screen.getAllByTestId("unverified-handler").length).toBeGreaterThan(0);
  });

  it("marks nothing unverified for a best-effort label", () => {
    view(detailGolden("api-endpoint-route"), vi.fn(), "overview");
    expect(screen.queryByTestId("detail-precision-unverified")).toBeNull();
    expect(screen.queryByTestId("unverified-handler")).toBeNull();
  });

  it("shows a documenting OpenAPI operation", () => {
    view(detailGolden("api-endpoint-route"), vi.fn(), "overview");
    const o = screen.getByTestId("openapi-state");
    expect(o.textContent).toContain("Documented");
    expect(o.textContent).toContain("listItems");
    expect(o.textContent).toContain("Inferred · 0.9");
    expect(o.textContent).toContain("responses 200");
  });

  it("says not in any spec, or no spec indexed", () => {
    view(detailGolden("api-endpoint-route-never"), vi.fn(), "overview");
    expect(screen.getByTestId("openapi-state").textContent).toBe("Not in any indexed spec");
  });

  it("says no spec indexed when there is none", () => {
    view(detailGolden("api-statuses-endpoint-route"), vi.fn(), "overview");
    expect(screen.getByTestId("openapi-state").textContent).toBe("No OpenAPI spec indexed");
  });

  it("renders a Tauri command with registration, handler and the wrapper footnote", () => {
    const openGap = view(detailGolden("api-endpoint-command"), vi.fn(), "overview");
    const detail = screen.getByTestId("endpoint-detail");
    expect(screen.getByRole("heading", { name: "t016d_greet" })).toBeTruthy();
    expect(detail.textContent).toContain("Tauri IPC");
    expect(detail.textContent).toContain("registry: app");
    expect(detail.textContent).toContain("generate_handler! entry");
    expect(detail.textContent).toContain("app/src/commands.rs:2");
    expect(detail.textContent).toContain("Fact");
    expect(screen.queryByTestId("openapi-state")).toBeNull();
    // The wrapper footnote is on the Callers tab.
    expect(openGap).not.toHaveBeenCalled();
  });

  it("renders a server action with its directive, handler and action uses (T024b)", () => {
    view(detailGolden("api-endpoint-action"), vi.fn(), "overview");
    const detail = screen.getByTestId("endpoint-detail");
    expect(screen.getByRole("heading", { name: "t016dSave" })).toBeTruthy();
    expect(detail.textContent).toContain("Server actions");
    expect(detail.textContent).toContain("'use server' module");
    expect(detail.textContent).toContain("next/app/actions.ts:5");
    expect(detail.textContent).not.toContain("generate_handler!");
  });

  it("lists a server action's uses as callers", () => {
    view(detailGolden("api-endpoint-action"));
    const detail = screen.getByTestId("endpoint-detail");
    expect(detail.textContent).toContain("T016dForm");
    expect(detail.textContent).toContain("ACTION t016dSave");
  });

  it("shows the wrapper footnote of a Tauri command on the Callers tab", () => {
    const openGap = view(detailGolden("api-endpoint-command"));
    fireEvent.click(screen.getByRole("button", { name: /See 1 wrapper site/ }));
    expect(openGap).toHaveBeenLastCalledWith("wrapper");
  });

  it("has the tabs Overview, Callers N and Flow, switching with clicks and arrows", () => {
    const d = detailGolden("api-endpoint-route");
    const onTab = vi.fn();
    render(
      <EndpointDetailView
        detail={d}
        summary={all.transports.find((t) => t.transport === d.transport)}
        openGap={vi.fn()}
        tab="callers"
        onTab={onTab}
        flow={<div>graph</div>}
        tabAside={<span>controls</span>}
      />,
    );
    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((t) => t.textContent)).toEqual(["Overview", `Callers${String(d.callers.length)}`, "Flow"]);
    expect(screen.getByRole("tab", { selected: true }).textContent).toContain("Callers");
    expect(screen.getByRole("tabpanel").getAttribute("aria-labelledby")).toBe("endpoint-tab-callers");
    // Flow controls only show on the Flow tab.
    expect(screen.queryByText("controls")).toBeNull();
    fireEvent.click(screen.getByRole("tab", { name: "Flow" }));
    expect(onTab).toHaveBeenLastCalledWith("flow");
    fireEvent.keyDown(screen.getByRole("tab", { selected: true }), { key: "ArrowLeft" });
    expect(onTab).toHaveBeenLastCalledWith("overview");
    fireEvent.keyDown(screen.getByRole("tab", { selected: true }), { key: "End" });
    expect(onTab).toHaveBeenLastCalledWith("flow");
  });

  it("keeps the route header and tabs sticky while the peek is open, outside the Flow tab only", () => {
    const d = detailGolden("api-endpoint-route");
    const view = (tab: DetailTab, peek: string | null) => (
      <EndpointDetailView detail={d} summary={undefined} openGap={vi.fn()} tab={tab} onTab={vi.fn()} peek={peek} onPeek={vi.fn()} flow={<div>graph</div>} tabAside={null} />
    );
    const { rerender } = render(view("callers", null));
    const head = () => document.querySelector("[data-detail-head]");
    // Closed: the wrapper adds no box, the header scrolls as before.
    expect(head()?.className).toBe("contents");
    expect(head()?.contains(screen.getByRole("tablist"))).toBe(true);
    expect(head()?.querySelector("h2")).not.toBeNull();
    rerender(view("callers", "x"));
    expect(head()?.className).toContain("sticky");
    expect(head()?.className).toContain("top-0");
    expect(head()?.className).toContain("bg-card");
    rerender(view("overview", "x"));
    expect(head()?.className).toContain("sticky");
    // The Flow tab keeps its own sticky tab row, which needs the wrapper
    // to add no box.
    rerender(view("flow", "x"));
    expect(head()?.className).toBe("contents");
    expect(screen.getByRole("tablist").parentElement?.className).toContain("sticky");
  });

  it("renders the Flow content and its controls on the Flow tab only", () => {
    const d = detailGolden("api-endpoint-route");
    render(
      <EndpointDetailView
        detail={d}
        summary={undefined}
        openGap={vi.fn()}
        tab="flow"
        onTab={vi.fn()}
        flow={<div>graph</div>}
        tabAside={<span>controls</span>}
      />,
    );
    expect(screen.getByText("graph")).toBeTruthy();
    expect(screen.getByText("controls")).toBeTruthy();
    expect(document.querySelectorAll("tr[data-caller]")).toHaveLength(0);
    expect(screen.queryByText("Handler")).toBeNull();
  });

  it("notes changed and missing files of the endpoint and handler", () => {
    const d = detailGolden("api-endpoint-route");
    d.endpoint.file_state = "changed";
    if (d.handler.node !== null) d.handler.node.file_state = "changed";
    view(d);
    const notes = screen.getByTestId("stale-note");
    // Endpoint and handler share server/app.ts: one note.
    expect(notes.children).toHaveLength(1);
    expect(notes.textContent).toContain("server/app.ts changed since the last index");
    expect(notes.textContent).toContain("run graphvein index");
  });

  it("has no stale note when files are unchanged", () => {
    view(detailGolden("api-endpoint-route"));
    expect(screen.queryByTestId("stale-note")).toBeNull();
  });
});
