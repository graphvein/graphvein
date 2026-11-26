// Small presentational pieces of the schema section: badges and glyphs for
// completeness, the unknown style, key chips, banners and the freshness
// banner. Class names are written out in full so Tailwind finds them.

import { Check, CircleAlert, TriangleAlert } from "lucide-react";
import type { KeyboardEvent, ReactNode } from "react";

import type { StatusView } from "@/hooks/useStatus";
import { cn } from "@/lib/utils";
import { BADGE } from "@/sections/api/parts";
import type { Cell, Completeness } from "@/sections/schema/cells";

/** HalfCircle is the quiet "complete as far as the migrations show" glyph
 * (◑ drawn as SVG so it has the same size in every font). */
export function HalfCircle({ className }: { className?: string }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" className={cn("size-3.5 flex-none", className)}>
      <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M8 2a6 6 0 0 1 0 12z" fill="currentColor" />
    </svg>
  );
}

const TONE: Record<Completeness["tone"], string> = {
  complete: "bg-linked-soft text-linked",
  in_source: "bg-muted text-foreground",
  partial: "bg-dynamic-soft text-dynamic border-dynamic/50",
  unknown: "border-dashed border-muted-foreground/70 text-muted-foreground",
};

/** CompletenessBadge is the full badge (detail header, differing rows). */
export function CompletenessBadge({ c, className }: { c: Completeness; className?: string }) {
  return (
    <span className={cn(BADGE, "font-medium", TONE[c.tone], className)} title={c.title} data-completeness={c.tone}>
      {c.tone === "complete" && <Check aria-hidden="true" className="size-3" />}
      {c.tone === "in_source" && <HalfCircle className="size-3" />}
      {c.tone === "partial" && <TriangleAlert aria-hidden="true" className="size-3" />}
      {c.text}
    </span>
  );
}

/** QuietGlyph is the quiet mark of a table in the set's common state:
 * check only for `complete`, half circle for `complete_in_source`. */
export function QuietGlyph({ c }: { c: Completeness }) {
  if (c.tone === "complete")
    return (
      <span title="Complete" data-completeness="complete" className="inline-flex text-linked">
        <Check aria-hidden="true" className="size-3.5" />
        <span className="sr-only">complete</span>
      </span>
    );
  return (
    <span title="Complete as far as the migrations show" data-completeness="in_source" className="inline-flex text-muted-foreground">
      <HalfCircle />
      <span className="sr-only">complete as far as the migrations show</span>
    </span>
  );
}

/** CellText shows a rendered value; unknown values are italic and muted,
 * with the raw value in the tooltip. */
export function CellText({ c, className }: { c: Cell; className?: string }) {
  return (
    <span className={cn(c.unknown && "text-muted-foreground italic", className)} title={c.title} data-rule={c.rule}>
      {c.tail !== undefined && c.text.endsWith(c.tail) ? (
        <>
          {c.text.slice(0, c.text.length - c.tail.length)}
          <span className="font-sans text-[11.5px] text-muted-foreground italic" data-testid="cell-tail">
            {c.tail}
          </span>
        </>
      ) : (
        c.text
      )}
    </span>
  );
}

/** KeyChipView draws a PK/UQ chip; unknown keys are a dashed "PK?" chip,
 * false draws nothing. */
export function KeyChipView({ c }: { c: Cell }) {
  if (c.text === "") return null;
  return (
    <span
      className={cn(
        "inline-flex h-[18px] items-center rounded-[4px] border px-1 font-mono text-[11px] font-semibold",
        c.unknown ? "border-dashed border-muted-foreground/70 text-muted-foreground" : "border-transparent bg-brand-soft text-brand-text",
      )}
      title={c.title}
      data-rule={c.rule}
    >
      {c.text}
    </span>
  );
}

/** Chip is a small labelled value in a header row. */
export function Chip({ label, children, title }: { label: string; children: ReactNode; title?: string }) {
  return (
    <span className="inline-flex items-baseline gap-1 text-[12.5px] text-muted-foreground" title={title}>
      {label} <span className="text-foreground">{children}</span>
    </span>
  );
}

/** Banner is a note above content (warn: amber; info: muted; error: red). */
export function Banner({ tone, children, testId }: { tone: "warn" | "info" | "error"; children: ReactNode; testId?: string }) {
  const Icon = tone === "info" ? CircleAlert : TriangleAlert;
  return (
    <div
      role="note"
      data-testid={testId}
      className={cn(
        "flex items-start gap-2 rounded-lg border px-3 py-2 text-[13px]",
        tone === "warn" && "border-stale/60 bg-stale/12",
        tone === "info" && "border-border bg-muted/50",
        tone === "error" && "border-unmatched/40 bg-unmatched-soft",
      )}
    >
      <Icon aria-hidden="true" className={cn("mt-0.5 size-4 flex-none", tone === "warn" ? "text-dynamic" : tone === "error" ? "text-unmatched" : "text-muted-foreground")} />
      <div className="min-w-0 [overflow-wrap:anywhere]">{children}</div>
    </div>
  );
}

/** FreshnessBanner is the API section's stale/unknown banner, plus the
 * note that files beyond the listed changed files are not marked. */
export function FreshnessBanner({ status }: { status: StatusView }) {
  if (status.kind === "loading") return null;
  if (status.kind === "ok") {
    const f = status.status.freshness;
    if (f.state === "fresh" || f.state === "not_indexed") return null;
    if (f.state === "stale") {
      const n = f.changed_count ?? f.changed_files.length;
      return (
        <Banner tone="warn" testId="stale-banner">
          <b className="font-semibold">The index is stale: {n === 1 ? "1 file changed" : `${String(n)} files changed`} since it was built.</b> Tables, columns and
          lines below describe the last index; files that changed are marked. Run graphvein index to update.
          {f.changed_files_truncated && ` Changed files beyond the first ${String(f.changed_files.length)} are not listed, so other files are not marked.`}
        </Banner>
      );
    }
  }
  return (
    <Banner tone="warn" testId="stale-banner">
      <b className="font-semibold">Freshness is unknown.</b> The schema below may not match the working tree.
    </Banner>
  );
}

/** ChangedMark marks a file that changed since the index. */
export function ChangedMark({ show }: { show: boolean }) {
  if (!show) return null;
  return (
    <span className="rounded-[4px] border border-dynamic/45 px-[5px] text-[11px] leading-4 font-semibold text-dynamic" title="Changed since the last index; the line may be outdated">
      changed
    </span>
  );
}

/** changedFiles is the set of files the polled status lists as changed. */
export function changedFiles(status: StatusView): Set<string> {
  return status.kind === "ok" ? new Set(status.status.freshness.changed_files) : new Set();
}

/** TabButton is one tab of the title row: it names the panel it controls,
 * and only the selected tab is in the Tab order (the arrow keys move
 * between tabs, handled by the caller's onKeyDown). */
export function TabButton({
  on,
  onClick,
  children,
  testId,
  id,
  controls,
  onKeyDown,
}: {
  on: boolean;
  onClick: () => void;
  children: ReactNode;
  testId?: string;
  id: string;
  controls: string;
  onKeyDown: (e: KeyboardEvent<HTMLButtonElement>) => void;
}) {
  return (
    <button
      type="button"
      role="tab"
      id={id}
      aria-selected={on}
      aria-controls={controls}
      tabIndex={on ? 0 : -1}
      onKeyDown={onKeyDown}
      data-testid={testId}
      className={cn("inline-flex h-7 items-center gap-1.5 rounded-md px-[11px] text-[13px] font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring", on && "bg-background text-foreground shadow-[0_1px_3px_rgba(0,0,0,.1),0_1px_2px_rgba(0,0,0,.06)]")}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

export const OUTLINE_BUTTON =
  "inline-flex h-7 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-[12.5px] font-medium hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:size-3.5";

/** fileLine writes a node's file name (last segment) and start line. */
export function fileLine(file: string, line: number | null | undefined): string {
  const base = file.split("/").pop() ?? file;
  return line === null || line === undefined ? base : `${base}:${String(line)}`;
}

/** tableName is a table node's `name` attribute, falling back to the
 * qualified name. */
export function tableName(node: { attributes: Record<string, unknown>; qualified_name: string; name: string }): string {
  const n = node.attributes.name;
  if (typeof n === "string") return n;
  return node.qualified_name !== "" ? node.qualified_name : node.name;
}
