import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type { IdeLink, IdeStatus } from "@/api/endpoints";
import { EditorProvider } from "@/hooks/useEditor";
import { useSource } from "@/hooks/useSource";
import { EDITOR_KEY } from "@/lib/editorChoice";
import { IdeButton } from "@/sections/api/parts";
import { EditorControl, controlName, describeActive } from "@/shell/EditorPicker";
import { editorState, ideFetcher, ideInfo } from "@/test/ideFixtures";
import { sourceResponse, stubSource } from "@/test/sourceFixtures";
import type { SourceParams } from "@/api/source";

afterEach(() => {
  window.localStorage.clear();
});

async function picker(fetcher = ideFetcher()) {
  render(
    <EditorProvider fetcher={fetcher}>
      <EditorControl />
    </EditorProvider>,
  );
  await waitFor(() => {
    expect(screen.getByTestId("editor-control-name").textContent).not.toBe("Loading");
  });
  return screen.getByTestId("editor-control");
}

describe("EditorControl", () => {
  it("opens with the keyboard, moves with Up and Down, selects with Space and returns focus on Esc", async () => {
    const trigger = await picker();
    expect(trigger.getAttribute("aria-haspopup")).toBe("dialog");
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    trigger.focus();
    fireEvent.click(trigger);
    await screen.findByTestId("editor-picker");
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByTestId("editor-option-default"));
    });
    const group = screen.getByRole("radiogroup", { name: "Editor" });
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "ArrowUp" });
    expect(document.activeElement).toBe(screen.getByTestId("editor-option-off"));
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "ArrowUp" });
    expect(document.activeElement).toBe(screen.getByTestId("editor-option-zed"));
    // Moving does not select.
    expect(screen.getByTestId("editor-option-default").getAttribute("aria-checked")).toBe("true");
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: " " });
    expect(screen.getByTestId("editor-option-zed").getAttribute("aria-checked")).toBe("true");
    expect(window.localStorage.getItem(EDITOR_KEY)).toBe("zed");
    // The popover stays open; a verified preset has no note line, a
    // limited one only its short note.
    expect(screen.getByTestId("editor-picker")).toBeTruthy();
    expect(screen.queryByTestId("editor-note-zed")).toBeNull();
    expect(screen.getByTestId("editor-note-jetbrains").textContent).toBe("macOS only");
    expect(screen.getByTestId("editor-picker-now").textContent).toBe("Links open in Zed, your choice in this browser.");
    fireEvent.keyDown(group, { key: "Home" });
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByTestId("editor-picker")).toBeNull();
    });
    expect(document.activeElement).toBe(trigger);
    expect(trigger.getAttribute("aria-label")).toBe("Editor for links: Zed, your choice in this browser");
  });

  it("selects by mouse, and Use repository default removes the key", async () => {
    window.localStorage.setItem(EDITOR_KEY, "cursor");
    const trigger = await picker();
    expect(trigger.getAttribute("aria-label")).toMatch(/^Editor for links: Cursor, your choice in this browser\. Partly verified: /);
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByTestId("editor-option-default"));
    expect(window.localStorage.getItem(EDITOR_KEY)).toBeNull();
    expect(screen.getByTestId("editor-control-name").textContent).toBe("VS Code");
    // No long note inline; a status icon opens its own, without selecting.
    expect(screen.getByTestId("editor-picker").textContent).not.toContain("IntelliJ IDEA only");
    expect(screen.queryByTestId("editor-show-notes")).toBeNull();
    const icon = screen.getByRole("button", { name: "About IntelliJ IDEA support" });
    expect(icon.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(icon);
    expect(icon.getAttribute("aria-expanded")).toBe("true");
    expect((await screen.findByTestId("preset-note")).textContent).toContain("macOS only, IntelliJ IDEA only");
    expect(screen.getByTestId("editor-option-default").getAttribute("aria-checked")).toBe("true");
    expect(window.localStorage.getItem(EDITOR_KEY)).toBeNull();
  });

  it("shows the notices of a saved choice the server does not offer and of a config error", async () => {
    window.localStorage.setItem(EDITOR_KEY, "sublime");
    const trigger = await picker(ideFetcher(ideInfo({ available: false, error: "line 4: unknown ide editor \"none\"", editor: null, label: null, source: "repository" })));
    expect(trigger.getAttribute("aria-label")).toMatch(/Saved choice ignored: your saved editor “sublime”/);
    fireEvent.click(trigger);
    expect(await screen.findByTestId("editor-notice-ignored")).toBeTruthy();
    expect(screen.getByTestId("editor-notice-config").textContent).toMatch(/No links until you choose an editor/);
    expect(window.localStorage.getItem(EDITOR_KEY)).toBe("sublime");
  });

  it("says links are off by your choice on a config error with No editor links (C7)", async () => {
    window.localStorage.setItem(EDITOR_KEY, "off");
    const trigger = await picker(ideFetcher(ideInfo({ available: false, error: "bad yaml", editor: null, label: null, source: "repository" })));
    fireEvent.click(trigger);
    const notice = await screen.findByTestId("editor-notice-config");
    expect(notice.textContent).toMatch(/Links are off by your choice \(No editor links\)\./);
    expect(notice.textContent).not.toMatch(/until you choose/);
  });

  it("makes every status icon a tab stop that reveals the full note (round-2 constraint a)", async () => {
    const trigger = await picker();
    fireEvent.click(trigger);
    await screen.findByTestId("editor-picker");
    const icons = screen.getAllByRole("button", { name: /^About .* support$/ });
    // Only the presets that are not verified: Cursor and IntelliJ IDEA.
    expect(icons.map((i) => i.getAttribute("aria-label"))).toEqual(["About Cursor support", "About IntelliJ IDEA support"]);
    for (const icon of icons) {
      expect(icon.tabIndex).toBe(0);
      expect(icon.getAttribute("tabindex")).toBeNull();
    }
    act(() => {
      icons[0]?.focus();
    });
    expect(document.activeElement).toBe(icons[0]);
    expect((await screen.findByTestId("preset-note")).textContent).toContain("cursor://file is confirmed");
    // Esc closes the note, not the picker.
    fireEvent.keyDown(icons[0] as HTMLElement, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByTestId("preset-note")).toBeNull();
    });
    expect(screen.getByTestId("editor-picker")).toBeTruthy();
  });

  it("honours a stored No editor links when the editor list fails (C6)", async () => {
    window.localStorage.setItem(EDITOR_KEY, "off");
    const failing = () => Promise.reject(new Error("HTTP 500"));
    const link: IdeLink = { link: "vscode://file/r/a.ts:2:20", line: 2, column: 20, exact: true, line_only: false, caveat: null };
    const st: IdeStatus = { available: true, error: null, label: "Open in VS Code", editor: "vscode", source: "builtin" };
    render(
      <EditorProvider fetcher={failing}>
        <EditorControl />
        <IdeButton ide={link} status={st} label place="test" />
      </EditorProvider>,
    );
    await waitFor(() => {
      expect(screen.getByTestId("editor-control").getAttribute("aria-label")).toMatch(/could not be loaded/);
    });
    const trigger = screen.getByTestId("editor-control");
    expect(trigger.getAttribute("aria-label")).toBe(
      "Editor for links: No editor links, your choice in this browser. The editor list could not be loaded (HTTP 500); links stay off",
    );
    expect(screen.getByTestId("editor-control-name").textContent).toBe("No editor links");
    const b = screen.getByRole("button", { name: "Editor links are off in this browser (your choice)" });
    expect((b as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("link")).toBeNull();
    fireEvent.click(trigger);
    expect((await screen.findByTestId("editor-notice-failed")).textContent).toMatch(/Links stay off, as you chose\./);
  });
});

describe("describeActive", () => {
  const cfgErr = { available: false, error: "bad", editor: null, label: null, source: "repository" };
  it.each([
    ["builtin", editorState(), "VS Code", null, null, "Editor for links: VS Code, Graphvein's default (graphvein.yaml sets no editor)"],
    ["repo", editorState({ repository: { source: "repository" } }), "VS Code", "repo", null, "Editor for links: VS Code, the repository default from graphvein.yaml"],
    ["yours", editorState({ choice: "zed" }), "Zed", "yours", null, "Editor for links: Zed, your choice in this browser"],
    ["ignored", editorState({ ignored: "sublime", repository: { source: "repository" } }), "VS Code", "repo", "warn", /Saved choice ignored/],
    ["notsaved", editorState({ choice: "zed", saved: false }), "Zed", "yours", "warn", /your choice for this page\. Not saved/],
    ["cfgerr", editorState({ choice: "zed", repository: cfgErr }), "Zed", "yours", "bad", /Config error: graphvein.yaml could not be loaded \(bad\)/],
    ["cfgnone", editorState({ repository: cfgErr }), "None", null, "bad", /No editor, the repository default is unavailable/],
    ["partial", editorState({ choice: "cursor" }), "Cursor", "yours", "warn", /Partly verified/],
    ["limited", editorState({ choice: "jetbrains" }), "IntelliJ IDEA", "yours", "info", /Limited: macOS only, IntelliJ IDEA only/],
    ["off", editorState({ choice: "off" }), "No editor links", "yours", null, "Editor for links: No editor links, your choice in this browser"],
    ["off-loading", editorState({ choice: "off", info: { kind: "loading" } }), "No editor links", "yours", null, "Editor for links: No editor links, your choice in this browser"],
    ["off-failed", editorState({ choice: "off", info: { kind: "failed", error: "HTTP 500" } }), "No editor links", "yours", "warn", /links stay off$/],
    ["failed", editorState({ info: { kind: "failed", error: "HTTP 500" } }), "Repository default", null, "warn", /links use the repository default$/],
    ["custom", editorState({ repository: { custom: true, editor: null, label: "Open in Sublime Text", source: "repository" } }), "Sublime Text", "repo", "warn", /Not verified: custom link_template/],
  ])("%s", (_case, state, name, marker, badge, words) => {
    const a = describeActive(state);
    expect(a.name).toBe(name);
    expect(a.marker).toBe(marker);
    expect(a.badge).toBe(badge);
    if (typeof words === "string") expect(controlName(a)).toBe(words);
    else expect(controlName(a)).toMatch(words);
  });
});

describe("IDE buttons under the editor choice", () => {
  const link: IdeLink = { link: "zed://file/r/a.ts:2:19", line: 2, column: 19, exact: true, line_only: false, caveat: null };
  const st: IdeStatus = { available: true, error: null, label: "Open in Zed", editor: "zed", source: "request" };

  it("label and title come from the response", async () => {
    window.localStorage.setItem(EDITOR_KEY, "zed");
    render(
      <EditorProvider fetcher={ideFetcher()}>
        <IdeButton ide={link} status={st} label place="test" />
      </EditorProvider>,
    );
    const a = await screen.findByRole("link", { name: "Open in Zed, your choice in this browser" });
    expect(a.textContent).toBe("Open in Zed");
    // The shared first line (editorTip), then the link and its source.
    expect(a.getAttribute("title")).toBe("Open in Zed · change in Settings\nzed://file/r/a.ts:2:19\nZed, your choice in this browser");
    expect(screen.getByTestId("editor-mark").getAttribute("data-logo")).toBe("true");
  });

  it("is disabled with a reason when the choice is No editor links", async () => {
    window.localStorage.setItem(EDITOR_KEY, "off");
    render(
      <EditorProvider fetcher={ideFetcher()}>
        <IdeButton ide={link} status={st} label place="test" />
      </EditorProvider>,
    );
    const b = await screen.findByRole("button", { name: "Editor links are off in this browser (your choice)" });
    expect((b as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("link")).toBeNull();
  });
});

describe("changing the choice", () => {
  function Probe({ client }: { client: ReturnType<typeof stubSource> }) {
    const req = useSource(client, "function:a.ts#f", 8, 0);
    return <div data-testid="probe">{req.state.kind === "ok" ? (req.state.data.node.ide?.link ?? "no link") : req.state.kind}</div>;
  }

  it("waits for the preset list, then refetches with the new editor and never shows the old links", async () => {
    window.localStorage.setItem(EDITOR_KEY, "zed");
    const requests: SourceParams[] = [];
    const client = stubSource((p) => {
      const body = sourceResponse() as unknown as { node: { ide: IdeLink | null } };
      if (body.node.ide !== null) body.node.ide = { ...body.node.ide, link: `${p.editor ?? "vscode"}://file/x` };
      return body;
    }, requests);
    let answer: (() => void) | undefined;
    const fetcher = () =>
      new Promise<ReturnType<typeof ideInfo>>((resolve) => {
        answer = () => {
          resolve(ideInfo());
        };
      });
    render(
      <EditorProvider fetcher={fetcher}>
        <Probe client={client} />
        <EditorControl />
      </EditorProvider>,
    );
    expect(screen.getByTestId("probe").textContent).toBe("loading");
    expect(requests).toHaveLength(0);
    act(() => {
      answer?.();
    });
    await waitFor(() => {
      expect(screen.getByTestId("probe").textContent).toBe("zed://file/x");
    });
    expect(requests.map((r) => r.editor)).toEqual(["zed"]);
    fireEvent.click(screen.getByTestId("editor-control"));
    fireEvent.click(await screen.findByTestId("editor-option-jetbrains"));
    // The old link is dropped at once: loading, then the new editor's link.
    expect(screen.getByTestId("probe").textContent).toBe("loading");
    await waitFor(() => {
      expect(screen.getByTestId("probe").textContent).toBe("jetbrains://file/x");
    });
    expect(requests.map((r) => r.editor)).toEqual(["zed", "jetbrains"]);
  });
});
