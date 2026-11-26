import { act, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { VirtualList, findItem, type VirtualItem } from "@/components/VirtualList";

type Item = VirtualItem & { group: string };

function makeItems(groups: number, rowsPerGroup: number): Item[] {
  const out: Item[] = [];
  for (let g = 0; g < groups; g++) {
    out.push({ key: `h${String(g)}`, kind: "header", group: `G${String(g)}` });
    for (let r = 0; r < rowsPerGroup; r++) out.push({ key: `g${String(g)}r${String(r)}`, kind: "row", group: `G${String(g)}` });
  }
  return out;
}

function List({ items, initial = null }: { items: Item[]; initial?: string | null }) {
  const [sel, setSel] = useState<string | null>(initial);
  return (
    <VirtualList
      items={items}
      rowHeight={34}
      headerHeight={36}
      overscan={5}
      fallbackHeight={340}
      selectedKey={sel}
      onSelect={setSel}
      optionId={(k) => `opt-${k}`}
      optionLabel={(i) => i.key}
      ariaLabel="Endpoints"
      renderRow={(i) => <span>{i.key}</span>}
      renderHeader={(i) => <span>{i.group}</span>}
    />
  );
}

describe("findItem", () => {
  it("finds the item at an offset", () => {
    const offsets = [0, 36, 70, 104];
    expect(findItem(offsets, 0)).toBe(0);
    expect(findItem(offsets, 35)).toBe(0);
    expect(findItem(offsets, 36)).toBe(1);
    expect(findItem(offsets, 1000)).toBe(3);
    expect(findItem(offsets, -5)).toBe(0);
  });
});

describe("VirtualList", () => {
  it("renders only the window plus overscan for 5,000 rows", () => {
    render(<List items={makeItems(1, 5000)} />);
    const options = screen.getAllByRole("option");
    // 340 px / 34 px = 10 rows plus the header, plus 5 overscan below.
    expect(options.length).toBeLessThan(20);
    expect(options[0]?.getAttribute("aria-setsize")).toBe("5000");
    expect(options[0]?.getAttribute("aria-posinset")).toBe("1");
  });

  it("updates the window when scrolled", () => {
    render(<List items={makeItems(1, 5000)} />);
    const box = screen.getByRole("listbox");
    act(() => {
      box.scrollTop = 36 + 34 * 2000;
      fireEvent.scroll(box);
    });
    const keys = screen.getAllByRole("option").map((o) => o.textContent);
    expect(keys).toContain("g0r2000");
    expect(keys).not.toContain("g0r0");
    expect(keys.length).toBeLessThan(25);
  });

  it("moves the selection with the keyboard and keeps it in view", () => {
    render(<List items={makeItems(2, 100)} />);
    const box = screen.getByRole("listbox");
    fireEvent.keyDown(box, { key: "ArrowDown" });
    expect(box.getAttribute("aria-activedescendant")).toBe("opt-g0r0");
    fireEvent.keyDown(box, { key: "ArrowDown" });
    expect(box.getAttribute("aria-activedescendant")).toBe("opt-g0r1");
    fireEvent.keyDown(box, { key: "ArrowUp" });
    expect(box.getAttribute("aria-activedescendant")).toBe("opt-g0r0");
    fireEvent.keyDown(box, { key: "End" });
    expect(box.getAttribute("aria-activedescendant")).toBe("opt-g1r99");
    // The last row is at the bottom of the viewport.
    const lastTop = 36 * 2 + 34 * 199;
    expect(box.scrollTop).toBe(lastTop + 34 - 340);
    expect(screen.getByRole("option", { selected: true }).textContent).toBe("g1r99");
    fireEvent.keyDown(box, { key: "Home" });
    expect(box.getAttribute("aria-activedescendant")).toBe("opt-g0r0");
    expect(box.scrollTop).toBe(0);
    fireEvent.keyDown(box, { key: "PageDown" });
    expect(box.getAttribute("aria-activedescendant")).toBe("opt-g0r9"); // one page: 340 / 34 - 1 rows
    fireEvent.keyDown(box, { key: "PageUp" });
    expect(box.getAttribute("aria-activedescendant")).toBe("opt-g0r0");
  });

  it("scrolls a deep-linked selection into view", () => {
    render(<List items={makeItems(1, 1000)} initial="g0r600" />);
    expect(screen.getByRole("option", { selected: true }).textContent).toBe("g0r600");
  });

  it("shows the current group in the sticky header", () => {
    render(<List items={makeItems(3, 50)} />);
    const box = screen.getByRole("listbox");
    expect(screen.getByTestId("sticky-header").textContent).toBe("G0");
    act(() => {
      box.scrollTop = 36 + 34 * 50 + 36 + 34 * 10; // inside group 1
      fireEvent.scroll(box);
    });
    expect(screen.getByTestId("sticky-header").textContent).toBe("G1");
    act(() => {
      box.scrollTop = (36 + 34 * 50) * 2 + 5; // just inside group 2
      fireEvent.scroll(box);
    });
    expect(screen.getByTestId("sticky-header").textContent).toBe("G2");
  });

  it("selects a row on click", () => {
    render(<List items={makeItems(1, 10)} />);
    fireEvent.click(screen.getByText("g0r3"));
    expect(screen.getByRole("option", { selected: true }).textContent).toBe("g0r3");
  });
});
