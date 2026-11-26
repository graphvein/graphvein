import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import cursorDark from "@/assets/editor-logos/cursor-cube-2d-dark.svg";
import cursorLight from "@/assets/editor-logos/cursor-cube-2d-light.svg";
import { CLEAR_SPACE, CURSOR_ASPECT, EditorMark, type MarkBadge, type SourceMarker } from "@/components/EditorMark";

// The DOM structure behind the brand terms (A1): on an official logo the
// marker and badge sit in the side column, never inside the tile; on a
// monogram they keep the corner overlay. Playwright measures the geometry.
describe("EditorMark", () => {
  const markers: SourceMarker[] = ["yours", "repo", null];
  const badges: MarkBadge[] = ["warn", "bad", "info", null];

  it.each(["vscode", "jetbrains", "zed", "cursor"])("keeps marker and badge out of the %s logo tile", (editor) => {
    for (const marker of markers) {
      for (const badge of badges) {
        const { unmount } = render(<EditorMark mark={{ kind: "preset", editor }} marker={marker} badge={badge} />);
        const tile = screen.getByTestId("editor-tile");
        expect(tile.querySelector('[data-testid="editor-logo"]')).not.toBeNull();
        expect(tile.querySelector('[data-testid="editor-marker"], [data-testid="editor-badge"]')).toBeNull();
        const side = screen.queryByTestId("editor-mark-side");
        expect(side !== null).toBe(marker !== null || badge !== null);
        expect(screen.queryAllByTestId("editor-marker")).toHaveLength(marker === null ? 0 : 1);
        expect(screen.queryAllByTestId("editor-badge")).toHaveLength(badge === null ? 0 : 1);
        if (side !== null) {
          for (const el of side.querySelectorAll("[data-testid]")) expect(el.className).not.toMatch(/\babsolute\b/);
        }
        unmount();
      }
    }
  });

  it("draws no border inside IntelliJ IDEA's clear space", () => {
    render(<EditorMark mark={{ kind: "preset", editor: "jetbrains" }} />);
    expect(screen.getByTestId("editor-tile").className).toContain("border-transparent");
    expect(screen.getByTestId("editor-tile").className).not.toContain("border-border");
  });

  it("keeps the corner overlay on monograms and the small IntelliJ monogram", () => {
    for (const mark of [{ kind: "preset", editor: "vscodium" }, { kind: "custom", letters: "Su" }, { kind: "off" }] as const) {
      const { unmount } = render(<EditorMark mark={mark} marker="yours" badge="warn" />);
      const tile = screen.getByTestId("editor-tile");
      expect(tile.querySelector('[data-testid="editor-marker"]')).not.toBeNull();
      expect(tile.querySelector('[data-testid="editor-badge"]')).not.toBeNull();
      expect(screen.queryByTestId("editor-mark-side")).toBeNull();
      unmount();
    }
    render(<EditorMark mark={{ kind: "preset", editor: "jetbrains" }} size="sm" />);
    expect(screen.getByTestId("editor-mark").getAttribute("data-logo")).toBeNull();
    expect(screen.queryByTestId("editor-logo")).toBeNull();
  });

  it("records a clear-space rule for every logo", () => {
    expect(CLEAR_SPACE).toEqual({ vscode: 0.25, jetbrains: 0.125, zed: 0, cursor: 0 });
  });

  // T016k2: Cursor shows its official 2D cube at both sizes (the picker and
  // the "Open in" buttons), the dark-theme file under a dark OS scheme,
  // drawn at the cube's own aspect ratio.
  it.each(["sm", "md"] as const)("draws the Cursor cube, not a monogram, at size %s", (size) => {
    expect(cursorLight).not.toBe(cursorDark);
    render(<EditorMark mark={{ kind: "preset", editor: "cursor" }} size={size} />);
    expect(screen.getByTestId("editor-mark").getAttribute("data-logo")).toBe("true");
    expect(screen.getByTestId("editor-tile").textContent).toBe("");
    const img = screen.getByTestId<HTMLImageElement>("editor-logo");
    expect(img.getAttribute("src")).toBe(cursorLight);
    const source = img.parentElement?.querySelector("source");
    expect(source?.getAttribute("media")).toBe("(prefers-color-scheme: dark)");
    expect(source?.getAttribute("srcset")).toBe(cursorDark);
    const height = size === "sm" ? 13 : 18;
    expect(img.style.height).toBe(`${String(height)}px`);
    expect(parseFloat(img.style.width) / parseFloat(img.style.height)).toBeCloseTo(CURSOR_ASPECT, 5);
  });
});
