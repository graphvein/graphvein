import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type { UiFetcher } from "@/api/ui";
import { LabelProvider } from "@/hooks/useLabels";
import { LABELS_KEY } from "@/lib/labelChoice";
import { ProjectView, ambiguityText } from "@/sections/project/ProjectSection";
import { CONFLICT, project } from "@/test/projectFixtures";

afterEach(() => {
  window.localStorage.clear();
});

const noDefault: UiFetcher = () => Promise.resolve({ api_version: 0, labels: null, error: null });

describe("Project view (T022 B9)", () => {
  it("explains the default rules when nothing is declared, and lists members with checkout and freshness", () => {
    render(<ProjectView project={project()} repository="react" />);
    expect(screen.getByTestId("no-declarations").textContent).toContain("matched across project repositories; base URL not proven");
    const rows = screen.getAllByTestId("project-member");
    expect(rows).toHaveLength(3);
    expect(rows[2]?.textContent).toContain("no local checkout here");
    expect(screen.queryByTestId("stale-notice")).toBeNull();
    expect(screen.queryByTestId("project-conflicts")).toBeNull();
  });

  it("groups ambiguous calls by their candidate providers, never as links", () => {
    render(<ProjectView project={project()} repository="react" />);
    const group = screen.getByTestId("ambiguity-group");
    expect(group.getAttribute("data-row")).toBe("PL-R11");
    expect(within(group).getAllByTestId("repo-badge").map((b) => b.textContent)).toEqual(["axum-ddd", "express"]);
    expect(group.textContent).toContain("routes in axum-ddd and express");
    expect(group.textContent).toContain("no link is made");
    fireEvent.click(within(group).getByRole("button", { name: /Show/ }));
    const source = within(group).getByTestId("ambiguous-source");
    expect(source.textContent).toContain("2 candidate routes");
    expect(source.textContent).toContain("apps/article/routes.ts:9");
  });

  it("words the other rows", () => {
    expect(ambiguityText({ row: "PL-R10", providers: ["express"], count: 1, sources: [] })).toContain("tied routes in express");
    expect(ambiguityText({ row: "PL-S3", providers: ["monorepo"], count: 3, sources: [] })).toContain("link scope declarations do not decide");
  });

  it("shows the stale-link notice with its reasons, and the no-pass notice", () => {
    const { unmount } = render(<ProjectView project={project({ link_pass: { state: "stale", pass: 3, links: 27, stale_links: 27, outcomes: {}, stale_reasons: ["the manifest's declarations changed"] } })} repository="react" />);
    expect(screen.getByTestId("stale-notice").textContent).toContain("The manifest's declarations changed");
    unmount();
    render(<ProjectView project={project({ link_pass: { state: "none", pass: 0, links: 0, stale_links: 0, outcomes: {}, stale_reasons: [] } })} repository="react" />);
    expect(screen.getByTestId("no-pass-notice")).toBeTruthy();
  });

  it("flags a stored in-repository link that conflicts with a scope declaration", () => {
    const p = project({
      declarations: {
        consumes: [{ consumer: "monorepo/web", provider: "monorepo", scope: "orders", default_scope: false, text: "`monorepo/web` consumes `monorepo/orders` (bases: VITE_ORDERS)" }],
        scopes: [{ repository: "monorepo", name: "users", path: "apps/users" }],
      },
      conflicts: [CONFLICT],
    });
    render(<ProjectView project={p} repository="react" />);
    const c = screen.getByTestId("conflict");
    expect(c.textContent).toContain("conflicts with declaration");
    expect(c.textContent).toContain("scope monorepo/users");
    expect(c.textContent).toContain("monorepo/web consumes monorepo/orders (bases: VITE_ORDERS)");
    expect(c.textContent).toContain("The stored link is kept");
    expect(screen.getByTestId("declaration").textContent).toBe("monorepo/web consumes monorepo/orders (bases: VITE_ORDERS)");
  });

  it("counts outcomes in both label styles", async () => {
    const { unmount } = render(<ProjectView project={project()} repository="react" />);
    const plain = screen.getAllByTestId("outcome-count").map((o) => o.textContent);
    expect(plain).toEqual(["27Linked across repositories", "2Several possible routes", "4Path not known"]);
    unmount();
    window.localStorage.setItem(LABELS_KEY, "technical");
    render(
      <LabelProvider fetcher={noDefault}>
        <ProjectView project={project()} repository="react" />
      </LabelProvider>,
    );
    const technical = (await screen.findAllByTestId("outcome-count")).map((o) => o.textContent);
    expect(technical).toEqual(["27linked", "2ambiguous", "4dynamic"]);
    expect(screen.getByTestId("ambiguity-group").textContent).toContain("PL-R11");
  });
});
