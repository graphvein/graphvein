import { describe, expect, it } from "vitest";

import type { ConfigKey } from "@/api/config";
import {
  appliesWords,
  changedFromDefault,
  draftSummary,
  draftValue,
  inUseWords,
  keyShown,
  ladder,
  lineFields,
  operations,
  parseLines,
  shortHash,
  withDraft,
} from "@/sections/settings/model";
import { sampleConfig } from "@/test/settingsFixtures";

function keyOf(name: string, o: Partial<ConfigKey> = {}): ConfigKey {
  const k = sampleConfig().keys.find((x) => x.key === name);
  if (k === undefined) throw new Error(`no key ${name}`);
  return { ...k, ...o };
}

describe("settings model (T039 ST-U)", () => {
  it("precedence ladder", () => {
    // Row ST-49: browser > repository > built-in; exactly one rung in use.
    const labels = keyOf("ui.labels"); // file: plain at line 4
    let r = ladder(labels, { text: "technical" });
    expect(r.map((x) => x.scope)).toEqual(["browser", "repository", "builtin"]);
    expect(r.filter((x) => x.inUse).map((x) => x.scope)).toEqual(["browser"]);
    expect(inUseWords(r)).toBe("technical, from this browser");

    r = ladder(labels, { text: null });
    expect(r.filter((x) => x.inUse).map((x) => x.scope)).toEqual(["repository"]);
    expect(r[1]?.line).toBe(4);
    expect(inUseWords(r)).toBe("plain, from graphvein.yaml");

    const editor = keyOf("ide.editor"); // origin builtin
    r = ladder(editor, { text: null });
    expect(r.filter((x) => x.inUse).map((x) => x.scope)).toEqual(["builtin"]);
    expect(r[1]?.text).toBe("not set");
    expect(inUseWords(r)).toBe("vscode, from the built-in default");

    // ide.link_template locks ide.editor: the template is the repository
    // value at its line; neither the file's editor nor the built-in
    // default is in use.
    const locked = keyOf("ide.editor", { editable: false, locked_by: { key: "ide.link_template", line: 3 } });
    r = ladder(locked, { text: null });
    expect(r.filter((x) => x.inUse).map((x) => x.scope)).toEqual(["repository"]);
    expect(r[1]?.line).toBe(3);
    expect(inUseWords(r)).toBe("the link template (ide.link_template), from graphvein.yaml line 3");
    r = ladder({ ...locked, origin: "file", value: "cursor", line: 2 }, { text: null });
    expect(r[1]?.text).toBe("the link template (ide.link_template); ide.editor: cursor is not used");
    expect(r[1]?.line).toBe(3);
    r = ladder(locked, { text: "Zed" });
    expect(r.filter((x) => x.inUse).map((x) => x.scope)).toEqual(["browser"]);

    // A key without a browser choice has two rungs.
    r = ladder(keyOf("index.shape_depth"), null);
    expect(r.map((x) => x.scope)).toEqual(["repository", "builtin"]);

    // Not set and no default: nothing in use.
    r = ladder(keyOf("tests.include"), null);
    expect(r.some((x) => x.inUse)).toBe(false);
    expect(inUseWords(r)).toBe("nothing: not set");
  });

  it("an unknown origin shows no value and never the built-in default as in use", () => {
    const k = keyOf("index.shape_depth", { origin: "unknown", value: null, line: null });
    const r = ladder(k, null);
    expect(r[0]?.text).toBe("unknown");
    expect(r[0]?.unknown).toBe(true);
    expect(r.some((x) => x.inUse)).toBe(false);
    expect(inUseWords(r)).toBe("unknown: graphvein.yaml could not be read");
    // A browser choice still applies (it does not depend on the file).
    const l = ladder(keyOf("ui.labels", { origin: "unknown", value: null, line: null }), { text: "plain" });
    expect(l.filter((x) => x.inUse).map((x) => x.scope)).toEqual(["browser"]);
    expect(changedFromDefault(k, null)).toBe(false);
  });

  it("filters: changed from default and this browser only; drafted keys always show", () => {
    const labels = keyOf("ui.labels");
    const depth = keyOf("index.shape_depth");
    const base = keyOf("openapi.base_paths");
    expect(keyShown("all", depth, null, false)).toBe(true);
    // depth equals its default (6 = 6): not changed.
    expect(keyShown("changed", depth, null, false)).toBe(false);
    expect(keyShown("changed", depth, null, true)).toBe(true);
    expect(keyShown("changed", base, null, false)).toBe(true);
    expect(keyShown("changed", labels, { text: null }, false)).toBe(false);
    expect(keyShown("changed", labels, { text: "technical" }, false)).toBe(true);
    expect(keyShown("browser", labels, { text: null }, false)).toBe(true);
    expect(keyShown("browser", depth, null, false)).toBe(false);
  });

  it("drafts: set, remove, back to the file's value, operations in form order", () => {
    const config = sampleConfig();
    const depth = keyOf("index.shape_depth");
    const include = keyOf("tests.include");
    const base = keyOf("openapi.base_paths");
    let d = withDraft({}, depth, 8);
    expect(d["index.shape_depth"]).toEqual({ op: "set", value: 8 });
    expect(draftValue(d, depth)).toBe(8);
    d = withDraft(d, depth, 6);
    expect(d["index.shape_depth"]).toBeUndefined();
    // Emptying a key that is not in the file is no change.
    d = withDraft(d, include, []);
    expect(Object.keys(d)).toEqual([]);
    d = withDraft(d, base, null);
    expect(d["openapi.base_paths"]).toEqual({ op: "remove" });
    expect(draftValue(d, base)).toBeNull();
    d = withDraft(d, include, ["e2e/**"]);
    d = withDraft(d, depth, 3);
    expect(operations(config, d)).toEqual([
      { op: "set", key: "index.shape_depth", value: 3 },
      { op: "set", key: "tests.include", value: ["e2e/**"] },
      { op: "remove", key: "openapi.base_paths" },
    ]);
    const s = draftSummary(config, d);
    expect(s).toEqual({ count: 3, nextRun: 3 });
    expect(appliesWords(s)).toBe("all apply at the next index run");
    expect(appliesWords({ count: 2, nextRun: 2 })).toBe("both apply at the next index run");
    expect(appliesWords({ count: 1, nextRun: 0 })).toBe("applies at once");
    expect(appliesWords({ count: 3, nextRun: 1 })).toBe("1 apply at the next index run, the others at once");
  });

  it("parses list and map lines; a malformed map line is refused", () => {
    expect(parseLines("a\n\n b \n", "list")).toEqual(["a", "b"]);
    expect(parseLines("  \n", "list")).toBeNull();
    expect(parseLines("apps/x: /api\nweb: /v1", "map")).toEqual([
      { key: "apps/x", value: "/api" },
      { key: "web", value: "/v1" },
    ]);
    expect(parseLines("apps/x /api", "map")).toBeUndefined();
  });

  it("YAML view: each line names the field it feeds; nothing for a file that does not load", () => {
    const f = lineFields(sampleConfig());
    expect(f.get(4)).toEqual({ text: "Label style", tone: "field" });
    expect(f.get(6)).toEqual({ text: "Route shape depth", tone: "field" });
    expect(f.get(9)).toEqual({ text: "OpenAPI base paths", tone: "field" });
    expect(f.get(11)).toEqual({ text: "Explorer path values", tone: "editor" });
    expect(f.get(14)).toEqual({ text: "no form", tone: "none" });
    expect(f.get(1)).toBeUndefined();
    expect(lineFields(sampleConfig({ config_error: "yaml: line 3: bad" })).size).toBe(0);
  });

  it("shortens hashes", () => {
    expect(shortHash("sha256:3f9c0000")).toBe("sha256:3f9c…");
    expect(shortHash("absent")).toBe("no file yet");
    expect(shortHash(null)).toBe("unknown");
  });
});
