// The freshness badge in the top bar. VISION: stale data is never presented
// as current, so only a successful answer whose state is "fresh" renders
// "Fresh"; every other case (stale, not indexed, unknown, an unrecognised
// state, a failed request) uses the high-contrast warning treatment, and the
// text always says which case it is (never colour alone).
//
// T016j (variant D): the badge keeps describing the index the views show
// while a re-index runs. An override from the re-index model replaces a
// "Fresh" that the lock state does not prove ("Indexed T") and shows
// "Indexed T', checking freshness..." between a commit and the status answer
// that names the new run. The badge reserves the width of its widest label
// (hidden sizer rows drawn by CSS only), so it never moves between states.

import { CircleAlert, Clock, RefreshCw, TriangleAlert } from "lucide-react";
import { useState, type ReactNode } from "react";

import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { IndexSettings } from "@/api/indexSettings";
import type { Status } from "@/api/status";
import type { StatusView } from "@/hooks/useStatus";
import { COUNT_SIZER_SAMPLES, formatCount } from "@/lib/count";
import { formatShortTime, formatTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import type { BadgeOverride } from "@/shell/reindexModel";

/** At most this many changed paths are listed in the tooltip. */
export const TOOLTIP_PATH_LIMIT = 20;

const base =
  "inline-flex h-8 items-center whitespace-nowrap rounded-md border border-transparent px-3 text-[13px] font-semibold tabular-nums";
const row = "col-start-1 row-start-1 inline-flex items-center gap-2";
const freshStyle = "border-border font-medium text-foreground";
const warnStyle = "bg-stale text-stale-foreground ring-[3px] ring-stale/22";
// Fresh files with changed index settings (ADR 0088, row ST-48): amber, not
// the stale warning, since every fact still matches the files.
const settingsStyle = "border-dynamic/50 bg-dynamic-soft font-medium text-foreground";

/** SETTINGS_CHANGED is the badge's words for an index built with other
 * index settings than graphvein.yaml now sets. */
export const SETTINGS_CHANGED = "settings changed";

/** changedSettings is the comparison when it says "changed"; null for
 * every other state, and for an absent comparison, which proves nothing. */
export function changedSettings(status: Status): IndexSettings | null {
  const s = status.freshness.settings;
  return s !== undefined && s.state === "changed" ? s : null;
}

/** filesChanged is "1 file changed" or "N files changed", N formatted
 * like the Re-index slot's counts (full up to 9,999, then "12.3K"). */
export function filesChanged(count: number): string {
  return count === 1 ? "1 file changed" : `${formatCount(count)} files changed`;
}

function Small({ children, fresh }: { children: ReactNode; fresh: boolean }) {
  return (
    <small className={cn("text-xs @max-[1299px]/top:hidden", fresh ? "text-muted-foreground" : "font-medium opacity-85")}>{children}</small>
  );
}

function BadgeBody({ status }: { status: Status }) {
  const { freshness, index } = status;
  const finished = index.last_run?.finished_at;
  const settings = changedSettings(status) !== null;
  switch (freshness.state) {
    case "fresh":
      return settings ? (
        <>
          <span aria-hidden="true" className="size-2 rounded-full bg-dynamic-bar ring-[3px] ring-dynamic/20" />
          Fresh · {SETTINGS_CHANGED}
          {finished !== undefined && <Small fresh>indexed {formatShortTime(finished)}</Small>}
        </>
      ) : (
        <>
          <span aria-hidden="true" className="size-2 rounded-full bg-linked ring-[3px] ring-linked/18" />
          Fresh
          {finished !== undefined && <Small fresh>indexed {formatShortTime(finished)}</Small>}
        </>
      );
    case "stale":
      return (
        <>
          <TriangleAlert aria-hidden="true" className="size-[15px]" />
          Stale: {filesChanged(freshness.changed_count ?? freshness.changed_files.length)}
          {finished !== undefined && (
            <Small fresh={false}>
              since {formatShortTime(finished)}
              {settings && ` · ${SETTINGS_CHANGED}`}
            </Small>
          )}
          {finished === undefined && settings && <Small fresh={false}>{SETTINGS_CHANGED}</Small>}
        </>
      );
    case "not_indexed":
      return (
        <>
          <TriangleAlert aria-hidden="true" className="size-[15px]" />
          Not indexed
        </>
      );
    case "unknown":
      return (
        <>
          <CircleAlert aria-hidden="true" className="size-[15px]" />
          Freshness unknown
        </>
      );
  }
}

function OverrideBody({ override }: { override: BadgeOverride }) {
  const time = formatShortTime(override.finishedAt);
  return override.kind === "checking" ? (
    <>
      <RefreshCw aria-hidden="true" className="size-[15px]" />
      Indexed
      <Small fresh>{time}, checking freshness...</Small>
    </>
  ) : (
    <>
      <Clock aria-hidden="true" className="size-[15px]" />
      Indexed
      <Small fresh>{time}</Small>
    </>
  );
}

/** A sample time for the sizer: a two-digit day and hour, so most real
 * times fit in the reserved width. */
const SIZER_TIME = new Date(2026, 11, 28, 12, 58).toISOString();

/** badgeSizerLabels lists the widest labels the badge can show for these
 * times (main text, small text). The Stale rows reserve room for every
 * count formatCount can produce. */
export function badgeSizerLabels(times: readonly string[]): [string, string][] {
  const rows: [string, string][] = [];
  for (const t of new Set([SIZER_TIME, ...times])) {
    const time = formatShortTime(t);
    for (const c of COUNT_SIZER_SAMPLES) {
      rows.push([`Stale: ${c} files changed`, `since ${time}`]);
      rows.push([`Stale: ${c} files changed`, `since ${time} · ${SETTINGS_CHANGED}`]);
    }
    rows.push(["Indexed", `${time}, checking freshness...`]);
    rows.push(["Fresh", `indexed ${time}`]);
    rows.push([`Fresh · ${SETTINGS_CHANGED}`, `indexed ${time}`]);
  }
  return rows;
}

/** Sizer rows take space but show nothing and are not in the text: the
 * labels are drawn by CSS from data attributes. */
function Sizer({ times }: { times: string[] }) {
  return (
    <>
      {badgeSizerLabels(times).map(([main, small]) => (
        <span key={main + small} aria-hidden="true" data-testid="badge-sizer" className={cn(row, "invisible")}>
          <span className="size-[15px]" />
          <span data-l={main} className="font-semibold after:content-[attr(data-l)]" />
          <small data-l={small} className="text-xs font-medium after:content-[attr(data-l)] @max-[1299px]/top:hidden" />
        </span>
      ))}
    </>
  );
}

function ChangedFiles({ status }: { status: Status }) {
  const { changed_files: files, changed_count: count, changed_files_truncated: truncated } = status.freshness;
  const shown = files.slice(0, TOOLTIP_PATH_LIMIT);
  const more = (count ?? files.length) - shown.length;
  return (
    <>
      <p className="font-medium">Changed since the last index:</p>
      <ul className="font-mono text-[11.5px] leading-snug">
        {shown.map((f) => (
          <li key={f} className="break-all">
            {f}
          </li>
        ))}
      </ul>
      {more > 0 && <p>and {String(more)} more</p>}
      {truncated && <p>(list truncated by the server)</p>}
      <p>Run graphvein index to update.</p>
    </>
  );
}

/** FreshnessDetails is the tooltip content for a view; null when there is
 * nothing to explain yet (loading). */
export function FreshnessDetails({ view, override = null }: { view: StatusView; override?: BadgeOverride | null }) {
  if (view.kind === "loading") return null;
  if (view.kind === "ok" && override !== null) {
    return (
      <div className="grid gap-1">
        {override.kind === "checking" ? (
          <>
            <p>A re-index committed the index of {formatTime(override.finishedAt)}.</p>
            <p>Freshness is shown when a status check names that run.</p>
          </>
        ) : override.reason === "held" ? (
          <>
            <p>The views show the index of {formatTime(override.finishedAt)}.</p>
            <p>An index run holds the lock, so this badge does not say Fresh until the run ends.</p>
          </>
        ) : (
          <>
            <p>The views show the index of {formatTime(override.finishedAt)}.</p>
            <p>Whether an index run is in progress is not known, so this badge does not say Fresh.</p>
          </>
        )}
        <p className="opacity-80">Checked {formatTime(view.status.freshness.checked_at)}</p>
      </div>
    );
  }
  if (view.kind === "checking") {
    return (
      <div className="grid gap-1">
        <p>The last answer is too old to show; a new check is running.</p>
        <p>Freshness is unknown until it answers.</p>
      </div>
    );
  }
  if (view.kind === "failed") {
    return (
      <div className="grid gap-1">
        <p>The last status request failed: {view.error.message}</p>
        <p>Freshness is unknown until a check succeeds.</p>
        <p className="opacity-80">Attempted {formatTime(view.at)}</p>
      </div>
    );
  }
  const { freshness } = view.status;
  let body: ReactNode;
  switch (freshness.state) {
    case "fresh":
      body = <p>The index matches the working tree.</p>;
      break;
    case "stale":
      body = <ChangedFiles status={view.status} />;
      break;
    case "not_indexed":
      body = <p>This repository has no index yet. Run graphvein index.</p>;
      break;
    case "unknown":
      body =
        freshness.reported_state !== "unknown" ? (
          <p>The server reported a freshness state this UI does not know: {freshness.reported_state}</p>
        ) : (
          <p>Graphvein could not check freshness: {freshness.error ?? "no reason given"}</p>
        );
      break;
  }
  return (
    <div className="grid gap-1">
      {body}
      <SettingsDetails settings={freshness.settings} />
      <p className="opacity-80">Checked {formatTime(freshness.checked_at)}</p>
    </div>
  );
}

/** SettingsDetails is the tooltip's settings line (row ST-48): "changed"
 * names the keys, "unknown" (or a state this UI does not know) says why,
 * "unchanged" is said only here; nothing for an absent comparison or an
 * index that does not exist. */
export function SettingsDetails({ settings }: { settings: IndexSettings | undefined }) {
  if (settings === undefined || settings.state === "not_indexed") return null;
  switch (settings.state) {
    case "changed":
      return (
        <p data-testid="settings-details">
          graphvein.yaml's index settings changed since the last index run: {settings.changed_keys.join(", ")}. Re-index to apply.
        </p>
      );
    case "unchanged":
      return <p data-testid="settings-details">Index settings unchanged since the last index run.</p>;
    case "unknown":
      return (
        <p data-testid="settings-details">
          Whether graphvein.yaml's index settings changed since the last index run is unknown: {settings.reported_state !== "unknown" ? `the server reported ${settings.reported_state}` : (settings.reason ?? "no reason given")}.
        </p>
      );
  }
}

/**
 * FreshnessBadge shows the index freshness of the current view. It is a
 * button whose action is to show the details: hover and keyboard focus open
 * the tooltip as usual, and a click (or Enter/Space) opens it too, so touch
 * and keyboard users reach the changed-file list; Escape, moving focus
 * away or a click elsewhere closes it.
 */
export function FreshnessBadge({ view, override = null }: { view: StatusView; override?: BadgeOverride | null }) {
  const [open, setOpen] = useState(false);
  const times: string[] = [];
  if (view.kind === "ok" && view.status.index.last_run) times.push(view.status.index.last_run.finished_at);
  if (override !== null) times.push(override.finishedAt);
  if (view.kind === "loading") {
    return (
      <span role="status" aria-live="polite">
        <span className="sr-only">Checking freshness</span>
        <span className={cn(base, "grid")}>
          <Skeleton aria-hidden="true" className="col-start-1 row-start-1 -mx-[13px] -my-px h-8" />
          <Sizer times={times} />
        </span>
      </span>
    );
  }
  const ov = view.kind === "ok" ? override : null;
  const fresh = ov === null && view.kind === "ok" && view.status.freshness.state === "fresh";
  const amber = ov === null && view.kind === "ok" && view.status.freshness.state === "fresh" && changedSettings(view.status) !== null;
  const checking = view.kind === "checking";
  const neutral = fresh || checking || ov !== null;
  return (
    <span role="status" aria-live="polite">
      <Tooltip open={open} onOpenChange={setOpen}>
        <TooltipTrigger asChild>
          <button
            type="button"
            data-state-kind={amber ? "fresh-settings" : fresh ? "fresh" : checking ? "checking" : ov !== null ? `run-${ov.kind}` : "warning"}
            data-testid="freshness-badge"
            className={cn(base, "grid", amber ? settingsStyle : neutral ? freshStyle : warnStyle)}
            onClick={(event) => {
              // Radix closes a tooltip on click; here the click is the
              // button's action, showing the details.
              event.preventDefault();
              setOpen(true);
            }}
          >
            <span className={cn(row, neutral && "font-medium")}>
              {ov !== null ? (
                <OverrideBody override={ov} />
              ) : view.kind === "ok" ? (
                <BadgeBody status={view.status} />
              ) : checking ? (
                <>
                  <RefreshCw aria-hidden="true" className="size-[15px] animate-spin" />
                  Checking...
                </>
              ) : (
                <>
                  <CircleAlert aria-hidden="true" className="size-[15px]" />
                  Status unavailable
                </>
              )}
            </span>
            <Sizer times={times} />
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom" align="end" sideOffset={6} className="max-w-md">
          <FreshnessDetails view={view} override={ov} />
        </TooltipContent>
      </Tooltip>
    </span>
  );
}
