// The source cards of the set strip (T018f R-1, 2.1): one card per group
// (source label, group name, tool · dialect · package or, for models, tool
// and the common directory of its files, and counts); a dashed card for a
// source the index proves was read and that holds nothing ("No model table
// claimed. Not 'none in the project'"); a dashed card per tool found but
// not read ("Alembic migrations found, not read yet"). A source the index
// does not prove read gets no card. The selected card is highlighted;
// clicking a card selects its group.

import type { SchemaGroup, UnreadTool } from "@/api/schema";
import { cn } from "@/lib/utils";
import type { SourceCard } from "@/sections/schema/state";
import { groupCounts, notCountedText, setLabel } from "@/sections/schema/SetStrip";
import { tableListReason } from "@/sections/schema/cells";
import { groupKeyOf } from "@/sections/schema/params";
import { DialectNote, dialectHint, GroupChip } from "@/sections/schema/precision";
import { WhyButton } from "@/sections/schema/WhyPopover";

/** SourceLabel is the coloured source chip of a card. */
export function SourceLabel({ source }: { source: string | null }) {
  const models = source === "models";
  return (
    <span className={cn("inline-flex h-5 items-center rounded-[4px] px-1.5 text-xs font-medium", models ? "bg-ipc-soft text-ipc" : "bg-http-soft text-http")}>
      {models ? "Models" : source === "migrations" ? "Migrations" : (source ?? "Other producer")}
    </span>
  );
}

/** commonDir is the longest directory every file shares ("" for none). */
export function commonDir(files: readonly string[]): string {
  if (files.length === 0) return "";
  const parts = files.map((f) => f.split("/").slice(0, -1));
  const first = parts[0] ?? [];
  let n = first.length;
  for (const p of parts) {
    let i = 0;
    while (i < n && i < p.length && p[i] === first[i]) i++;
    n = i;
  }
  return n === 0 ? "" : `${first.slice(0, n).join("/")}/`;
}

/** cardLine2 is a group card's second line. */
function cardLine2(g: SchemaGroup): string {
  const v = (k: "tool" | "dialect" | "package") => (g.conflicts.includes(k) ? "conflicting values" : (g[k] ?? `${k} unknown`));
  if (g.source === "models") {
    const dir = g.files === null ? "files unknown" : commonDir(g.files.map((f) => f.file)) || "repository root";
    return `${v("tool")} · ${dir}`;
  }
  if (g.set === null) return "Tables from another producer";
  return `${v("tool")} · ${v("dialect")} · ${v("package")}`;
}

function plural(n: number, one: string, many: string): string {
  return `${String(n)} ${n === 1 ? one : many}`;
}

function GroupCard({ g, selected, onSelect }: { g: SchemaGroup; selected: boolean; onSelect: () => void }) {
  const c = groupCounts(g);
  const t = (testId: string) => (selected ? { "data-testid": testId } : {});
  // The chip is a button of its own, so it sits beside the card's select
  // button inside the card's frame, never inside it.
  return (
    <div
      className={cn(
        "grid h-full w-[236px] flex-none content-start rounded-lg border max-[1000px]:h-auto max-[1000px]:w-full has-[>button:first-child:hover]:bg-accent",
        selected ? "border-l-4 border-brand bg-brand-soft/60" : "bg-background",
      )}
    >
      <button
        type="button"
        aria-pressed={selected}
        onClick={onSelect}
        className="grid content-start gap-0.5 rounded-lg px-3 pt-2 pb-1 text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        data-testid="source-card"
        data-kind="group"
        data-source={g.source ?? ""}
        data-key={groupKeyOf(g)}
      >
        <span className="flex min-w-0 items-center gap-1.5">
          <SourceLabel source={g.source} />
          <span className="truncate font-mono text-[13px] font-semibold" {...t("set-name")}>
            {setLabel(g)}
          </span>
        </span>
        <span className="truncate text-xs text-muted-foreground" title={cardLine2(g)}>
          {cardLine2(g)}
        </span>
        <span className="truncate text-xs">
          {c.tables === null ? (
            <i className="text-muted-foreground" {...t("count-tables")}>
              {notCountedText(g)}
            </i>
          ) : (
            <>
              <b className="font-semibold tabular-nums" {...t("count-tables")}>
                {c.tables}
              </b>{" "}
              {c.tables === 1 ? "table" : "tables"}
            </>
          )}
          {" · "}
          {g.source === "models" ? (
            g.files === null ? (
              <i className="text-muted-foreground">model files unknown</i>
            ) : (
              plural(g.files.length, "model file", "model files")
            )
          ) : (
            <>
              <b className="font-semibold tabular-nums" {...t("count-migrations")}>
                {c.migrations}
              </b>{" "}
              {c.migrations === 1 ? "migration" : "migrations"}
            </>
          )}
        </span>
      </button>
      {g.precision !== null && (
        // A dialect note wraps below the chip when both do not fit; the row
        // then takes less padding, so the card keeps the strip's fixed height.
        <span className={cn("flex min-w-0 px-3 pb-2", dialectHint(g, g.precision) !== null && "flex-wrap items-center gap-x-1.5 gap-y-0 pb-1")}>
          <GroupChip group={g} label={g.precision} testId="card-precision" />
          <DialectNote group={g} label={g.precision} />
        </span>
      )}
    </div>
  );
}

const DASHED = "grid h-full w-[236px] max-[1000px]:h-auto max-[1000px]:w-full flex-none content-start gap-0.5 rounded-lg border border-dashed border-muted-foreground/60 px-3 py-2 text-left";

/** EmptyCard is the dashed card of a source that was read and holds no
 * group. The models card names model_list_reasons (T018f Revision 2, B1:
 * .py files not read or a scan gap), since those files may define model
 * tables. */
function EmptyCard({ source, reasons }: { source: "migrations" | "models"; reasons: readonly string[] }) {
  const models = source === "models";
  const reasonTexts = models ? reasons.map((r) => tableListReason(r, null).text) : [];
  const short = reasons.length === 0 || !models ? null : reasons.length === 1 && reasons[0] === "python_scan_gap" ? "Scan gaps may hide .py files." : "Some .py files were not read.";
  return (
    <div className={DASHED} data-testid="source-card" data-kind="empty" data-source={source}>
      <span>
        <SourceLabel source={source} />
      </span>
      <span className="text-xs">{models ? "No model table claimed." : "No migration set found."}</span>
      {short !== null && (
        <span className="text-xs text-dynamic" data-testid="empty-reason">
          {short}
        </span>
      )}
      <span className="text-xs text-muted-foreground">
        Not &quot;none in the project&quot; ·{" "}
        <WhyButton
          testId="source-why"
          className="text-brand-text underline underline-offset-[3px]"
          why={() => ({
            kind: "source",
            title: models ? "No model table claimed" : "No migration set found",
            source,
            lead: models ? (
              <>
                The model pass ran on this index and claimed no model table. It reads SQLModel classes with <span className="font-mono">table=True</span> in the indexed Python files; other
                ORMs, SQLAlchemy Core tables and classes it cannot read are not claimed, so the project may still define tables.
                {reasonTexts.map((text) => (
                  <span key={text} className="mt-1 block" data-testid="empty-reason-text">
                    {text}
                  </span>
                ))}
              </>
            ) : (
              <>
                The migration pass ran on this index and found no sqlx migration set: a <span className="font-mono">migrations/</span> directory in a Cargo package that depends on sqlx.
                Other tools and layouts are not looked for, so the project may still have migrations.
              </>
            ),
            entry: null,
            entryLabel: "What Graphvein assumes, in the Assumptions tab",
          })}
        >
          why
        </WhyButton>
      </span>
    </div>
  );
}

/** toolLabel is an unread tool's name for a reader ("Alembic", "Prisma"
 * for prisma-migrate, "Drizzle" for drizzle-kit); an unknown tool is shown
 * as sent. */
export function toolLabel(tool: string): string {
  return tool === "alembic" ? "Alembic" : tool === "prisma-migrate" ? "Prisma" : tool === "drizzle-kit" ? "Drizzle" : tool;
}

/** unreadTitle is an unread tool's card title: "<Tool> migrations found,
 * not read yet" for the tools the server's rules name. */
export function unreadTitle(tool: UnreadTool): string {
  if (tool.tool === "alembic" && tool.path === null) return "Alembic configuration found; script directory not located";
  if (tool.tool === "alembic" || tool.tool === "prisma-migrate" || tool.tool === "drizzle-kit") return `${toolLabel(tool.tool)} migrations found, not read yet`;
  return `${tool.tool} found, not read yet`;
}

function UnreadLead({ tool }: { tool: UnreadTool }) {
  if (tool.tool === "prisma-migrate") {
    return (
      <>
        The index holds a Prisma Migrate directory <span className="font-mono">{tool.path ?? "unknown"}</span>
        {tool.marker !== null && <> (with <span className="font-mono">{tool.marker}</span>)</>}
        {tool.files !== null && <> with {plural(tool.files, "migration file", "migration files")}</>} beside a Prisma schema. Graphvein reads the Prisma schema&apos;s models, not
        its migrations, so tables the migrations create or change are not compared with the models here. This names what was found and nothing else: other tools and layouts
        are not looked for.
      </>
    );
  }
  if (tool.tool === "drizzle-kit") {
    return (
      <>
        The index holds a drizzle-kit migrations folder <span className="font-mono">{tool.path ?? "unknown"}</span>
        {tool.marker !== null && <> (with <span className="font-mono">{tool.marker}</span>)</>}
        {tool.files !== null && <> with {plural(tool.files, "migration file", "migration files")}</>}, the folder a Drizzle config names as its <span className="font-mono">out</span>.
        Graphvein reads the Drizzle schema&apos;s tables, not its migrations, so tables the migrations create or change are not compared with the models here. This names what
        was found and nothing else: other tools and layouts are not looked for.
      </>
    );
  }
  const alembic = tool.tool === "alembic";
  return (
    <>
      The index holds {tool.path === null ? <>{alembic ? "an Alembic configuration" : `a ${tool.tool} configuration`} (<span className="font-mono">{tool.marker ?? "unknown"}</span>) whose script directory was not located</> : (
        <>
          {alembic ? "an Alembic script directory" : `a ${tool.tool} directory`} <span className="font-mono">{tool.path}</span>
          {tool.marker !== null && <> (configured by <span className="font-mono">{tool.marker}</span>)</>}
          {tool.files !== null && <> with {plural(tool.files, "revision file", "revision files")}</>}
        </>
      )}
      . Graphvein does not read {alembic ? "Alembic revisions" : `${tool.tool} files`} yet, so their tables are not in this view and nothing here is compared with them. This names
      what was found and nothing else: other tools and layouts are not looked for.
    </>
  );
}

function UnreadCard({ tool }: { tool: UnreadTool }) {
  const name = tool.path ?? tool.marker ?? "path unknown";
  const title = unreadTitle(tool);
  return (
    <div className={DASHED} data-testid="source-card" data-kind="unread" data-tool={tool.tool}>
      <span className="flex min-w-0 items-center gap-1.5">
        <SourceLabel source="migrations" />
        <span className="truncate font-mono text-[13px] font-semibold" title={name}>
          {name}
        </span>
      </span>
      <span className="text-xs font-semibold">{title}</span>
      <span className="text-xs text-muted-foreground">
        {tool.files === null ? <i>files unknown</i> : plural(tool.files, "file", "files")} ·{" "}
        <WhyButton
          testId="unread-why"
          className="text-brand-text underline underline-offset-[3px]"
          why={() => ({
            kind: "unread",
            title,
            source: "migrations",
            lead: <UnreadLead tool={tool} />,
            entry: null,
          })}
        >
          what this means
        </WhyButton>
      </span>
    </div>
  );
}

/** SourceCards renders the cards; selected is the selected group. */
export function SourceCards({ cards, selected, onSelect }: { cards: readonly SourceCard[]; selected: SchemaGroup; onSelect: (g: SchemaGroup) => void }) {
  return (
    <div className="flex min-w-0 gap-2 overflow-x-auto p-2 max-[1000px]:flex-col max-[1000px]:overflow-visible" data-testid="source-cards">
      {cards.map((c, i) =>
        c.kind === "group" ? (
          <GroupCard key={groupKeyOf(c.group)} g={c.group} selected={c.group === selected} onSelect={() => { onSelect(c.group); }} />
        ) : c.kind === "empty" ? (
          <EmptyCard key={`empty:${c.source}`} source={c.source} reasons={c.reasons} />
        ) : (
          <UnreadCard key={`unread:${String(i)}`} tool={c.tool} />
        ),
      )}
    </div>
  );
}
