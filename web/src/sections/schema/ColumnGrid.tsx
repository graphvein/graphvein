// The columns grid of a table (D5, D6, T018b2 section 5's cell rules):
// #, Column, Type (as written), Null, Default, Keys, References, Line.
// Every cell comes from cells.ts; foreign keys are drawn only from stored
// edges, and references with another status are red "unresolved" text,
// never links (rule 14).

import type { ReactNode } from "react";

import type { Claim, ColumnLink } from "@/api/schema";
import { rawText } from "@/api/schema";
import type { PresentedNode } from "@/api/endpoints";
import { useEditor } from "@/hooks/useEditor";
import { EDITOR_OFF, ideLabel } from "@/lib/ide";
import { cn } from "@/lib/utils";
import { autoCell, defaultCell, dialectFacts, foreignKeyCell, keyChip, modelTypeCell, nullCell, referenceEntries, toolTypeCell, typeCell, type Cell } from "@/sections/schema/cells";
import { columnDialects, columnUnclaimed, dialectDefault, dialectType, DialectValues } from "@/sections/schema/Dialects";
import { RowChip } from "@/sections/schema/RowChip";
import { factLabel, mappedClaims, UnverifiedChip } from "@/sections/schema/Verification";
import { WhyButton, type Why, type WhyReason } from "@/sections/schema/WhyPopover";
import { columnLabel, type Names } from "@/sections/schema/names";
import { CellText, ChangedMark, KeyChipView } from "@/sections/schema/parts";
import { ExternalLink } from "lucide-react";
import { EditorLink } from "@/components/EditorLink";
import { WrongFactFlag } from "@/components/WrongFactFlag";
import { useReport, type WrongFact } from "@/shell/report";
import { VIEW_GROUPS } from "@/sections/changes/markers";
import { NodeMarker } from "@/shell/ChangeMarker";

export { referenceEntries, type RefEntry } from "@/sections/schema/cells";

export interface GridColumn {
  /** A presented node (/table) or a stored one (/sets: ide null). */
  node: PresentedNode;
  default_state: string;
  /** Stored outgoing foreign_key edges. */
  links: Pick<ColumnLink, "target_column" | "target_table">[];
  /** The column's claims (T018f): /table's list with links, else the
   * stored attribute; null when not recorded. */
  claims: Claim[] | null;
}

/** GridWhy is what the grid's popovers need from the table (T018f 2.2). */
export interface GridWhy {
  table: string;
  /** The catalogue's row source: "migrations" or "models/<tool>"
   * (rowSource). */
  source: string;
  restsOn: string[] | null;
  /** The table's reasons as popover lines (the "why" of a null fact);
   * fact is set for model reasons (T018f 1.1). */
  reasons: (WhyReason & { fact?: string | null })[];
  /** A model column's reasons as popover lines, by column identity. */
  columnReasons: (col: GridColumn) => (WhyReason & { fact: string | null })[];
  /** The model group's tool and the table's dialect, for the
   * "unverified" popover of mapped facts (T018g0); absent: unknown. */
  tool?: string | null;
  dialect?: string | null;
  /** The table's run-time dialects (ADR 0065); absent or null: none. */
  dialects?: string[] | null;
}

/** cellWhy is the popover of an unknown cell: the column's reasons for
 * that fact (models), else the table's reasons, with the assumptions the
 * table's known facts rest on. */
export function cellWhy(w: GridWhy, col: GridColumn, column: string, fact: string, label: string): Why {
  const own = w.columnReasons(col);
  const forFact = own.filter((r) => r.fact === fact || r.fact === null);
  const tableForFact = w.reasons.filter((r) => r.fact === undefined || r.fact === null || r.fact === fact);
  const reasons: WhyReason[] = forFact.length > 0 ? forFact : own.length > 0 ? own : tableForFact.length > 0 ? tableForFact : w.reasons;
  const rows = [...new Set(reasons.map((r) => r.row).filter((r): r is string => r !== null))];
  return {
    kind: "cell",
    title: column === "" ? `${label} of ${w.table} is unknown` : `${label} of ${w.table}.${column} is unknown`,
    source: w.source,
    lead: reasons.length === 0 ? "The source does not prove it, and the index recorded no reason for this fact. It is not \"no\" and not a default." : "The source does not prove it; it is not \"no\" and not a default.",
    reasons,
    rows,
    assumptions: { heading: "Known facts of this table rest on", ids: w.restsOn },
    entry: rows[0] !== undefined ? `row:${w.source}:${rows[0]}` : null,
  };
}

/** Unknown is an unknown cell as a popover trigger (T018f 2.2). */
function Unknown({ c, why, className }: { c: Cell; why: () => Why; className?: string }) {
  return (
    <WhyButton why={why} className={cn("text-muted-foreground italic underline decoration-dotted underline-offset-[3px]", className)} label={`${c.text}: why`} testId="unknown-cell">
      <span title={c.title} data-rule={c.rule}>
        {c.text}
      </span>
    </WhyButton>
  );
}

function actions(ref: Record<string, unknown>): string {
  const parts: string[] = [];
  if (typeof ref.on_delete === "string") parts.push(`on delete ${ref.on_delete}`);
  if (typeof ref.on_update === "string") parts.push(`on update ${ref.on_update}`);
  return parts.join(" ");
}

/** References renders a column's links and its unresolved references. */
export function References({ col, names, onTable, visible, hiddenTest = () => false }: { col: GridColumn; names: Names; onTable: (id: string) => void; visible: (id: string) => boolean; hiddenTest?: (id: string) => boolean }) {
  const attrs = col.node.attributes;
  const entries = referenceEntries(attrs);
  const linkedRefs = entries.filter((e): e is { ok: true; ref: Record<string, unknown>; linked: boolean } => e.ok && e.linked);
  const items: ReactNode[] = col.links.map((l) => {
    const label = columnLabel(names, l.target_column);
    // The referential actions are read from the references text of the
    // linked entry whose table matches the target's name, when there is one.
    // Display-only (T018b2 decision 7): the match never decides a link,
    // which comes from the stored edge alone; it only picks which stored
    // on_delete/on_update text is printed beside that link.
    const target = names.column(l.target_column);
    // Two linked entries naming the same table text are ambiguous: no
    // actions are shown then, rather than one entry's.
    const refs = linkedRefs.filter((e) => e.ref.table === target?.table);
    const act = refs.length === 1 && refs[0] !== undefined ? actions(refs[0].ref) : "";
    const tableId = l.target_table;
    return (
      <span key={`l:${l.target_column}`} className="block" data-testid="fk-link">
        {tableId !== null && visible(tableId) ? (
          <button type="button" className="font-mono text-[12.5px] whitespace-nowrap text-http underline decoration-dotted underline-offset-[3px] hover:decoration-solid" onClick={() => { onTable(tableId); }} title={`Open ${label}`}>
            → {label}
          </button>
        ) : (
          <span className="font-mono text-[12.5px] whitespace-nowrap" title={tableId === null ? "The target column has no stored table" : hiddenTest(tableId) ? "The target table is test-scope; Include tests shows it" : "The target table is in a hidden set"}>
            → {label}
          </span>
        )}
        {tableId !== null && !visible(tableId) && hiddenTest(tableId) && <TestHiddenMark />}
        {act !== "" && <span className="text-muted-foreground"> {act}</span>}
      </span>
    );
  });
  entries.forEach((e, i) => {
    if (e.ok && e.linked) {
      if (col.links.length === 0) {
        items.push(
          <span key={`n${String(i)}`} className="block text-muted-foreground italic" title="The reference is marked linked but no foreign_key edge is stored (index inconsistency)" data-testid="linked-no-edge">
            no stored link: {typeof e.ref.table === "string" ? e.ref.table : rawText(e.ref.table)}
          </span>,
        );
      }
      return;
    }
    if (e.ok && e.ref.status === "external") {
      // T024c1 SB-M2: a platform table outside the set (Supabase's
      // auth.users, storage tables), assumed to exist as SB-V2 states.
      const assumes = Array.isArray(e.ref.assumes) ? (e.ref.assumes as unknown[]).map((a) => rawText(a)).join(", ") : "";
      items.push(
        <span key={`x${String(i)}`} className="block" title={`A platform table outside the migration set${assumes !== "" ? `; assumption ${assumes}` : ""}`} data-testid="external-ref">
          → <span className="font-mono">{typeof e.ref.table === "string" ? e.ref.table : rawText(e.ref.table)}</span>
          {Array.isArray(e.ref.columns) && <span className="font-mono">.{(e.ref.columns as unknown[]).map((c) => rawText(c)).join(", ")}</span>}{" "}
          <span className="text-muted-foreground">(platform{assumes !== "" ? `, ${assumes}` : ""})</span>
        </span>,
      );
      return;
    }
    const text = e.ok ? (typeof e.ref.table === "string" ? e.ref.table : rawText(e.ref.table)) : "unreadable reference";
    const reason = e.ok ? (typeof e.ref.reason === "string" ? e.ref.reason : `status ${rawText(e.ref.status)}`) : rawText(e.raw);
    items.push(
      <span key={`u${String(i)}`} className="block text-unmatched" title={reason} data-testid="unresolved-ref">
        unresolved: <span className="font-mono">{text}</span> <span className="text-muted-foreground">({reason})</span>
      </span>,
    );
  });
  return <>{items}</>;
}

/** ColumnUnverified is a model column's "unverified" chip: the facts whose
 * claims are not oracle-verified (T018g0), and the per-dialect values that
 * have no claim at all (T018g0b Revision 1), so the chip is never missing
 * while one of the column's lines is unverified; nothing otherwise. */
function ColumnUnverified({ c, name, why, completeness }: { c: GridColumn; name: string; why: GridWhy; completeness: string }) {
  const mapped = mappedClaims(c.claims);
  const missing = columnUnclaimed(c.node.attributes, c.default_state, c.claims, why.dialects ?? null, completeness);
  return <UnverifiedChip name={name} source={why.source} tool={why.tool ?? null} dialect={why.dialect ?? null} facts={[...mapped.map((m) => factLabel(m)), ...missing.map((m) => factLabel(m))]} claims={mapped} note={missing.length > 0} />;
}

/** TestHiddenMark is the visible mark of a table Include tests hides (O3,
 * T018f Revision 2 C7): the same mark the overlap note and Model files
 * show, so a hidden target is never read as absent. */
export function TestHiddenMark() {
  return (
    <span className="ml-1 rounded-[3px] bg-dynamic-soft px-1 font-sans text-[11px] font-semibold whitespace-nowrap text-dynamic" data-testid="test-hidden-mark">
      test, hidden
    </span>
  );
}

export interface ColumnGridProps {
  columns: GridColumn[];
  /** Model table (T018f): mapped type and annotation, model defaults with a
   * PY chip, IX keys, no position column. */
  model?: boolean;
  why: GridWhy;
  completeness: string;
  names: Names;
  changed: Set<string>;
  onTable: (id: string) => void;
  visible: (id: string) => boolean;
  /** A table Include tests hides (O3); its links carry the visible mark. */
  hiddenTest?: (id: string) => boolean;
  compact?: boolean;
}

/** columnFact is a column row's wrong-fact report (T040): the column's
 * cells as the grid shows them, with their cell rules. */
export function columnFact(why: GridWhy, name: string, node: PresentedNode, cells: { type: Cell; nul: Cell; def: Cell; pk: Cell; uq: Cell }): WrongFact {
  const { type, nul, def, pk, uq } = cells;
  const keys = [pk, uq].filter((k) => k.text !== "").map((k) => k.text);
  return {
    fact: `column ${why.table}.${name}: type ${type.text}, null ${nul.text}, default ${def.text}${keys.length > 0 ? `, keys ${keys.join(" ")}` : ""}`,
    where: [node.span !== null ? `${node.file}:${String(node.span.start_line)}` : node.file],
    rule: `${why.source}; cells ${[type.rule, nul.rule, def.rule].join(", ")}`,
    shown: null,
  };
}

/** ColumnGrid renders the grid with its heading and key lines. */
export function ColumnGrid({ columns, completeness, names, changed, onTable, visible, hiddenTest, compact = false, model = false, why }: ColumnGridProps) {
  const lastKnown = completeness !== "complete" && completeness !== "complete_in_source";
  // "No editor links" (ADR 0050): the line stays, without its link.
  const { off } = useEditor();
  // Rows are flag groups only while the switch is on (markup unchanged off).
  const flags = useReport().url !== null;
  const cells = columns.map((c) => {
    const a = c.node.attributes;
    // ADR 0065: per-dialect facts, read only on a model column that has them.
    const ds = model ? columnDialects(a, why.dialects ?? null) : [];
    const flatAuto = model && Object.hasOwn(a, "autoincrement") && !dialectFacts(a).has("autoincrement");
    return {
    c,
    dType: model ? dialectType(a, ds, completeness) : null,
    dDef: model ? dialectDefault(a, c.default_state, ds, completeness) : null,
    tool: model ? toolTypeCell(a, why.tool ?? null) : null,
    ai: flatAuto ? autoCell(a.autoincrement, completeness) : null,
    type: model ? modelTypeCell(c.node.attributes) : typeCell(c.node.attributes),
    nul: nullCell(c.node.attributes),
    def: defaultCell(c.default_state, c.node.attributes, completeness),
    pk: keyChip(c.node.attributes, "primary_key"),
    uq: keyChip(c.node.attributes, "unique"),
    ix: model ? keyChip(c.node.attributes, "index") : null,
    fk: foreignKeyCell(c.node.attributes, completeness, c.links.length),
    };
  });
  const anyPy = cells.some((x) => x.def.rule === "default.python" || (x.dDef?.lines.some((l) => l.parts.some((p) => p.rule === "default.python")) ?? false));
  const anyHalf = cells.some((x) => x.def.rule === "default.none_in_source");
  const th = "px-2 py-1.5 text-left text-xs font-medium text-muted-foreground";
  const td = "px-2 py-1.5 align-top";
  return (
    <section className="grid gap-2" data-testid="column-grid">
      <h3 className="m-0 flex items-baseline gap-2 text-[14px] font-semibold">
        Columns <span className="font-normal text-muted-foreground tabular-nums" data-testid="grid-count">{columns.length}</span>
        {lastKnown && <span className="text-xs font-normal text-dynamic">last known from source</span>}
      </h3>
      {columns.length === 0 ? (
        <p className="m-0 text-[13px] text-muted-foreground" data-testid="no-columns">
          {lastKnown
            ? "No columns known. The table's body was not read (LIKE, OF, PARTITION OF, INHERITS or an unread body); this is not \"no columns\"."
            : completeness === "complete_in_source"
              ? "No columns ◑ (as far as the migrations show)."
              : "No columns."}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className={cn("w-full border-collapse text-[13px]", compact && "text-[12.5px]")}>
            <thead>
              <tr className="border-b">
                {!model && <th className={th}>#</th>}
                <th className={th}>Column</th>
                <th className={th}>{model ? "Type / annotation" : "Type (as written)"}</th>
                <th className={th}>Null</th>
                <th className={th}>Default</th>
                <th className={th}>Keys</th>
                {!compact && <th className={th}>References</th>}
                {!compact && <th className={cn(th, "text-right")}>Line</th>}
              </tr>
            </thead>
            <tbody>
              {cells.map(({ c, type, dType, dDef, tool, ai, nul, def, pk, uq, ix, fk }) => {
                const name = typeof c.node.attributes.name === "string" ? c.node.attributes.name : c.node.name;
                const pos = c.node.attributes.position;
                const ide = off ? null : c.node.ide;
                return (
                  <tr key={c.node.identity} className={cn(flags && "group/fact", "border-b last:border-b-0")} data-testid="column-row" data-column={name}>
                    {!model && (
                      <td className={cn(td, "text-muted-foreground tabular-nums")} data-testid="cell-position">
                        {typeof pos === "number" && Number.isInteger(pos) ? (
                          pos
                        ) : (
                          <i title={`position: ${Object.hasOwn(c.node.attributes, "position") ? rawText(pos) : "missing"} (not known)`} aria-label="position unknown">
                            ?
                          </i>
                        )}
                      </td>
                    )}
                    <td className={cn(td, "font-mono")}>
                      {name}
                      <NodeMarker id={c.node.identity} groups={VIEW_GROUPS.tables} className="ml-1.5 align-[1px]" />
                      {!model && <RowChip claims={c.claims} source={why.source} restsOn={why.restsOn} name={`${why.table}.${name}`} />}
                      {model && <ColumnUnverified c={c} name={`${why.table}.${name}`} why={why} completeness={completeness} />}
                    </td>
                    <td className={td} data-testid="cell-type">
                      {dType !== null ? (
                        <DialectValues value={dType} attrs={c.node.attributes} claims={c.claims} tool={why.tool ?? null} testId="dialect-type" why={(p, ds) => cellWhy(why, c, name, p.rule.startsWith("auto.") ? "autoincrement" : "type", `${p.rule.startsWith("auto.") ? "Autoincrement" : "Type"} on ${ds.join(", ")}`)} />
                      ) : type.unknown ? (
                        <Unknown c={type} why={() => cellWhy(why, c, name, "type", "Type")} />
                      ) : (
                        <CellText c={type} className="font-mono text-http" />
                      )}
                      {/* The annotation is the source's own text: shown beside per-dialect types too (Revision 1, C3). */}
                      {type.second !== undefined && (
                        <span className="block font-mono text-xs text-muted-foreground">
                          {type.second}
                        </span>
                      )}
                      {tool !== null && tool.rule === "tool_type.written" && (
                        <span className="mt-0.5 block text-[11px] text-muted-foreground" title={tool.title} data-testid="tool-type" data-rule={tool.rule}>
                          <span className="font-sans">{tool.label}:</span> <span className="font-mono">{tool.value}</span>
                        </span>
                      )}
                    </td>
                    <td className={cn(td, "whitespace-nowrap")} data-testid="cell-null">
                      {nul.unknown ? <Unknown c={nul} why={() => cellWhy(why, c, name, "nullable", "Nullability")} /> : <CellText c={nul} />}
                    </td>
                    <td className={cn(td, "font-mono text-[12.5px]")} data-testid="cell-default">
                      {dDef !== null ? (
                        <DialectValues value={dDef} attrs={c.node.attributes} claims={c.claims} tool={why.tool ?? null} testId="dialect-default" mono={false} why={(_, ds) => cellWhy(why, c, name, "server_default", `Server default on ${ds.join(", ")}`)} />
                      ) : def.unknown ? (
                        <Unknown c={def} why={() => cellWhy(why, c, name, "default", "Default")} />
                      ) : (
                        <CellText c={def} />
                      )}
                      {def.rule === "default.python" && (
                        <span className="ml-1 rounded-[3px] bg-ipc-soft px-1 font-sans text-[10.5px] font-semibold text-ipc" title="Python-side default; the database has none" data-testid="py-chip">
                          PY
                        </span>
                      )}
                    </td>
                    <td className={td} data-testid="cell-keys">
                      <span className="flex flex-wrap gap-1">
                        {pk.unknown ? <Unknown c={pk} why={() => cellWhy(why, c, name, "primary_key", "Primary key membership")} className="not-italic" /> : <KeyChipView c={pk} />}
                        {uq.unknown ? <Unknown c={uq} why={() => cellWhy(why, c, name, "unique", "Uniqueness")} className="not-italic" /> : <KeyChipView c={uq} />}
                        {ix !== null && (ix.unknown ? <Unknown c={ix} why={() => cellWhy(why, c, name, "index", "Index")} className="not-italic" /> : <KeyChipView c={ix} />)}
                        {ai !== null && (ai.unknown ? <Unknown c={ai} why={() => cellWhy(why, c, name, "autoincrement", "Autoincrement")} className="not-italic" /> : <KeyChipView c={ai} />)}
                        {c.links.length > 0 && <span className="inline-flex h-[18px] items-center rounded-[4px] bg-http-soft px-1 font-mono text-[11px] font-semibold text-http">FK</span>}
                        {c.links.length === 0 && fk.rule === "fk.present" && referenceEntries(c.node.attributes).some((e) => e.ok && e.ref.status === "external") && (
                          <span className="inline-flex h-[18px] items-center rounded-[4px] border border-http px-1 font-mono text-[11px] font-semibold text-http" title="A foreign key to a platform table outside the migration set" data-testid="fk-external-chip">
                            FK
                          </span>
                        )}
                        {c.links.length === 0 && fk.rule === "fk.present" && !referenceEntries(c.node.attributes).some((e) => e.ok && e.ref.status === "external") && (
                          <span className="inline-flex h-[18px] items-center rounded-[4px] border border-dashed border-unmatched px-1 font-mono text-[11px] font-semibold text-unmatched" title="A foreign key whose target did not resolve to a stored column" data-testid="fk-unresolved-chip">
                            FK
                          </span>
                        )}
                        {fk.unknown && <Unknown c={{ ...fk, text: "FK?" }} why={() => cellWhy(why, c, name, "foreign_keys", "Foreign keys")} className="not-italic" />}
                      </span>
                    </td>
                    {!compact && (
                      <td className={cn(td, "text-[12.5px]")} data-testid="cell-refs">
                        <References col={c} names={names} onTable={onTable} visible={visible} {...(hiddenTest !== undefined ? { hiddenTest } : {})} />
                        {fk.unknown && <Unknown c={fk} why={() => cellWhy(why, c, name, "foreign_keys", "Foreign keys")} />}
                      </td>
                    )}
                    {!compact && (
                      <td className={cn(td, "text-right whitespace-nowrap text-muted-foreground")}>
                        <ChangedMark show={changed.has(c.node.file)} />{" "}
                        {ide !== null ? (
                          <EditorLink href={ide.link} place={c.node.identity} label={ideLabel(ide.link)} detail={ide.link} className="inline-flex items-center gap-0.5 hover:text-foreground">
                            :{c.node.span?.start_line ?? "?"}
                            <ExternalLink aria-hidden="true" className="size-3" />
                          </EditorLink>
                        ) : (
                          <span {...(off ? { title: EDITOR_OFF } : {})}>:{c.node.span?.start_line ?? "?"}</span>
                        )}
                        <WrongFactFlag className="ml-0.5 align-middle" fact={() => columnFact(why, name, c.node, { type, nul, def, pk, uq })} />
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {model && (
        <p className="m-0 text-xs text-muted-foreground" data-testid="model-order-key">
          {why.tool === "diesel" ? "Listed in the table! block's order; schema.rs may sort columns (column_sorting), so it is not the database's column position." : "Listed in source order (class chain); models declare no column position."}
          {anyPy && " PY: a Python-side default; the database has no default."}
        </p>
      )}
      {anyHalf && (
        <p className="m-0 text-xs text-muted-foreground" data-testid="half-key">
          none ◑ no default as far as the migrations show (opaque statements run after this table is created).
        </p>
      )}
      {lastKnown && (
        <p className="m-0 text-xs text-muted-foreground" data-testid="partial-key">
          More columns may exist. Defaults that are not written read unknown, never &quot;none&quot;.
        </p>
      )}
    </section>
  );
}
