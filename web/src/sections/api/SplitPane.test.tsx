import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SplitPane, WIDTH_KEY, clampWidth, defaultWidth } from "@/sections/api/SplitPane";

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

function pane() {
  render(<SplitPane list={<div>list</div>} detail={<div>detail</div>} fallbackWidth={1100} />);
  return screen.getByRole("separator");
}

describe("SplitPane", () => {
  it("defaults to 42 % clamped to 360-560 and clamps to the minimums", () => {
    expect(defaultWidth(1100)).toBe(462);
    expect(defaultWidth(600)).toBe(360);
    expect(defaultWidth(2000)).toBe(560);
    expect(clampWidth(100, 1100)).toBe(320);
    expect(clampWidth(900, 1100)).toBe(660);
    expect(clampWidth(500, 600)).toBe(320);
  });

  it("is a focusable vertical separator with its value", () => {
    const sep = pane();
    expect(sep.getAttribute("aria-orientation")).toBe("vertical");
    expect(sep.tabIndex).toBe(0);
    expect(sep.getAttribute("aria-valuenow")).toBe("462");
    expect(sep.getAttribute("aria-valuemin")).toBe("320");
    expect(sep.getAttribute("aria-valuemax")).toBe("660");
  });

  it("moves by 16 px with Left/Right, clamps, stores, and resets on double-click", () => {
    const sep = pane();
    fireEvent.keyDown(sep, { key: "ArrowRight" });
    expect(sep.getAttribute("aria-valuenow")).toBe("478");
    expect(window.localStorage.getItem(WIDTH_KEY)).toBe("478");
    fireEvent.keyDown(sep, { key: "ArrowLeft" });
    fireEvent.keyDown(sep, { key: "ArrowLeft" });
    expect(sep.getAttribute("aria-valuenow")).toBe("446");
    for (let i = 0; i < 30; i++) fireEvent.keyDown(sep, { key: "ArrowLeft" });
    expect(sep.getAttribute("aria-valuenow")).toBe("320");
    for (let i = 0; i < 40; i++) fireEvent.keyDown(sep, { key: "ArrowRight" });
    expect(sep.getAttribute("aria-valuenow")).toBe("660");
    fireEvent.doubleClick(sep);
    expect(sep.getAttribute("aria-valuenow")).toBe("462");
    expect(window.localStorage.getItem(WIDTH_KEY)).toBeNull();
  });

  it("restores a stored width", () => {
    window.localStorage.setItem(WIDTH_KEY, "400");
    expect(pane().getAttribute("aria-valuenow")).toBe("400");
  });

  it("works when storage throws", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    const sep = pane();
    expect(sep.getAttribute("aria-valuenow")).toBe("462");
    fireEvent.keyDown(sep, { key: "ArrowRight" });
    expect(sep.getAttribute("aria-valuenow")).toBe("478");
  });
});
