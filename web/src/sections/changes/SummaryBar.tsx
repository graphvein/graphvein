// The Changes section's summary (V3 "summary bar" with V1's density): both
// sides (what, run, time, commit, dirty, extractor), the total and the
// moves counted, the change kinds over all groups, the groups not
// extracted (never a zero), and the banners: extractor changed (with the
// O7 recheck offer), HEAD moved since the index, partial (naming the gap
// files). While a branch comparison is indexed, the progress card covers
// the same box, so nothing below moves (T019 Revision 2: reserved height).

import type { ChangeRun, ChangesSummary, SideInfo } from "@/api/changes";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { ChangeChip, NotExtractedChip, Tag } from "@/sections/changes/chips";
import { baseLabel, changeLabel, changeTotals, groupLabel, partialReasonText, plural, refLabel, runSeq, shortCommit, sideName } from "@/sections/changes/words";
import { Banner, OUTLINE_BUTTON } from "@/sections/schema/parts";
import type { RefProgress } from "@/shell/compare";

/** SUMMARY_MIN is the reserved height of the summary box (one sides line,
 * one counts line; two lines more below 600 px), which the progress card
 * fits into. */
export const SUMMARY_MIN = "min-h-[64px] max-[599px]:min-h-[118px]";

function Side({ side, base, runs, which }: { side: SideInfo; base: string; runs: readonly ChangeRun[]; which: "base" | "head" }) {
  const seq = side.kind === "run" || side.kind === "pin" || side.kind === "current" ? runSeq(side.run, runs) : null;
  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5" data-testid={`side-${side.kind}`}>
      <b className="font-semibold">{sideName(side, base, runs, which)}</b>
      {seq !== null && side.kind !== "run" && <span className="text-muted-foreground">· {seq}</span>}
      <span className="text-muted-foreground">· {formatShortTime(side.finished_at)}</span>
      {side.commit !== null && side.kind !== "ref" && <span className="font-mono text-muted-foreground">· {shortCommit(side.commit)}</span>}
      {side.dirty === true && <Tag title="the working tree had uncommitted changes when indexed">dirty</Tag>}
      <span className="font-mono text-[11.5px] text-muted-foreground">· {side.extractor.version}</span>
      {side.partial && (
        <Tag tone="warn" title="this side has knowledge gaps">
          partial
        </Tag>
      )}
    </span>
  );
}

function GapFiles({ side, label }: { side: SideInfo; label: string }) {
  if (!side.partial) return null;
  const shown = side.gaps.slice(0, 3);
  const rest = side.gaps.length - shown.length;
  return (
    <span>
      {label}:{" "}
      {shown.length === 0 ? (
        "no file named"
      ) : (
        <>
          {shown.map((g, i) => (
            <span key={g}>
              {i > 0 ? ", " : ""}
              <code className="font-mono">{g}</code>
            </span>
          ))}
          {rest > 0 ? ` and ${String(rest)} more` : ""}
        </>
      )}
      {side.gaps_truncated ? " (list cut)" : ""}
      {side.partial_reasons.length > 0 ? ` (${side.partial_reasons.map(partialReasonText).join("; ")})` : ""}.{" "}
    </span>
  );
}

export function SummaryBar({ data, runs, onRecheck }: { data: ChangesSummary; runs: readonly ChangeRun[]; onRecheck: (base: string) => void }) {
  const s = data.summary;
  const moves = s.groups.reduce((n, g) => n + g.moves, 0);
  const hidden = s.groups.reduce((n, g) => n + g.hidden_tests, 0);
  const totals = changeTotals(s.groups);
  return (
    <div className="grid gap-1.5" data-testid="changes-summary">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[12.5px]">
        <Side side={s.base} base={data.base} runs={runs} which="base" />
        <span aria-label="compared with" className="text-muted-foreground">
          {"→"}
        </span>
        <Side side={s.head} base={data.base} runs={runs} which="head" />
      </div>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[12.5px]">
        <b className="tabular-nums" data-testid="summary-total">
          {plural(s.total, "change")}
        </b>
        <span className="text-muted-foreground" title="records equal once spans are dropped: counted, not listed">
          · {plural(moves, "move")} counted
        </span>
        {s.file_metadata_only > 0 && <span className="text-muted-foreground">· {plural(s.file_metadata_only, "file")} with line endings changed only (not a change)</span>}
        {hidden > 0 && <span className="text-muted-foreground">· {plural(hidden, "test-scope item")} hidden</span>}
        <span className="flex flex-wrap gap-1" aria-label="changes by kind">
          {totals.map(([k, n]) => (
            <ChangeChip key={k} change={k} label={`${n.toLocaleString("en-US")} ${changeLabel(k)}`} />
          ))}
        </span>
        {s.not_extracted.map((g) => (
          <NotExtractedChip key={g}>{groupLabel(g)}: not extracted</NotExtractedChip>
        ))}
      </div>
      {s.extractor_changed && (
        <Banner tone="info" testId="extractor-changed">
          <b>Extractor changed</b> between the sides ({s.base.extractor.version} {"→"} {s.head.extractor.version}). Changes in unchanged files are labelled <i>extractor or relink</i>.
          {s.extractor_recheck !== null && (
            <>
              {" "}
              <button
                type="button"
                className={cn(OUTLINE_BUTTON, "ml-1 h-6")}
                onClick={() => {
                  onRecheck(s.extractor_recheck ?? "");
                }}
              >
                Compare both commits with {s.head.extractor.version}
              </button>
            </>
          )}
        </Banner>
      )}
      {s.head_commit_differs && (
        <Banner tone="info" testId="head-moved">
          HEAD has moved since the current index was made: the merge base was taken with the commit the index recorded.
        </Banner>
      )}
      {s.partial && (
        <Banner tone="warn" testId="partial">
          <b>Partial comparison.</b> <GapFiles side={s.base} label={`base (${baseLabel(data.base, runs)})`} />
          <GapFiles side={s.head} label="head" />
          Facts there are <ChangeChip change="unknown_now" />, not removed.
        </Banner>
      )}
    </div>
  );
}

/** RefCard is the branch-indexing progress card. The ref job reports its
 * state only (no file counts), so the bar is indeterminate. */
export function RefCard({ progress: ref, onDismiss }: { progress: RefProgress; onDismiss: () => void }) {
  const label = refLabel(ref.base, ref.head);
  // The job's commits are the merge base, then the head commit when it
  // differs (POST /api/v0/changes/refs).
  const commits = ref.job?.commits ?? [];
  const commitText = commits.length === 0 ? "the merge base and head" : commits.map((c, i) => `${i === 0 ? "merge base" : "head"} ${shortCommit(c)}`).join(" and ");
  const failed = ref.error !== null || ref.job?.state === "failed" || ref.job?.state === "cancelled";
  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="ref-progress"
      className={cn("absolute inset-0 z-10 grid content-start gap-1.5 overflow-hidden rounded-lg border px-3 py-2 text-[12.5px]", failed ? "border-dynamic-bar/50 bg-dynamic-soft" : "border-brand/30 bg-card")}
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
        <b>Branch {label}</b>
        <span className="text-muted-foreground">
          {failed
            ? "indexing failed"
            : ref.job === null
              ? "starting"
              : `indexing ${commitText}${ref.job.indexed.length > 0 ? ` · ${String(ref.job.indexed.length)} indexed` : ""}`}
        </span>
        <span className="flex-1" />
        {failed && (
          <button type="button" className={cn(OUTLINE_BUTTON, "h-6")} onClick={onDismiss}>
            Dismiss
          </button>
        )}
      </div>
      {!failed && (
        <div className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden="true">
          <div className="h-full w-1/3 animate-pulse rounded-full bg-brand" />
        </div>
      )}
      <div className="text-[11.5px] text-muted-foreground">
        {failed
          ? `${ref.job?.error?.code ?? (ref.error !== null ? "request failed" : "cancelled")}: ${ref.job?.error?.message ?? ref.error?.message ?? "the job was cancelled"}`
          : "The panes keep the previous comparison until this one answers."}
      </div>
    </div>
  );
}
