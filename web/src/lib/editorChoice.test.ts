import { afterEach, describe, expect, it, vi } from "vitest";

import { EDITOR_KEY, MAX_STORED, OFF, readChoice, resolveChoice, subscribeChoice, writeChoice } from "@/lib/editorChoice";

const PRESETS = ["vscode", "vscode-insiders", "cursor", "vscodium", "jetbrains", "zed"];

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe("editorChoice", () => {
  it("uses the versioned key", () => {
    expect(EDITOR_KEY).toBe("graphvein.editor.v1");
  });

  it("reads, writes and clears the choice", () => {
    expect(readChoice()).toEqual({ value: null, saved: true });
    expect(writeChoice("zed")).toBe(true);
    expect(window.localStorage.getItem(EDITOR_KEY)).toBe("zed");
    expect(readChoice()).toEqual({ value: "zed", saved: true });
    expect(writeChoice(OFF)).toBe(true);
    expect(readChoice().value).toBe("off");
    expect(writeChoice(null)).toBe(true);
    expect(window.localStorage.getItem(EDITOR_KEY)).toBeNull();
  });

  it("never reads another version's key", () => {
    window.localStorage.setItem("graphvein.editor.v2", "zed");
    window.localStorage.setItem("graphvein.editor", "zed");
    expect(readChoice().value).toBeNull();
  });

  it("ignores an unlisted or overlong value and keeps it", () => {
    expect(resolveChoice("zed", PRESETS)).toEqual({ choice: "zed", ignored: null });
    expect(resolveChoice(OFF, PRESETS)).toEqual({ choice: "off", ignored: null });
    expect(resolveChoice(null, PRESETS)).toEqual({ choice: null, ignored: null });
    expect(resolveChoice("sublime", PRESETS)).toEqual({ choice: null, ignored: "sublime" });
    expect(resolveChoice("Zed", PRESETS)).toEqual({ choice: null, ignored: "Zed" });
    const long = "z".repeat(MAX_STORED + 1);
    expect(resolveChoice(long, [...PRESETS, long])).toEqual({ choice: null, ignored: long });
    window.localStorage.setItem(EDITOR_KEY, "sublime");
    resolveChoice(readChoice().value, PRESETS);
    expect(window.localStorage.getItem(EDITOR_KEY)).toBe("sublime");
  });

  it("reports blocked storage as not saved", () => {
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(blocked);
    expect(readChoice()).toEqual({ value: null, saved: false });
    expect(writeChoice("zed")).toBe(false);
    expect(writeChoice(null)).toBe(false);
  });

  it("follows other tabs through the storage event", () => {
    const seen: (string | null)[] = [];
    const stop = subscribeChoice((v) => seen.push(v));
    window.dispatchEvent(new StorageEvent("storage", { key: EDITOR_KEY, newValue: "zed" }));
    window.dispatchEvent(new StorageEvent("storage", { key: "graphvein.api.listWidth", newValue: "300" }));
    window.dispatchEvent(new StorageEvent("storage", { key: EDITOR_KEY, newValue: null }));
    window.dispatchEvent(new StorageEvent("storage", { key: null }));
    stop();
    window.dispatchEvent(new StorageEvent("storage", { key: EDITOR_KEY, newValue: "cursor" }));
    expect(seen).toEqual(["zed", null, null]);
  });
});
