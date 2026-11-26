// The shared editor link and its first-click chooser (developer decision
// 2026-11-25): asked only while nothing chose an editor, remember on and
// off, Esc, a click outside, the keyboard, the link opened in the picked
// editor, the shared tooltip, and that every editor link of the app goes
// through EditorLink.

import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useEffect, useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { RepositoryIde } from "@/api/ide";
import { EditorLink, editorNav, PENDING_MS, resetEditorLinks } from "@/components/EditorLink";
import { EditorProvider, useEditor } from "@/hooks/useEditor";
import { EDITOR_KEY } from "@/lib/editorChoice";
import { CHOOSE_EDITOR, editorTip } from "@/lib/ide";
import { ideFetcher, ideInfo } from "@/test/ideFixtures";

const VSCODE = "vscode://file/r/a.ts:2:20";
const ZED = "zed://file/r/a.ts:2:19";

/** View stands for a section: its link is built by the server for the
 * editor of its request, and it shows nothing while the request for a new
 * editor runs (useApiRequest drops the old answer). */
function View() {
  const { requestEditor } = useEditor();
  const [answered, setAnswered] = useState<{ editor: string | null } | null>(null);
  useEffect(() => {
    if (requestEditor === undefined) return;
    const t = setTimeout(() => {
      setAnswered({ editor: requestEditor });
    }, 5);
    return () => {
      clearTimeout(t);
    };
  }, [requestEditor]);
  // The answer for another editor is dropped at once, as useApiRequest does.
  if (answered === null || requestEditor === undefined || answered.editor !== requestEditor) return <span data-testid="loading">loading</span>;
  const shown = answered.editor;
  const href = shown === "zed" ? ZED : VSCODE;
  const label = shown === "zed" ? "Open in Zed" : "Open in VS Code";
  return (
    <EditorLink href={href} place="function:a.ts#f" label={label} detail={href} aria-label={label} data-testid="link">
      {label}
    </EditorLink>
  );
}

function show(repository: Partial<RepositoryIde> = {}) {
  return render(
    <EditorProvider fetcher={ideFetcher(ideInfo(repository))}>
      <button type="button" data-testid="elsewhere">
        elsewhere
      </button>
      <View />
    </EditorProvider>,
  );
}

let opened: string[] = [];

beforeEach(() => {
  opened = [];
  vi.spyOn(editorNav, "open").mockImplementation((href: string) => {
    opened.push(href);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
  resetEditorLinks();
});

async function link(): Promise<HTMLElement> {
  return screen.findByTestId("link");
}

describe("the first-click chooser", () => {
  it("opens instead of the link while neither this browser nor graphvein.yaml chose an editor", async () => {
    show();
    const a = await link();
    expect(a.getAttribute("title")).toBe(`${CHOOSE_EDITOR}\n${VSCODE}`);
    expect(a.getAttribute("aria-haspopup")).toBe("dialog");
    expect(a.getAttribute("aria-expanded")).toBe("false");
    const click = fireEvent.click(a);
    // The click did not navigate (default prevented).
    expect(click).toBe(false);
    const chooser = await screen.findByTestId("editor-chooser");
    expect(a.getAttribute("aria-expanded")).toBe("true");
    expect(within(chooser).getByText("Open with")).toBeTruthy();
    // The picker's presets, with their availability notes.
    const options = within(chooser).getAllByRole("option");
    expect(options.map((o) => o.getAttribute("data-testid"))).toEqual(ideInfo().presets.map((p) => `editor-chooser-${p.editor}`));
    expect(screen.getByTestId("editor-chooser-short-jetbrains").textContent).toBe("macOS only");
    expect(within(screen.getByTestId("editor-chooser-jetbrains")).getByTestId("editor-mark")).toBeTruthy();
    expect(screen.getByTestId<HTMLInputElement>("editor-chooser-remember").checked).toBe(true);
    expect(screen.getByTestId("editor-chooser-settings").getAttribute("href")).toBe("#/settings?at=editor");
    expect(opened).toEqual([]);
  });

  it("keeps each row to its name and one short line, with the full note behind a status icon", async () => {
    show();
    fireEvent.click(await link());
    const chooser = await screen.findByTestId("editor-chooser");
    // No long note inline; the short lines only where a preset is not verified.
    expect(chooser.textContent).not.toContain("IntelliJ IDEA only");
    expect(chooser.textContent).not.toContain("cursor://file is confirmed");
    expect(screen.getByTestId("editor-chooser-short-cursor").textContent).toBe("column assumed");
    expect(screen.queryByTestId("editor-chooser-short-vscode")).toBeNull();
    // Icons: amber for partly verified, grey for limited, none when verified.
    expect(screen.queryByTestId("preset-status-vscode")).toBeNull();
    expect(screen.queryByTestId("preset-status-zed")).toBeNull();
    const cursor = screen.getByRole("button", { name: "About Cursor support" });
    expect(cursor.getAttribute("data-status")).toBe("partly_verified");
    expect(cursor.className).toContain("text-dynamic");
    expect(screen.getByRole("button", { name: "About IntelliJ IDEA support" }).className).toContain("text-muted-foreground");
    // Keyboard focus shows the note.
    act(() => {
      cursor.focus();
    });
    const note = await screen.findByTestId("preset-note");
    expect(note.textContent).toContain("Partly verified.");
    expect(note.textContent).toContain("so the column is marked as not exact");
    // Enter and a click show the note and never pick the editor.
    fireEvent.keyDown(cursor, { key: "Enter" });
    fireEvent.click(cursor);
    expect(screen.getByTestId("preset-note")).toBeTruthy();
    expect(window.localStorage.getItem(EDITOR_KEY)).toBeNull();
    expect(opened).toEqual([]);
    expect(screen.getByTestId("editor-chooser")).toBeTruthy();
    // Esc closes the note first, then the chooser.
    fireEvent.keyDown(cursor, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByTestId("preset-note")).toBeNull();
    });
    expect(screen.getByTestId("editor-chooser")).toBeTruthy();
    fireEvent.keyDown(document.activeElement ?? cursor, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByTestId("editor-chooser")).toBeNull();
    });
    expect(opened).toEqual([]);
  });

  it("opens the note on a click or tap and keeps it while the pointer is away", async () => {
    show();
    fireEvent.click(await link());
    await screen.findByTestId("editor-chooser");
    const idea = screen.getByRole("button", { name: "About IntelliJ IDEA support" });
    fireEvent.click(idea);
    const note = await screen.findByTestId("preset-note");
    expect(note.textContent).toContain("Limited.");
    expect(note.textContent).toContain("macOS only, IntelliJ IDEA only");
    expect(window.localStorage.getItem(EDITOR_KEY)).toBeNull();
  });

  it.each([
    ["this browser", { stored: "zed", repository: {} }],
    ["the repository's editor", { stored: null, repository: { source: "repository", editor: "zed", label: "Open in Zed" } }],
    ["the repository's link template", { stored: null, repository: { source: "repository", editor: null, custom: true, label: "Open in Sublime" } }],
  ])("is not offered once %s chose", async (_case, { stored, repository }) => {
    if (stored !== null) window.localStorage.setItem(EDITOR_KEY, stored);
    show(repository);
    const a = await link();
    await waitFor(() => {
      expect(a.isConnected ? a : screen.getByTestId("link")).toBeTruthy();
    });
    const now = screen.getByTestId("link");
    expect(now.getAttribute("aria-haspopup")).toBeNull();
    expect(now.getAttribute("title")?.split("\n")[0]).toMatch(/^Open in (Zed|VS Code) · change in Settings$/);
    // The click is left to the browser: the link opens directly.
    expect(fireEvent.click(now)).toBe(true);
    expect(screen.queryByTestId("editor-chooser")).toBeNull();
  });

  it("is not offered while the preset list loads or failed", async () => {
    render(
      <EditorProvider fetcher={() => Promise.reject(new Error("HTTP 500"))}>
        <EditorLink href={VSCODE} place="p" label="Open in VS Code" data-testid="link">
          Open
        </EditorLink>
      </EditorProvider>,
    );
    const a = await link();
    await act(async () => {
      await Promise.resolve();
    });
    expect(fireEvent.click(a)).toBe(true);
    expect(screen.queryByTestId("editor-chooser")).toBeNull();
  });

  it("remember on: stores the choice as Settings does and opens this link in the picked editor", async () => {
    show();
    fireEvent.click(await link());
    fireEvent.click(await screen.findByTestId("editor-chooser-zed"));
    expect(window.localStorage.getItem(EDITOR_KEY)).toBe("zed");
    expect(screen.queryByTestId("editor-chooser")).toBeNull();
    // The view asks again with editor=zed (it unmounts the link meanwhile);
    // the rebuilt link opens once it is here, once.
    await waitFor(() => {
      expect(opened).toEqual([ZED]);
    });
    const a = await link();
    expect(a.getAttribute("href")).toBe(ZED);
    expect(a.getAttribute("title")?.split("\n")[0]).toBe("Open in Zed · change in Settings");
    // From now on the link opens directly.
    expect(fireEvent.click(a)).toBe(true);
    expect(screen.queryByTestId("editor-chooser")).toBeNull();
  });

  it("remember off: applies the choice to this page only; the next page load asks again", async () => {
    const first = show();
    fireEvent.click(await link());
    fireEvent.click(await screen.findByTestId("editor-chooser-remember"));
    fireEvent.click(screen.getByTestId("editor-chooser-zed"));
    await waitFor(() => {
      expect(opened).toEqual([ZED]);
    });
    expect(window.localStorage.getItem(EDITOR_KEY)).toBeNull();
    first.unmount();
    show();
    const a = await link();
    expect(a.getAttribute("href")).toBe(VSCODE);
    expect(fireEvent.click(a)).toBe(false);
    expect(await screen.findByTestId("editor-chooser")).toBeTruthy();
  });

  it("picking the editor the links already use opens this link at once", async () => {
    show();
    fireEvent.click(await link());
    fireEvent.click(await screen.findByTestId("editor-chooser-vscode"));
    expect(opened).toEqual([VSCODE]);
    expect(window.localStorage.getItem(EDITOR_KEY)).toBe("vscode");
  });

  it("Esc closes without opening anything and returns focus to the link", async () => {
    show();
    const a = await link();
    a.focus();
    fireEvent.click(a);
    await screen.findByTestId("editor-chooser");
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByTestId("editor-chooser")).toBeNull();
    });
    expect(document.activeElement).toBe(a);
    expect(opened).toEqual([]);
    expect(window.localStorage.getItem(EDITOR_KEY)).toBeNull();
  });

  it("a click outside closes without opening anything", async () => {
    show();
    fireEvent.click(await link());
    await screen.findByTestId("editor-chooser");
    // Radix listens for a pointer down outside from the next tick on.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });
    // A whole click: Radix dismisses on the click that follows the pointer
    // down outside.
    const out = screen.getByTestId("elsewhere");
    fireEvent.pointerDown(out, { button: 0 });
    fireEvent.mouseDown(out, { button: 0 });
    fireEvent.mouseUp(out, { button: 0 });
    fireEvent.click(out, { button: 0 });
    await waitFor(() => {
      expect(screen.queryByTestId("editor-chooser")).toBeNull();
    });
    expect(opened).toEqual([]);
    expect(window.localStorage.getItem(EDITOR_KEY)).toBeNull();
  });

  it("works from the keyboard: focus on the first editor, arrows move, Enter picks", async () => {
    show();
    const a = await link();
    a.focus();
    fireEvent.click(a); // Enter on a link is a click.
    const first = await screen.findByTestId("editor-chooser-vscode");
    await waitFor(() => {
      expect(document.activeElement).toBe(first);
    });
    expect(first.getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("listbox").getAttribute("aria-labelledby")).toBeTruthy();
    fireEvent.keyDown(first, { key: "ArrowDown" });
    const second = screen.getAllByRole("option")[1];
    expect(document.activeElement).toBe(second);
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "End" });
    const zed = screen.getByTestId("editor-chooser-zed");
    expect(document.activeElement).toBe(zed);
    expect(zed.getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(zed, { key: "ArrowDown" });
    expect(document.activeElement).toBe(first);
    fireEvent.keyDown(first, { key: "ArrowUp" });
    expect(document.activeElement).toBe(zed);
    fireEvent.keyDown(zed, { key: "Enter" });
    expect(window.localStorage.getItem(EDITOR_KEY)).toBe("zed");
    await waitFor(() => {
      expect(opened).toEqual([ZED]);
    });
  });

  it("a pick whose link comes back only after the wait opens nothing", async () => {
    function Late({ href }: { href: string }) {
      return (
        <EditorLink href={href} place="p" label="Open in VS Code" data-testid="link">
          Open
        </EditorLink>
      );
    }
    const { rerender } = render(
      <EditorProvider fetcher={ideFetcher()}>
        <Late href={VSCODE} />
      </EditorProvider>,
    );
    const a = await link();
    await waitFor(() => {
      expect(a.getAttribute("aria-haspopup")).toBe("dialog");
    });
    fireEvent.click(a);
    fireEvent.click(await screen.findByTestId("editor-chooser-zed"));
    const now = Date.now();
    vi.spyOn(Date, "now").mockReturnValue(now + PENDING_MS + 1);
    rerender(
      <EditorProvider fetcher={ideFetcher()}>
        <Late href={ZED} />
      </EditorProvider>,
    );
    await act(async () => {
      await Promise.resolve();
    });
    expect(opened).toEqual([]);
  });
});

describe("editorTip", () => {
  it("names the editor and where to change it, or asks while nothing is chosen", () => {
    expect(editorTip("Open in VS Code", false)).toBe("Open in VS Code · change in Settings");
    expect(editorTip("Open in editor", false)).toBe("Open in editor · change in Settings");
    expect(editorTip("Open in VS Code", true)).toBe("Choose an editor");
  });
});

// Every editor link of the app renders through EditorLink. Vite reads every
// source file as text at test time (no Node file API).
const SOURCES = import.meta.glob<string>(["/src/**/*.ts", "/src/**/*.tsx", "!/src/**/*.test.ts", "!/src/**/*.test.tsx"], {
  query: "?raw",
  import: "default",
  eager: true,
});

/** SHARED is the one component that renders an editor link's anchor. */
const SHARED = "/src/components/EditorLink.tsx";

/** OPENS_ELSEWHERE are the files allowed to read an editor link without
 * rendering EditorLink, each with its reason. None may open one: every
 * editor link opens through EditorLink or its chooser. */
const OPENS_ELSEWHERE: Readonly<Record<string, string>> = {
  // Jump-to's model: picks the row's target (the editor link or a hash);
  // JumpTo.tsx opens it through the shared chooser (checked below).
  "/src/shell/jumpModel.ts": "targetOf",
};

/** EDITOR_HREF matches an anchor whose href is a server-built editor link:
 * a presented view's href, a node's or item's ide.link, a why source's
 * link.link. */
const EDITOR_HREF = /<a\b[^>]*\bhref=\{[^}]*(?:\bview\.href\b|\bide\??\.link\b|\.link\.link\b)[^}]*\}/s;

describe("one editor link (every editor link goes through EditorLink)", () => {
  it("no component renders an editor link's anchor of its own", () => {
    expect(Object.keys(SOURCES).length).toBeGreaterThan(50);
    const offences: string[] = [];
    for (const [file, text] of Object.entries(SOURCES)) {
      if (file === SHARED) continue;
      if (EDITOR_HREF.test(text)) offences.push(`${file}: renders an editor link without EditorLink (components/EditorLink.tsx)`);
      if (/location\.(assign|replace)\(/.test(text)) offences.push(`${file}: opens an editor link without EditorLink`);
    }
    expect(offences).toEqual([]);
  });

  it("every component that presents an editor link uses EditorLink", () => {
    const presenting = Object.entries(SOURCES)
      .filter(([file, text]) => file !== "/src/lib/ide.ts" && /\bpresentIde\(|\bIdeMark\b|\.ide\.link\b|\bide\.link\b|\.link\.link\b/.test(text))
      .map(([file]) => file)
      .filter((file) => file !== "/src/components/IdeParts.tsx" && OPENS_ELSEWHERE[file] === undefined);
    expect(presenting.length).toBeGreaterThanOrEqual(9);
    // EditorLink, or its chooser for a list row that opens a link (Jump-to).
    const without = presenting.filter((file) => !/<EditorLink\b|<EditorChooserContent\b/.test(SOURCES[file] ?? ""));
    expect(without).toEqual([]);
  });

  it("Jump-to opens editor links through the shared chooser, with no bypass", () => {
    const jump = SOURCES["/src/shell/JumpTo.tsx"] ?? "";
    expect(jump).toContain("<EditorChooserContent");
    expect(jump).toContain("chooserNeeded(editor)");
    expect(jump).toContain("editorNav.open(");
    expect(jump).not.toMatch(/location\.(assign|replace)\(/);
  });

  it("the test's pattern catches a hand-made editor anchor", () => {
    expect(EDITOR_HREF.test('<a href={view.href} title="x">')).toBe(true);
    expect(EDITOR_HREF.test("<a className='x' href={n.ide.link}>")).toBe(true);
    expect(EDITOR_HREF.test("<a href={s.link.link}>")).toBe(true);
    expect(EDITOR_HREF.test("<a href={routeHash(r)}>")).toBe(false);
  });
});
