// The schema section's pages without a home (T018b2 section 3, round 3
// variant Y, Revision 1): not indexed, schema not known (Q4), no migrations
// found (D1), no set could be read (Q6), all sets test-only (Q5), and the
// strips for sets and paths the index holds nothing for (Q6 "unreadable",
// covering failed sets, sets without records, unreadable manifests and
// scan gaps). None of them says "No migrations found" unless found is
// exactly "none".

import { CircleHelp, Database, Plus, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

import type { SchemaGroup, SchemaSets, SchemaSummary, UnreadPath, UnreadTool } from "@/api/schema";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { Banner, OUTLINE_BUTTON } from "@/sections/schema/parts";
import { joinNames, MIGRATION_TOOLS, refusalReason, toolLabel } from "@/sections/schema/tools";

function Card({ icon, title, sub, children, testId }: { icon: ReactNode; title: string; sub: ReactNode; children?: ReactNode; testId: string }) {
  return (
    <div className="mx-auto mt-6 grid w-full max-w-[760px] gap-4 rounded-xl border bg-card px-7 py-6" data-testid={testId}>
      <div className="flex items-start gap-3">
        <div className="grid size-10 flex-none place-items-center rounded-lg bg-muted text-muted-foreground">{icon}</div>
        <div className="min-w-0">
          <h2 className="m-0 text-lg font-semibold">{title}</h2>
          <p className="m-0 text-[13px] text-muted-foreground">{sub}</p>
        </div>
      </div>
      {children}
    </div>
  );
}

const MONO = "font-mono text-[12.5px]";

/** NotIndexed is the API section's not-indexed text. */
export function NotIndexed({ repository }: { repository: string }) {
  return (
    <div className="rounded-xl border bg-card px-6 py-10 text-center text-[13px] text-muted-foreground" data-testid="not-indexed">
      {repository} has no index yet. Run <span className="font-mono text-foreground">graphvein index</span> and reload.
    </div>
  );
}

/** UnknownPage is Q4: the schema is not known for this index. sets, when
 * loaded (scan_incomplete, manifest_unreadable), lists the paths. */
export function UnknownPage({
  summary,
  found,
  reason,
  current,
  sets,
}: {
  summary: SchemaSummary;
  found: string;
  reason: string | null;
  /** The server's extractor version (status), for the re-index action. */
  current: string | null;
  sets: SchemaSets | null;
}) {
  const v = summary.index?.indexer.version ?? "unknown";
  const name = summary.index?.indexer.name ?? "unknown";
  const repo = <b className="font-mono font-semibold">{summary.repository}</b>;
  const reindex = (
    <p className="m-0 text-[13px]">
      Re-index with this binary{current !== null && <> (<span className={MONO}>{current}</span>)</>} to see the schema: <span className={MONO}>graphvein index</span>, then
      reload.
    </p>
  );
  let why: ReactNode;
  let action: ReactNode = reindex;
  switch (found === "unknown" ? reason : "__found__") {
    case "version_ambiguous":
      why = (
        <>
          This index was built with <b className={MONO}>{v}</b>. Indexes with that version were built both before and after schema extraction existed, and the
          run records nothing that tells them apart, so Graphvein cannot say whether {repo} has migration sets.
        </>
      );
      break;
    case "predates_schema":
      why = (
        <>
          This index was built with <b className={MONO}>{v}</b>, before Graphvein extracted database schemas, so it holds no answer about {repo}&apos;s migration
          sets.
        </>
      );
      break;
    case "external_indexer":
      why = (
        <>
          This index was produced by <b className={MONO}>{name}</b> <span className={MONO}>{v}</span>, not by Graphvein. It stores no migration set, but another
          indexer may not extract schemas at all, so the absence says nothing.
        </>
      );
      action = (
        <p className="m-0 text-[13px]">
          Index with <span className={MONO}>graphvein index</span> to have Graphvein look for sqlx migration sets.
        </p>
      );
      break;
    case "version_unreadable":
      why = (
        <>
          The index&apos;s extractor version <b className={MONO}>{v}</b> is not one Graphvein can read, so it cannot tell whether schema extraction ran.
        </>
      );
      break;
    case "scan_incomplete":
      why = (
        <>
          The scanner did not read {summary.scan_gaps === null || summary.scan_gaps <= 0 ? "some paths" : `${String(summary.scan_gaps)} ${summary.scan_gaps === 1 ? "path" : "paths"}`}
          {sets !== null && sets.scan_gaps.length > 0 && <> ({scanGapKinds(sets.scan_gaps)}, listed below)</>}. A migration set behind a path it did not read would be missed, so
          Graphvein cannot say that {repo} has no migration set.
        </>
      );
      action = (
        <p className="m-0 text-[13px]">Fix what the scanner reported (permissions, symlinks, unreadable ignore files), then re-index.</p>
      );
      break;
    case "manifest_unreadable":
      why = (
        <>
          {summary.unreadable_manifests === null ? "A Cargo manifest" : `${String(summary.unreadable_manifests)} Cargo ${summary.unreadable_manifests === 1 ? "manifest" : "manifests"}`}{" "}
          could not be read; any of them may declare a migration set.
        </>
      );
      action = <p className="m-0 text-[13px]">Fix the manifest, then re-index.</p>;
      break;
    default:
      why = (
        <>
          The server reported <span className={MONO}>found {found}</span>
          {reason !== null && (
            <>
              , reason <span className={MONO}>{reason}</span>
            </>
          )}
          , which this UI does not know.
        </>
      );
  }
  return (
    <Card
      icon={<CircleHelp aria-hidden="true" className="size-5" />}
      title="Schema not known for this index"
      sub={<>This is not &quot;no migrations found&quot;: the index cannot answer the question.</>}
      testId="schema-unknown"
    >
      <Banner tone="info">{why}</Banner>
      {action}
      {sets !== null && <UnreadStrips sets={sets} />}
      <p className="m-0 text-[12.5px] text-muted-foreground">
        From GET <span className="font-mono">/api/v0/schema/summary</span>: found <span className="font-mono">{found}</span>, reason{" "}
        <span className="font-mono">{reason ?? "null"}</span>.
      </p>
    </Card>
  );
}

/** scanGapKinds names what the scanner did not read, by the codes /sets
 * sends (T070: what the API says was incomplete, never a manifest kind the
 * repository may not have): "2 symlinks not followed, 1 directory not
 * listed". An unknown code is counted under its own name. */
export function scanGapKinds(gaps: UnreadPath[]): string {
  const kinds: Record<string, [string, string]> = {
    symlink: ["symlink not followed", "symlinks not followed"],
    scan_read: ["directory not listed", "directories not listed"],
    invalid_path: ["path with an unsupported name", "paths with an unsupported name"],
    ignore_read: ["ignore file not read", "ignore files not read"],
    file_read: ["Cargo.toml not read", "Cargo.toml files not read"],
    file_binary: ["Cargo.toml not read", "Cargo.toml files not read"],
    file_not_utf8: ["Cargo.toml not read", "Cargo.toml files not read"],
    file_not_regular: ["Cargo.toml not read", "Cargo.toml files not read"],
  };
  const counts = new Map<string, { n: number; words: [string, string] }>();
  for (const g of gaps) {
    const words = kinds[g.code] ?? [g.code, g.code];
    const c = counts.get(words[1]) ?? { n: 0, words };
    c.n++;
    counts.set(words[1], c);
  }
  return [...counts.values()].map((c) => `${String(c.n)} ${c.n === 1 ? c.words[0] : c.words[1]}`).join(", ");
}

/** refusedModels names the model tools of the refusals and of a GORM v1
 * unread entry, each once, in the order they come. */
function refusedModels(summary: Partial<Pick<SchemaSummary, "unread_tools" | "refused_sources">>): string[] {
  const out: string[] = [];
  for (const r of summary.refused_sources ?? []) out.push(toolLabel(r.tool));
  for (const t of summary.unread_tools ?? []) if (t.tool === "gorm-v1") out.push(toolLabel(t.tool));
  return [...new Set(out)];
}

/** noneTitle is the found-none page's title (T018f): never "No migrations
 * found" while a migration tool is found but not read, and (T070) never
 * "No ... model tables found" while a model source is found and refused;
 * models are named only when the index proves their pass ran. */
export function noneTitle(summary: Pick<SchemaSummary, "unread_tools" | "sources_read"> & Partial<Pick<SchemaSummary, "refused_sources">>): string {
  const unread = summary.unread_tools ?? [];
  if ((summary.refused_sources ?? []).length > 0) {
    const models = refusedModels(summary);
    const migrations = [...new Set(unread.filter((t) => MIGRATION_TOOLS.has(t.tool)).map((t) => toolLabel(t.tool)))];
    const other = unread.some((t) => !MIGRATION_TOOLS.has(t.tool) && t.tool !== "gorm-v1");
    let what = `${joinNames(models)} models`;
    if (migrations.length > 0) what += migrations.join() === models.join() ? " and migrations" : ` and ${joinNames(migrations)} migrations`;
    if (other) what += " and other schema files";
    return `Graphvein found ${what} but did not read them`;
  }
  if (unread.some((t) => t.tool === "alembic")) return "Graphvein found Alembic migrations but does not read them yet";
  if (unread.some((t) => t.tool === "prisma-migrate")) return "Graphvein found Prisma migrations but does not read them yet";
  if (unread.some((t) => t.tool === "drizzle-kit")) return "Graphvein found Drizzle migrations but does not read them yet";
  if (unread.length > 0 && unread.every((t) => t.tool === "gorm-v1")) return "Graphvein found GORM v1 models but does not read them yet";
  if (unread.length > 0) return "Graphvein found migrations it does not read yet";
  return summary.sources_read?.includes("models") === true ? "No migrations or model tables found" : "No migrations found";
}

/** unreadWhat names a found-but-not-read tool on the found-none page by
 * what the server's rule found (T066: every tool unread_tools can name). */
function unreadWhat(t: UnreadTool): ReactNode {
  const at = (what: string) => (
    <>
      {what} <span className={MONO}>{t.path}</span>
    </>
  );
  if (t.path === null) {
    return (
      <>
        {t.tool === "alembic" ? "Alembic" : t.tool} configuration <span className={MONO}>{t.marker ?? "unknown"}</span>, script directory not located
      </>
    );
  }
  switch (t.tool) {
    case "alembic":
      return at("Alembic script directory");
    case "prisma-migrate":
      return at("Prisma migrations directory");
    case "drizzle-kit":
      return at("Drizzle migrations folder");
    case "sequelize-migrations":
      return at("Sequelize migrations folder");
    case "typeorm-migrations":
      return at("TypeORM migrations folder");
    case "diesel-migrations":
      return at("Diesel migrations directory");
    case "gorm-v1":
      return at("GORM v1 imports in");
    default:
      return at(t.tool);
  }
}

/** schemaPassesText is the found-none footer's first sentence: what this
 * index looked for, from the summary's sources_read (T066: never a
 * Cargo-only description on a repository of another stack). */
export function schemaPassesText(summary: Pick<SchemaSummary, "index" | "sources_read">): string {
  const of = summary.index !== null ? ` of ${formatShortTime(summary.index.finished_at)}` : "";
  const read = summary.sources_read;
  if (read === null) return `Which schema sources the index${of} looked for is not known.`;
  const migrations = read.includes("migrations");
  const models = read.includes("models");
  if (migrations && models) return `The index${of} looked for migration sets and model tables in every indexed file.`;
  if (migrations) return `The index${of} looked for migration sets in every indexed file; it was built before Graphvein read ORM models.`;
  if (models) return `The index${of} looked for model tables in every indexed file.`;
  return `The index${of} was built before Graphvein extracted database schemas.`;
}

/** NoneFound is D1's "No migrations found" page (found exactly none). */
export function NoneFound({ summary }: { summary: SchemaSummary }) {
  const unread = summary.unread_tools ?? [];
  const refused = summary.refused_sources ?? [];
  const models = summary.sources_read?.includes("models") === true;
  return (
    <Card
      icon={<Database aria-hidden="true" className="size-5" />}
      title={noneTitle(summary)}
      sub={
        <>
          Graphvein found no migration set{models && " or model table"} it can read in <b className="font-mono font-semibold text-foreground">{summary.repository}</b>. This does not
          mean the project has no database.
        </>
      }
      testId="schema-none"
    >
      {refused.length > 0 && (
        <ul className="m-0 grid gap-1.5 pl-5 text-[13px]" data-testid="none-refused">
          {refused.map((r, i) => (
            <li key={i} className="[overflow-wrap:anywhere]">
              <b className="font-semibold">{toolLabel(r.tool)} models</b> found in <span className={MONO}>{r.file}</span>, not read:{" "}
              <span className="text-muted-foreground">{refusalReason(r)}</span>
            </li>
          ))}
        </ul>
      )}
      {unread.length > 0 && (
        <ul className="m-0 grid gap-1 pl-5 text-[13px]" data-testid="none-unread-tools">
          {unread.map((t, i) => (
            <li key={i}>
              {unreadWhat(t)}
              {t.files !== null && <> ({t.files} {t.files === 1 ? "file" : "files"})</>}: not read yet.
            </li>
          ))}
        </ul>
      )}
      <div className="grid grid-cols-2 gap-3 text-[13px] max-[900px]:grid-cols-1">
        <div className="rounded-lg border p-3.5" data-testid="none-looks-for">
          <b className="font-semibold">What Graphvein looks for</b>
          <ul className="mt-1.5 mb-0 grid gap-1 pl-5">
            <li>
              SQL migration sets: sqlx <span className={MONO}>migrations/</span> in a Cargo package that depends on <span className={MONO}>sqlx</span>, and Supabase{" "}
              <span className={MONO}>supabase/migrations</span> beside <span className={MONO}>supabase/config.toml</span>
            </li>
            {models && (
              <>
                <li>
                  Python: SQLModel classes with <span className={MONO}>table=True</span>
                </li>
                <li>TypeScript: Prisma schemas, Drizzle schemas, Sequelize v6 models, TypeORM entities, Supabase generated types</li>
                <li>
                  Rust: Diesel <span className={MONO}>schema.rs</span>; Go: GORM v2 models
                </li>
              </>
            )}
          </ul>
        </div>
        <div className="rounded-lg border p-3.5" data-testid="none-not-read">
          <b className="font-semibold">Not read yet</b>
          <ul className="mt-1.5 mb-0 grid gap-1 pl-5">
            {!models && <li>ORM models: this index was built before Graphvein read them</li>}
            <li>Migrations of Alembic, Prisma Migrate, drizzle-kit, Sequelize, TypeORM and Diesel, and GORM v1 models (listed above when found)</li>
            {models && <li>SQLAlchemy declarative classes that are not SQLModel, Django models</li>}
            <li>
              Other tools: Flyway, golang-migrate, knex, Rails, plain <span className={MONO}>schema.sql</span>
            </li>
            <li>
              SQL inside code strings, and a sqlx <span className={MONO}>migrate!(&quot;&lt;dir&gt;&quot;)</span> path other than the default
            </li>
          </ul>
        </div>
      </div>
      <p className="m-0 text-[12.5px] text-muted-foreground" data-testid="none-footer">
        {schemaPassesText(summary)} Files that ignore rules or the default exclusions (<span className="font-mono">target</span>, <span className="font-mono">vendor</span>,{" "}
        <span className="font-mono">node_modules</span>, nested Git repositories) leave out are outside the index. Re-indexing after adding migrations updates this view.
      </p>
    </Card>
  );
}

function Strip({ title, path, children, tone, testId }: { title: string; path: string; children: ReactNode; tone: "warn" | "error"; testId: string }) {
  return (
    <div
      className={cn("grid grid-cols-[minmax(200px,280px)_1fr] gap-x-4 rounded-xl border border-l-4 bg-card px-4 py-2.5 text-[13px] max-[1000px]:grid-cols-1", tone === "error" ? "border-l-unmatched-bar" : "border-l-dynamic-bar")}
      data-testid={testId}
    >
      <div className="min-w-0 font-mono text-[12.5px] [overflow-wrap:anywhere]">{path}</div>
      <div className="grid min-w-0 gap-0.5">
        <span className="flex flex-wrap items-center gap-2">
          <span className="inline-flex items-center gap-1 rounded-md border border-dynamic/50 bg-dynamic-soft px-1.5 text-xs font-semibold text-dynamic">
            <TriangleAlert aria-hidden="true" className="size-3" />
            {title}
          </span>
        </span>
        <div className="text-muted-foreground [overflow-wrap:anywhere]">{children}</div>
      </div>
    </div>
  );
}

function countsText(c: Record<string, number>): string {
  const e = Object.entries(c);
  return e.length === 0 ? "no diagnostics" : e.map(([k, n]) => `${k} ${String(n)}`).join(", ");
}

/** UnreadStrips lists every set and path the index holds nothing for: one
 * strip per failed set, set without records and unreadable manifest, and
 * one strip for the scan gaps (Q6 variant Y with Revision 1). */
export function UnreadStrips({ sets }: { sets: SchemaSets }) {
  const gaps = sets.scan_gaps;
  if (sets.failed_sets.length + sets.sets_without_records.length + sets.unreadable_manifests.length + gaps.length === 0) return null;
  return (
    <div className="grid gap-2" data-testid="unread-strips">
      {sets.failed_sets.map((f, i) => (
        <Strip key={`f${String(i)}`} title="Could not be read" path={f.package} tone="error" testId="failed-set">
          No tables or migrations stored for this package; this is not an empty set. <span className="text-foreground">{f.message}</span>
        </Strip>
      ))}
      {sets.sets_without_records.map((r, i) => (
        <Strip key={`r${String(i)}`} title="Nothing stored" path={r.package} tone="warn" testId="recordless-set">
          The schema pass found a migration set here but stored no table or migration for it (for example every member unreadable); this is not an empty set, and
          whether it is test-only is unknown. Diagnostics: <span className="font-mono text-foreground">{countsText(r.diagnostic_counts)}</span>
        </Strip>
      ))}
      {sets.unreadable_manifests.map((m, i) => (
        <Strip key={`m${String(i)}`} title="Manifest not read" path={m.file} tone="warn" testId="unreadable-manifest">
          Whether it declares a migration set is unknown. <span className="font-mono">{m.code}</span>: <span className="text-foreground">{m.message}</span>
        </Strip>
      ))}
      {gaps.length > 0 && (
        <Strip title="Not scanned" path={`${String(gaps.length)} ${gaps.length === 1 ? "path" : "paths"}`} tone="warn" testId="scan-gaps">
          The scanner did not read these paths; a migration set behind them would be missed.
          <ul className="m-0 mt-1 grid max-h-40 gap-0.5 overflow-auto pl-5">
            {gaps.map((g, i) => (
              <li key={i}>
                <span className="font-mono text-foreground">{g.file === "" ? "(path not recorded)" : g.file}</span> <span className="font-mono">{g.code}</span>: {g.message}
              </li>
            ))}
          </ul>
        </Strip>
      )}
    </div>
  );
}

/** FailedOnly is Q6's page: the index holds no group, and a set it found
 * could not be read or stored nothing. */
export function FailedOnly({ sets }: { sets: SchemaSets }) {
  const n = sets.failed_sets.length + sets.sets_without_records.length;
  return (
    <Card
      icon={<TriangleAlert aria-hidden="true" className="size-5" />}
      title="No migration set could be read"
      sub={
        <>
          Graphvein found {n === 1 ? "1 migration set" : `${String(n)} migration sets`} but holds no table or migration for {n === 1 ? "it" : "them"}. This is not &quot;no
          migrations found&quot;.
        </>
      }
      testId="schema-failed-only"
    >
      <UnreadStrips sets={sets} />
      <p className="m-0 text-[12.5px] text-muted-foreground">
        Messages are the stored <span className="font-mono">schema_*</span> diagnostics. Re-indexing does not help unless the cause changes.
      </p>
    </Card>
  );
}

/** TestsOnly is Q5's page: every set is test-only and Include tests is
 * off. The button does what the checkbox does. */
export function TestsOnly({ hidden, sets, onInclude }: { hidden: SchemaGroup[]; sets: SchemaSets; onInclude: () => void }) {
  const n = hidden.length;
  // Sets the index found but holds nothing for (failed, nothing stored) and
  // manifests it could not read have an unknown scope: the page then speaks
  // of the stored sets only and counts the others, never "all sets"
  // (review fix).
  const m = sets.failed_sets.length + sets.sets_without_records.length + sets.unreadable_manifests.length;
  const stored = n === 1 ? "The only stored set is test-only" : `All ${String(n)} stored sets are test-only`;
  const title = m === 0 ? (n === 1 ? "The only migration set is test-only" : `All ${String(n)} migration sets are test-only`) : `${stored}, and ${String(m)} whose scope is unknown`;
  return (
    <Card
      icon={<Database aria-hidden="true" className="size-5" />}
      title={title}
      sub={
        <>
          They are hidden because Include tests is off. Graphvein found them; this is not &quot;no migrations found&quot;.
          {m > 0 && <> Nothing is stored for the {m === 1 ? "one" : String(m)} whose scope is unknown (listed below), so whether {m === 1 ? "it is" : "they are"} test-only is not known.</>}
        </>
      }
      testId="schema-tests-only"
    >
      <div className="flex items-center gap-3">
        <button type="button" className={OUTLINE_BUTTON} onClick={onInclude} data-testid="include-tests-button">
          <Plus aria-hidden="true" />
          Include tests
        </button>
        <span className="text-[13px] text-muted-foreground">Same as the Include tests checkbox above.</span>
      </div>
      <div className="rounded-lg border p-3.5 text-[13px]">
        <b className="font-semibold">Hidden sets</b> <span className="text-muted-foreground">({n}, scope test)</span>
        <ul className="mt-1.5 mb-0 grid max-h-[360px] gap-0.5 overflow-auto pl-5">
          {hidden.map((g) => (
            <li key={g.set ?? "~"} className="font-mono text-[12.5px]">
              {g.set ?? "No migration set"}/ <span className="rounded-[3px] bg-dynamic-soft px-1 font-sans text-[11px] font-semibold text-dynamic">test</span>
            </li>
          ))}
        </ul>
      </div>
      <UnreadStrips sets={sets} />
    </Card>
  );
}

/** Changed is the page for a summary that said sets while /sets (asked
 * again once) holds none: the index changed between the reads. */
export function Changed({ sets }: { sets: SchemaSets }) {
  return (
    <Card
      icon={<CircleHelp aria-hidden="true" className="size-5" />}
      title="No migration set in the loaded data"
      sub={<>Data changed while loading: the summary named migration sets, but the set list read afterwards holds none. This is not &quot;no migrations found&quot;.</>}
      testId="schema-changed"
    >
      <UnreadStrips sets={sets} />
      <p className="m-0 text-[13px]">Reload when indexing has finished.</p>
    </Card>
  );
}
