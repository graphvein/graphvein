// The Migrations tab (D4, T018b2 section 6): the visible groups'
// migrations in API order, and the selected migration's detail from
// /api/v0/schema/migration with Previous/Next (buttons only), statement
// counts (null is "not analysed", never 0), creates and alters, and every
// stored diagnostic verbatim (the view does not parse spans).

import { ChevronLeft, ChevronRight } from "lucide-react";
import type { KeyboardEvent, ReactNode } from "react";
import { useRef } from "react";

import type { MigrationDetail, SchemaGroup, SchemaMigration } from "@/api/schema";
import { attrInt, attrString, attrStringList, rawText } from "@/api/schema";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { appliedCell, completenessBadge, countCell, orderCell } from "@/sections/schema/cells";
import { CellText, ChangedMark, CompletenessBadge, OUTLINE_BUTTON, QuietGlyph } from "@/sections/schema/parts";
import { groupKeyOf } from "@/sections/schema/params";
import { setLabel } from "@/sections/schema/SetStrip";
import type { ObjectsState } from "@/sections/schema/state";
import { IdeOpen } from "@/sections/schema/TableDetail";
import { VIEW_GROUPS } from "@/sections/changes/markers";
import { NodeMarker } from "@/shell/ChangeMarker";

function Row({ m, i, selected, changed }: { m: SchemaMigration; i: number; selected: boolean; changed: boolean }) {
  const a = m.attributes;
  const opaque = attrInt(a, "opaque");
  const unsupported = attrInt(a, "unsupported");
  const desc = attrString(a, "description");
  const version = attrString(a, "version");
  return (
    <span className="flex min-w-0 flex-1 items-center gap-2">
      <span className="grid min-w-0 flex-1">
        <span className={cn("truncate font-mono text-[13px]", selected && "font-semibold text-brand-text")}>
          {i + 1}. {m.file.split("/").pop()}
        </span>
        <span className="flex items-center gap-1.5 truncate text-xs text-muted-foreground">
          version {version ?? "unknown"}
          {desc !== null && desc !== "" && ` · ${desc}`}
          <ChangedMark show={changed} />
        </span>
      </span>
      <NodeMarker id={m.id} groups={VIEW_GROUPS.migrations} as="decoration" />
      {(opaque === null || unsupported === null) && (
        <span
          className="flex-none rounded-md border border-dashed border-muted-foreground/70 px-1.5 text-xs text-muted-foreground italic"
          title={`opaque: ${rawText(a.opaque)}, unsupported: ${rawText(a.unsupported)} (not analysed)`}
          data-testid="counts-unknown"
        >
          counts unknown
        </span>
      )}
      {opaque !== null && opaque > 0 && <span className="flex-none rounded-md border px-1.5 text-xs">{opaque} opaque</span>}
      {unsupported !== null && unsupported > 0 && <span className="flex-none rounded-md border border-dynamic/50 bg-dynamic-soft px-1.5 text-xs text-dynamic">{unsupported} unsupported</span>}
      {m.creates.length > 0 && (
        <span className="flex-none rounded-md bg-muted px-1.5 text-xs">
          +{m.creates.length} {m.creates.length === 1 ? "table" : "tables"}
        </span>
      )}
    </span>
  );
}

export interface MigrationListProps {
  groups: SchemaGroup[];
  selected: string | null;
  selectedGroup: SchemaGroup | null;
  onSelect: (id: string, g: SchemaGroup) => void;
  onSelectGroup: (g: SchemaGroup) => void;
  changed: Set<string>;
}

/** MigrationList renders the list card of the tab. */
export function MigrationList({ groups, selected, selectedGroup, onSelect, onSelectGroup, changed }: MigrationListProps) {
  const box = useRef<HTMLDivElement>(null);
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    const buttons = [...(box.current?.querySelectorAll<HTMLButtonElement>("button[data-migration-row]") ?? [])];
    const i = buttons.findIndex((b) => b === document.activeElement);
    const next = buttons[i === -1 ? 0 : event.key === "ArrowDown" ? Math.min(buttons.length - 1, i + 1) : Math.max(0, i - 1)];
    if (next === undefined) return;
    event.preventDefault();
    next.focus();
    next.click();
  };
  const tools = [...new Set(groups.map((g) => g.tool))];
  const known = tools.filter((t): t is string => t !== null);
  const unknownTools = groups.filter((g) => g.tool === null).length;
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card">
      <div ref={box} className="min-h-0 flex-1 overflow-auto" onKeyDown={onKeyDown} data-testid="migration-list">
        {groups.map((g) => (
          <div key={groupKeyOf(g)} role="group" aria-label={setLabel(g)}>
            {groups.length > 1 && (
              <button
                type="button"
                className={cn("flex h-[34px] w-full items-center gap-2 border-y border-l-4 bg-muted/60 px-3 text-left text-[12.5px]", selectedGroup === g ? "border-l-brand" : "border-l-transparent")}
                onClick={() => { onSelectGroup(g); }}
                data-testid="group-header"
              >
                <span className="truncate font-mono font-semibold">{setLabel(g)}</span>
                {g.test_only === true && <span className="rounded-[3px] bg-dynamic-soft px-1 text-[11px] font-semibold text-dynamic">test</span>}
                <span className="flex-1" />
                <span className="text-muted-foreground tabular-nums">
                  {g.migrations.length} {g.migrations.length === 1 ? "migration" : "migrations"}
                </span>
              </button>
            )}
            {g.migrations.length === 0 && <div className="border-b px-4 py-2 text-xs text-muted-foreground">No migration stored for this group.</div>}
            {g.migrations.map((m, i) => (
              <button
                key={m.id}
                type="button"
                data-migration-row={m.id}
                aria-current={selected === m.id ? "true" : undefined}
                className={cn(
                  "flex w-full items-center border-b border-l-[3px] px-3 py-1.5 text-left hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none focus-visible:ring-inset",
                  selected === m.id ? "border-l-brand bg-brand-soft" : "border-l-transparent",
                )}
                onClick={() => { onSelect(m.id, g); }}
                data-testid="migration-row"
              >
                <Row m={m} i={i} selected={selected === m.id} changed={changed.has(m.file)} />
              </button>
            ))}
          </div>
        ))}
      </div>
      <div className="flex h-[30px] flex-none items-center border-t px-3 text-xs text-muted-foreground" data-testid="migration-footer">
        {tools.length === 1 && tools[0] === "sqlx" ? "Applied in sqlx order (version ascending)." : footerText(known, unknownTools)}
      </div>
    </div>
  );
}

/** footerText names the order of each known tool and counts the groups
 * whose tool is not known (the set: null group among them), never listing
 * "unknown" as if it were a tool (Review fixes R3). */
export function footerText(known: readonly string[], unknownGroups: number): string {
  const parts = [...known];
  if (unknownGroups > 0) parts.push(`unknown tool for ${String(unknownGroups)} ${unknownGroups === 1 ? "group" : "groups"}`);
  return `Order by tool: ${parts.join(", ")}.`;
}

const STATS: [string, string][] = [
  ["statements", "Statements"],
  ["modeled", "Modeled"],
  ["no_effect", "No effect"],
  ["unsupported", "Unsupported"],
  ["opaque", "Opaque"],
  ["errors", "Errors"],
];

function Sub({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  return (
    <section className="grid gap-2 border-t px-5 py-3.5">
      <h3 className="m-0 flex items-baseline gap-2 text-[14px] font-semibold">
        {title}
        {aside !== undefined && <span className="text-xs font-normal text-muted-foreground">{aside}</span>}
      </h3>
      {children}
    </section>
  );
}

/** effectsText is what the Creates or Alters section says when its list is
 * empty. Only an analysed migration of an extracted group states an
 * absence, and "No table" only when no opaque, unsupported or failed
 * statement could have created one (Review fixes R1). null: the list is
 * not empty. */
export function effectsText(kind: "creates" | "alters", n: number, objects: ObjectsState, attrs: Record<string, unknown>): { text: string; unknown: boolean } | null {
  if (n > 0) return null;
  if (objects === "unknown") return { text: "unknown (whether the set's tables were extracted is not known)", unknown: true };
  const counts = ["statements", "opaque", "unsupported", "errors"].map((k) => attrInt(attrs, k));
  if (objects === "not_extracted" || counts.some((c) => c === null)) return { text: "not extracted (the statements were not analysed)", unknown: true };
  if (kind === "alters") return { text: "No modeled ALTER, RENAME or DROP.", unknown: false };
  const unmodeled = (counts[1] ?? 0) + (counts[2] ?? 0) + (counts[3] ?? 0);
  if (unmodeled === 0) return { text: "No table.", unknown: false };
  return { text: `No modeled CREATE TABLE; ${String(unmodeled)} opaque, unsupported or failed ${unmodeled === 1 ? "statement" : "statements"} may create tables.`, unknown: true };
}

function Effects({ e, testId }: { e: { text: string; unknown: boolean } | null; testId: string }) {
  if (e === null) return null;
  return (
    <span className={cn("text-[13px] text-muted-foreground", e.unknown && "italic")} data-testid={testId}>
      {e.text}
    </span>
  );
}

export interface MigrationDetailProps {
  migration: SchemaMigration;
  /** Whether the migration's group had its tables extracted. */
  objects: ObjectsState;
  detail: MigrationDetail | null;
  onMigration: (id: string) => void;
  onTable: (id: string) => void;
  visible: (id: string) => boolean;
  changed: Set<string>;
}

function directionText(d: unknown): string {
  if (d === "simple") return "simple";
  if (d === "up") return "up";
  if (d === "down") return "down (revert)";
  return `direction ${rawText(d)}`;
}

/** MigrationDetailView renders the selected migration. Facts shown before
 * /migration answers come from /sets (same index run). */
export function MigrationDetailView({ migration, objects, detail, onMigration, onTable, visible, changed }: MigrationDetailProps) {
  const a = detail?.migration.attributes ?? migration.attributes;
  // A count beside Creates/Alters only when the empty list is a stated
  // absence, never "0" beside "not extracted".
  const countAside = (kind: "creates" | "alters", n: number) => (detail === null || effectsText(kind, n, objects, a)?.unknown === true ? undefined : String(n));
  const version = attrString(a, "version");
  const vtext = attrString(a, "version_text");
  const desc = attrString(a, "description");
  const tool = attrString(a, "tool");
  const ext = attrStringList(a, "extensions");
  const name = migration.file.split("/").pop() ?? migration.file;
  const tableChip = (t: { id: string; name: unknown; completeness: string }) => {
    const c = completenessBadge(t.completeness, {});
    const label = typeof t.name === "string" ? t.name : t.id;
    const mark = c.tone === "complete" || c.tone === "in_source" ? <QuietGlyph c={c} /> : <CompletenessBadge c={c} />;
    return visible(t.id) ? (
      <button key={t.id} type="button" className="inline-flex h-7 items-center gap-1.5 rounded-md border bg-background px-2 font-mono text-[12.5px] hover:bg-accent" onClick={() => { onTable(t.id); }} data-testid="creates-chip">
        {label} {mark}
      </button>
    ) : (
      <span key={t.id} className="inline-flex h-7 items-center gap-1.5 rounded-md border px-2 font-mono text-[12.5px]" title="In a hidden set" data-testid="creates-chip">
        {label} {mark}
      </span>
    );
  };
  return (
    <div className="grid content-start" data-testid="migration-detail" data-migration={name}>
      <div className="grid gap-2 px-5 pt-4 pb-3">
        <div className="flex flex-wrap items-center gap-2.5">
          <h2 className="m-0 font-mono text-xl font-semibold">{name}</h2>
          <span className="rounded-md border px-1.5 text-xs" data-testid="migration-badge">
            {directionText(a.direction)} · <CellText c={appliedCell(a)} /> · <CellText c={orderCell(a)} />
          </span>
          <ChangedMark show={changed.has(migration.file)} />
          <NodeMarker id={migration.id} groups={VIEW_GROUPS.migrations} />
          <span className="flex-1" />
          <button type="button" className={OUTLINE_BUTTON} disabled={detail?.previous === null || detail === null} onClick={() => { if (detail?.previous) onMigration(detail.previous); }} data-testid="mig-previous">
            <ChevronLeft aria-hidden="true" />
            Previous
          </button>
          <button type="button" className={OUTLINE_BUTTON} disabled={detail?.next === null || detail === null} onClick={() => { if (detail?.next) onMigration(detail.next); }} data-testid="mig-next">
            Next
            <ChevronRight aria-hidden="true" />
          </button>
          <IdeOpen ide={detail?.migration.ide ?? null} status={detail?.ide ?? null} testId="migration-ide" place={migration.id} />
        </div>
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-[12.5px] text-muted-foreground">
          <span>
            version <span className="font-mono text-foreground">{version ?? <i>unknown</i>}</span>
          </span>
          {vtext !== null && vtext !== version && (
            <span>
              written as <span className="font-mono text-foreground">{vtext}</span>
            </span>
          )}
          {desc !== null && (
            <span>
              description <span className="text-foreground">{desc}</span>
            </span>
          )}
          <span>
            tool <span className="text-foreground">{tool ?? <i>unknown</i>}</span>
          </span>
          {ext.kind === "list" && ext.items.length > 0 && (
            <span>
              extensions <span className="font-mono text-foreground">{ext.items.join(", ")}</span>
            </span>
          )}
        </div>
      </div>
      <Sub title="Statements">
        <div className="grid grid-cols-6 overflow-hidden rounded-lg border max-[1100px]:grid-cols-3" data-testid="statement-counts">
          {STATS.map(([k, label]) => {
            const c = countCell(a, k);
            return (
              <div key={k} className="grid gap-0.5 border-r px-3 py-2 last:border-r-0" data-stat={k}>
                <CellText c={c} className={cn(!c.unknown && "text-lg font-semibold tabular-nums", c.unknown && "text-[13px]")} />
                <span className="text-xs text-muted-foreground">{label}</span>
              </div>
            );
          })}
        </div>
      </Sub>
      <Sub title="Creates" aside={countAside("creates", detail?.creates.length ?? 0)}>
        {detail === null ? (
          <Skeleton className="h-7 w-1/2" />
        ) : detail.creates.length === 0 ? (
          <Effects e={effectsText("creates", 0, objects, a)} testId="creates-none" />
        ) : (
          <div className="flex flex-wrap gap-1.5">{detail.creates.map(tableChip)}</div>
        )}
      </Sub>
      <Sub title="Alters" aside={countAside("alters", detail?.alters.length ?? 0)}>
        {detail === null ? (
          <Skeleton className="h-7 w-1/2" />
        ) : detail.alters.length === 0 ? (
          <Effects e={effectsText("alters", 0, objects, a)} testId="alters-none" />
        ) : (
          <div className="flex flex-wrap gap-1.5">{detail.alters.map(tableChip)}</div>
        )}
      </Sub>
      <Sub
        title="Diagnostics"
        aside={detail === null ? undefined : detail.diagnostics_total > detail.diagnostics.length ? `showing ${String(detail.diagnostics.length)} of ${String(detail.diagnostics_total)}` : String(detail.diagnostics_total)}
      >
        {detail === null ? (
          <Skeleton className="h-16" />
        ) : detail.diagnostics.length === 0 ? (
          detail.diagnostics_total > 0 ? (
            <span className="text-[13px] text-muted-foreground italic" data-testid="diagnostics-not-listed">
              {detail.diagnostics_total} {detail.diagnostics_total === 1 ? "diagnostic" : "diagnostics"} stored, not listed in this answer.
            </span>
          ) : (
            <span className="text-[13px] text-muted-foreground">No stored diagnostic for this file.</span>
          )
        ) : (
          <table className="w-full border-collapse text-[13px]" data-testid="diagnostics">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="px-2 py-1 font-medium">Severity</th>
                <th className="px-2 py-1 font-medium">Code</th>
                <th className="px-2 py-1 font-medium">Stored message</th>
              </tr>
            </thead>
            <tbody>
              {detail.diagnostics.map((d, i) => (
                <tr key={i} className="border-b align-top last:border-b-0">
                  <td className="px-2 py-1">
                    <span className="rounded-md bg-muted px-1.5 text-xs">{d.severity}</span>
                  </td>
                  <td className="px-2 py-1 font-mono text-[12.5px]">{d.code}</td>
                  <td className="px-2 py-1 [overflow-wrap:anywhere]">{d.message}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <p className="m-0 text-xs text-muted-foreground">Diagnostics are shown as stored; the view does not parse byte spans into lines.</p>
      </Sub>
    </div>
  );
}
