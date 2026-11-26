import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import type { IdeFetcher } from "@/api/ide";
import type { UiFetcher } from "@/api/ui";
import type { StatusView } from "@/hooks/useStatus";
import { EditorProvider } from "@/hooks/useEditor";
import { LabelProvider } from "@/hooks/useLabels";
import { DEPTH_KEYS } from "@/lib/depthChoice";
import { EDITOR_KEY } from "@/lib/editorChoice";
import { LABELS_KEY } from "@/lib/labelChoice";
import { SettingsSection, type SettingsReindex } from "@/sections/settings/SettingsSection";
import type { ReindexModel } from "@/shell/reindexModel";
import { goldenStatus } from "@/test/fixtures";
import { editResult, fakeConfigClient, sampleConfig, SAMPLE_YAML, type EditCall } from "@/test/settingsFixtures";

const status: StatusView = { kind: "ok", status: goldenStatus("fresh") };

function idleReindex(start = vi.fn()): SettingsReindex {
  const model: ReindexModel = { badge: null, slot: { kind: "idle", primary: false }, startBlocked: null };
  return { model, start };
}

function show(client: ReturnType<typeof fakeConfigClient>, reindex: SettingsReindex = idleReindex()) {
  return render(<SettingsSection status={status} client={client} reindex={reindex} />);
}

function keyRow(name: string): HTMLElement {
  const row = screen.getAllByTestId("settings-key").find((r) => r.getAttribute("data-key") === name);
  if (row === undefined) throw new Error(`no row ${name}`);
  return row;
}

afterEach(() => {
  window.localStorage.clear();
});

describe("Settings page (T039 ST-U)", () => {
  it("shows every key's ladder with the value in use and origin chips; idle save bar", async () => {
    show(fakeConfigClient([sampleConfig()], () => editResult()));
    await screen.findByTestId("settings-page");
    const labels = keyRow("ui.labels");
    expect(within(labels).getByText("graphvein.yaml:4")).toBeTruthy();
    expect(within(labels).getByTestId("settings-in-use").textContent).toBe("In use: plain, from graphvein.yaml");
    const editor = keyRow("ide.editor");
    expect(within(editor).getByTestId("settings-in-use").textContent).toBe("In use: vscode, from the built-in default");
    expect(within(editor).getByText("Built-in default")).toBeTruthy();
    expect(within(keyRow("tests.include")).getByTestId("settings-in-use").textContent).toBe("In use: nothing: not set");
    // Sections and page index.
    expect(screen.getByTestId("settings-labels")).toBeTruthy();
    expect(screen.getByTestId("settings-indexing")).toBeTruthy();
    expect(within(screen.getByTestId("settings-index")).getByText("Other keys")).toBeTruthy();
    // Keys edited elsewhere name their editor with a link.
    const pv = screen.getByTestId("settings-elsewhere-explorer.path_values");
    expect(pv.textContent).toContain("graphvein.yaml:11");
    // They deep-link to the Explorer's editors (path values, try it
    // environment picker).
    expect(within(pv).getByRole("link").getAttribute("href")).toBe("#/explorer?edit=path-values");
    expect(within(screen.getByTestId("settings-elsewhere-tryit.environments")).getByRole("link").getAttribute("href")).toBe("#/explorer?edit=environments");
    expect(screen.getByTestId("settings-other-keys").textContent).toContain("changes.journal_runs");
    expect(screen.getByTestId("settings-ignore").textContent).toContain("no .graphveinignore at the root");
    expect(screen.getByTestId("settings-savebar").textContent).toContain("No unsaved changes");
    // The idle sentence wraps (two lines fit the bar) instead of being cut.
    const idle = screen.getByTestId("settings-savebar-idle");
    expect(idle.className).not.toContain("truncate");
    expect(idle.querySelector(".line-clamp-2")?.textContent).toContain("previewed before they are written");
  });

  it("the preview says only what the server compared: lost comments listed, never that comments are kept", async () => {
    const lost = editResult({
      changes: ["sets openapi.base_paths: changes `contracts: /api/v1` (was `/api`); removes `legacy: /old`", "drops the comment # retire in 2027 (line 7)"],
      lost_comments: [{ line: 7, text: "# retire in 2027" }],
      reformatted: true,
    });
    for (const [result, want, absent] of [
      // No lost comment: nothing is said about comments.
      [editResult(), "Valid. Keys the form does not edit are unchanged.", "comment"],
      [lost, "re-encoded with two-space indentation and without blank lines. 1 comment of graphvein.yaml will be lost (listed above).", "still in the result"],
      [editResult({ lost_comments: null, other_keys_changed: true }), "Valid. Written only if", "comment"],
    ] as const) {
      const view = show(fakeConfigClient([sampleConfig()], () => result));
      await screen.findByTestId("settings-page");
      fireEvent.change(within(keyRow("index.shape_depth")).getByRole("textbox"), { target: { value: "8" } });
      fireEvent.click(screen.getByTestId("settings-review"));
      const dialog = await screen.findByTestId("settings-preview");
      const valid = await within(dialog).findByTestId("settings-preview-valid");
      expect(valid.textContent).toContain(want);
      expect(valid.textContent).not.toContain(absent);
      expect(valid.textContent).not.toContain("comments are kept");
      if (result === lost) {
        // Items are words with their values as code, never JSON.
        const changes = within(dialog).getByTestId("settings-preview-changes");
        expect(Array.from(changes.querySelectorAll("code")).map((c) => c.textContent)).toEqual(["contracts: /api/v1", "/api", "legacy: /old"]);
        expect(changes.textContent).toContain("drops the comment # retire in 2027 (line 7)");
      }
      view.unmount();
    }
  });

  it("filter: this browser only and changed from default", async () => {
    show(fakeConfigClient([sampleConfig()], () => editResult()));
    await screen.findByTestId("settings-page");
    fireEvent.click(screen.getByTestId("settings-filter-browser"));
    expect(screen.getAllByTestId("settings-key").map((r) => r.getAttribute("data-key"))).toEqual(["ide.editor", "ui.labels"]);
    expect(screen.queryByTestId("settings-other-keys")).toBeNull();
    fireEvent.click(screen.getByTestId("settings-filter-changed"));
    // ui.labels plain = default; base paths differ from "none".
    expect(screen.getAllByTestId("settings-key").map((r) => r.getAttribute("data-key"))).toEqual(["openapi.base_paths"]);
  });

  it("unsaved -> preview -> write applies the previewed result hash, then names what the index does not hold yet", async () => {
    const calls: EditCall[] = [];
    const after = sampleConfig({
      hash: "sha256:bbbb",
      index_settings: { state: "changed", changed_keys: ["index.shape_depth"], unknown_keys: [], reason: null },
    });
    const client = fakeConfigClient([sampleConfig(), after], (c) => editResult({ applied: c.resultHash !== undefined }), calls);
    const start = vi.fn();
    show(client, idleReindex(start));
    await screen.findByTestId("settings-page");
    fireEvent.change(within(keyRow("index.shape_depth")).getByRole("textbox"), { target: { value: "8" } });
    const bar = screen.getByTestId("settings-savebar");
    expect(bar.textContent).toContain("1 change to graphvein.yaml");
    expect(bar.textContent).toContain("applies at the next index run");
    expect(within(keyRow("index.shape_depth")).getByTestId("settings-unsaved-mark")).toBeTruthy();

    fireEvent.click(screen.getByTestId("settings-review"));
    const dialog = await screen.findByTestId("settings-preview");
    await within(dialog).findByText("sets index.shape_depth to 8 (was 6)");
    expect(within(dialog).getByTestId("edit-diff").textContent).toContain("+  shape_depth: 8");
    expect(within(dialog).getByTestId("settings-preview-valid").textContent).toContain("sha256:3f9c…");
    expect(calls[0]).toEqual({ request: { operations: [{ op: "set", key: "index.shape_depth", value: 8 }] }, resultHash: undefined, base: "sha256:3f9c0000aaaa" });

    fireEvent.click(within(dialog).getByTestId("settings-write"));
    await waitFor(() => {
      expect(screen.queryByTestId("settings-preview")).toBeNull();
    });
    expect(calls[1]?.resultHash).toBe("sha256:bbbb");
    const notice = await screen.findByTestId("settings-reindex-notice");
    expect(notice.textContent).toContain("graphvein.yaml written.");
    expect(notice.textContent).toContain("1 value is not in the index yet: route shape depth");
    expect(within(keyRow("index.shape_depth")).getByTestId("settings-not-indexed").textContent).toBe("written, not in the index yet");
    expect(screen.getByTestId("settings-savebar").textContent).toContain("No unsaved changes");
    fireEvent.click(within(notice).getByTestId("settings-reindex"));
    expect(start).toHaveBeenCalledTimes(1);
  });

  it("the notice shows the shell's running re-index with its progress", async () => {
    const changed = sampleConfig({ index_settings: { state: "changed", changed_keys: ["index.shape_depth"], unknown_keys: [], reason: null } });
    const model: ReindexModel = {
      badge: null,
      startBlocked: "running",
      slot: {
        kind: "running",
        job: { id: "j", state: "running", reported_state: "running", started_at: "2026-11-24T21:00:00Z", ended_at: null, phase: "parsing", progress: { done: 212, total: 430, unit: "files" }, replaces: null, run: null, error: null },
      },
    };
    show(fakeConfigClient([changed], () => editResult()), { model, start: vi.fn() });
    const notice = await screen.findByTestId("settings-reindex-notice");
    expect(notice.textContent).toContain("Indexing 212 / 430");
    expect(notice.textContent).toContain("parsing");
    expect(notice.textContent).toContain("also shown on Re-index in the top bar");
  });

  it("a 409 config_changed reloads the file, says nothing was written and drops the draft", async () => {
    const client = fakeConfigClient([sampleConfig(), sampleConfig({ hash: "sha256:cccc" })], () => new ApiError("http", "graphvein.yaml changed", 409, "config_changed"));
    show(client);
    await screen.findByTestId("settings-page");
    fireEvent.change(within(keyRow("index.shape_depth")).getByRole("textbox"), { target: { value: "8" } });
    fireEvent.click(screen.getByTestId("settings-review"));
    expect((await screen.findByTestId("settings-reloaded")).textContent).toContain("Nothing was written");
    expect(screen.queryByTestId("settings-preview")).toBeNull();
    await waitFor(() => {
      expect(client.fetches).toBe(2);
    });
    await waitFor(() => {
      expect(screen.getByTestId("settings-savebar").textContent).toContain("No unsaved changes");
    });
  });

  it("an invalid result shows the decoder's problems and cannot be written", async () => {
    const client = fakeConfigClient([sampleConfig()], () => editResult({ valid: false, problems: ["yaml: line 6: field shape_dept not found"] }));
    show(client);
    await screen.findByTestId("settings-page");
    fireEvent.change(within(keyRow("index.shape_depth")).getByRole("textbox"), { target: { value: "9" } });
    fireEvent.click(screen.getByTestId("settings-review"));
    const dialog = await screen.findByTestId("settings-preview");
    expect((await within(dialog).findByTestId("settings-preview-invalid")).textContent).toContain("field shape_dept not found");
    expect(within(dialog).getByTestId<HTMLButtonElement>("settings-write").disabled).toBe(true);
  });

  it("an out-of-range integer is not drafted", async () => {
    show(fakeConfigClient([sampleConfig()], () => editResult()));
    await screen.findByTestId("settings-page");
    fireEvent.change(within(keyRow("index.shape_depth")).getByRole("textbox"), { target: { value: "11" } });
    expect(screen.getByTestId("settings-savebar").textContent).toContain("No unsaved changes");
    expect(within(keyRow("index.shape_depth")).getByText("a whole number from 1 to 10")).toBeTruthy();
  });

  it("YAML view: line numbers, the field each line feeds, and Preview posts the whole text", async () => {
    const calls: EditCall[] = [];
    show(fakeConfigClient([sampleConfig()], () => editResult(), calls));
    await screen.findByTestId("settings-page");
    fireEvent.click(screen.getByTestId("settings-yaml-open"));
    const view = screen.getByTestId("settings-yaml");
    const fields = within(view).getByTestId("settings-yaml-fields");
    expect(fields.children[3]?.textContent).toBe("Label style");
    expect(fields.children[10]?.textContent).toBe("Explorer path values");
    expect(fields.children[13]?.textContent).toBe("no form");
    const preview = within(view).getByTestId<HTMLButtonElement>("settings-yaml-preview");
    expect(preview.disabled).toBe(true);
    const text = SAMPLE_YAML.replace("shape_depth: 6", "shape_depth: 7");
    fireEvent.change(within(view).getByRole("textbox", { name: "graphvein.yaml" }), { target: { value: text } });
    expect(view.textContent).toContain("the field names show for the file as read");
    fireEvent.click(preview);
    await screen.findByTestId("settings-preview");
    expect(calls[0]?.request).toEqual({ text });
  });

  it("config error: the decoder's message, no repository values, typed edits off, YAML offered", async () => {
    const broken = sampleConfig({
      config_error: "graphvein.yaml: yaml: line 3: mapping values are not allowed in this context",
      keys: sampleConfig().keys.map((k) => ({ ...k, origin: "unknown", value: null, line: null, choices: k.choices.length > 0 ? k.choices : undefined })),
      other_keys: [],
    });
    show(fakeConfigClient([broken], () => editResult()));
    const err = await screen.findByTestId("settings-config-error");
    expect(err.textContent).toContain("mapping values are not allowed");
    const depth = keyRow("index.shape_depth");
    expect(within(depth).queryByRole("textbox")).toBeNull();
    expect(within(depth).getByTestId("rung-repository-value").textContent).toBe("unknown");
    expect(within(depth).getByTestId("settings-in-use").textContent).toBe("In use: unknown: graphvein.yaml could not be read");
    fireEvent.click(within(err).getByRole("button", { name: /Fix in the YAML view/ }));
    expect(screen.getByTestId("settings-yaml")).toBeTruthy();
  });

  it("a member without a checkout: the reason, nothing editable", async () => {
    const none = sampleConfig({
      exists: null,
      hash: null,
      text: null,
      editable: false,
      reason: "web has no local checkout here",
      keys: sampleConfig().keys.map((k) => ({ ...k, origin: "unknown", value: null, line: null, editable: false, choices: k.choices.length > 0 ? k.choices : undefined })),
      other_keys: [],
      index_settings: null,
    });
    show(fakeConfigClient([none], () => editResult()));
    expect((await screen.findByTestId("settings-readonly")).textContent).toContain("no local checkout");
    expect(screen.queryAllByRole("textbox")).toHaveLength(0);
    expect(screen.getByTestId<HTMLButtonElement>("settings-yaml-open").disabled).toBe(true);
  });

  it("editor and labels moved from the sidebar: stored choices kept under their keys", async () => {
    window.localStorage.setItem(LABELS_KEY, "technical");
    window.localStorage.setItem(EDITOR_KEY, "off");
    const ui: UiFetcher = () => Promise.resolve({ api_version: 0, labels: null, error: null });
    render(
      <LabelProvider fetcher={ui}>
        <SettingsSection status={status} client={fakeConfigClient([sampleConfig()], () => editResult())} />
      </LabelProvider>,
    );
    await screen.findByTestId("settings-page");
    const labels = keyRow("ui.labels");
    expect(within(labels).getByTestId("labels-technical").getAttribute("aria-checked")).toBe("true");
    expect(within(labels).getByTestId("settings-in-use").textContent).toBe("In use: technical, from this browser");
    fireEvent.click(within(labels).getByTestId("labels-unset"));
    expect(window.localStorage.getItem(LABELS_KEY)).toBeNull();
    expect(within(keyRow("ui.labels")).getByTestId("settings-in-use").textContent).toBe("In use: plain, from graphvein.yaml");
    expect(within(keyRow("ide.editor")).getByTestId("editor-control")).toBeTruthy();
    await act(async () => {
      await Promise.resolve();
    });
  });

  it("walk depths (T042 rows ST-31, ST-32): origin ladder, this browser's select writes the depth switches' key at once", async () => {
    window.localStorage.setItem(DEPTH_KEYS.chain, "9");
    const config = sampleConfig();
    const shape = config.keys.find((k) => k.key === "index.shape_depth");
    if (shape === undefined) throw new Error("no index.shape_depth in the sample");
    config.keys.push(
      { ...shape, key: "ui.impact_depth", value: 4, default: 3, origin: "file", line: 5, applies: "at_once" },
      { ...shape, key: "ui.chain_depth", value: 6, default: 6, origin: "builtin", line: null, applies: "at_once" },
    );
    show(fakeConfigClient([config], () => editResult()));
    await screen.findByTestId("settings-page");
    const impact = keyRow("ui.impact_depth");
    expect(within(screen.getByTestId("settings-labels")).getByText("Impact depth")).toBeTruthy();
    expect(within(impact).getByText("graphvein.yaml:5")).toBeTruthy();
    expect(within(impact).getByTestId("settings-in-use").textContent).toBe("In use: 4, from graphvein.yaml");
    expect(within(keyRow("ui.chain_depth")).getByTestId("settings-in-use").textContent).toBe("In use: 9, from this browser");
    fireEvent.change(within(impact).getByRole("combobox"), { target: { value: "7" } });
    expect(window.localStorage.getItem(DEPTH_KEYS.impact)).toBe("7");
    expect(within(keyRow("ui.impact_depth")).getByTestId("settings-in-use").textContent).toBe("In use: 7, from this browser");
    fireEvent.change(within(keyRow("ui.chain_depth")).getByRole("combobox"), { target: { value: "" } });
    expect(window.localStorage.getItem(DEPTH_KEYS.chain)).toBeNull();
    expect(within(keyRow("ui.chain_depth")).getByTestId("settings-in-use").textContent).toBe("In use: 6, from the built-in default");
  });

  it("ide.link_template set: the editor row names the template and its line, no preset in use; the browser rung says not set", async () => {
    const keys = sampleConfig().keys.map((k) =>
      k.key === "ide.editor" ? { ...k, editable: false, reason: "ide.link_template is set, so ide.editor does not decide the links; edit the YAML", locked_by: { key: "ide.link_template", line: 3 } } : k,
    );
    const ide: IdeFetcher = () =>
      Promise.resolve({ api_version: 0, presets: [{ editor: "vscode", label: "VS Code", status: "verified", note: "", short_note: "" }], repository: { available: true, error: null, editor: null, label: null, custom: true, source: "repository" } });
    render(
      <EditorProvider fetcher={ide}>
        <SettingsSection status={status} client={fakeConfigClient([sampleConfig({ keys: keys.map((k) => ({ ...k, choices: k.choices.length > 0 ? k.choices : undefined })) })], () => editResult())} />
      </EditorProvider>,
    );
    await screen.findByTestId("settings-page");
    const editor = keyRow("ide.editor");
    expect(within(editor).getByTestId("settings-in-use").textContent).toBe("In use: the link template (ide.link_template), from graphvein.yaml line 3");
    expect(within(editor).getByTestId("rung-repository-value").textContent).toBe("the link template (ide.link_template)");
    expect(within(editor).getByText("graphvein.yaml:3")).toBeTruthy();
    expect(within(editor).queryByText("Built-in default")).toBeNull();
    // No stored choice: the browser rung's control says so, not the
    // editor the links use.
    await waitFor(() => {
      expect(within(editor).getByTestId("editor-control-name").textContent).toBe("Not set");
    });
  });

  it("a whole-text edit that changes keys outside the form reads the editor defaults again", async () => {
    let fetches = 0;
    const ide: IdeFetcher = () => {
      fetches++;
      return Promise.resolve({ api_version: 0, presets: [], repository: { available: true, error: null, editor: "vscode", label: null, custom: false, source: "builtin" } });
    };
    const client = fakeConfigClient([sampleConfig()], (c) => editResult({ applied: c.resultHash !== undefined, changed_keys: [], other_keys_changed: true, reindex_needed: false }));
    render(
      <EditorProvider fetcher={ide}>
        <SettingsSection status={status} client={client} />
      </EditorProvider>,
    );
    await screen.findByTestId("settings-page");
    await waitFor(() => {
      expect(fetches).toBe(1);
    });
    fireEvent.click(screen.getByTestId("settings-yaml-open"));
    const view = screen.getByTestId("settings-yaml");
    fireEvent.change(within(view).getByRole("textbox", { name: "graphvein.yaml" }), { target: { value: `${SAMPLE_YAML}ide:\n  link_template: "x://{abs_path}"\n` } });
    fireEvent.click(within(view).getByTestId("settings-yaml-preview"));
    const dialog = await screen.findByTestId("settings-preview");
    await within(dialog).findByTestId("settings-preview-valid");
    fireEvent.click(within(dialog).getByTestId("settings-write"));
    await waitFor(() => {
      expect(fetches).toBe(2);
    });
  });

  it("puts the editor first in its own section, and the labels and depths under Labels and views", async () => {
    show(fakeConfigClient([sampleConfig()], () => editResult()));
    await screen.findByTestId("settings-page");
    const sections = [...screen.getByTestId("settings-form").querySelectorAll("section")];
    expect(sections[0]?.id).toBe("settings-editor");
    const editor = screen.getByTestId("settings-editor");
    expect(within(editor).getByRole("heading").textContent).toBe("Editor");
    expect(editor.textContent).toContain("which editor the file links open");
    expect(within(editor).getAllByTestId("settings-key").map((r) => r.getAttribute("data-key"))).toEqual(["ide.editor"]);
    const labels = screen.getByTestId("settings-labels");
    expect(within(labels).getByRole("heading").textContent).toBe("Labels and views");
    expect(within(labels).getAllByTestId("settings-key").map((r) => r.getAttribute("data-key"))).toEqual(["ui.labels"]);
    expect(screen.queryByText("Display")).toBeNull();
    // No link template in the file: no row for it.
    expect(screen.queryByTestId("settings-link-template")).toBeNull();
    // The page index links to the stable anchors.
    const index = screen.getByTestId("settings-index");
    expect(within(index).getByText("Editor").getAttribute("href")).toBe("#/settings?at=editor");
    expect(within(index).getByText("Labels and views").getAttribute("href")).toBe("#/settings?at=labels");
  });

  it("shows ide.link_template in the Editor section when graphvein.yaml sets it, not under Other keys", async () => {
    const config = sampleConfig();
    config.other_keys.push({ key: "ide.link_template", line: 3, edited_by: null });
    show(fakeConfigClient([config], () => editResult()));
    await screen.findByTestId("settings-page");
    const row = within(screen.getByTestId("settings-editor")).getByTestId("settings-link-template");
    expect(row.textContent).toContain("ide.link_template");
    expect(row.textContent).toContain("graphvein.yaml:3");
    expect(row.id).toBe("settings-row-ide.link_template");
    expect(within(screen.getByTestId("settings-other-keys")).queryByText("ide.link_template")).toBeNull();
    expect(within(screen.getByTestId("settings-other-keys")).getByText("changes.journal_runs")).toBeTruthy();
  });

  it.each([
    ["editor", "settings-editor"],
    ["labels", "settings-labels"],
    ["ide.editor", "settings-row-ide.editor"],
  ])("#/settings?at=%s scrolls to and focuses its anchor", async (at, id) => {
    window.history.replaceState(null, "", `#/settings?at=${at}`);
    const scrolled: string[] = [];
    // jsdom has no scrollIntoView: a stand-in records which anchor scrolls.
    Object.defineProperty(Element.prototype, "scrollIntoView", {
      configurable: true,
      value(this: Element) {
        scrolled.push(this.id);
      },
    });
    try {
      show(fakeConfigClient([sampleConfig()], () => editResult()));
      await screen.findByTestId("settings-page");
      await waitFor(() => {
        expect(document.activeElement?.id).toBe(id);
      });
      expect(scrolled).toContain(id);
    } finally {
      Reflect.deleteProperty(Element.prototype, "scrollIntoView");
      window.history.replaceState(null, "", "#/");
    }
  });
});
