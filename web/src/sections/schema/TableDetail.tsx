// The table detail (D2, D4, D5, D6, T018b2 section 5): header with the
// full completeness badge and chips, the Related strip, the state banner
// with every reason inline, the columns grid, relationships, indexes,
// unresolved references and history. The grid shows the facts of /sets
// while /table loads (both from one index run, so nothing flashes); the
// parts only /table has (history, neighbours, reasons with migration_ref,
// IDE links) wait for it.

import { ArrowLeft, ArrowRight, ExternalLink, X } from "lucide-react";
import type { ReactNode } from "react";

import type { IdeLink, IdeStatus } from "@/api/endpoints";
import type { JSONObject } from "@/api/endpoints";
import { attrInt, attrStringList, rawText, readClaims, readLink, stringsOrNull, type Claim, type OpaqueSite, type Overlap, type SchemaGroup, type SchemaTable, type TableDetail as Detail } from "@/api/schema";
import { EditorLink } from "@/components/EditorLink";
import { Skeleton } from "@/components/ui/skeleton";
import { useEditor } from "@/hooks/useEditor";
import { EDITOR_OFF, GENERIC_IDE_LABEL, ideLabel, presentIde } from "@/lib/ide";
import { cn } from "@/lib/utils";
import { absenceCell, type Cell, completenessBadge, foreignKeyCell, incomingAbsenceCell, type IncomingSet, primaryKeyCell, schemaCell, uniqueKeysCell } from "@/sections/schema/cells";
import { rowSource, useSchemaWhy, tableSource } from "@/sections/schema/catalogue";
import { cellWhy, ColumnGrid, referenceEntries, TestHiddenMark, type GridColumn, type GridWhy } from "@/sections/schema/ColumnGrid";
import { columnUnclaimed } from "@/sections/schema/Dialects";
import { OverlapNote, type OverlapTarget } from "@/sections/schema/OverlapNote";
import { RestsOn } from "@/sections/schema/RestsOn";
import { RowSecurity } from "@/sections/schema/RowSecurity";
import { RowChip } from "@/sections/schema/RowChip";
import { GroupChip } from "@/sections/schema/precision";
import { countLabel, countsUnrecorded, factLabel, mappedClaims, mappedCount, ORACLE, unrecorded, UnverifiedChip } from "@/sections/schema/Verification";
import { SourceLink, WhyButton, IdChip, type Why, type WhyReason, type WhySource } from "@/sections/schema/WhyPopover";
import { ModelRoutesPanel } from "@/sections/schema/ModelRoutes";
import { columnLabel, type Names } from "@/sections/schema/names";
import { Banner, CellText, ChangedMark, Chip, CompletenessBadge, fileLine, HalfCircle, OUTLINE_BUTTON, tableName } from "@/sections/schema/parts";
import { setLabel } from "@/sections/schema/SetStrip";
import { VIEW_GROUPS } from "@/sections/changes/markers";
import { NodeMarker } from "@/shell/ChangeMarker";
import { useProject } from "@/shell/project";

/** IdeOpen is the "Open in IDE" button (shortcut o); disabled with the
 * reason when there is no link or the browser chose "No editor links".
 * Its label, title and accessible name come from the response under the
 * editor precedence (presentIde, ADR 0050). */
export function IdeOpen({ ide, status, testId, place }: { ide: IdeLink | null; status: IdeStatus | null; testId: string; place: string }) {
  const editor = useEditor();
  const view = status === null && !editor.off ? null : presentIde(status, ide, editor);
  if (view === null || view.href === null) {
    const text = view?.text ?? GENERIC_IDE_LABEL;
    const title = view?.title ?? "Loading the IDE link";
    const name = view?.name ?? `${text} (Loading the IDE link)`;
    return (
      <button type="button" disabled title={title} aria-label={name} className={OUTLINE_BUTTON} data-testid={testId} aria-keyshortcuts="o">
        <ExternalLink aria-hidden="true" />
        {text}
      </button>
    );
  }
  return (
    <EditorLink href={view.href} place={place} label={view.text} detail={view.title} className={OUTLINE_BUTTON} aria-label={view.name} aria-keyshortcuts="o" data-testid={testId} data-ide-primary>
      <ExternalLink aria-hidden="true" />
      {view.text}
      <kbd className="rounded-[4px] border bg-muted px-1 font-mono text-[10.5px] text-muted-foreground">o</kbd>
    </EditorLink>
  );
}

function Section({ title, aside, children, testId }: { title: string; aside?: ReactNode; children: ReactNode; testId?: string }) {
  return (
    <section className="grid gap-2 border-t px-5 py-3.5" data-testid={testId}>
      <h3 className="m-0 flex flex-wrap items-baseline gap-2 text-[14px] font-semibold">
        {title}
        {aside !== undefined && <span className="text-xs font-normal text-muted-foreground">{aside}</span>}
      </h3>
      {children}
    </section>
  );
}

/** spanText writes a stored span: one line with its columns, or the start
 * and end positions when it runs across lines (the columns belong to
 * different lines then). "" when there is no readable span. */
export function spanText(span: unknown): string {
  if (span === null || typeof span !== "object" || Array.isArray(span)) return "";
  const s = span as Record<string, unknown>;
  const int = (v: unknown) => (typeof v === "number" && Number.isInteger(v) ? v : null);
  const sl = int(s.start_line);
  const el = int(s.end_line);
  const sc = int(s.start_column);
  const ec = int(s.end_column);
  if (sl === null) return "";
  if (el === null || el === sl) return sc !== null && ec !== null ? `line ${String(sl)}, columns ${String(sc)}-${String(ec)}` : `line ${String(sl)}`;
  return sc !== null && ec !== null ? `line ${String(sl)} column ${String(sc)} to line ${String(el)} column ${String(ec)}` : `lines ${String(sl)}-${String(el)}`;
}

/** unresolvedTexts names a table's unresolved references: from /table
 * when loaded, else from the columns' stored reference entries. */
export function unresolvedTexts(detail: Detail | null, columns: readonly GridColumn[]): string[] {
  const text = (r: unknown) => {
    const t = r !== null && typeof r === "object" && !Array.isArray(r) ? (r as Record<string, unknown>).table : undefined;
    return typeof t === "string" ? t : "unreadable reference";
  };
  if (detail !== null) return detail.unresolved_references.map((u) => text(u.reference));
  return columns.flatMap((c) => referenceEntries(c.node.attributes).filter((e) => !e.ok || !e.linked).map((e) => (e.ok ? text(e.ref) : "unreadable reference")));
}

function versionText(v: unknown): string {
  return typeof v === "string" ? v : v === null || v === undefined ? "unknown" : rawText(v);
}

/** migrationFile is the file of a migration identity
 * ("migration:<file>#migration"), or the identity itself. */
export function migrationFile(id: string): string {
  const m = /^migration:(.*)#[^#]*$/.exec(id);
  return m?.[1] ?? id;
}

/** reasonItem turns a stored reason (as /sets or /table holds it) into a
 * popover line: text, row id and where it is. Model reasons (T018f) carry
 * file, line and fact; migration reasons migration, span and, with /table,
 * migration_ref and link. */
export function reasonItem(r: JSONObject): WhyReason & { fact?: string | null } {
  const text = typeof r.reason === "string" ? r.reason : rawText(r.reason);
  const row = typeof r.row === "string" ? r.row : null;
  const link = readLink(r.link);
  const fact = Object.hasOwn(r, "fact") ? (typeof r.fact === "string" ? r.fact : null) : undefined;
  let where: WhySource | null = null;
  if (typeof r.file === "string" && !Object.hasOwn(r, "migration")) {
    where = { label: typeof r.line === "number" ? `${r.file}:${String(r.line)}` : r.file, link };
  } else if (typeof r.migration === "string") {
    const ref = r.migration_ref;
    const file = ref !== null && typeof ref === "object" && !Array.isArray(ref) && typeof (ref as JSONObject).file === "string" ? ((ref as JSONObject).file as string) : migrationFile(r.migration);
    const span = r.span !== null && typeof r.span === "object" ? (r.span as JSONObject) : null;
    const line = span !== null && typeof span.start_line === "number" ? `:${String(span.start_line)}` : "";
    where = { label: `${file}${line}`, link, migration: link === null ? r.migration : null };
  }
  return fact === undefined ? { text, row, where } : { text, row, where, fact };
}

/** siteItem turns an opaque site into a popover line. */
export function siteItem(s: OpaqueSite): WhyReason {
  const file = s.migration === null ? "migration not recorded" : migrationFile(s.migration);
  const line = s.span === null ? "" : `:${String(s.span.start_line)}`;
  return {
    text: `${s.kind ?? "opaque"} statement: its effect is not visible in source`,
    row: s.row,
    where: { label: `${file}${line}`, link: s.link, migration: s.link === null ? s.migration : null },
  };
}

/** tableReasons lists a table's reasons as popover lines: /table's (with
 * links) once loaded, else the stored attribute. */
export function tableReasons(attrs: Record<string, unknown>, detail: Detail | null): (WhyReason & { fact?: string | null })[] {
  const list = detail?.reasons ?? (Array.isArray(attrs.reasons) ? (attrs.reasons as unknown[]).filter((r): r is JSONObject => r !== null && typeof r === "object" && !Array.isArray(r)) : []);
  return list.map(reasonItem);
}

/** stateWhy is the popover of a partial, complete_in_source or unknown
 * badge: the reasons or the opaque statements, and what the table's known
 * facts rest on. */
export function stateWhy(name: string, completeness: string, attrs: Record<string, unknown>, detail: Detail | null, sites: OpaqueSite[] | null, source: string, restsOn: string[] | null): Why {
  const base = { source, assumptions: { heading: "Known facts of this table rest on", ids: restsOn } };
  if (completeness === "complete_in_source") {
    const reasons = sites === null ? [] : sites.map(siteItem);
    const rows = [...new Set(reasons.map((r) => r.row).filter((r): r is string => r !== null))];
    return {
      ...base,
      kind: "state",
      title: `${name}: complete as far as the migrations show`,
      lead:
        sites === null
          ? "Opaque statements run after this table is created; this index does not record every one of them (see the Migrations tab). Absences carry ◑."
          : "These opaque statements run after this table is created; their effects are not visible in source, so a live database may differ. Absences carry ◑.",
      reasons,
      rows,
      entry: rows[0] !== undefined ? `row:${source}:${rows[0]}` : null,
    };
  }
  if (completeness === "partial") {
    const reasons = tableReasons(attrs, detail);
    const rows = [...new Set(reasons.map((r) => r.row).filter((r): r is string => r !== null))];
    return {
      ...base,
      kind: "state",
      title: `${name}: partial, some facts unknown`,
      lead: "Facts these sites may change are unknown, never defaulted.",
      reasons,
      rows,
      entry: rows[0] !== undefined ? `row:${source}:${rows[0]}` : null,
    };
  }
  return { ...base, kind: "state", title: `${name}: state unknown`, lead: `The stored state (${rawText(attrs.state)}) is not one this view can read; every fact is last known from source.`, entry: null };
}

/** StateBanner states the table's completeness with every reason inline. */
function StateBanner({ completeness, attrs, detail, onMigration, why, model }: { completeness: string; attrs: Record<string, unknown>; detail: Detail | null; onMigration: (id: string) => void; why: GridWhy; model: boolean }) {
  if (completeness === "complete") return null;
  if (completeness === "complete_in_source") {
    const n = attrInt(attrs, "opaque_after");
    return (
      <div className="flex items-start gap-2 rounded-lg border bg-muted/50 px-3 py-2 text-[13px]" data-testid="state-banner">
        <HalfCircle className="mt-0.5" />
        <span>
          <b className="font-semibold">Complete as far as the migrations show.</b> {n === null ? "Opaque statements run" : `${String(n)} opaque ${n === 1 ? "statement runs" : "statements run"}`}{" "}
          after this table is created; their effects are not visible in source, so a live database may differ. Absence claims carry <HalfCircle className="inline size-3 align-[-2px]" />.{" "}
          <button type="button" className="font-medium text-brand-text underline underline-offset-[3px]" onClick={() => { onMigration(""); }}>
            See the statements
          </button>
        </span>
      </div>
    );
  }
  if (completeness === "partial") {
    const reasons = detail?.reasons ?? (Array.isArray(attrs.reasons) ? (attrs.reasons as Record<string, unknown>[]) : null);
    return (
      <Banner tone="warn" testId="state-banner">
        {model ? (
          <>
            <b className="font-semibold">Partial.</b> Facts these sites may change are unknown, never defaulted:
          </>
        ) : (
          <>
            <b className="font-semibold">
              Partial: {reasons === null ? "statements" : `${String(reasons.length)} ${reasons.length === 1 ? "statement" : "statements"}`} touching this table could not be modeled.
            </b>{" "}
            Columns, keys and indexes below are last known from source.
          </>
        )}
        {reasons !== null && (
          <ul className="m-0 mt-1 grid gap-1 pl-5" data-testid="reasons">
            {reasons.map((r, i) => {
              const text = typeof r.reason === "string" ? r.reason : rawText(r.reason);
              const rowId = typeof r.row === "string" ? r.row : null;
              const rowChip = rowId === null ? null : (
                <WhyButton
                  testId="reason-row"
                  label={`Contract row ${rowId}`}
                  why={() => ({ kind: "state", title: `Contract row ${rowId}`, source: why.source, lead: text, rows: [rowId], reasons: [], assumptions: { heading: "Known facts of this table rest on", ids: why.restsOn }, entry: `row:${why.source}:${rowId}` })}
                >
                  <IdChip id={rowId} kind="row" />
                </WhyButton>
              );
              if (typeof r.file === "string" && !Object.hasOwn(r, "migration")) {
                const item = reasonItem(r);
                return (
                  <li key={i}>
                    {rowChip} {text}
                    {item.where !== null && (
                      <>
                        {" · "}
                        {detail === null ? <span className="font-mono text-[12.5px]">{item.where.label}</span> : <SourceLink s={item.where} />}
                      </>
                    )}
                  </li>
                );
              }
              const mig = r.migration;
              // migration_ref comes only with /table: a skeleton while it
              // loads; absent or not an object in a loaded answer reads
              // unknown (never a skeleton forever, never "not stored").
              const ref = detail === null ? undefined : Object.hasOwn(r, "migration_ref") ? r.migration_ref : "missing";
              let where: ReactNode;
              if (mig === null) where = <span className="text-muted-foreground">set-level</span>;
              else if (ref === undefined) where = <Skeleton className="inline-block h-3 w-24" />;
              else if (ref === null) where = <span className="text-muted-foreground" title={rawText(mig)}>migration not stored</span>;
              else if (typeof ref !== "object" || Array.isArray(ref))
                where = (
                  <span className="text-muted-foreground italic" title={`migration_ref: ${ref === "missing" ? "missing" : rawText(ref)}`} data-testid="migration-ref-unknown">
                    migration unknown
                  </span>
                );
              else {
                const mr = ref as Record<string, unknown>;
                const id = typeof mr.id === "string" ? mr.id : null;
                const file = typeof mr.file === "string" ? mr.file : rawText(mr.file);
                const bytes = spanText(r.span);
                where = (
                  <span>
                    {id !== null ? (
                      <button type="button" className="font-mono text-brand-text underline underline-offset-[3px]" onClick={() => { onMigration(id); }}>
                        {file.split("/").pop()}
                      </button>
                    ) : (
                      <span className="font-mono">{file}</span>
                    )}{" "}
                    <span className="text-muted-foreground">
                      (version {versionText(mr.version)}
                      {bytes !== "" && <>, <span className="whitespace-nowrap" data-testid="reason-span">{bytes}</span></>})
                    </span>
                  </span>
                );
              }
              return (
                <li key={i}>
                  {rowChip} {text} · {where}
                </li>
              );
            })}
          </ul>
        )}
      </Banner>
    );
  }
  return (
    <div className="rounded-lg border border-dashed border-muted-foreground/70 px-3 py-2 text-[13px]" data-testid="state-banner" title={`state ${rawText(attrs.state)}, opaque_after ${rawText(attrs.opaque_after)}`}>
      <b className="font-semibold">State unknown.</b> The stored state ({rawText(attrs.state)}, opaque after {rawText(attrs.opaque_after)}) is not one the view can read; every fact below is last known from source.
    </div>
  );
}

/** relatedNoneCell is the Related strip's "no foreign keys": it denies
 * both directions, so it needs the outgoing rule (R1) and the incoming,
 * whole-set rule (R2) to hold; otherwise it shows why not. */
export function relatedNoneCell(identity: string, completeness: string, unresolved: readonly string[], set: IncomingSet, fkUnknown = 0): Cell {
  const out = absenceCell(completeness, "no foreign keys", unresolved);
  if (out.rule === "absence.unresolved") return out;
  // A column whose foreign keys are not known (C5) may reference a table.
  if (fkUnknown > 0 && out.rule !== "absence.unknown") {
    return { rule: "absence.unknown", text: "none known", unknown: true, title: `The foreign keys of ${String(fkUnknown)} ${fkUnknown === 1 ? "column are" : "columns are"} not known` };
  }
  const inc = incomingAbsenceCell(identity, completeness, "no foreign keys", set);
  return inc.rule === "absence.incoming.proven" ? out : inc;
}

function Related({ detail, names, onTable, visible, hiddenTest, completeness, identity, set, fkUnknown }: { detail: Detail | null; names: Names; onTable: (id: string) => void; visible: (id: string) => boolean; hiddenTest: (id: string) => boolean; completeness: string; identity: string; set: IncomingSet; fkUnknown: number }) {
  if (detail === null) return <Skeleton className="h-6 w-1/2" />;
  const chip = (dir: "out" | "in", n: Detail["neighbours"]["outgoing"][number]) => {
    const id = n.table.identity;
    const label = tableName(n.table);
    const Icon = dir === "out" ? ArrowRight : ArrowLeft;
    const body = (
      <>
        <Icon aria-hidden="true" className="size-3" />
        <span className="font-mono">{label}</span>
        {n.pairs.length > 1 && <span className="text-muted-foreground tabular-nums">{n.pairs.length}</span>}
      </>
    );
    const cls = "inline-flex h-6 items-center gap-1 rounded-md border bg-background px-2 text-[12.5px]";
    const title = `${dir === "out" ? "This table references" : "References this table"}: ${n.pairs.map((p) => `${columnLabel(names, p.source_column)} → ${columnLabel(names, p.target_column)}`).join(", ")}`;
    return visible(id) ? (
      <button key={`${dir}:${id}`} type="button" className={cn(cls, "hover:bg-accent")} title={title} onClick={() => { onTable(id); }} data-testid="related-chip">
        {body}
      </button>
    ) : (
      <span key={`${dir}:${id}`} className={cls} title={`${title} (${hiddenTest(id) ? "test-scope; Include tests shows it" : "in a hidden set"})`} data-testid="related-chip">
        {body}
        {hiddenTest(id) && <TestHiddenMark />}
      </span>
    );
  };
  const out = detail.neighbours.outgoing;
  const inc = detail.neighbours.incoming;
  return (
    <div className="flex flex-wrap items-center gap-1.5 text-[12.5px] text-muted-foreground" data-testid="related">
      <span>Related</span>
      {out.length + inc.length === 0 && (
        <span data-testid="related-none">
          <CellText c={relatedNoneCell(identity, completeness, unresolvedTexts(detail, []), set, fkUnknown)} />
        </span>
      )}
      {inc.map((n) => chip("in", n))}
      {out.map((n) => chip("out", n))}
      {detail.truncated !== null && (
        <span className="text-dynamic" data-testid="neighbours-truncated">
          +{detail.truncated.tables_omitted} tables not shown (cap)
        </span>
      )}
      <span className="ml-1 text-xs">← references this table · → this table references</span>
    </div>
  );
}

function Relationships({ detail, names, name, completeness, identity, set }: { detail: Detail | null; names: Names; name: string; completeness: string; identity: string; set: IncomingSet }) {
  if (detail === null) return <Skeleton className="h-12" />;
  const out = detail.neighbours.outgoing;
  const inc = detail.neighbours.incoming;
  const list = (ns: typeof out, empty: Cell, testId: string) =>
    ns.length === 0 ? (
      <span className="text-muted-foreground" data-testid={testId}>
        <CellText c={empty} />
      </span>
    ) : (
      <ul className="m-0 grid gap-1 pl-0">
        {ns.map((n) => (
          <li key={n.table.identity} className="list-none rounded-md border px-2.5 py-1.5">
            <b className="font-mono font-semibold">{tableName(n.table)}</b>{" "}
            {!["complete", "complete_in_source"].includes(n.completeness) && <CompletenessBadge c={completenessBadge(n.completeness, n.table.attributes)} />}
            {n.pairs.map((p) => (
              <span key={`${p.source_column}|${p.target_column}`} className="block font-mono text-xs text-muted-foreground">
                {columnLabel(names, p.source_column)} → {columnLabel(names, p.target_column)}
              </span>
            ))}
          </li>
        ))}
      </ul>
    );
  return (
    <div className="grid grid-cols-2 gap-3 text-[13px] max-[1100px]:grid-cols-1">
      <div className="grid content-start gap-1">
        <span className="text-xs text-muted-foreground">← references {name}</span>
        {list(inc, incomingAbsenceCell(identity, completeness, `no table references ${name}`, set), "incoming-none")}
      </div>
      <div className="grid content-start gap-1">
        <span className="text-xs text-muted-foreground">{name} references →</span>
        {list(out, absenceCell(completeness, `${name} references no table`, unresolvedTexts(detail, [])), "outgoing-none")}
      </div>
    </div>
  );
}

type IndexNode = { attributes: Record<string, unknown>; identity: string; span: { start_line: number } | null; ide: IdeLink | null };

function Indexes({ nodes, completeness, chip }: { nodes: IndexNode[]; completeness: string; chip: (n: IndexNode) => ReactNode }) {
  const lastKnown = completeness !== "complete" && completeness !== "complete_in_source";
  // "No editor links" (ADR 0050): the line stays, without its link.
  const { off } = useEditor();
  const list = (key: string, attrs: Record<string, unknown>) => {
    const l = attrStringList(attrs, key);
    return l.kind === "list" ? l.items : null;
  };
  // null is a stored "not written"; missing or another JSON type is unknown.
  const optional = (attrs: Record<string, unknown>, key: string): { kind: "value"; v: string } | { kind: "null" } | { kind: "unknown"; raw: string } => {
    const v = attrs[key];
    if (typeof v === "string") return { kind: "value", v };
    if (v === null) return { kind: "null" };
    return { kind: "unknown", raw: Object.hasOwn(attrs, key) ? rawText(v) : "missing" };
  };
  return (
    <>
      {nodes.length === 0 ? (
        <p className="m-0 text-[13px] text-muted-foreground" data-testid="no-indexes">
          {completeness === "complete"
            ? "No CREATE INDEX statement targets this table."
            : completeness === "complete_in_source"
              ? "No CREATE INDEX as far as the migrations show ◑."
              : "No CREATE INDEX known (last known from source)."}
        </p>
      ) : (
        <ul className="m-0 grid gap-1 pl-0 text-[13px]" data-testid="indexes">
          {nodes.map((n) => {
            const a = n.attributes;
            const name = optional(a, "name");
            const els = list("elements", a);
            const inc = list("include", a);
            const indexMethod = optional(a, "using");
            const where = optional(a, "where");
            const u = a.unique;
            return (
              <li key={n.identity} className="flex list-none flex-wrap items-baseline gap-x-2 border-b py-1 last:border-b-0" data-testid="index-row">
                <span className="font-mono" data-testid="index-name">
                  {name.kind === "value" ? (
                    name.v
                  ) : name.kind === "null" ? (
                    <span className="text-muted-foreground italic">unnamed</span>
                  ) : (
                    <span className="text-muted-foreground italic" title={`name: ${name.raw}`}>
                      name unknown
                    </span>
                  )}
                </span>
                {chip(n)}
                <span className="font-mono text-muted-foreground">{els === null ? "elements unknown" : `(${els.join(", ")})`}</span>
                {u === true && <span className="rounded-[4px] bg-brand-soft px-1 text-[11px] font-semibold text-brand-text">unique{lastKnown ? " · last known" : ""}</span>}
                {u !== true && u !== false && <span className="text-xs text-muted-foreground italic">unique unknown</span>}
                <span className="text-xs text-muted-foreground" data-testid="index-using">
                  {indexMethod.kind === "value" ? `using ${indexMethod.v}` : indexMethod.kind === "null" ? "using not written" : <i title={`using: ${indexMethod.raw}`}>using unknown</i>}
                </span>
                {inc !== null && inc.length > 0 && <span className="font-mono text-xs text-muted-foreground">include ({inc.join(", ")})</span>}
                {where.kind === "value" && <span className="font-mono text-xs text-muted-foreground">where {where.v}</span>}
                {where.kind === "unknown" && (
                  <i className="text-xs text-muted-foreground" title={`where: ${where.raw}`}>
                    where unknown
                  </i>
                )}
                <span className="flex-1" />
                {n.ide !== null && !off ? (
                  <EditorLink href={n.ide.link} place={n.identity} label={ideLabel(n.ide.link)} detail={n.ide.link} className="inline-flex items-center gap-0.5 text-xs text-muted-foreground hover:text-foreground">
                    :{n.span?.start_line ?? "?"}
                    <ExternalLink aria-hidden="true" className="size-3" />
                  </EditorLink>
                ) : (
                  <span className="text-xs text-muted-foreground" {...(off ? { title: EDITOR_OFF } : {})}>
                    :{n.span?.start_line ?? "?"}
                  </span>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <p className="m-0 text-xs text-muted-foreground">Indexes Postgres creates for primary keys and UNIQUE are not listed. CHECK and EXCLUDE constraints are not extracted.</p>
    </>
  );
}

function History({ detail, table, onMigration }: { detail: Detail | null; table: SchemaTable; onMigration: (id: string) => void }) {
  if (detail === null) return <Skeleton className="h-10" />;
  const n = attrInt(table.node.attributes, "opaque_after");
  return (
    <ul className="m-0 grid gap-1.5 pl-0 text-[13px]" data-testid="history">
      {table.created_by === null && <li className="list-none text-muted-foreground">Creating migration not stored.</li>}
      {detail.history.map((h, i) => (
        <li key={i} className="list-none">
          <b className="font-semibold">{h.event === "created" ? "Created" : h.event === "altered" ? "Altered" : h.event}</b> in{" "}
          <button type="button" className="font-mono text-brand-text underline underline-offset-[3px]" onClick={() => { onMigration(h.migration.id); }}>
            {h.migration.file}
          </button>{" "}
          <span className="text-muted-foreground">
            (version {versionText(h.migration.version)}
            {typeof h.migration.order === "number" ? `, order ${String(h.migration.order)}` : ", order unknown"})
          </span>
          {h.evidence.length > 0 && (
            <span className="ml-1 font-mono text-xs text-muted-foreground">{h.evidence.map((e) => `:${e.span === null ? "?" : String(e.span.start_line)}`).join(" ")}</span>
          )}
        </li>
      ))}
      {table.completeness === "complete_in_source" && n !== null && (
        <li className="list-none text-muted-foreground">
          {n} opaque {n === 1 ? "statement runs" : "statements run"} after this table is created; their effects are not shown.
        </li>
      )}
      {table.altered_by.length === 0 && (table.completeness === "complete" || table.completeness === "complete_in_source") && (
        <li className="list-none text-muted-foreground" data-testid="no-alters">
          <CellText c={absenceCell(table.completeness, "No modeled ALTER, RENAME or DROP touches this table.")} />
        </li>
      )}
    </ul>
  );
}

/** declaredIn names where a model group's tool declares a table: Prisma
 * in a `model` block, Drizzle in a table definition (the table builder
 * call and its extra config), SQLModel (and any other tool) in a class.
 * The indexes section says it so a Prisma or Drizzle table is not
 * described as a class. */
export function declaredIn(tool: string | null): string {
  if (tool === "prisma") return "the model block";
  if (tool === "drizzle") return "the table definition";
  if (tool === "diesel") return "the table! block";
  if (tool === SUPABASE_TYPES) return "the generated types";
  return "the class";
}

/** definedBy is the header chip of a model table: its label and the name
 * it shows. A Diesel table is a `table!` block, named by its Rust path in
 * the schema file (the node's qualified name, e.g. "users" or
 * "audit.events"); every other tool's table is a class, from its `class`
 * attribute (ADR 0062 Revision 2). */
export function definedBy(tool: string | null, attrs: Record<string, unknown>, qualifiedName: string | null | undefined): { label: string; name: string | null } {
  if (tool === "diesel") return { label: "table!", name: typeof qualifiedName === "string" && qualifiedName !== "" ? qualifiedName : null };
  // T024c3: a table read from Supabase's generated types is a Tables
  // entry, named <schema>.<table> (the node's qualified name).
  if (tool === SUPABASE_TYPES) return { label: "generated type", name: typeof qualifiedName === "string" && qualifiedName !== "" ? qualifiedName : null };
  return { label: "class", name: className(attrs) };
}

/** SUPABASE_TYPES is the group tool of tables read from Supabase's
 * generated TypeScript types (T024c3, ADR 0077). */
export const SUPABASE_TYPES = "supabase_types";

/** sourceChipText is a table's source chip: "migration", "model", or for
 * Supabase's generated types "generated types" (T024c3: they are no ORM
 * model), followed by the tool. */
export function sourceChipText(model: boolean, tool: string | null): string {
  if (model && tool === SUPABASE_TYPES) return "generated types · supabase";
  const base = model ? "model" : "migration";
  return tool !== null ? `${base} · ${tool}` : base;
}

/** ModelIndexes lists a model table's stored index, unique and check
 * constraint attributes (ADR 0054: model indexes are table attributes);
 * a value that is not a list reads unknown. */
export function ModelIndexes({ attrs, tool }: { attrs: Record<string, unknown>; tool: string | null }) {
  const entry = (x: unknown) => {
    if (x === null || typeof x !== "object" || Array.isArray(x)) return rawText(x);
    const o = x as Record<string, unknown>;
    const cols = Array.isArray(o.columns) ? `(${o.columns.map((c) => (typeof c === "string" ? c : rawText(c))).join(", ")})` : "";
    const name = typeof o.name === "string" ? o.name : "unnamed";
    const extra = typeof o.sqltext === "string" ? ` ${o.sqltext}` : o.unique === true ? " unique" : "";
    return `${name} ${cols}${extra}`.trim();
  };
  const list = (key: string, label: string) => {
    const v = attrs[key];
    return (
      <li key={key} className="list-none" data-testid={`model-${key}`}>
        <span className="text-muted-foreground">{label}:</span>{" "}
        {Array.isArray(v) ? (
          v.length === 0 ? (
            <span className="text-muted-foreground">none declared in {declaredIn(tool)}</span>
          ) : (
            <span className="font-mono">{v.map(entry).join("; ")}</span>
          )
        ) : (
          <i className="text-muted-foreground" title={`${key}: ${rawText(v)}`}>
            unknown
          </i>
        )}
      </li>
    );
  };
  return (
    <ul className="m-0 grid gap-1 pl-0 text-[13px]" data-testid="model-indexes">
      {list("indexes", "Indexes")}
      {list("unique_constraints", "Unique constraints")}
      {list("check_constraints", "Check constraints")}
      <li className="list-none text-xs text-muted-foreground">
        Column-level index and unique flags are in the grid (IX, UQ). DDL hooks: {attrs.ddl_hooks === false ? "none" : attrs.ddl_hooks === true ? "present" : <i>unknown</i>}.
      </li>
    </ul>
  );
}

/** RelationList is the shared list of ORM-level relations: plain text,
 * never foreign-key chips or links. What the text says about the database
 * depends on the tool (relationTexts), so the caller passes it. An entry
 * that is not an object is shown raw. */
function RelationList({ value, title, aside, note, testId, item }: { value: unknown; title: string; aside: string; note: string; testId: string; item: (o: Record<string, unknown>) => ReactNode }) {
  if (!Array.isArray(value) || value.length === 0) return null;
  return (
    <Section title={title} aside={aside} testId={testId}>
      <p className="m-0 mb-1.5 text-[12.5px] text-muted-foreground">{note}</p>
      <ul className="m-0 grid gap-0.5 pl-0 text-[13px]">
        {value.map((x, i) => {
          if (x === null || typeof x !== "object" || Array.isArray(x)) {
            return (
              <li key={i} className="list-none font-mono" data-testid="emulated-relation">
                {rawText(x)}
              </li>
            );
          }
          return (
            <li key={i} className="list-none" data-testid="emulated-relation">
              {item(x as Record<string, unknown>)}
            </li>
          );
        })}
      </ul>
    </Section>
  );
}

/** relationTexts holds each tool's heading and note for its ORM-level
 * relations. Prisma's emulated relations (relationMode "prisma") have no
 * foreign key by definition, so its text says so. A Drizzle relations()
 * entry says nothing about the database either way: the same pair of
 * tables may or may not also have a foreign key, so its text never states
 * one (T018g2 review) and points to the Foreign keys section. */
export const relationTexts = {
  prisma: {
    title: "Relations enforced by Prisma, not the database",
    aside: 'relationMode = "prisma"',
    note: "The database has no foreign key for these: Prisma Client checks them. They are not drawn as links.",
  },
  drizzle: {
    title: "ORM relations (Drizzle relations())",
    aside: "relational queries only",
    note: "Used by Drizzle's relational queries only. Database foreign keys, if any, are listed under Foreign keys.",
  },
} as const;

const relationNames = (v: unknown) => (Array.isArray(v) ? v.map((c) => (typeof c === "string" ? c : rawText(c))).join(", ") : rawText(v));
const relationText = (v: unknown) => (typeof v === "string" ? v : rawText(v));

/** EmulatedRelations lists a Prisma model table's relations under
 * relationMode "prisma" (attribute emulated_relations, T018g1 review):
 * Prisma enforces them, the database holds no foreign key. */
export function EmulatedRelations({ value }: { value: unknown }) {
  return (
    <RelationList
      value={value}
      {...relationTexts.prisma}
      testId="emulated-relations"
      item={(o) => (
        <>
          <span className="font-mono">{relationText(o.field)}</span> ({relationNames(o.columns)}) →{" "}
          <span className="font-mono">{relationText(o.table)}</span> ({relationNames(o.references)})
          {typeof o.model === "string" && <span className="text-muted-foreground"> · model {o.model}</span>}
        </>
      )}
    />
  );
}

/** OrmRelations lists a Drizzle table's relations() entries (attribute
 * orm_relations, row DR-R1, ADR 0059): they drive Drizzle's relational
 * query API only and are never database facts, so they are plain text
 * like Prisma's emulated relations, with a text that states nothing about
 * the database's foreign keys (relationTexts). A target the front end
 * could not resolve reads "unknown table"; columns are shown only for a
 * one() with literal fields and references. */
export function OrmRelations({ value }: { value: unknown }) {
  return (
    <RelationList
      value={value}
      {...relationTexts.drizzle}
      testId="orm-relations"
      item={(o) => (
        <>
          <span className="font-mono">{relationText(o.name)}</span> <span className="text-muted-foreground">({relationText(o.kind)})</span> →{" "}
          {typeof o.table === "string" ? <span className="font-mono">{o.table}</span> : <i className="text-muted-foreground">unknown table</i>}
          {Array.isArray(o.columns) && Array.isArray(o.references) && (
            <>
              {" "}
              ({relationNames(o.columns)} → {relationNames(o.references)})
            </>
          )}
        </>
      )}
    />
  );
}

/** className is a model table's class name from its `class` identity. */
export function className(attrs: Record<string, unknown>): string | null {
  const c = attrs.class;
  if (typeof c !== "string") return null;
  const i = c.lastIndexOf("#");
  return i < 0 ? c : c.slice(i + 1);
}

/** definedFiles lists the files a model table's columns are defined in
 * (its class chain's files), in column order; [] when unknown. */
export function definedFiles(columns: readonly GridColumn[]): string[] {
  const out: string[] = [];
  for (const c of columns) {
    const d = c.node.attributes.defined_in;
    const f = d !== null && typeof d === "object" && !Array.isArray(d) ? (d as Record<string, unknown>).file : undefined;
    if (typeof f === "string" && !out.includes(f)) out.push(f);
  }
  return out;
}

export interface TableDetailProps {
  table: SchemaTable;
  group: SchemaGroup;
  /** The loaded /table answer, or null while it loads. */
  detail: Detail | null;
  names: Names;
  changed: Set<string>;
  /** Show the set chip (more than one visible group). */
  multiSet: boolean;
  onTable: (id: string) => void;
  /** Opens the Migrations tab on a migration ("" for the set's first). */
  onMigration: (id: string) => void;
  visible: (id: string) => boolean;
  /** Overview panel: compact grid and a close button. */
  onClose?: () => void;
  /** The other table of an overlap, from /sets (T018f). */
  overlapTarget?: (o: Overlap) => OverlapTarget;
  /** A table Include tests hides (O3): links to it carry the visible
   * "test, hidden" mark (T018f Revision 2, C7). */
  hiddenTest?: (id: string) => boolean;
}

/** columnReasons reads a model column's stored reasons as popover lines,
 * reusing the table reason's link for the same file and line. */
function columnReasonsOf(col: GridColumn, tableLines: readonly WhyReason[]): (WhyReason & { fact: string | null })[] {
  const list = col.node.attributes.reasons;
  if (!Array.isArray(list)) return [];
  return list
    .filter((r): r is JSONObject => r !== null && typeof r === "object" && !Array.isArray(r))
    .map((r) => {
      const item = reasonItem(r);
      const where = item.where;
      const link = where === null || where.link !== null ? null : (tableLines.find((t) => t.where?.label === where.label)?.where?.link ?? null);
      return { ...item, where: where === null ? null : link === null ? where : { ...where, link }, fact: item.fact ?? null };
    });
}

/** TableDetailView renders the detail pane of one table. */
export function TableDetailView({ table, group, detail, names, changed, multiSet, onTable, onMigration, visible, onClose, overlapTarget, hiddenTest = () => false }: TableDetailProps) {
  const { catalogue } = useSchemaWhy();
  const project = useProject();
  const attrs = detail?.table.attributes ?? table.node.attributes;
  const completeness = detail?.completeness ?? table.completeness;
  const badge = completenessBadge(completeness, attrs);
  const name = tableName(detail?.table ?? table.node);
  const created = detail?.history.find((h) => h.event === "created");
  const previous = attrStringList(attrs, "previous_names");
  const uk = uniqueKeysCell(attrs, completeness);
  const model = tableSource(table) === "models";
  const source = rowSource(table, group.tool);
  const restsOn = detail !== null ? detail.rests_on : table.rests_on;
  const overlaps = detail?.overlaps ?? table.overlaps;
  const sites = detail?.opaque_sites ?? table.opaque_sites;
  const links = new Map<string, GridColumn["links"]>();
  for (const fk of group.foreign_keys) links.set(fk.source_column, [...(links.get(fk.source_column) ?? []), fk]);
  const columns: GridColumn[] =
    detail !== null
      ? detail.columns.map((c) => ({ node: c.node, default_state: c.default_state, links: c.foreign_keys, claims: c.claims }))
      : table.columns.map((c) => ({ node: c.node, default_state: c.default_state, links: links.get(c.node.identity) ?? [], claims: readClaims(c.node.attributes.claims) }));
  const indexNodes = detail !== null ? detail.indexes : table.indexes;
  const indexClaims = (id: string, a: Record<string, unknown>): Claim[] | null => (detail !== null ? (detail.index_claims[id] ?? null) : readClaims(a.claims));
  const reasons = tableReasons(attrs, detail);
  const dialect = typeof attrs.dialect === "string" ? attrs.dialect : null;
  // ADR 0065: a table whose dialect is chosen at run time.
  const dialects = stringsOrNull(attrs.dialects) ?? detail?.dialects ?? null;
  const why: GridWhy = { table: name, source, restsOn, reasons, columnReasons: (c) => columnReasonsOf(c, reasons), tool: group.tool, dialect, dialects };
  // T018g0: facts from the ORM's own mapping, not checked by its oracle.
  const mapped = model ? columns.flatMap((c) => mappedClaims(c.claims).map((m) => ({ claim: m, label: `${tableName(c.node)}.${factLabel(m)}` }))) : [];
  // T018g0b Revision 1: a per-dialect value with no claim is unverified too.
  const unclaimed = model ? columns.flatMap((c) => columnUnclaimed(c.node.attributes, c.default_state, c.claims, dialects, completeness).map((m) => `${tableName(c.node)}.${factLabel(m)}`)) : [];
  // Revision 2: anything but oracle (mapped, missing, unknown) is unverified.
  const tableMapped = model && attrs.verification !== ORACLE;
  // Until /table loads, /sets' counts stand in for the column claims.
  const countMapped = model && detail === null ? mappedCount(table) : 0;
  const pk = primaryKeyCell(attrs, completeness);
  const defined = model ? definedBy(group.tool, attrs, table.node.qualified_name) : null;
  const files = model ? definedFiles(columns) : [];
  const anyPy = model && columns.some((c) => c.default_state === "python");
  const badgeWhy = () => stateWhy(name, completeness, attrs, detail, sites, source, restsOn);
  return (
    <div className="grid content-start" data-testid="table-detail" data-table={name}>
      <div className="sticky top-0 z-10 grid content-start gap-1.5 overflow-y-auto border-b bg-card px-5 pt-4 pb-3 min-[1000px]:h-[124px]" data-testid="detail-header">
        <div className="flex flex-wrap items-center gap-2.5">
          <h2 className="m-0 font-mono text-xl font-semibold">{name}</h2>
          <NodeMarker id={table.node.identity} groups={VIEW_GROUPS.tables} />
          {badge.tone === "complete" ? (
            <CompletenessBadge c={badge} />
          ) : (
            <WhyButton why={badgeWhy} testId="state-why" label={`${badge.text}: why`}>
              <CompletenessBadge c={badge} />
            </WhyButton>
          )}
          <span className={cn("inline-flex h-5 items-center rounded-[4px] px-1.5 text-xs font-medium", model ? "bg-ipc-soft text-ipc" : "bg-http-soft text-http")} data-testid="source-chip">
            {sourceChipText(model, group.tool)}
          </span>
          {/* T020: the scope chip comes before the fact labels (unverified), never merged with them (O13). */}
          <GroupChip group={group} label={detail?.precision ?? group.precision} testId="table-precision" bare />
          {model && (mapped.length > 0 || unclaimed.length > 0 || tableMapped || countMapped > 0) && (
            <UnverifiedChip name={name} source={source} tool={group.tool} dialect={dialect} facts={[...(tableMapped ? ["table facts (name, schema, keys, constraints)"] : []), ...mapped.map((m) => m.label), ...unclaimed, ...(countMapped > 0 ? [countLabel(countMapped)] : [])]} claims={mapped.map((m) => m.claim)} own={tableMapped || countMapped > 0} note={(tableMapped && unrecorded(attrs.verification)) || (countMapped > 0 && countsUnrecorded(table)) || unclaimed.length > 0} testId="table-unverified-chip" />
          )}
          <ChangedMark show={changed.has(table.node.file)} />
          <span className="flex-1" />
          <IdeOpen ide={detail?.table.ide ?? null} status={detail?.ide ?? null} testId="table-ide" place={table.node.identity} />
          {onClose !== undefined && (
            <button type="button" className={OUTLINE_BUTTON} onClick={onClose} aria-label="Close the table panel" title="Close (Esc)" aria-keyshortcuts="Escape" data-testid="panel-close">
              <X aria-hidden="true" />
            </button>
          )}
        </div>
        <div className="flex flex-wrap gap-x-4 gap-y-1">
          {model ? (
            <Chip label={defined?.label ?? "class"}>
              <span className="font-mono">{defined?.name ?? <i className="text-muted-foreground">unknown</i>}</span>
              <span className="ml-1 font-mono text-muted-foreground">· {fileLine(table.node.file, table.node.span?.start_line)}</span>
            </Chip>
          ) : (
            <Chip label="schema">
              <CellText c={schemaCell(attrs)} />
            </Chip>
          )}
          {model && files.length > 1 && (
            <Chip label="columns from" title="The files of the class chain that define the columns (defined_in)">
              <span className="font-mono">{files.map((f) => f.split("/").pop()).join(" ← ")}</span>
            </Chip>
          )}
          {multiSet && <Chip label="set"><span className="font-mono">{setLabel(group)}</span></Chip>}
          {!model && (
            <Chip label="created in">
              {table.created_by === null ? (
                <span className="text-muted-foreground italic">creating migration not stored</span>
              ) : created !== undefined ? (
                <span className="font-mono">{fileLine(created.migration.file, table.node.span?.start_line)}</span>
              ) : (
                <span className="font-mono">{fileLine(table.node.file, table.node.span?.start_line)}</span>
              )}
            </Chip>
          )}
          <Chip label="primary key">
            {pk.unknown ? (
              <WhyButton why={() => cellWhy(why, { node: table.node, default_state: "", links: [], claims: null }, "", "primary_key", "The primary key")} testId="pk-why" className="text-muted-foreground italic underline decoration-dotted underline-offset-[3px]">
                <span title={pk.title}>unknown</span>
              </WhyButton>
            ) : (
              <CellText c={pk} className="font-mono" />
            )}
          </Chip>
          {uk.rule === "uk.list" && (
            <Chip label="unique">
              <CellText c={uk} className="font-mono" />
            </Chip>
          )}
          {uk.rule === "uk.unknown" && !model && (
            <Chip label="unique keys">
              <CellText c={{ ...uk, text: "unknown" }} />
            </Chip>
          )}
          {previous.kind === "list" && previous.items.length > 0 && (
            <Chip label="previous names">
              <span className="font-mono">{previous.items.join(", ")}</span>
            </Chip>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <RestsOn ids={restsOn} source={source} testId="table-rests-on" />
          {anyPy && (
            <span className="text-[12.5px] text-muted-foreground">
              Python-side defaults are marked <span className="rounded-[3px] bg-ipc-soft px-1 text-[10.5px] font-semibold text-ipc">PY</span>
            </span>
          )}
          {!model && catalogue.kind === "failed" && (
            <span className="text-[12.5px] text-muted-foreground italic" data-testid="row-chips-unavailable">
              Row chips need the assumption catalogue, which did not load.
            </span>
          )}
        </div>
      </div>
      <div className="grid gap-2 px-5 pt-3 pb-3">
        <Related detail={detail} names={names} onTable={onTable} visible={visible} hiddenTest={hiddenTest} completeness={completeness} identity={table.node.identity} set={group} fkUnknown={columns.filter((c) => foreignKeyCell(c.node.attributes, completeness, c.links.length).unknown).length} />
        {overlaps !== null && overlaps.length > 0 && overlapTarget !== undefined && <OverlapNote overlaps={overlaps} here={tableSource(table)} name={name} target={overlapTarget} onTable={onTable} />}
        <StateBanner completeness={completeness} attrs={attrs} detail={detail} onMigration={onMigration} why={why} model={model} />
      </div>
      <div className="border-t px-5 py-3.5">
        <ColumnGrid columns={columns} completeness={completeness} names={names} changed={changed} onTable={onTable} visible={visible} hiddenTest={hiddenTest} compact={onClose !== undefined} model={model} why={why} />
      </div>
      {model && (detail === null || detail.model_routes !== null) && (
        <Section title="Routes that accept or return this model" aside={detail?.model_routes?.class != null ? `same declaration: ${className(attrs) ?? "the class"} · from stored route shapes` : "from stored route shapes"} testId="model-routes-section">
          <ModelRoutesPanel value={detail === null ? undefined : detail.model_routes} ide={detail?.ide ?? null} tool={group.tool} repository={project !== null ? (detail?.repository ?? null) : null} />
        </Section>
      )}
      {onClose !== undefined && (
        <Section title="Foreign keys" aside="stored edges only">
          <ul className="m-0 grid gap-0.5 pl-0 text-[12.5px]">
            {columns.flatMap((c) => c.links.map((l) => (
              <li key={`${c.node.identity}|${l.target_column}`} className="list-none font-mono">
                {columnLabel(names, c.node.identity)} → {columnLabel(names, l.target_column)}
              </li>
            )))}
            {columns.every((c) => c.links.length === 0) && (
              <li className="list-none text-muted-foreground" data-testid="panel-fk-none">
                <CellText c={absenceCell(completeness, "No foreign key from this table.", unresolvedTexts(detail, columns))} />
              </li>
            )}
          </ul>
        </Section>
      )}
      {onClose === undefined && (
        <Section title="Relationships" aside={detail === null ? undefined : `one foreign-key hop · ${String(detail.neighbours.outgoing.length)} out · ${String(detail.neighbours.incoming.length)} in`}>
          <Relationships detail={detail} names={names} name={name} completeness={completeness} identity={table.node.identity} set={group} />
        </Section>
      )}
      {model && <EmulatedRelations value={attrs.emulated_relations} />}
      {model && <OrmRelations value={attrs.orm_relations} />}
      {model ? (
        <Section title="Indexes and constraints" aside={`as declared in ${declaredIn(group.tool)}`} testId="indexes-section">
          <ModelIndexes attrs={attrs} tool={group.tool} />
        </Section>
      ) : (
        <Section title="Indexes" aside={`${String(indexNodes.length)} from CREATE INDEX${completeness !== "complete" && completeness !== "complete_in_source" ? " · last known" : ""}`} testId="indexes-section">
          <Indexes nodes={indexNodes} completeness={completeness} chip={(n) => <RowChip claims={indexClaims(n.identity, n.attributes)} source={source} restsOn={restsOn} name={`index ${typeof n.attributes.name === "string" ? n.attributes.name : "unnamed"}`} />} />
        </Section>
      )}
      {!model && <RowSecurity attrs={attrs} completeness={completeness} />}
      {detail !== null && detail.unresolved_references.length > 0 && (
        <Section title="Unresolved references" aside="never drawn as links" testId="unresolved-section">
          <ul className="m-0 grid gap-0.5 pl-0 text-[13px]">
            {detail.unresolved_references.map((u, i) => {
              const r = u.reference as Record<string, unknown> | null;
              const ok = r !== null && typeof r === "object" && !Array.isArray(r);
              return (
                <li key={i} className="list-none text-unmatched">
                  <span className="font-mono">{columnLabel(names, u.column)}</span> → unresolved:{" "}
                  <span className="font-mono">{ok && typeof r.table === "string" ? r.table : rawText(ok ? r.table : r)}</span>{" "}
                  <span className="text-muted-foreground">({ok && typeof r.reason === "string" ? r.reason : `status ${rawText(ok ? r.status : r)}`})</span>
                </li>
              );
            })}
          </ul>
        </Section>
      )}
      {model ? (
        <Section title="History" aside="model table" testId="history-section">
          <p className="m-0 text-[13px] text-muted-foreground" data-testid="model-history">
            {detail === null ? <Skeleton className="h-4 w-1/2" /> : detail.history.length === 0 ? "No migration Graphvein reads touches a model table; it is defined by its model. This does not mean the project has no migrations." : `${String(detail.history.length)} history events`}
          </p>
        </Section>
      ) : (
        <Section title="History" aside="final schema; events from migrations" testId="history-section">
          <History detail={detail} table={table} onMigration={onMigration} />
        </Section>
      )}
    </div>
  );
}
