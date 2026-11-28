// "What Graphvein couldn't read" (T040, variant A): five summary cards,
// then one section per kind, each item linked to code. Every part comes
// from GET /api/v0/unread (claim contract couldnt-read). Precision rules:
// - a count whose part is not available is "not available" with why,
//   never zero (rows CR-2, CR-3, CR-29);
// - no card or heading adds counts of different units (row CR-31);
// - "not read" is said only of files not read (row CR-19); a file
//   without facts is "no fact stored from this file" (row CR-22);
// - paths left out by rule and nested repositories have no record here,
//   so the page says nothing about them either way.
// Project mode: the project scope (the members' sums, then each member's
// sections) or one member (row CR-28, CR-29).

import { Fragment, useMemo, useState, type ReactNode } from "react";

import type { SchemaSummary } from "@/api/schema";
import { fetchUnread, type FileWithoutFacts, type UnreadFetcher, type UnreadList, type UnreadPage } from "@/api/unread";
import type { IdeLink, IdeStatus } from "@/api/endpoints";
import { ParseFailedNotice, ProjectParseFailedNotice } from "@/components/ParseFailedNotice";
import { useApiRequest } from "@/hooks/useApiRequest";
import { useEditor, withEditor } from "@/hooks/useEditor";
import type { StatusView } from "@/hooks/useStatus";
import { copyText } from "@/lib/clipboard";
import { formatCount } from "@/lib/count";
import { cn } from "@/lib/utils";
import { replaceHash, useHash } from "@/router";
import { IdeButton, Method, WrappedPath } from "@/sections/api/parts";
import { apiHash, parseApiParams } from "@/sections/api/params";
import { explorerPrefixHash } from "@/sections/explorer/params";
import { refusalReason, toolLabel } from "@/sections/schema/tools";
import { COPY_HEADER, DOCUMENT_SCOPE_PRODUCTION, DOCUMENT_SCOPE_TESTS, ROUTES_WITHHELD_TITLE, SCHEMA_REFUSED_TITLE, cards, schemaText, extensionLabel, fileGroups, pageText, pathsSentence, parseUnreadParams, projectText, reasonText, routePathsComplete, unplacedText, unreadHash, type Card } from "@/sections/unread/model";
import { useProject } from "@/shell/project";

/** SHOWN is how many items a list shows before "Show all". */
const SHOWN = 8;

function CardView({ card, onOpen }: { card: Card; onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="grid content-start gap-1 rounded-[calc(var(--radius)+2px)] border bg-card p-3.5 text-left hover:border-ring/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      data-testid="unread-card"
      data-card={card.id}
    >
      <span className={cn("text-[26px] leading-none font-semibold tabular-nums", card.value === null && "text-muted-foreground")}>{card.value === null ? "–" : formatCount(card.value)}</span>
      <span className="text-[13.5px] font-medium">{card.title}</span>
      {card.missing !== null && <span className="text-[12.5px] text-dynamic">{card.missing}</span>}
      {card.lines.map((l) => (
        <span key={l} className="text-[12.5px] leading-snug text-muted-foreground">
          {l}
        </span>
      ))}
    </button>
  );
}

function Panel({ id, title, chip, source, children }: { id: string; title: string; chip?: string; source: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-h`} className="scroll-mt-3 rounded-[calc(var(--radius)+2px)] border bg-card" data-testid="unread-section">
      <header className="flex flex-wrap items-baseline gap-x-2.5 gap-y-1 border-b px-4 py-2.5">
        <h2 id={`${id}-h`} className="m-0 text-[15px] font-semibold">
          {title}
        </h2>
        {chip !== undefined && <span className="rounded-md border px-1.5 text-[12.5px] tabular-nums">{chip}</span>}
        <span className="text-[12.5px] text-muted-foreground">{source}</span>
      </header>
      <div className="grid gap-3 px-4 py-3">{children}</div>
    </section>
  );
}

/** Item is one row: what, where (linked to the editor) and an action. */
function Item({ what, file, line, ide, status, aside, note, testId }: { what: ReactNode; file: string; line: number | null; ide: IdeLink | null; status: IdeStatus; aside?: ReactNode; note?: ReactNode; testId?: string }) {
  return (
    <li className="grid gap-0.5 border-b border-dashed py-1.5 last:border-b-0" data-testid={testId ?? "unread-item"}>
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-[13px]">
        <span className="min-w-0 [overflow-wrap:anywhere]">{what}</span>
        <span className="text-muted-foreground">·</span>
        <WrappedPath file={file} line={line} />
        <span className="flex-1" />
        {aside !== undefined && <span className="text-[12.5px] text-muted-foreground">{aside}</span>}
        <IdeButton ide={ide} status={status} place={`${file}:${String(line ?? "")}`} />
      </div>
      {note !== undefined && <div className="text-[12.5px] text-muted-foreground">{note}</div>}
    </li>
  );
}

/** Bounded shows the first items of a list and a "Show all" switch; it
 * says when the server's list was cut at the request limit. */
function Bounded<T>({ id, title, list, render, empty }: { id?: string; title: ReactNode; list: UnreadList<T>; render: (item: T, i: number) => ReactNode; empty?: string }) {
  const [all, setAll] = useState(false);
  if (list.total === 0) return empty !== undefined ? <p className="m-0 text-[13px] text-muted-foreground">{empty}</p> : null;
  const items = all ? list.items : list.items.slice(0, SHOWN);
  return (
    <div id={id} className="scroll-mt-3 grid gap-1">
      <div className="text-[13px] font-medium">
        {title} <span className="font-normal text-muted-foreground tabular-nums">{formatCount(list.total)}</span>
      </div>
      <ul className="m-0 grid list-none p-0">{items.map(render)}</ul>
      <div className="flex flex-wrap gap-3 text-[12.5px] text-muted-foreground">
        {list.items.length > SHOWN && (
          <button
            type="button"
            className="font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]"
            onClick={() => {
              setAll(!all);
            }}
          >
            {all ? "Show fewer" : `Show all ${formatCount(list.items.length)}`}
          </button>
        )}
        {list.truncated && <span>The first {formatCount(list.items.length)} of {formatCount(list.total)} are listed.</span>}
      </div>
    </div>
  );
}

function routeLink(id: string, tests: boolean): string {
  return apiHash({ ...parseApiParams(""), sel: id, tests });
}

/** MissingPartLink opens the Explorer's value editor for one missing
 * prefix; in project mode it first selects the member the hop is in (the
 * Explorer shows one member's document). */
function MissingPartLink({ prefixKey, repository }: { prefixKey: string; repository: string }) {
  const project = useProject();
  return (
    <a
      href={explorerPrefixHash(prefixKey)}
      onClick={() => {
        if (project !== null && project.repository !== repository && project.project.members.some((m) => m.repository === repository)) project.select(repository);
      }}
      className="mr-2 font-medium text-brand-text hover:underline"
      data-testid="enter-missing-part-link"
    >
      Enter the missing part
    </a>
  );
}

function RouteLink({ id, label, title, method, tests }: { id: string; label: string; title?: string; method: string | null; tests: boolean }) {
  return (
    <a href={routeLink(id, tests)} title={title} className="inline-flex min-w-0 items-baseline gap-1.5 hover:underline">
      <Method method={method} />
      <span className="font-mono text-[12.5px] [overflow-wrap:anywhere]">{label}</span>
    </a>
  );
}

function FilesWithoutFacts({ page }: { page: UnreadPage }) {
  const wf = page.files.without_facts;
  const [filter, setFilter] = useState("");
  const groups = useMemo(() => fileGroups(wf.files.items, wf.by_category, filter), [wf, filter]);
  if (!wf.available) {
    return <p className="m-0 text-[13px] text-muted-foreground">Files with no fact stored: not available ({reasonText(wf.reason, wf.error)}).</p>;
  }
  if (wf.files.total === 0) return <p className="m-0 text-[13px] text-muted-foreground">No file is listed: in this scope a fact is stored from every file whose language has no reader.</p>;
  return (
    <div className="grid gap-2" data-testid="files-without-facts">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <div className="text-[13px]">
          <span className="font-medium">{formatCount(wf.files.total)} files with no fact stored</span>
          <span className="text-muted-foreground">: no reader for their language, and no fact is stored from them. Other passes read some such files (Dockerfiles, go.mod and compose files hold facts and are not listed).</span>
        </div>
        <input
          type="search"
          value={filter}
          onChange={(e) => {
            setFilter(e.target.value);
          }}
          placeholder="Filter paths"
          aria-label="Filter the files with no fact stored"
          className="ml-auto h-8 w-[240px] max-w-full rounded-md border bg-background px-2.5 text-[13px]"
          data-testid="files-filter"
        />
      </div>
      {groups.map((g) => (
        <details key={g.category} open={!g.collapsed || filter.trim() !== ""} className="rounded-md border" data-testid="file-group" data-category={g.category}>
          <summary className="flex cursor-pointer items-center gap-2 px-3 py-2 text-[13px] font-medium">
            {g.label}
            <span className="ml-auto font-normal text-muted-foreground tabular-nums">
              {filter.trim() !== "" ? `${formatCount(g.shown)} of ${formatCount(g.count)}` : `${formatCount(g.count)} ${g.count === 1 ? "file" : "files"}`}
            </span>
          </summary>
          <div className="grid gap-1 border-t px-3 py-2">
            {g.extensions.length === 0 && <p className="m-0 text-[12.5px] text-muted-foreground">No listed path matches the filter.</p>}
            {g.extensions.map((e) => (
              <ExtensionList key={e.extension} extension={e.extension} files={e.files} status={page.ide} open={filter.trim() !== "" || g.extensions.length === 1} />
            ))}
          </div>
        </details>
      ))}
      {wf.files.truncated && <p className="m-0 text-[12.5px] text-muted-foreground">The first {formatCount(wf.files.items.length)} of {formatCount(wf.files.total)} are listed; the counts are complete.</p>}
    </div>
  );
}

function ExtensionList({ extension, files, status, open }: { extension: string; files: FileWithoutFacts[]; status: IdeStatus; open: boolean }) {
  return (
    <details open={open} className="group/ext">
      <summary className="flex cursor-pointer items-center gap-2 py-1 text-[13px]">
        <span className="font-mono">{extensionLabel(extension)}</span>
        <span className="ml-auto text-muted-foreground tabular-nums">
          {formatCount(files.length)} {files.length === 1 ? "file" : "files"}
        </span>
      </summary>
      <ul className="m-0 grid list-none p-0 pl-3">
        {files.map((f) => (
          <li key={f.file} className="flex items-center gap-2 py-0.5" data-testid="file-without-facts">
            <WrappedPath file={f.file} />
            {f.test === true && <span className="rounded-[4px] border px-1 text-[11px] text-muted-foreground">test</span>}
            <span className="flex-1" />
            <IdeButton ide={f.ide} status={status} place={f.file} />
          </li>
        ))}
      </ul>
    </details>
  );
}

/** RunItem is an item of a diagnostic without a file (a run-wide
 * failure): the message only. */
function RunItem({ message }: { message: string }) {
  return (
    <li className="flex flex-wrap items-center gap-2 border-b border-dashed py-1.5 text-[13px] last:border-b-0" data-testid="unread-item">
      <span className="text-muted-foreground">whole run</span>
      <span className="[overflow-wrap:anywhere]">{message}</span>
    </li>
  );
}

/** PageSections renders one repository's sections. */
function PageSections({ page, prefix }: { page: UnreadPage; prefix: string }) {
  const tests = page.scope !== "production";
  const rp = page.route_paths;
  const cc = page.client_calls;
  const sh = page.shapes;
  const fi = page.files;
  const code = page.code_calls;
  const ide = page.ide;
  const notIndexed = page.index === null;
  if (notIndexed) return <p className="m-0 text-[13px] text-muted-foreground">{page.repository} is not indexed: nothing is known, so nothing is listed or counted.</p>;
  const routesTitle = `${formatCount(rp.unplaced.total)} ${rp.unplaced.total === 1 ? "route" : "routes"}`;
  const sentence = pathsSentence(page);
  return (
    <>
      <Panel id={`${prefix}route-paths`} title="Route paths" chip={routesTitle} source="from the Explorer's missing parts and the stored route diagnostics">
        {!rp.document.available && <p className="m-0 text-[13px] text-dynamic">Routes without a full path and mounts: not available ({reasonText(rp.document.reason, rp.document.error)}).</p>}
        {rp.document.available && (
          <p className="m-0 text-[12.5px] text-muted-foreground" data-testid="unread-document-scope">
            Mounts and routes without a full path: {tests ? DOCUMENT_SCOPE_TESTS : DOCUMENT_SCOPE_PRODUCTION}.
          </p>
        )}
        <Bounded
          title="Mounts whose prefix is not a constant"
          list={rp.mounts}
          render={(m) => (
            <Item
              key={m.key}
              what={<span className="font-mono text-[12.5px]">{m.hop}</span>}
              file={m.file}
              line={m.line}
              ide={m.ide}
              status={ide}
              aside={
                <>
                  <MissingPartLink prefixKey={m.key} repository={page.repository} />
                  <b className="font-semibold text-foreground">{formatCount(m.routes)}</b> {m.routes === 1 ? "route" : "routes"} below it
                </>
              }
              note="A prefix on the way to these routes is not a constant (for example read from configuration); their paths are shown with … for it."
            />
          )}
        />
        <Bounded
          title="Routes without a full path"
          list={rp.unplaced}
          render={(r) => {
            const u = unplacedText(r);
            return (
              <Item
                key={r.route}
                what={<RouteLink id={r.route} label={u.text} title={u.title} method={r.method} tests={false} />}
                file={r.file}
                line={r.line}
                ide={r.ide}
                status={ide}
                aside={<span data-testid="unplaced-marker">{u.marker}</span>}
                note={r.unknown ?? undefined}
                testId="unplaced-route"
              />
            );
          }}
        />
        <Bounded
          title="Patterns built at run time"
          list={rp.dynamic}
          render={(d, i) => <Item key={`${d.file}:${String(i)}`} what={<span className="font-mono text-[12.5px]">{d.expression ?? d.message}</span>} file={d.file} line={d.line} ide={d.ide} status={ide} aside={d.code === "route_dynamic_methods" ? "methods built at run time" : "pattern built at run time"} />}
        />
        <Bounded
          title="Router forms outside the route contract"
          list={rp.router_forms}
          render={(r) => (
            <Item
              key={r.file}
              what={r.forms !== null ? `${formatCount(r.forms)} ${r.forms === 1 ? "form" : "forms"}${r.by_form !== null ? ` (${Object.entries(r.by_form).map(([k, v]) => `${k.replace(/_/g, " ")} ${String(v)}`).join(", ")})` : ""}` : "forms not counted"}
              file={r.file}
              line={null}
              ide={r.ide}
              status={ide}
              aside="claims nothing"
              note={r.message}
            />
          )}
        />
        <Bounded
          title="Mounts whose router is not known (no path claimed through them)"
          list={rp.mounts_unresolved}
          render={(d, i) => <Item key={`${d.file}:${String(i)}`} what={<span className="font-mono text-[12.5px]">{d.expression ?? d.message}</span>} file={d.file} line={d.line} ide={d.ide} status={ide} aside="no path claimed" />}
        />
        <Bounded
          title={ROUTES_WITHHELD_TITLE}
          list={rp.routes_withheld}
          render={(d, i) => <Item key={`${d.file}:${String(i)}`} what={<span className="font-mono text-[12.5px]">{d.expression ?? d.message}</span>} file={d.file} line={d.line} ide={d.ide} status={ide} aside="path withheld" />}
        />
        <Bounded
          title="Go route recognition failed (these routes are not known)"
          list={rp.failed}
          render={(d, i) => (d.file === "" ? <RunItem key={`run:${String(i)}`} message={d.message} /> : <Item key={`${d.file}:${String(i)}`} what={d.message} file={d.file} line={d.line} ide={d.ide} status={ide} aside="routes not known" />)}
        />
        {routePathsComplete(page) &&
          (sentence.gaps.length === 0 ? (
            <p className="m-0 text-[13px] text-muted-foreground" data-testid="unread-paths-complete">
              {sentence.opening}
            </p>
          ) : (
            <p className="m-0 text-[13px] text-muted-foreground" data-testid="unread-paths-complete">
              {sentence.opening}
              {sentence.gaps.map((g) => (
                <Fragment key={g.list}>
                  ;{" "}
                  <button
                    type="button"
                    className="font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]"
                    onClick={() => {
                      document.getElementById(`${prefix}${g.list}`)?.scrollIntoView({ block: "start" });
                    }}
                    data-testid={`unread-${g.list}-link`}
                  >
                    {g.text}
                  </button>
                </Fragment>
              ))}
              .
            </p>
          ))}
      </Panel>
      <Panel id={`${prefix}calls`} title="Calls" chip={`${formatCount(cc.dynamic.total + cc.wrapper.total)} client calls`} source="client calls from the API section; in-code calls from the stored per-file counts">
        <Bounded
          title="Client calls with a URL or method not known statically"
          list={cc.dynamic}
          render={(c) => <Item key={c.id} what={<span className="font-mono text-[12.5px]">{c.name}</span>} file={c.file} line={c.line} ide={c.ide} status={ide} aside={c.owner !== null ? `in ${c.owner}` : undefined} />}
        />
        <Bounded
          title="Client calls through a wrapper"
          list={cc.wrapper}
          render={(c) => <Item key={c.id} what={<span className="font-mono text-[12.5px]">{c.name}</span>} file={c.file} line={c.line} ide={c.ide} status={ide} aside={c.owner !== null ? `in ${c.owner}` : undefined} />}
        />
        {code.available ? (
          <Bounded
            title={
              <>
                In-code calls not resolved: {formatCount(code.external ?? 0)} to external code, {formatCount(code.unknown ?? 0)} unknown, in {formatCount(code.files_counted ?? 0)} files counted
                {(code.files_not_counted ?? 0) > 0 && <span className="font-normal text-muted-foreground"> ({formatCount(code.files_not_counted ?? 0)} files of these languages not counted)</span>}
                <span className="font-normal text-muted-foreground"> · files</span>
              </>
            }
            list={code.files}
            render={(f) => <Item key={f.file} what={`${formatCount(f.external)} external · ${formatCount(f.unknown)} unknown`} file={f.file} line={null} ide={f.ide} status={ide} />}
          />
        ) : (
          <p className="m-0 text-[13px] text-muted-foreground">In-code calls not resolved: not available ({reasonText(code.reason, code.error)}).</p>
        )}
      </Panel>
      <Panel id={`${prefix}shapes`} title="Route shapes" chip={`${formatCount(sh.sections_not_read.total)} routes`} source={`what each route reads and answers, of ${formatCount(sh.routes)} routes in scope`}>
        <div className="flex flex-wrap gap-1.5 text-[12.5px]" data-testid="sections-matrix">
          {Object.entries(sh.sections).map(([k, v]) => (
            <span key={k} className={cn("rounded-md border px-1.5", v > 0 ? "text-foreground" : "text-muted-foreground")}>
              {k} not read <b className="tabular-nums">{formatCount(v)}</b>
            </span>
          ))}
        </div>
        <Bounded title="Routes without a stored shape (every section not read)" list={sh.without_shape} render={(r) => <Item key={r.id} what={<RouteLink id={r.id} label={r.label} method={r.method} tests={tests} />} file={r.file} line={r.line} ide={r.ide} status={ide} />} />
        <Bounded title="Stored shapes that do not decode" list={sh.shape_invalid} render={(r) => <Item key={r.id} what={<RouteLink id={r.id} label={r.label} method={r.method} tests={tests} />} file={r.file} line={r.line} ide={r.ide} status={ide} />} />
        <Bounded title="Routes with sections not read" list={sh.sections_not_read} render={(r) => <Item key={r.id} what={<RouteLink id={r.id} label={r.label} method={r.method} tests={tests} />} file={r.file} line={r.line} ide={r.ide} status={ide} aside={r.sections.join(", ")} />} />
      </Panel>
      <Panel id={`${prefix}depth`} title="Depth caps" chip={`${formatCount(page.depth_caps.routes.total)} routes`} source="route shapes that stopped following a chain at the depth limit">
        <Bounded
          title="Route shapes cut at the depth cap"
          list={page.depth_caps.routes}
          empty="No route shape stopped at the depth cap."
          render={(r) => <Item key={r.id} what={<RouteLink id={r.id} label={r.label} method={r.method} tests={tests} />} file={r.file} line={r.line} ide={r.ide} status={ide} aside={r.depth_cap !== null ? `${formatCount(r.depth_cap)} ${r.depth_cap === 1 ? "chain" : "chains"} at depth ${String(r.depth_limit ?? "?")}` : undefined} />}
        />
      </Panel>
      <Panel id={`${prefix}files`} title="Files" source="from the stored file records and diagnostics">
        {!fi.scope_applied && <p className="m-0 text-[12.5px] text-dynamic">graphvein.yaml does not load, so test files are not known: every file is listed.</p>}
        <Bounded
          id={`${prefix}files-not-read`}
          title="Files not read"
          list={fi.not_read}
          render={(f) => <Item key={f.file} what={f.message} file={f.file} line={f.line} ide={f.ide} status={ide} aside={f.stored ? f.code : `${f.code} (no record stored)`} />}
        />
        <Bounded
          id={`${prefix}syntax-errors`}
          title="Files read with syntax errors (tree-sitter recovered; nothing is claimed beyond the file there)"
          list={fi.syntax_errors}
          render={(f) => <Item key={f.file} what={f.test === true ? "test file" : "file"} file={f.file} line={f.line} ide={f.ide} status={ide} />}
        />
        <Bounded
          title="Records dropped as invalid (when a fact record, no fact from that file is stored)"
          list={fi.records_dropped}
          render={(f, i) => (f.file === "" ? <RunItem key={`run:${String(i)}`} message={f.message} /> : <Item key={`${f.file}:${String(i)}`} what={f.message} file={f.file} line={f.line} ide={f.ide} status={ide} aside={f.code} />)}
        />
        <Bounded
          title="TypeScript or JavaScript structure outside the claim contract (claims nothing)"
          list={fi.structure_outside_contract}
          render={(f) => <Item key={f.file} what={f.message} file={f.file} line={f.line} ide={f.ide} status={ide} />}
        />
        <Bounded
          title="Paths the scanner did not index (not counted as files)"
          list={fi.scanner_gaps}
          render={(g, i) => (
            <li key={`${g.path}:${String(i)}`} className="flex flex-wrap items-center gap-2 border-b border-dashed py-1.5 text-[13px] last:border-b-0" data-testid="unread-item">
              <WrappedPath file={g.path} />
              <span className="text-muted-foreground">{g.message}</span>
            </li>
          )}
        />
        <FilesWithoutFacts page={page} />
        <p className="m-0 text-[12.5px] text-muted-foreground">
          Paths left out by ignore rules (.gitignore, .graphveinignore, the built-in names) and nested repositories have no record in the index, so this page lists none of them and says nothing about them.
        </p>
      </Panel>
    </>
  );
}

/** SchemaPart lists the model sources the schema summary names as found
 * and refused (T070): the Schema view's "found, not read" items, so this
 * page names them too. Shown only when there is one. */
function SchemaPart({ summary }: { summary: SchemaSummary }) {
  const refused = summary.refused_sources ?? [];
  if (refused.length === 0) return null;
  return (
    <Panel id="schema-refused" title={SCHEMA_REFUSED_TITLE} chip={`${formatCount(refused.length)} ${refused.length === 1 ? "source" : "sources"}`} source={`from the schema summary of ${summary.repository}`}>
      <p className="m-0 text-[13px] text-muted-foreground">
        A model linker found these and refused them as a whole, so the Schema view holds no model table from them. The message is the stored reason.
      </p>
      <ul className="m-0 grid list-none gap-0 p-0" data-testid="unread-schema-refused">
        {refused.map((r, i) => (
          <li key={i} className="grid gap-0.5 border-b border-dashed py-1.5 text-[13px] last:border-b-0" data-testid="unread-schema-item">
            <span className="min-w-0 [overflow-wrap:anywhere]">
              <b className="font-semibold">{toolLabel(r.tool)} models</b> found, not read · <WrappedPath file={r.file} line={null} />
            </span>
            <span className="text-[12.5px] text-muted-foreground [overflow-wrap:anywhere]">{refusalReason(r)}</span>
          </li>
        ))}
      </ul>
    </Panel>
  );
}

function scrollTo(id: string): void {
  const el = document.getElementById(id);
  if (el !== null && typeof el.scrollIntoView === "function") el.scrollIntoView({ block: "start", behavior: "smooth" });
}

/** UnreadSection is the page. fetcher replaces the request in tests. */
export function UnreadSection({ status, fetcher = fetchUnread, schema = null }: { status: StatusView; fetcher?: UnreadFetcher; schema?: SchemaSummary | null }) {
  const hash = useHash();
  const params = parseUnreadParams(hash);
  const project = useProject();
  const editor = useEditor();
  const [copied, setCopied] = useState<string>("");
  const member = project === null ? undefined : (params.member !== null && project.project.members.some((m) => m.repository === params.member) ? params.member : "all");
  const scope = params.tests ? "all" : "production";
  const requestEditor = editor.requestEditor;
  const run = status.kind === "ok" ? (status.status.index.last_run?.finished_at ?? "") : "";
  const request = useApiRequest(
    JSON.stringify(["unread", scope, member ?? null, requestEditor ?? null, run]),
    (signal) => fetcher({ scope, ...(member !== undefined ? { member } : {}), ...withEditor(requestEditor) }, { signal }),
    requestEditor === undefined,
  );
  const load = request.state;

  const answer = load.kind === "ok" ? load.data : null;
  const cardList = answer === null ? null : answer.kind === "page" ? cards(answer.page.counts, {}, answer.page.scope) : cards(answer.project.totals, answer.project.not_available, answer.project.scope);
  const firstPage = answer === null ? null : answer.kind === "page" ? answer.page : null;
  const ext = status.kind === "ok" ? status.status.extractor.current : null;
  const indexed = firstPage?.index ?? null;

  const copy = () => {
    if (answer === null) return;
    const lines = [COPY_HEADER, "", ...(answer.kind === "page" ? pageText(answer.page) : projectText(answer.project)), ...schemaText(schema?.refused_sources ?? null)];
    void copyText(lines.join("\n")).then((ok) => {
      setCopied(ok ? "Copied" : "Could not copy: select the text on the page");
    });
  };

  return (
    <div className="grid gap-3.5" data-testid="unread-page">
      <div className="flex flex-wrap items-start gap-3">
        <div className="grid min-w-0 flex-1 gap-1">
          <h1 className="m-0 text-[22px] leading-tight font-semibold tracking-[-0.01em]">What Graphvein couldn&apos;t read</h1>
          <p className="m-0 text-[13px] text-muted-foreground">
            {project !== null ? `Project ${project.project.project}` : (firstPage?.repository ?? "")}
            {indexed !== null && ` · index of ${new Date(indexed.finished_at).toLocaleString()}`}
            {ext !== null && ` · ${ext}`}. Facts behind these items are shown as unknown in the other views, never as absent.{" "}
            {params.tests ? "Production and test code." : "Production code only."}
          </p>
          {project === null && status.kind === "ok" && <ParseFailedNotice count={status.status.index.parse_failed ?? 0} onPage className="rounded-md border bg-dynamic-soft px-3 py-2" />}
          {project !== null && (
            <ProjectParseFailedNotice
              members={project.project.members.filter((m) => member === "all" || m.repository === member).map((m) => ({ repository: m.repository, count: m.parse_failed }))}
              className="rounded-md border bg-dynamic-soft px-3 py-2"
            />
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2.5">
          {project !== null && (
            <label className="inline-flex items-center gap-1.5 text-[13px]">
              <span className="text-muted-foreground">Show</span>
              <select
                value={member}
                onChange={(e) => {
                  replaceHash(unreadHash({ ...params, member: e.target.value === "all" ? null : e.target.value }));
                }}
                className="h-8 rounded-md border bg-background px-2 text-[13px]"
                data-testid="unread-member"
              >
                <option value="all">Project scope (all members)</option>
                {project.project.members.map((m) => (
                  <option key={m.repository} value={m.repository}>
                    {m.repository}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label className="inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-[13px]">
            <input
              type="checkbox"
              checked={params.tests}
              onChange={(e) => {
                replaceHash(unreadHash({ ...params, tests: e.target.checked }));
              }}
            />
            Include tests
          </label>
          <button type="button" onClick={copy} disabled={answer === null} className="h-8 rounded-md border bg-background px-3 text-[13px] font-medium shadow-xs hover:bg-accent disabled:opacity-50" data-testid="unread-copy">
            Copy as text
          </button>
          <span role="status" className="text-[12.5px] text-muted-foreground">
            {copied}
          </span>
        </div>
      </div>
      {load.kind === "failed" && <p className="m-0 text-[13px] text-destructive">The page could not be loaded: {load.error.message}</p>}
      <div className="grid grid-cols-5 gap-3 max-[1180px]:grid-cols-3 max-[700px]:grid-cols-2" data-testid="unread-cards">
        {cardList === null
          ? Array.from({ length: 5 }, (_, i) => <div key={i} className="h-[118px] animate-pulse rounded-[calc(var(--radius)+2px)] border bg-muted/40" />)
          : cardList.map((c) => (
              <CardView
                key={c.id}
                card={c}
                onOpen={() => {
                  const target = answer?.kind === "project" ? `${answer.project.members[0]?.repository ?? ""}-${c.id}` : c.id;
                  scrollTo(target);
                }}
              />
            ))}
      </div>
      {answer?.kind === "project" && (
        <p className="m-0 text-[12.5px] text-muted-foreground" data-testid="unread-project-note">
          Project totals are the members&apos; sums: {answer.project.members.map((m) => m.repository).join(", ")}. Each member&apos;s items follow.
        </p>
      )}
      {answer?.kind === "page" && <PageSections page={answer.page} prefix="" />}
      {answer?.kind === "project" &&
        answer.project.members.map((m) => (
          <div key={m.repository} className="grid gap-3.5" data-testid="unread-member-part" data-repository={m.repository}>
            <h2 className="m-0 mt-1 text-[16px] font-semibold">{m.repository}</h2>
            <PageSections page={m} prefix={`${m.repository}-`} />
          </div>
        ))}
      {answer !== null && schema !== null && <SchemaPart summary={schema} />}
    </div>
  );
}
