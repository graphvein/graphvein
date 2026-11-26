// The compare chip in the top bar (T019 Revision 2): it shows the base the
// Changes section and every view's markers use, with the change count
// against it, a one-click off, and a menu to pick the base (previous run,
// a journal run, a pin, a branch), pin or unpin, and index a branch
// comparison. An expired or unknown base shows the refusal on the chip.

import { GitCompareArrows, X } from "lucide-react";
import { useEffect, useId, useRef, useState, type FocusEvent as ReactFocusEvent, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";

import { ApiError } from "@/api/client";
import type { ChangesBases, ChangeRun } from "@/api/changes";
import { formatCount } from "@/lib/count";
import { cn } from "@/lib/utils";
import { refusalOf } from "@/sections/changes/refusal";
import { baseLabel, refLabel, runText, shortCommit } from "@/sections/changes/words";
import { OUTLINE_BUTTON } from "@/sections/schema/parts";
import { MARKERS_OFF_HEAD, markersApply, refBaseOf, useCompare, validBase, type CompareState } from "@/shell/compare";

/** ChipView is what the chip shows. */
export interface ChipView {
  /** "Since " before the base; hidden below 1400 px, where the top bar
   * has no room for it. */
  lead: string;
  text: string;
  /** The count badge; null: none (off, loading, refused). */
  count: number | null;
  /** "on": brand; "off": muted; "refused": amber; "busy": brand with a
   * pulsing badge. */
  tone: "on" | "off" | "refused" | "busy";
  title: string;
  /** Why the views show no markers although the comparison answered
   * (R5-1); null when markers follow the comparison. */
  note: string | null;
}

/** chipView derives the chip from the compare state (pure, tested). */
export function chipView(c: Pick<CompareState, "choice" | "summary" | "bases" | "ref" | "run">): ChipView {
  const runs = c.bases.state.kind === "ok" ? c.bases.state.data.runs : [];
  const label = baseLabel(c.choice.base, runs);
  if (c.run === "none") return { lead: "", text: "No index", count: null, tone: "off", title: "Nothing is indexed yet: there is nothing to compare", note: null };
  if (c.ref !== null) {
    // The same label as the answered comparison (R5-6).
    const what = refLabel(c.ref.base, c.ref.head);
    if (c.ref.error !== null || c.ref.job?.state === "failed" || c.ref.job?.state === "cancelled") {
      return { lead: "", text: `${what} (refused)`, count: null, tone: "refused", title: "Indexing the branch comparison failed; open the menu for the reason", note: null };
    }
    return { lead: "", text: `${what} · indexing`, count: null, tone: "busy", title: "Indexing the merge base and the head commit for this comparison", note: null };
  }
  if (!c.choice.on) return { lead: "", text: "Compare off", count: null, tone: "off", title: "Comparing is off: no markers in any view. Open to pick a base.", note: null };
  const s = c.summary.state;
  if (s.kind === "failed") {
    const r = refusalOf(s.error, { base: c.choice.base, baseText: label, runs: c.bases.state.kind === "ok" ? runs : null, retention: null });
    if (r.code === "first_run") return { lead: "", text: "No base yet", count: null, tone: "off", title: r.title, note: null };
    return { lead: "", text: `${label} (${r.chip})`, count: null, tone: "refused", title: `${r.title}. Markers are hidden in every view until a comparison answers.`, note: null };
  }
  if (s.kind === "ok") {
    if (!markersApply(s.data)) return { lead: "Since ", text: label, count: s.data.summary.total, tone: "on", title: `Base for Changes; ${MARKERS_OFF_HEAD}`, note: MARKERS_OFF_HEAD };
    return { lead: "Since ", text: label, count: s.data.summary.total, tone: "on", title: "Base for Changes and for the markers in every view", note: null };
  }
  return { lead: "Since ", text: label, count: null, tone: "busy", title: "Loading the comparison", note: null };
}

/** mergeBaseNote is the Branch option's note about the comparison shown
 * (R5-5): the merge base commit its base side was read from. */
export function mergeBaseNote(c: Pick<CompareState, "summary">): string | null {
  if (c.summary.state.kind !== "ok") return null;
  const b = c.summary.state.data.summary.base;
  if (b.kind !== "ref" || b.commit === null) return null;
  return `The comparison shown is from the merge base ${shortCommit(b.commit)}.`;
}

const RADIO = "mt-0.5 size-3.5 flex-none accent-[hsl(var(--brand))]";

type Kind = "previous" | "run" | "pin" | "branch";

function kindOf(base: string): Kind {
  if (base.startsWith("run:")) return "run";
  if (base.startsWith("pin:")) return "pin";
  if (base.startsWith("ref:")) return "branch";
  return "previous";
}

function errText(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  return err instanceof Error ? err.message : String(err);
}

/** CompareMenu is the base picker. */
function CompareMenu({ c, onClose }: { c: CompareState; onClose: () => void }) {
  const bases: ChangesBases | null = c.bases.state.kind === "ok" ? c.bases.state.data : null;
  const runs: ChangeRun[] = bases?.runs ?? [];
  const older = runs.slice(1);
  const id = useId();
  const [kind, setKind] = useState<Kind>(kindOf(c.choice.base));
  const [run, setRun] = useState<string>(c.choice.base.startsWith("run:") ? c.choice.base.slice(4) : (older[0]?.run_id ?? ""));
  const [pin, setPin] = useState<string>(c.choice.base.startsWith("pin:") ? c.choice.base.slice(4) : (bases?.pins[0]?.name ?? ""));
  const refSpec = c.choice.base.startsWith("ref:") ? c.choice.base.slice(4).split("..") : [];
  const [refBase, setRefBase] = useState(refSpec[0] ?? "");
  const [refHead, setRefHead] = useState(refSpec[1] ?? "");
  const [pinName, setPinName] = useState("");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const first = useRef<HTMLInputElement>(null);

  useEffect(() => {
    first.current?.focus();
  }, []);

  const head = runs[0] ?? null;
  const previous = runs[1] ?? null;
  const pinValid = pinName.trim() !== "" && new TextEncoder().encode(pinName.trim()).length <= 200;
  let target: string | null = null;
  if (kind === "previous") target = "previous";
  else if (kind === "run" && run !== "") target = `run:${run}`;
  else if (kind === "pin" && pin !== "") target = `pin:${pin}`;
  const branchValid = refBase.trim() !== "" && validBase(refBaseOf(refBase.trim(), refHead.trim()));

  const apply = () => {
    if (kind === "branch") {
      if (!branchValid) return;
      c.startRef(refBase.trim(), refHead.trim() === "" ? null : refHead.trim());
      onClose();
      return;
    }
    if (target === null) return;
    c.setBase(target);
    onClose();
  };
  const doPin = () => {
    if (!pinValid) return;
    setBusy(true);
    setNote(null);
    c.pin(pinName.trim(), head?.run_id ?? null).then(
      () => {
        setBusy(false);
        setPin(pinName.trim());
        setKind("pin");
        setPinName("");
        setNote(`Pinned ${head === null ? "the latest run" : `run ${String(head.seq)}`} as ${pinName.trim()}.`);
      },
      (err: unknown) => {
        setBusy(false);
        setNote(`Could not pin: ${errText(err)}`);
      },
    );
  };
  const doUnpin = (name: string) => {
    setBusy(true);
    setNote(null);
    c.unpin(name).then(
      () => {
        setBusy(false);
        setNote(`Unpinned ${name}.`);
        if (pin === name) setPin("");
      },
      (err: unknown) => {
        setBusy(false);
        setNote(`Could not unpin: ${errText(err)}`);
      },
    );
  };

  const refFailure = c.ref !== null && (c.ref.error !== null || c.ref.job?.state === "failed" || c.ref.job?.state === "cancelled");
  const mergeBase = mergeBaseNote(c);
  const offNote = chipView(c).note;
  const refusal = refFailure
    ? refusalOf(c.ref?.error ?? c.ref?.job?.error ?? { code: "cancelled", message: "the job was cancelled" }, { base: c.choice.base, baseText: refLabel(c.ref?.base ?? "", c.ref?.head ?? null), runs, retention: bases?.retention ?? null })
    : null;

  const option = (k: Kind, title: string, body: ReactNode, disabled = false) => (
    <div className={cn("flex gap-2 rounded-md px-2 py-1.5", kind === k && "bg-brand-soft", disabled && "opacity-60")}>
      <input
        ref={k === "previous" ? first : undefined}
        type="radio"
        name={`${id}-kind`}
        id={`${id}-${k}`}
        className={RADIO}
        checked={kind === k}
        disabled={disabled}
        onChange={() => {
          setKind(k);
        }}
      />
      <div className="grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)] gap-1">
        <label htmlFor={`${id}-${k}`} className="text-[13px] font-medium">
          {title}
        </label>
        {body}
      </div>
    </div>
  );

  return (
    <div
      role="dialog"
      aria-label="Compare against"
      data-testid="compare-menu"
      className="absolute top-[calc(100%+6px)] right-0 z-50 grid w-[430px] grid-cols-[minmax(0,1fr)] gap-1 rounded-xl border bg-popover p-3 text-[13px] shadow-lg max-[599px]:fixed max-[599px]:inset-x-3 max-[599px]:top-[58px] max-[599px]:w-auto max-[599px]:max-h-[calc(100vh-72px)] max-[599px]:overflow-auto"
    >
      <div className="mb-1 font-semibold">
        Compare against <span className="font-normal text-muted-foreground">· used by Changes and by the markers in every view</span>
      </div>
      {offNote !== null && (
        <div className="rounded-md border border-dashed border-dynamic-bar px-2.5 py-1 text-[12px] text-dynamic" data-testid="markers-off-note">
          {offNote.charAt(0).toUpperCase() + offNote.slice(1)}.
        </div>
      )}
      {refusal !== null && (
        <div role="alert" className="rounded-md border border-dynamic-bar/50 bg-dynamic-soft/70 px-2.5 py-1.5 text-[12.5px]">
          <b>{refusal.title}</b> <span className="font-mono text-[11px]">{refusal.code}</span>
          <div>{refusal.body}</div>
          <button type="button" className={cn(OUTLINE_BUTTON, "mt-1.5")} onClick={c.dismissRef}>
            Dismiss
          </button>
        </div>
      )}
      {option("previous", "Previous run", <span className="text-xs text-muted-foreground">{previous === null ? "no previous run yet (the journal starts with the next index run)" : runText(previous)}</span>, previous === null)}
      {option(
        "run",
        "Run",
        older.length === 0 ? (
          <span className="text-xs text-muted-foreground">no older journal run</span>
        ) : (
          <select
            aria-label="Journal run"
            className="h-7 w-full rounded-md border border-input bg-background px-1.5 text-[12.5px]"
            value={run}
            onChange={(e) => {
              setRun(e.target.value);
              setKind("run");
            }}
          >
            {older.map((r) => (
              <option key={r.run_id} value={r.run_id}>
                {runText(r)}
              </option>
            ))}
          </select>
        ),
        older.length === 0,
      )}
      {option(
        "pin",
        "Pin",
        <div className="grid gap-1.5">
          {bases === null || bases.pins.length === 0 ? (
            <span className="text-xs text-muted-foreground">no pins yet</span>
          ) : (
            <ul className="grid gap-1" aria-label="Pins">
              {bases.pins.map((p) => (
                <li key={p.id} className="flex min-w-0 items-center gap-2 text-[12.5px]">
                  <button
                    type="button"
                    className={cn("min-w-0 truncate rounded px-1 text-left hover:bg-accent", pin === p.name && kind === "pin" && "font-semibold text-brand-text")}
                    aria-pressed={pin === p.name && kind === "pin"}
                    onClick={() => {
                      setPin(p.name);
                      setKind("pin");
                    }}
                  >
                    {p.name}
                  </button>
                  <span className="truncate text-xs text-muted-foreground">{runText(p.run)}</span>
                  <button type="button" className={cn(OUTLINE_BUTTON, "ml-auto h-6 px-2")} disabled={busy} onClick={() => { doUnpin(p.name); }} aria-label={`Unpin ${p.name}`}>
                    Unpin
                  </button>
                </li>
              ))}
            </ul>
          )}
          <div className="flex items-center gap-1.5">
            <input
              aria-label="Pin name"
              placeholder={head === null ? "pin name" : `name for run ${String(head.seq)}`}
              className="h-7 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-[12.5px]"
              value={pinName}
              maxLength={200}
              onChange={(e) => {
                setPinName(e.target.value);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  doPin();
                }
              }}
            />
            <button type="button" className={cn(OUTLINE_BUTTON, "h-7")} disabled={!pinValid || busy} onClick={doPin}>
              {head === null ? "Pin latest run" : `Pin run ${String(head.seq)}`}
            </button>
          </div>
          {bases !== null && <span className="text-[11.5px] text-muted-foreground">At most {String(bases.retention.max_pins)} pins; a pin keeps its run comparable after the journal moves on.</span>}
        </div>,
      )}
      {option(
        "branch",
        "Branch",
        <div className="grid grid-cols-[minmax(0,1fr)] gap-1">
          <div className="flex items-center gap-1.5">
            <input
              aria-label="Base ref"
              placeholder="base ref (main)"
              className="h-7 min-w-0 flex-1 rounded-md border border-input bg-background px-2 font-mono text-[12px]"
              value={refBase}
              onChange={(e) => {
                setRefBase(e.target.value);
                setKind("branch");
              }}
            />
            <span aria-hidden="true" className="text-muted-foreground">
              {"→"}
            </span>
            <input
              aria-label="Head ref (optional)"
              placeholder="current index"
              className="h-7 min-w-0 flex-1 rounded-md border border-input bg-background px-2 font-mono text-[12px]"
              value={refHead}
              onChange={(e) => {
                setRefHead(e.target.value);
                setKind("branch");
              }}
            />
          </div>
          <span className="text-[11.5px] text-muted-foreground">
            Compares the merge base of the base ref and {refHead.trim() === "" ? "the commit the current index recorded" : "the head ref"} with {refHead.trim() === "" ? "the current index" : "the head ref's snapshot"}. Both commits are indexed first with your local git; nothing is fetched.
          </span>
          {mergeBase !== null && (
            <span className="text-[11.5px] text-muted-foreground" data-testid="merge-base-note">
              {mergeBase}
            </span>
          )}
        </div>,
      )}
      {note !== null && (
        <div role="status" className="px-2 text-[12px] text-muted-foreground">
          {note}
        </div>
      )}
      <div className="mt-1 flex items-center justify-between gap-2 border-t pt-2">
        <button
          type="button"
          className={OUTLINE_BUTTON}
          onClick={() => {
            c.setOn(!c.choice.on);
            onClose();
          }}
        >
          {c.choice.on ? "Turn compare off" : "Turn compare on"}
        </button>
        <button
          type="button"
          className="inline-flex h-7 items-center rounded-md bg-brand px-3 text-[12.5px] font-medium text-brand-foreground disabled:opacity-50"
          disabled={kind === "branch" ? !branchValid : target === null}
          onClick={apply}
        >
          {kind === "branch" ? "Index and compare" : "Apply"}
        </button>
      </div>
    </div>
  );
}

/** CompareChip is the chip, its off button and its menu. */
export function CompareChip() {
  const c = useCompare();
  const view = chipView(c);
  const wrap = useRef<HTMLDivElement>(null);
  const chip = useRef<HTMLButtonElement>(null);
  const { menuOpen, setMenuOpen } = c;

  useEffect(() => {
    if (!menuOpen) return;
    const onDown = (e: MouseEvent) => {
      if (wrap.current !== null && e.target instanceof Node && !wrap.current.contains(e.target)) setMenuOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("mousedown", onDown);
    };
  }, [menuOpen, setMenuOpen]);

  const close = () => {
    setMenuOpen(false);
    chip.current?.focus();
  };
  const onKey = (e: ReactKeyboardEvent) => {
    if (e.key === "Escape" && menuOpen) {
      e.stopPropagation();
      close();
    }
  };
  // Focus moving to a control outside the chip and its menu (Tab, or a
  // click on another control) closes the menu (R5-6). A blur without a
  // new focus target (a click on the menu's text) keeps it; a press
  // outside closes it through the mousedown listener.
  const onBlur = (e: ReactFocusEvent) => {
    if (!menuOpen) return;
    const next = e.relatedTarget;
    if (next instanceof Node && wrap.current !== null && !wrap.current.contains(next)) setMenuOpen(false);
  };
  const commit = c.summary.state.kind === "ok" && c.summary.state.data.summary.base.kind === "ref" ? c.summary.state.data.summary.base.commit : null;
  return (
    <div ref={wrap} className="relative flex min-w-0 flex-1 items-center" onKeyDown={onKey} onBlur={onBlur}>
      <button
        ref={chip}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={menuOpen}
        title={`${view.lead}${view.text}${view.count !== null ? ` (${formatCount(view.count)} changes)` : ""}. ${view.title}${commit === null ? "" : ` (merge base ${shortCommit(commit)})`}`}
        data-testid="compare-chip"
        data-tone={view.tone}
        onClick={() => {
          setMenuOpen(!menuOpen);
        }}
        className={cn(
          "inline-flex h-[30px] min-w-0 max-w-[340px] items-center gap-1.5 rounded-full border px-3 text-[12.5px] whitespace-nowrap",
          view.tone === "on" || view.tone === "busy" ? "border-brand/45 bg-brand-soft text-brand-text" : view.tone === "refused" ? "border-dynamic-bar/60 bg-dynamic-soft text-dynamic" : "border-border text-muted-foreground",
          (view.tone === "on" || view.tone === "busy") && "rounded-r-none border-r-0",
        )}
      >
        <GitCompareArrows aria-hidden="true" className="size-3.5 flex-none" />
        <span className="min-w-0 truncate" data-testid="compare-text">
          {view.lead !== "" && <span className="@max-[1299px]/top:hidden">{view.lead}</span>}
          {view.text}
        </span>
        {view.count !== null && (
          <span className="flex-none rounded-full bg-brand px-1.5 text-[11px] font-semibold text-brand-foreground tabular-nums" data-testid="compare-count">
            {formatCount(view.count)}
          </span>
        )}
        {view.tone === "busy" && view.count === null && <span aria-hidden="true" className="h-3.5 w-6 flex-none animate-pulse rounded-full bg-brand/40" />}
        {view.note !== null && (
          <span className="flex-none rounded-full border border-dashed border-dynamic-bar px-1.5 text-[11px] leading-4 text-dynamic" data-testid="compare-markers-off">
            markers off
          </span>
        )}
      </button>
      {(view.tone === "on" || view.tone === "busy") && (
        <button
          type="button"
          aria-label="Turn compare off"
          title="Turn compare off (markers off in every view)"
          data-testid="compare-off"
          className="inline-flex h-[30px] w-7 flex-none items-center justify-center rounded-r-full border border-l-0 border-brand/45 bg-brand-soft text-brand-text hover:bg-brand/15"
          onClick={() => {
            c.setOn(false);
            if (c.ref !== null) c.dismissRef();
          }}
        >
          <X aria-hidden="true" className="size-3.5" />
        </button>
      )}
      {menuOpen && <CompareMenu c={c} onClose={close} />}
    </div>
  );
}
