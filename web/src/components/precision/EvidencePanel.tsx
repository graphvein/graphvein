// The Evidence popover's content (T020, Variant A with Variant B's
// coverage table): the label and its scope, the rule row's evidence
// (Precise) or the reason (Best effort), the run's eligibility, and the
// coverage of this index from GET /api/v0/precision. Every label and count
// is the server's; the table's languages and families are the ones the
// server lists, never a map kept in the UI.

import { X } from "lucide-react";
import { useContext, type ReactNode } from "react";

import type { PrecisionResponse, PrecisionRule } from "@/api/precision";
import { useApiRequest } from "@/hooks/useApiRequest";
import { cn } from "@/lib/utils";
import { BestEffortIcon, LabelTag, PrecisionLoaderContext, type Focus } from "@/components/precision/parts";
import { bestEffortLead, NEVER_COMPLETE, PRECISE_TEXT, REPORTED_TEXT, reasonText, type ChipState } from "@/components/precision/words";
import { CoverageTable } from "@/components/precision/CoverageTable";

function RowId({ id }: { id: string }) {
  return <span className="rounded-[4px] bg-muted px-[5px] py-px font-mono text-[11px] font-semibold text-muted-foreground">{id}</span>;
}

function Tests({ list }: { list: readonly { package: string; name: string }[] }) {
  return (
    <>
      {list.map((t, i) => (
        <span key={`${t.package}/${t.name}`}>
          {i > 0 && ", "}
          <code className="font-mono text-[12px]">{t.name}</code> in <code className="font-mono text-[12px]">{t.package}</code>
        </span>
      ))}
    </>
  );
}

function Quotes({ list }: { list: readonly { adr: string; quote: string }[] }) {
  return (
    <>
      {list.map((q) => (
        <span key={`${q.adr}:${q.quote}`} className="block">
          ADR {q.adr} review notes: &ldquo;{q.quote}&rdquo;
        </span>
      ))}
    </>
  );
}

/** RowEvidence is the five pieces of evidence of a rule row. */
function RowEvidence({ rule }: { rule: PrecisionRule }) {
  const e = rule.evidence;
  return (
    <dl className="m-0 grid grid-cols-[124px_minmax(0,1fr)] gap-x-3 gap-y-1 [&_dd]:m-0 [&_dd]:[overflow-wrap:anywhere] [&_dt]:text-muted-foreground" data-testid="precision-evidence">
      <dt>Claim contract</dt>
      <dd>
        <Tests list={e.contract} />
      </dd>
      <dt>Golden fixtures</dt>
      <dd>
        <code className="font-mono text-[12px]">{e.goldens.join(", ")}</code>
      </dd>
      <dt>Corpus checks</dt>
      <dd>
        <Quotes list={e.corpus} />
      </dd>
      <dt>Independent review</dt>
      <dd>
        <Quotes list={e.review} />
      </dd>
      <dt>Oracle</dt>
      <dd>{e.oracle.length > 0 ? <Tests list={e.oracle} /> : <span>none: {e.oracle_none ?? "no reason given"}</span>}</dd>
      <dt>Falls to Best effort</dt>
      <dd>{rule.refusal}</dd>
    </dl>
  );
}

/** RunLine says whether the rule table applies to this index. */
function RunLine({ data }: { data: PrecisionResponse }) {
  const run = data.run;
  const version = data.index?.indexer.version ?? "unknown";
  let warn = true;
  let text: ReactNode;
  if (run === null) {
    text = "Nothing is indexed yet.";
  } else if (run.refusal === "external_indexer") {
    text = (
      <>
        Built by <b className="font-semibold">{data.index?.indexer.name ?? data.files?.reported_by ?? "an external indexer"}</b> (external indexer, ADR 0010). File labels are shown as
        that indexer reported them; Graphvein did not check them, and no scope in this run is Precise.
      </>
    );
  } else if (run.refusal === "extractor_mismatch") {
    text = (
      <>
        Indexed with <code className="font-mono">{version}</code>; this binary has <code className="font-mono">{data.binary_extractor}</code>. Every scope is shown as Best effort until
        you run <code className="font-mono">graphvein index</code>.
      </>
    );
  } else if (run.refusal !== null) {
    text = <>The rule table does not apply to this index ({run.refusal}).</>;
  } else {
    warn = false;
    text = (
      <>
        Built by Graphvein&apos;s own indexer with <code className="font-mono">{version}</code>, the same as this binary: the rule table applies.
      </>
    );
  }
  return (
    <div className={cn("flex items-start gap-2 rounded-lg border px-2.5 py-1", warn ? "border-dynamic-bar/45 bg-dynamic-soft/70" : "bg-muted/50")} data-testid="precision-run">
      <span aria-hidden="true" className={cn("mt-1.5 size-2 flex-none rounded-full", warn ? "bg-dynamic-bar" : "bg-linked")} />
      <div>{text}</div>
    </div>
  );
}

/** Loaded is the part that needs /precision. */
function Loaded({ data, state, focus }: { data: PrecisionResponse; state: Exclude<ChipState, { kind: "none" }>; focus: Focus }) {
  const row = state.kind === "precise" || state.kind === "best_effort" ? state.row : null;
  const rule = row !== null ? (data.rules.find((r) => r.id === row) ?? null) : null;
  return (
    <>
      {state.kind === "precise" &&
        (rule !== null ? (
          <RowEvidence rule={rule} />
        ) : (
          <p className="m-0 text-muted-foreground italic">Row {row ?? "unknown"} is not in this server&apos;s rule table.</p>
        ))}
      {state.kind === "best_effort" && rule !== null && (
        <p className="m-0 text-muted-foreground" data-testid="precision-refused-row">
          Row {rule.id} ({rule.scope}) would apply; it is refused for this index.
        </p>
      )}
      {state.kind === "best_effort" && rule === null && (state.reason === null || state.reason === "default_branch") && (
        <p className="m-0 text-muted-foreground">Row D covers {data.default.covers}.</p>
      )}
      <RunLine data={data} />
      <CoverageTable data={data} focus={focus} />
    </>
  );
}

/** EvidencePanel is the popover body; it loads /precision when shown. */
export function EvidencePanel({ state, scope, focus, indexer, onClose }: { state: Exclude<ChipState, { kind: "none" }>; scope: string; focus: Focus; indexer: string | null; onClose: () => void }) {
  const load = useContext(PrecisionLoaderContext);
  const { state: req, retry } = useApiRequest("precision", (signal) => load({ signal }));
  const row = state.kind === "precise" || state.kind === "best_effort" ? state.row : null;
  return (
    <div data-label={state.kind}>
      <header className="sticky top-0 z-[1] flex flex-wrap items-center gap-2 bg-popover px-3.5 pt-2.5 pb-1.5">
        <h3 className="m-0 text-[14px] font-semibold">{scope}</h3>
        {state.kind === "reported" ? (
          <span className="inline-flex h-[18px] items-center gap-1 rounded-[5px] border border-dashed border-other-bar pr-1.5 pl-1 text-[11px] text-other">
            <BestEffortIcon dashed className="size-[11px]" />
            reported by {indexer ?? "the external indexer"}: {state.value}
          </span>
        ) : (
          <LabelTag precise={state.kind === "precise"}>{state.kind === "precise" ? "Precise" : "Best effort"}</LabelTag>
        )}
        {row !== null && <RowId id={state.kind === "best_effort" && state.reason !== null && state.reason !== "default_branch" && row !== "D" ? `${row} refused` : row} />}
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="ml-auto grid size-[26px] place-items-center rounded-md text-muted-foreground outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
          data-testid="precision-close"
        >
          <X aria-hidden="true" className="size-4" />
        </button>
      </header>
      <div className="grid gap-2 px-3.5 pb-3">
        <p className="m-0 leading-[1.4]">{state.kind === "precise" ? PRECISE_TEXT : state.kind === "reported" ? REPORTED_TEXT : bestEffortLead(state.reason)}</p>
        {state.kind === "best_effort" && (
          <dl className="m-0 grid grid-cols-[124px_minmax(0,1fr)] gap-x-3 [&_dd]:m-0 [&_dt]:text-muted-foreground">
            <dt>Why</dt>
            <dd data-testid="precision-why">{reasonText(state.reason, state.side)}</dd>
          </dl>
        )}
        {req.kind === "ok" ? (
          <Loaded data={req.data} state={state} focus={focus} />
        ) : req.kind === "failed" ? (
          <p className="m-0 text-muted-foreground" role="alert">
            The evidence could not be loaded ({req.error.message}).{" "}
            <button type="button" className="text-brand-text underline underline-offset-[3px]" onClick={retry}>
              Retry
            </button>
          </p>
        ) : (
          <p className="m-0 text-muted-foreground" aria-live="polite">
            Loading the evidence…
          </p>
        )}
      </div>
      <div className="flex flex-wrap gap-x-3 gap-y-1 border-t px-3.5 py-2 text-[11.5px] text-muted-foreground" data-testid="precision-foot">
        <span>{NEVER_COMPLETE}</span>
        <span>
          Rule table: <code className="font-mono">docs/extract/precision.md</code>
        </span>
      </div>
    </div>
  );
}
