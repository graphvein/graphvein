import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { MIN_BOTTOM, MIN_TOP, SPLIT_STEP, VerticalSplit, clampBottom, defaultBottom } from "@/components/VerticalSplit";

const KEY = "graphvein.test.split";

function bottomHeight(): number {
  return Number.parseInt(screen.getByTestId("split-bottom").style.height, 10);
}

beforeEach(() => {
  window.localStorage.clear();
});

describe("VerticalSplit", () => {
  it("clamps the bottom height to both minimums", () => {
    expect(defaultBottom(1000)).toBe(540);
    expect(clampBottom(10, 1000)).toBe(MIN_BOTTOM);
    expect(clampBottom(5000, 1000)).toBe(1000 - MIN_TOP);
    expect(clampBottom(500, 200)).toBe(MIN_BOTTOM);
  });

  it("takes a smaller bottom minimum when a caller asks for one", () => {
    expect(clampBottom(10, 1000, MIN_TOP, 100)).toBe(100);
    expect(clampBottom(500, 400, 290, 100)).toBe(110);
    expect(clampBottom(500, 300, 290, 100)).toBe(100);
    render(<VerticalSplit top={<p>top</p>} bottom={<p>bottom</p>} storageKey={KEY} label="Resize" fallbackHeight={400} minTop={290} minBottom={100} />);
    expect(bottomHeight()).toBe(110);
    const handle = screen.getByRole("separator", { name: "Resize" });
    expect(handle.getAttribute("aria-valuemin")).toBe("100");
    fireEvent.keyDown(handle, { key: "End" });
    expect(bottomHeight()).toBe(100);
  });

  it("shows no handle without a bottom pane", () => {
    render(<VerticalSplit top={<p>top</p>} bottom={null} storageKey={KEY} label="Resize" />);
    expect(screen.queryByRole("separator")).toBeNull();
  });

  it("resizes with the keyboard, keeps the height and resets on double-click", () => {
    render(<VerticalSplit top={<p>top</p>} bottom={<p>bottom</p>} storageKey={KEY} label="Resize" fallbackHeight={1000} />);
    const handle = screen.getByRole("separator", { name: "Resize" });
    expect(handle.getAttribute("aria-orientation")).toBe("horizontal");
    expect(bottomHeight()).toBe(540);
    fireEvent.keyDown(handle, { key: "ArrowUp" });
    expect(bottomHeight()).toBe(540 + SPLIT_STEP);
    fireEvent.keyDown(handle, { key: "ArrowDown" });
    fireEvent.keyDown(handle, { key: "ArrowDown" });
    expect(bottomHeight()).toBe(540 - SPLIT_STEP);
    expect(window.localStorage.getItem(KEY)).toBe(String(540 - SPLIT_STEP));
    fireEvent.keyDown(handle, { key: "End" });
    expect(bottomHeight()).toBe(MIN_BOTTOM);
    fireEvent.keyDown(handle, { key: "Home" });
    expect(bottomHeight()).toBe(1000 - MIN_TOP);
    fireEvent.doubleClick(handle);
    expect(bottomHeight()).toBe(540);
    expect(window.localStorage.getItem(KEY)).toBeNull();
  });

  it("takes a smaller default share and a taller top minimum, with a height per key", () => {
    const other = "graphvein.test.split.other";
    const flow = <VerticalSplit top={<p>top</p>} bottom={<p>bottom</p>} storageKey={other} label="Resize" fallbackHeight={1000} defaultFraction={0.3} minTop={500} />;
    const { rerender } = render(flow);
    expect(bottomHeight()).toBe(300);
    fireEvent.keyDown(screen.getByRole("separator", { name: "Resize" }), { key: "Home" });
    expect(bottomHeight()).toBe(500);
    expect(window.localStorage.getItem(other)).toBe("500");
    // Switching keys reads that key's height (none here: the default).
    rerender(<VerticalSplit top={<p>top</p>} bottom={<p>bottom</p>} storageKey={KEY} label="Resize" fallbackHeight={1000} />);
    expect(bottomHeight()).toBe(540);
    rerender(flow);
    expect(bottomHeight()).toBe(500);
  });
});
