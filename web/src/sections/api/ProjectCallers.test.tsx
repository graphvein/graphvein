import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type { IdeStatus } from "@/api/endpoints";
import type { UiFetcher } from "@/api/ui";
import { EditorProvider } from "@/hooks/useEditor";
import { LabelProvider } from "@/hooks/useLabels";
import { EDITOR_KEY } from "@/lib/editorChoice";
import { LABELS_KEY } from "@/lib/labelChoice";
import { ProjectCallers } from "@/sections/api/ProjectCallers";
import { ideFetcher, ideInfo } from "@/test/ideFixtures";
import { ASSUME, projectEndpoint } from "@/test/projectFixtures";

afterEach(() => {
  window.localStorage.clear();
});

const noDefault: UiFetcher = () => Promise.resolve({ api_version: 0, labels: null, error: null });

describe("cross-repository callers (T022 B9)", () => {
  it("lists other repositories' callers with repository, assumption, inferred wording and stale marking", () => {
    render(<ProjectCallers project={projectEndpoint()} repository="express" />);
    const rows = screen.getAllByTestId("project-caller");
    expect(rows).toHaveLength(2);
    expect(within(rows[0] as HTMLElement).getByTestId("repo-badge").textContent).toBe("react");
    expect(within(rows[0] as HTMLElement).getByTestId("project-link-label").textContent).toBe("Inferred · 0.7");
    expect(within(rows[0] as HTMLElement).getByTestId("project-link-label").getAttribute("title")).toBe("Inferred across repositories: calls_route project link, inferred 0.7. The number is a fixed rank per match rule, not a probability.");
    expect(within(rows[0] as HTMLElement).getByTestId("project-assumption").textContent).toBe(`Assumption: ${ASSUME}`);
    expect(rows[0]?.textContent).toContain("src/shared/api/api.gen.ts:40");
    expect(rows[1]?.getAttribute("data-stale")).toBe("true");
    expect(within(rows[1] as HTMLElement).getByTestId("stale-link").getAttribute("title")).toContain("react was indexed again after the link pass");
    expect(rows[1]?.textContent).toContain("react no longer stores this call");
    const possible = screen.getByTestId("project-possible");
    expect(possible.textContent).toContain("May be called by 2 calls of other repositories");
    const items = screen.getAllByTestId("project-possible-caller");
    expect(items[0]?.textContent).toContain("may call it (ambiguous across repositories; 3 candidate routes in axum, express)");
    expect(items[1]?.textContent).toContain("may call it (not linked: a candidate provider had no stored index; no stored index: ghost)");
    expect(screen.queryByTestId("project-declaration-conflicts")).toBeNull();
  });

  it("marks in-repository callers that conflict with a scope declaration", () => {
    const p = projectEndpoint();
    p.declaration_conflicts = [{ call: "client_call:web", consumer: "mono/web", declared: ["`mono/web` consumes `mono/orders`"], target: "route:x", target_scope: "users" }];
    render(<ProjectCallers project={p} repository="mono" />);
    const box = screen.getByTestId("project-declaration-conflicts");
    expect(box.textContent).toContain("1 in-repository caller is linked inside the repository; the declaration names another scope");
    expect(box.textContent).toContain("the link reaches scope users");
  });

  it("uses the technical label style", async () => {
    window.localStorage.setItem(LABELS_KEY, "technical");
    render(
      <LabelProvider fetcher={noDefault}>
        <ProjectCallers project={projectEndpoint()} repository="express" />
      </LabelProvider>,
    );
    const labels = await screen.findAllByTestId("project-link-label");
    expect(labels[0]?.textContent).toBe("inferred 0.7");
  });

  it("renders nothing on a single-repository server", () => {
    const { container } = render(<ProjectCallers project={null} repository="express" />);
    expect(container.textContent).toBe("");
  });
});

// Project-caller links follow the browser's and the repository's choice
// like every other editor link: the server builds them with editor= and
// the member's graphvein.yaml and says how (ide_status); the view uses that
// status and the shared first-click chooser.
describe("project caller editor links", () => {
  function withLink(link: string, status: IdeStatus) {
    const p = projectEndpoint();
    const first = p.callers[0];
    if (first?.node === null || first === undefined) throw new Error("fixture: the first caller has no node");
    first.node.ide = { link, line: 40, column: 1, exact: true, line_only: false, caveat: null };
    first.ide_status = status;
    return p;
  }

  it("names the browser's choice the server applied, never 'ignored'", async () => {
    window.localStorage.setItem(EDITOR_KEY, "zed");
    const p = withLink("zed://file/r/src/shared/api/api.gen.ts:40:1", { available: true, error: null, label: "Open in Zed", editor: "zed", source: "request" });
    render(
      <EditorProvider fetcher={ideFetcher(ideInfo())}>
        <ProjectCallers project={p} repository="express" />
      </EditorProvider>,
    );
    const button = await screen.findByTestId("ide-button");
    await waitFor(() => {
      expect(button.getAttribute("aria-label")).toContain("your choice in this browser");
    });
    expect(button.getAttribute("aria-label")).not.toContain("ignored");
    expect(button.getAttribute("href")).toBe("zed://file/r/src/shared/api/api.gen.ts:40:1");
    expect(button.getAttribute("aria-haspopup")).toBeNull();
  });

  it("names the member repository's editor from its graphvein.yaml", async () => {
    const p = withLink("cursor://file/r/src/shared/api/api.gen.ts:40:1", { available: true, error: null, label: "Open in Cursor", editor: "cursor", source: "repository" });
    render(
      <EditorProvider fetcher={ideFetcher(ideInfo({ source: "repository", editor: "cursor", label: "Open in Cursor" }))}>
        <ProjectCallers project={p} repository="express" />
      </EditorProvider>,
    );
    const button = await screen.findByTestId("ide-button");
    await waitFor(() => {
      expect(button.getAttribute("aria-label")).toContain("the repository default from graphvein.yaml");
    });
    expect(button.getAttribute("aria-haspopup")).toBeNull();
  });

  it("asks with the shared chooser while nothing chose an editor", async () => {
    const p = withLink("vscode://file/r/src/shared/api/api.gen.ts:40:1", { available: true, error: null, label: "Open in VS Code", editor: "vscode", source: "builtin" });
    render(
      <EditorProvider fetcher={ideFetcher(ideInfo())}>
        <ProjectCallers project={p} repository="express" />
      </EditorProvider>,
    );
    const button = await screen.findByTestId("ide-button");
    await waitFor(() => {
      expect(button.getAttribute("aria-haspopup")).toBe("dialog");
    });
    fireEvent.click(button);
    expect(await screen.findByTestId("editor-chooser")).toBeTruthy();
  });
});
