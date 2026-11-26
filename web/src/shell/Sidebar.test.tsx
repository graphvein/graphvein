import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { StatusView } from "@/hooks/useStatus";
import { parseRoute } from "@/router";
import { ProjectProvider } from "@/shell/project";
import { Sidebar } from "@/shell/Sidebar";
import { project } from "@/test/projectFixtures";
import { goldenStatus } from "@/test/fixtures";

const LOADING: StatusView = { kind: "loading" };

describe("Sidebar", () => {
  it("collapses to an icon rail with the same section names", () => {
    render(<Sidebar route={parseRoute("#/api")} repository="fixture" status={LOADING} collapsed />);
    const rail = screen.getByTestId("sidebar-rail");
    const api = screen.getByRole("link", { name: "API" });
    expect(api.getAttribute("aria-current")).toBe("page");
    expect(api.getAttribute("title")).toBe("API");
    expect(rail.textContent).toContain("Review, coming soon");
    expect(screen.getByRole("link", { name: "Architecture" }).getAttribute("href")).toBe("#/architecture");
    expect(screen.getByRole("link", { name: "Third-party licences" }).getAttribute("href")).toBe("/third-party-licenses.txt");
    // The rail has no version footer, whatever the status.
    expect(screen.queryByTestId("sidebar-versions")).toBeNull();
    expect(rail.textContent).not.toContain("unknown");
  });

  it("lists Couldn't read before Review without a count tag, and the Jump to box", () => {
    let opened = 0;
    render(
      <Sidebar
        route={parseRoute("#/unread")}
        repository="fixture"
        status={LOADING}
        onJump={() => {
          opened += 1;
        }}
      />,
    );
    const link = screen.getByRole("link", { name: "Couldn't read" });
    expect(link.getAttribute("href")).toBe("#/unread");
    expect(link.getAttribute("aria-current")).toBe("page");
    expect(link.querySelector('[data-testid="section-tag"]')).toBeNull();
    const items = [...screen.getByRole("navigation", { name: "Sections" }).querySelectorAll("li")].map((li) => li.textContent);
    expect(items.findIndex((t) => t.startsWith("Couldn't read"))).toBe(items.findIndex((t) => t.startsWith("Review")) - 1);
    screen.getByTestId("jump-to-button").click();
    expect(opened).toBe(1);
  });

  it("shows the full sidebar otherwise", () => {
    render(<Sidebar route={parseRoute("#/api")} repository="fixture" status={LOADING} />);
    expect(screen.queryByTestId("sidebar-rail")).toBeNull();
    expect(screen.getByText("Graphvein")).toBeTruthy();
  });

  // T016i D5: no value is claimed before the server answered.
  for (const view of [{ kind: "loading" }, { kind: "checking" }] as const) {
    it(`shows skeletons and no "unknown" while ${view.kind}`, () => {
      render(<Sidebar route={parseRoute("#/api")} repository={undefined} status={view} />);
      const footer = screen.getByTestId("sidebar-versions");
      expect(footer.querySelectorAll('[data-testid="version-skeleton"]')).toHaveLength(2);
      expect(footer.textContent).not.toContain("unknown");
      expect(footer.textContent).not.toContain("unavailable");
      expect(footer.textContent.match(/Checking the server/g)).toHaveLength(1);
    });
  }

  it('says "unavailable" after a failure, keeping no earlier value', () => {
    render(<Sidebar route={parseRoute("#/api")} repository="fixture" status={{ kind: "failed", error: new Error("down"), at: new Date() }} />);
    const footer = screen.getByTestId("sidebar-versions");
    expect(footer.textContent).toContain("Extractors unavailable");
    expect(footer.textContent).toContain("Server unavailable");
    expect(footer.textContent).not.toContain("unknown");
    expect(footer.querySelectorAll('[data-testid="version-skeleton"]')).toHaveLength(0);
  });

  it("shows the versions of an ok answer", () => {
    const status = goldenStatus("fresh");
    render(<Sidebar route={parseRoute("#/api")} repository="fixture" status={{ kind: "ok", status }} />);
    const footer = screen.getByTestId("sidebar-versions");
    expect(footer.textContent).toContain(`Extractors ${status.extractor.current}`);
    expect(footer.textContent).toContain(`Server ${status.server_version}`);
    expect(footer.textContent).not.toContain("Checking the server");
  });

  // T022 MR-UI: the project lives in the sidebar.
  it("lists a single repository as before, with no member list", () => {
    render(<Sidebar route={parseRoute("#/api")} repository="fixture" status={LOADING} />);
    expect(screen.queryByTestId("project-members")).toBeNull();
    expect(screen.queryByRole("link", { name: "Project" })).toBeNull();
    expect(screen.getByText("Repository").parentElement?.nextElementSibling?.textContent).toBe("fixture");
  });

  it("in project mode: Project first under Sections, the member list under Repository", () => {
    render(
      <ProjectProvider initial={project()} fetcher={() => Promise.resolve({ kind: "single" })}>
        {(r) => <Sidebar route={parseRoute("#/api")} repository={r} status={LOADING} />}
      </ProjectProvider>,
    );
    const links = screen.getByRole("navigation", { name: "Sections" }).querySelectorAll("ul:first-of-type > li > a");
    expect(links[0]?.textContent).toBe("Project");
    const list = screen.getByTestId("project-members");
    expect(list.textContent).toContain("shop");
    expect(screen.getAllByTestId("member-button")).toHaveLength(3);
    expect(screen.getByTestId("default-member")).toBeTruthy();
  });

  it("in project mode the rail has the member icon", () => {
    render(
      <ProjectProvider initial={project()} fetcher={() => Promise.resolve({ kind: "single" })}>
        {(r) => <Sidebar route={parseRoute("#/api")} repository={r} status={LOADING} collapsed />}
      </ProjectProvider>,
    );
    expect(screen.getByTestId("members-rail").getAttribute("aria-label")).toBe("Repository react in project shop");
    expect(screen.queryByTestId("project-members")).toBeNull();
  });
});
