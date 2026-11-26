// The Re-index slot of the top bar (T016j variant D). Idle, it is the
// Re-index action. While a run holds the lock, after a failure, or when
// re-index is unavailable, the same button discloses a details popover
// (aria-expanded; click, Enter or Space; Escape and the Close button close
// it). It never starts a second run. The slot reserves the width of its
// widest label, so the badge, Check and the slot never move between states.

import { Ban, Check, ChevronDown, Circle, CircleQuestionMark, CircleX, Database, LoaderCircle, Lock, X } from "lucide-react";
import { Popover } from "radix-ui";
import { useId, useRef, useState, type ReactNode } from "react";

import { PHASES, type Job } from "@/api/indexRun";
import type { StatusView } from "@/hooks/useStatus";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { filesChanged } from "@/shell/FreshnessBadge";
import {
  announcement,
  formatCount,
  PHASE_LABELS,
  SLOT_SIZER_LABELS,
  slotLabel,
  type ReindexModel,
  type Slot,
} from "@/shell/reindexModel";

const row = "col-start-1 row-start-1 inline-flex items-center justify-center gap-1.5";

/** disclosure is true for the states in which the slot opens details. */
function isDisclosure(slot: Slot): boolean {
  return slot.kind !== "idle" && slot.kind !== "pending" && slot.kind !== "checking";
}

function SlotIcon({ slot }: { slot: Slot }) {
  const cls = "size-4";
  switch (slot.kind) {
    case "pending":
    case "checking":
      return <LoaderCircle aria-hidden="true" className={cn(cls, "animate-spin")} />;
    case "elsewhere":
      return <Lock aria-hidden="true" className={cls} />;
    case "failed":
    case "not_started":
      return <CircleX aria-hidden="true" className={cls} />;
    case "cancelled":
      return <Ban aria-hidden="true" className={cls} />;
    case "outcome_unknown":
      return <CircleQuestionMark aria-hidden="true" className={cls} />;
    default:
      return <Database aria-hidden="true" className={cls} />;
  }
}

function slotStyle(slot: Slot): string {
  switch (slot.kind) {
    case "idle":
      return slot.primary
        ? "border-brand bg-brand text-brand-foreground hover:bg-brand/90"
        : "bg-background hover:bg-accent";
    case "pending":
    case "checking":
      return "cursor-progress bg-background text-muted-foreground";
    case "running":
    case "elsewhere":
      return "border-brand/45 bg-brand-soft text-brand-text";
    case "failed":
    case "not_started":
      return "border-unmatched/55 bg-unmatched-soft text-unmatched";
    case "cancelled":
    case "outcome_unknown":
      return "bg-background text-foreground";
    case "unavailable":
      return "bg-background text-muted-foreground";
  }
}

/** accessibleName says what the button does in this state. */
function accessibleName(slot: Slot): string {
  switch (slot.kind) {
    case "idle":
      return "Re-index";
    case "pending":
      return "Starting a re-index";
    case "checking":
      return "Reading the re-index outcome";
    case "running": {
      const p = slot.job?.state === "running" ? slot.job.progress : null;
      return p === null
        ? "Indexing, show details"
        : `Indexing, ${String(p.done)} of ${String(p.total)} ${p.unit}, show details`;
    }
    case "elsewhere":
      return "Indexing in another process, show details";
    case "unavailable":
      return "Re-index unavailable, show why";
    default:
      return `${slotLabel(slot).replace(" · details", "")}, show details`;
  }
}

function ProgressBar({ slot }: { slot: Slot }) {
  if (slot.kind !== "running" && slot.kind !== "elsewhere") return null;
  const p = slot.kind === "running" && slot.job?.state === "running" ? slot.job.progress : null;
  return (
    <span
      aria-hidden="true"
      data-testid="reindex-bar"
      data-determinate={p !== null && p.total > 0 ? "true" : "false"}
      className="absolute inset-x-2.5 bottom-[3px] h-[3px] overflow-hidden rounded-sm bg-brand/18"
    >
      {p !== null && p.total > 0 ? (
        <i className="absolute inset-y-0 left-0 rounded-sm bg-brand" style={{ width: `${String((p.done / p.total) * 100)}%` }} />
      ) : (
        <i className="gv-indet absolute inset-y-0 left-0 w-[38%] rounded-sm bg-brand" />
      )}
    </span>
  );
}

/** shownIndex names the index the views show, from the status answer (or
 * the run the job replaced when the status is not available). */
export function shownIndex(status: StatusView, job: Job | null): string {
  if (status.kind === "ok") {
    const last = status.status.index.last_run;
    if (last === null) return "no index (not indexed)";
    const f = status.status.freshness;
    const extra =
      f.state === "stale" ? ` (Stale, ${filesChanged(f.changed_count ?? f.changed_files.length)})` : "";
    return `the index of ${formatShortTime(last.finished_at)}${extra}`;
  }
  if (job?.replaces) return `the index of ${formatShortTime(job.replaces.finished_at)}`;
  return "the last committed index";
}

function PhaseList({ job }: { job: Job }) {
  const listed = PHASES.filter((p) => p !== "starting" && p !== "finishing");
  const phase = job.phase;
  const known = phase !== null && (PHASES as readonly string[]).includes(phase);
  const done = job.state === "committed" || phase === "finishing";
  const at = known ? PHASES.indexOf(phase as (typeof PHASES)[number]) : -1;
  return (
    <>
      {phase !== null && !known && <p data-testid="reindex-raw-phase">Current phase: {phase}</p>}
      <ul className="grid gap-1" data-testid="reindex-phases">
        {listed.map((p) => {
          const i = PHASES.indexOf(p);
          const state = !known && !done ? "plain" : done || i < at ? "done" : i === at ? "now" : "todo";
          const progress = state === "now" ? job.progress : null;
          return (
            <li
              key={p}
              data-phase={p}
              data-state={state}
              className={cn(
                "flex items-center gap-2",
                state === "now" && "font-medium text-brand-text",
                state === "todo" && "text-muted-foreground",
              )}
            >
              {state === "done" ? (
                <Check aria-hidden="true" className="size-3.5" />
              ) : state === "now" ? (
                <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" />
              ) : (
                <Circle aria-hidden="true" className="size-3.5" />
              )}
              <span>{PHASE_LABELS[p]}</span>
              <span className="sr-only">{state === "done" ? "(done)" : state === "now" ? "(in progress)" : ""}</span>
              {progress !== null && (
                <span className="ml-auto tabular-nums">
                  {formatCount(progress.done)} / {formatCount(progress.total)} {progress.unit}
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </>
  );
}

function ErrorBox({ code, message }: { code: string; message: string }) {
  return (
    <p
      data-testid="reindex-error"
      className="rounded-md border border-unmatched/40 bg-unmatched-soft px-2.5 py-1.5 font-mono text-[12px] break-words text-unmatched"
    >
      {code}: {message}
    </p>
  );
}

interface DetailsProps {
  model: ReindexModel;
  status: StatusView;
  titleId: string;
  onStart: () => void;
  onDismiss: () => void;
  onClose: () => void;
}

function Details({ model, status, titleId, onStart, onDismiss, onClose }: DetailsProps) {
  const { slot, startBlocked } = model;
  let title: ReactNode;
  let body: ReactNode;
  let actions: "close" | "dismiss-retry" = "close";
  let startLabel = "Retry";
  const shown = (job: Job | null) => shownIndex(status, job);
  switch (slot.kind) {
    case "running": {
      const job = slot.job;
      title =
        job === null
          ? "This server's re-index is running"
          : job.state === "committed"
            ? "Re-index committed"
            : `Re-index started at ${formatShortTime(job.started_at)} by this server`;
      body = (
        <>
          {job !== null && <PhaseList job={job} />}
          {job?.state === "committed" && job.run !== null ? (
            <p>
              The new index of {formatShortTime(job.run.finished_at)} is committed; the views switch to it when a status
              check names it.
            </p>
          ) : (
            <p>The views show {shown(job)} until the new run commits.</p>
          )}
          <p className="text-muted-foreground">
            There is no cancel button: stopping graphvein serve (Ctrl+C) cancels the run and rolls it back.
          </p>
        </>
      );
      break;
    }
    case "elsewhere":
      title = "Another process is indexing";
      body = (
        <>
          <p>
            Another process holds the index lock for this store: <code>graphvein index</code> or another{" "}
            <code>graphvein serve</code>. Re-index is available again when it releases the lock.
          </p>
          <p className="text-muted-foreground">
            Its progress is not visible here. When the lock is released the views switch to its run if it committed
            one. Showing {shown(null)}.
          </p>
        </>
      );
      break;
    case "failed": {
      const job = slot.job;
      title = "Re-index failed";
      actions = "dismiss-retry";
      body = (
        <>
          {job.error !== null ? (
            <ErrorBox code={job.error.code} message={job.error.message} />
          ) : (
            <p>The server reported no error details.</p>
          )}
          <p>
            The run started at {formatShortTime(job.started_at)} was rolled back. The views still show {shown(job)}.
          </p>
        </>
      );
      break;
    }
    case "cancelled":
      title = "Re-index cancelled";
      actions = "dismiss-retry";
      startLabel = "Re-index";
      body = (
        <p>
          The run started at {formatShortTime(slot.job.started_at)} was cancelled (graphvein serve was stopping) and
          rolled back. The views still show {shown(slot.job)}.
        </p>
      );
      break;
    case "not_started":
      title = "Re-index could not start";
      actions = "dismiss-retry";
      body = (
        <>
          <ErrorBox code={slot.error.code} message={slot.error.message} />
          <p>No run started. The views still show {shown(null)}.</p>
        </>
      );
      break;
    case "outcome_unknown":
      title = "Re-index outcome unknown";
      actions = "dismiss-retry";
      startLabel = "Re-index";
      body = (
        <p>
          This tab started a re-index, but the server no longer reports it: graphvein serve restarted. Whether it
          committed is not known. The views show {shown(null)}.
        </p>
      );
      break;
    case "unavailable":
      title = "Re-index unavailable";
      body = <p data-testid="reindex-unavailable-reason">{slot.reason}</p>;
      break;
    default:
      return null;
  }
  return (
    <div className="grid gap-2.5">
      <div className="flex items-start gap-2">
        <h2 id={titleId} className="flex-1 text-[13.5px] font-semibold">
          {title}
        </h2>
        <button
          type="button"
          onClick={onClose}
          className="-m-1 rounded-sm p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
          aria-label="Close details"
        >
          <X aria-hidden="true" className="size-4" />
        </button>
      </div>
      {body}
      <div className="flex justify-end gap-2 border-t pt-2.5">
        {actions === "dismiss-retry" ? (
          <>
            <button type="button" onClick={onDismiss} className="h-8 rounded-md border bg-background px-3 font-medium hover:bg-accent">
              Dismiss
            </button>
            <button
              type="button"
              onClick={() => {
                if (startBlocked === null) onStart();
              }}
              aria-disabled={startBlocked !== null ? true : undefined}
              title={startBlocked ?? undefined}
              className={cn(
                "inline-flex h-8 items-center gap-1.5 rounded-md border border-brand bg-brand px-3 font-medium text-brand-foreground hover:bg-brand/90",
                startBlocked !== null && "cursor-not-allowed opacity-50",
              )}
            >
              <Database aria-hidden="true" className="size-4" />
              {startLabel}
            </button>
          </>
        ) : (
          <button type="button" onClick={onClose} className="h-8 rounded-md border bg-background px-3 font-medium hover:bg-accent">
            Close
          </button>
        )}
      </div>
      {actions === "dismiss-retry" && startBlocked !== null && (
        <p className="text-xs text-muted-foreground">{startBlocked}</p>
      )}
    </div>
  );
}

/** ReindexControl renders the slot for a model from deriveReindex. */
export function ReindexControl({
  model,
  status,
  onStart,
  onDismiss,
}: {
  model: ReindexModel;
  status: StatusView;
  onStart: () => void;
  onDismiss: () => void;
}) {
  const { slot } = model;
  const disclosure = isDisclosure(slot);
  const [open, setOpen] = useState(false);
  // A state that is not a disclosure closes the details, so they do not
  // reopen by themselves when the next run starts.
  if (open && !disclosure) setOpen(false);
  const button = useRef<HTMLButtonElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const id = useId();
  const contentId = `${id}-details`;
  const titleId = `${id}-title`;
  const shown = open && disclosure;
  const label = slotLabel(slot);
  return (
    <>
      <Popover.Root open={shown} onOpenChange={setOpen}>
        <Popover.Anchor asChild>
          <button
            ref={button}
            type="button"
            data-testid="reindex-slot"
            data-slot-kind={slot.kind}
            aria-label={accessibleName(slot)}
            aria-haspopup={disclosure ? "dialog" : undefined}
            aria-expanded={disclosure ? shown : undefined}
            aria-controls={disclosure && shown ? contentId : undefined}
            aria-disabled={slot.kind === "pending" || slot.kind === "checking" ? true : undefined}
            title={
              slot.kind === "idle"
                ? "Re-index this repository now (graphvein index with default flags)"
                : slot.kind === "unavailable"
                  ? slot.reason
                  : slot.kind === "pending"
                    ? "Waiting for the server to start the run"
                    : slot.kind === "checking"
                      ? "Reading the outcome of the run"
                      : "Show details"
            }
            onClick={() => {
              if (slot.kind === "idle") onStart();
              else if (disclosure) setOpen(!shown);
            }}
            className={cn(
              "relative inline-grid h-8 flex-none items-center overflow-hidden rounded-md border px-3 text-sm font-medium whitespace-nowrap tabular-nums shadow-xs outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50",
              slotStyle(slot),
              shown && "ring-1 ring-brand/45 ring-inset",
            )}
          >
            <span className={row}>
              <SlotIcon slot={slot} />
              {label}
              {disclosure && <ChevronDown aria-hidden="true" className="-ml-0.5 size-3.5 opacity-70" />}
            </span>
            {SLOT_SIZER_LABELS.map((l) => (
              <span key={l} aria-hidden="true" data-testid="slot-sizer" className={cn(row, "invisible")}>
                <span className="size-4" />
                <span data-l={l} className="after:content-[attr(data-l)]" />
                <span className="-ml-0.5 size-3.5" />
              </span>
            ))}
            <ProgressBar slot={slot} />
          </button>
        </Popover.Anchor>
        <Popover.Portal>
          <Popover.Content
            ref={content}
            id={contentId}
            role="dialog"
            aria-labelledby={titleId}
            side="bottom"
            align="end"
            sideOffset={6}
            collisionPadding={8}
            data-testid="reindex-details"
            className="z-50 w-[min(380px,calc(100vw-16px))] rounded-[calc(var(--radius)+2px)] border bg-popover p-3.5 text-[13px] leading-snug text-popover-foreground shadow-[0_10px_30px_rgba(0,0,0,.16),0_2px_6px_rgba(0,0,0,.08)] outline-none"
            onPointerDownOutside={(e) => {
              // A click on the slot toggles the details itself.
              if (e.target instanceof Node && button.current?.contains(e.target)) e.preventDefault();
            }}
            onCloseAutoFocus={(e) => {
              e.preventDefault();
              const active = document.activeElement;
              if (active === null || active === document.body || content.current?.contains(active)) {
                button.current?.focus();
              }
            }}
          >
            <Details
              model={model}
              status={status}
              titleId={titleId}
              onStart={onStart}
              onDismiss={() => {
                setOpen(false);
                onDismiss();
              }}
              onClose={() => {
                setOpen(false);
              }}
            />
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
      <span role="status" aria-live="polite" className="sr-only" data-testid="reindex-live">
        {announcement(slot)}
      </span>
    </>
  );
}
