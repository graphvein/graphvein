// The mark of an editor in the picker, the sidebar control and the IDE
// buttons (T016k). Official logos only for the editors whose brand
// guidelines allow it, under their conditions (ADR 0050,
// src/assets/editor-logos/README.md):
// - VS Code: the blue icon, unaltered, with clear space of at least 25% of
//   the icon around it (the box is always larger than the icon by that);
// - IntelliJ IDEA: the product icon, unaltered, never below 24 px, so the
//   small (button) size shows the monogram instead;
// - Zed: the logomark in full black, or full white on the dark theme;
// - Cursor: the 2D cube, the brand package's light-theme or dark-theme
//   file (T016k2; cursor.com/brand states no clear space or minimum size).
// Every other editor (VS Code Insiders, VSCodium, custom templates) gets a
// neutral monogram tile. The attribution lines are on
// the third-party licences page only.
//
// The source marker and caveat badge never touch an official logo or its
// clear space (Microsoft: nothing added to the icon; JetBrains: unaltered,
// never combined with other symbols, clear space of half the black
// square's width): next to a logo they sit in a column beside the tile.
// Only monograms, which are Graphvein's own drawing, carry them on the
// corners. CLEAR_SPACE records each logo's rule for the geometry test.

import cursorDark from "@/assets/editor-logos/cursor-cube-2d-dark.svg";
import cursorLight from "@/assets/editor-logos/cursor-cube-2d-light.svg";
import intellijLogo from "@/assets/editor-logos/intellij-idea.svg";
import vscodeLogo from "@/assets/editor-logos/vscode.svg";
import zedBlack from "@/assets/editor-logos/zed-logomark-black.png";
import zedWhite from "@/assets/editor-logos/zed-logomark-white.png";
import { cn } from "@/lib/utils";

/** MarkSize is the size of a mark: xs in a provenance chip's button, sm
 * in the IDE buttons, md in the picker and the sidebar. */
export type MarkSize = "xs" | "sm" | "md";

/** The two letters of each preset's monogram (round-2 mockups). */
const MONOGRAM: Record<string, string> = {
  vscode: "VS",
  "vscode-insiders": "VI",
  vscodium: "VC",
  jetbrains: "IJ",
  zed: "Ze",
};

/** What a mark stands for: a preset, a custom repository template (letters
 * from its label), "No editor links", or no link at all (an error). */
export type MarkKind = { kind: "preset"; editor: string } | { kind: "custom"; letters: string } | { kind: "off" } | { kind: "none" };

/** The source marker on the corner: your choice (filled dot), the
 * repository default (ring), or none (Graphvein's default). */
export type SourceMarker = "yours" | "repo" | null;

/** The caveat badge on the bottom corner. */
export type MarkBadge = "warn" | "bad" | "info" | null;

const SIZE = {
  // Provenance chips' icon-only buttons (T029): a bare 14 px box. VS Code
  // keeps its 25% clear space (9 px icon, 2.5 px each side); IntelliJ IDEA
  // shows the monogram, which drops its angle brackets to fit.
  xs: { box: "h-[14px] w-[14px] rounded-[3px] text-[7.5px]", vscode: 9, zed: 11, intellij: 0, cursor: 11 },
  // Buttons: a bare 18 px box.
  sm: { box: "h-[18px] w-[20px] rounded-[4px] text-[8.5px]", vscode: 11, zed: 13, intellij: 0, cursor: 13 },
  // Picker rows, the sidebar row and the rail button: a 28 px tile.
  md: { box: "h-[28px] w-[30px] rounded-[6px] text-[10.5px]", vscode: 16, zed: 18, intellij: 24, cursor: 18 },
} as const;

/** CURSOR_ASPECT is the Cursor cube's width over its height (the SVG
 * viewBox 466.73 x 532.09), so the logo is drawn at its own proportions:
 * SIZE gives its height and the width follows. */
export const CURSOR_ASPECT = 466.73 / 532.09;

/** CLEAR_SPACE is each logo's required clear space on every side, as a
 * fraction of the drawn logo's width: VS Code 25% (code.visualstudio.com/
 * brand); IntelliJ IDEA half the black square's width around the square,
 * which in the 64-unit icon (square 12..52) reaches 8 units past the
 * icon's edge, 1/8; Zed states none (zed.dev/brand); Cursor states none
 * (cursor.com/brand and its asset package, T016k2). Exported for tests. */
export const CLEAR_SPACE: Record<string, number> = { vscode: 0.25, jetbrains: 0.125, zed: 0, cursor: 0 };

function Monogram({ letters, bare = false }: { letters: string; bare?: boolean }) {
  if (bare) return <span className="font-mono leading-none font-bold tracking-[-.04em]">{letters}</span>;
  return (
    <span className="inline-flex items-center font-mono leading-none font-bold tracking-[-.02em]">
      <i className="mx-[.5px] font-medium text-muted-foreground not-italic">‹</i>
      {letters}
      <i className="mx-[.5px] font-medium text-muted-foreground not-italic">›</i>
    </span>
  );
}

function Logo({ editor, size }: { editor: string; size: MarkSize }) {
  const s = SIZE[size];
  if (editor === "vscode") return <img src={vscodeLogo} alt="" width={s.vscode} height={s.vscode} draggable={false} data-testid="editor-logo" />;
  if (editor === "zed") {
    return (
      <picture>
        <source srcSet={zedWhite} media="(prefers-color-scheme: dark)" />
        <img src={zedBlack} alt="" width={s.zed} height={s.zed} draggable={false} data-testid="editor-logo" />
      </picture>
    );
  }
  if (editor === "cursor") {
    // The brand package's LIGHT file (dark cube) is for light backgrounds,
    // its DARK file (light cube) for dark ones. Inline sizes keep the exact
    // aspect ratio, which integer width/height attributes would round.
    const style = { width: s.cursor * CURSOR_ASPECT, height: s.cursor };
    return (
      <picture>
        <source srcSet={cursorDark} media="(prefers-color-scheme: dark)" />
        <img src={cursorLight} alt="" style={style} draggable={false} data-testid="editor-logo" />
      </picture>
    );
  }
  if (editor === "jetbrains" && s.intellij >= 24) {
    return <img src={intellijLogo} alt="" width={s.intellij} height={s.intellij} draggable={false} data-testid="editor-logo" />;
  }
  return null;
}

/** hasLogo says whether a preset shows its official logo at size. */
export function hasLogo(editor: string, size: MarkSize): boolean {
  return editor === "vscode" || editor === "zed" || editor === "cursor" || (editor === "jetbrains" && SIZE[size].intellij >= 24);
}

/** EditorMark draws the tile with its marker and badge: on the tile's
 * corners for a monogram, in a column beside the tile for an official logo
 * (see the file comment). It is decorative: the control around it carries
 * the accessible name. */
export function EditorMark({
  mark,
  size = "md",
  marker = null,
  badge = null,
  className,
}: {
  mark: MarkKind;
  size?: MarkSize;
  marker?: SourceMarker;
  badge?: MarkBadge;
  className?: string;
}) {
  const logo = mark.kind === "preset" && hasLogo(mark.editor, size);
  const letters =
    mark.kind === "preset" ? (MONOGRAM[mark.editor] ?? mark.editor.slice(0, 2)) : mark.kind === "custom" ? mark.letters : mark.kind === "off" ? "–" : "!";
  const beside = logo && (marker !== null || badge !== null);
  const markerDot = marker !== null && (
    <span
      data-testid="editor-marker"
      className={cn(
        "size-2 rounded-full shadow-[0_0_0_1.5px_hsl(var(--background))]",
        !beside && "absolute -top-1 -right-1",
        marker === "yours" ? "bg-brand" : "border-2 border-muted-foreground bg-background",
      )}
    />
  );
  const badgeIcon = badge !== null && (
    <span
      data-testid="editor-badge"
      className={cn(
        "grid size-[13px] place-items-center rounded-full bg-background text-[9px] leading-none font-bold",
        !beside && "absolute -right-1.5 -bottom-1.5",
        badge === "warn" ? "text-dynamic" : badge === "bad" ? "text-unmatched" : "text-muted-foreground",
      )}
    >
      <BadgeIcon badge={badge} />
    </span>
  );
  const tile = (
    <span
      data-testid="editor-tile"
      className={cn(
        "relative inline-grid flex-none place-items-center border",
        SIZE[size].box,
        logo ? "border-transparent bg-transparent" : "border-border bg-muted text-foreground",
        (mark.kind === "custom" || mark.kind === "off") && "border-dashed bg-transparent",
        mark.kind === "off" && "text-muted-foreground",
        mark.kind === "none" && "border-unmatched/45 bg-unmatched-soft text-unmatched",
        // The tile's border frames VS Code and Zed outside their clear
        // space; IntelliJ IDEA's clear space reaches the tile's edge, so its
        // tile has no visible border.
        size === "md" && logo && mark.editor !== "jetbrains" && "border-border bg-background",
      )}
    >
      {mark.kind === "preset" && logo ? <Logo editor={mark.editor} size={size} /> : <Monogram letters={letters} bare={size === "xs"} />}
      {!beside && markerDot}
      {!beside && badgeIcon}
    </span>
  );
  const data = {
    "aria-hidden": true,
    "data-testid": "editor-mark",
    "data-mark": mark.kind === "preset" ? mark.editor : mark.kind,
    "data-logo": logo ? "true" : undefined,
    "data-marker": marker ?? undefined,
    "data-badge": badge ?? undefined,
  } as const;
  if (!beside) {
    return (
      <span {...data} className={cn("inline-flex flex-none", className)}>
        {tile}
      </span>
    );
  }
  // Beside the logo: a 13 px column, 2 px from the tile, marker on top and
  // badge at the bottom, so neither reaches the logo or its clear space.
  return (
    <span {...data} className={cn("inline-flex flex-none items-center gap-[2px]", className)}>
      {tile}
      <span data-testid="editor-mark-side" className="grid h-[28px] w-[13px] grid-rows-[13px_1fr_13px] justify-items-center">
        <span className="grid place-items-center">{markerDot}</span>
        <span />
        <span>{badgeIcon}</span>
      </span>
    </span>
  );
}

function BadgeIcon({ badge }: { badge: "warn" | "bad" | "info" }) {
  if (badge === "info") {
    return (
      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round">
        <circle cx="12" cy="12" r="9.5" />
        <path d="M12 11v5M12 7.5v.5" />
      </svg>
    );
  }
  return (
    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
      <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
      <path d="M12 9v4M12 17h.01" />
    </svg>
  );
}
