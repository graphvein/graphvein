import { afterEach, describe, expect, it, vi } from "vitest";

import { BANNER_KEY, LABELS_KEY, readLabels, subscribeLabels, writeBannerDismissed, writeStyle } from "@/lib/labelChoice";

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe("labelChoice", () => {
  it("uses versioned keys", () => {
    expect(LABELS_KEY).toBe("graphvein.labels.v1");
    expect(BANNER_KEY).toBe("graphvein.labels-banner.v1");
  });

  it("reads nothing chosen on first launch, then the choice and the dismissal", () => {
    expect(readLabels()).toEqual({ style: null, bannerDismissed: false, saved: true });
    expect(writeStyle("technical")).toBe(true);
    expect(window.localStorage.getItem(LABELS_KEY)).toBe("technical");
    expect(writeBannerDismissed()).toBe(true);
    expect(readLabels()).toEqual({ style: "technical", bannerDismissed: true, saved: true });
    expect(writeStyle("plain")).toBe(true);
    expect(readLabels().style).toBe("plain");
  });

  it("ignores another value and another version's key", () => {
    window.localStorage.setItem(LABELS_KEY, "Technical");
    window.localStorage.setItem("graphvein.labels.v2", "technical");
    expect(readLabels().style).toBeNull();
  });

  it("reports blocked storage as not saved and reads as nothing chosen", () => {
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    expect(readLabels()).toEqual({ style: null, bannerDismissed: false, saved: false });
    expect(writeStyle("technical")).toBe(false);
    expect(writeBannerDismissed()).toBe(false);
  });

  it("follows other tabs through the storage event", () => {
    let n = 0;
    const stop = subscribeLabels(() => {
      n++;
    });
    window.dispatchEvent(new StorageEvent("storage", { key: LABELS_KEY, newValue: "technical" }));
    window.dispatchEvent(new StorageEvent("storage", { key: "graphvein.editor.v1", newValue: "zed" }));
    window.dispatchEvent(new StorageEvent("storage", { key: null }));
    stop();
    window.dispatchEvent(new StorageEvent("storage", { key: LABELS_KEY, newValue: "plain" }));
    expect(n).toBe(2);
  });
});
