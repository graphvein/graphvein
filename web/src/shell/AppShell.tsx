// The application frame: sidebar, top bar and the current section. Every
// later view plugs into the content area by adding a section in router.ts.

import { Suspense, lazy, useCallback, useEffect, useState, type ReactNode } from "react";

import type { ArchitectureClient } from "@/api/architecture";
import type { ConfigClient } from "@/api/config";
import type { ChangesClient } from "@/api/changes";
import type { ApiClient } from "@/api/endpoints";
import type { IdeFetcher } from "@/api/ide";
import type { UiFetcher } from "@/api/ui";
import type { SearchFetcher } from "@/api/search";
import type { UnreadFetcher } from "@/api/unread";
import { httpIndexRunClient, type IndexRunClient } from "@/api/indexRun";
import { httpSchemaClient, type SchemaClient } from "@/api/schema";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";
import { EditorProvider } from "@/hooks/useEditor";
import { LabelProvider } from "@/hooks/useLabels";
import { cn } from "@/lib/utils";
import { useShellSchemaSets } from "@/hooks/useSchemaSets";
import { useSchemaSummary } from "@/hooks/useSchemaSummary";
import { useIndexRun } from "@/hooks/useIndexRun";
import { useStatus, type StatusFetcher, type StatusView } from "@/hooks/useStatus";
import { helpHash, parseRoute, useHash, type Route, type Section } from "@/router";
import { ApiSection } from "@/sections/api/ApiSection";
import { ArchitectureSection } from "@/sections/architecture/ArchitectureSection";
import { CatalogueLoaderContext } from "@/sections/changes/assumptions";
import { ChangesSection, defaultGroup } from "@/sections/changes/ChangesSection";
import { groupHash, parseChangesParams } from "@/sections/changes/params";
import { groupEntries } from "@/sections/changes/words";
import { NotFound } from "@/sections/NotFound";
import { HelpSection } from "@/sections/help/HelpSection";
import { OverviewSection } from "@/sections/overview/OverviewSection";
import { useHelpShortcut } from "@/sections/help/shortcut";
import { ProjectSection } from "@/sections/project/ProjectSection";
import { schemaEntryTag } from "@/sections/schema/entry";
import { SchemaSection } from "@/sections/schema/SchemaSection";
import { UnreadSection } from "@/sections/unread/UnreadSection";
import { JumpToDialog, useJumpShortcut } from "@/shell/JumpTo";
import { ReportProvider } from "@/shell/report";
import { deriveReindex } from "@/shell/reindexModel";
import { ConfigErrorScreen, useConfigProblem } from "@/shell/ConfigGate";
import { ServerBanner } from "@/shell/ServerBanner";
import { SwitchNotice, useRunSwitchNotice } from "@/shell/SwitchNotice";
import { CompareProvider, useCompare } from "@/shell/compare";
import { MarkersProvider } from "@/shell/markers";
import { Sidebar, type SubEntries } from "@/shell/Sidebar";
import { useSidebarMode, type SidebarMode } from "@/shell/sidebarMode";
import type { EntryTag } from "@/sections/schema/entry";
import { formatCount } from "@/lib/count";
import { TopBar } from "@/shell/TopBar";
import { NoCheckoutNote } from "@/shell/ProjectMembers";
import { useProject } from "@/shell/project";
import { FirstRunCard } from "@/shell/workspace/AddFolder";
import { SCROLL_ATTR } from "@/shell/workspace/memory";
import { OpeningNote, UnknownLinkNotice } from "@/shell/workspace/Notices";
import { useWorkspace } from "@/shell/workspace/WorkspaceRoot";

// The Explorer (T029) is its own chunk: the entry chunk does not grow for
// people who never open it.
const ExplorerSection = lazy(() => import("@/sections/explorer/ExplorerSection"));
// Settings (T039) is its own chunk too.
const SettingsSection = lazy(() => import("@/sections/settings/SettingsSection").then((m) => ({ default: m.SettingsSection })));
// Settings without an index: this machine's Storage only (T068 part 3).
const MachineSettings = lazy(() => import("@/sections/settings/StorageSection").then((m) => ({ default: m.MachineSettings })));

/** PROJECT_SECTION is the Project view (T022 B9): a route only under
 * `serve --project`, so a single-repository server keeps its sections. */
export const PROJECT_SECTION: Section = { id: "project", label: "Project", available: true };

/** projectRoute maps #/project to the Project view in project mode. */
function projectRoute(hash: string, project: boolean): Route {
  const q = hash.indexOf("?");
  const path = q === -1 ? hash : hash.slice(0, q);
  return project && path === "#/project" ? { kind: "section", section: PROJECT_SECTION } : parseRoute(hash);
}

/** gridColumns is the frame's two columns: the 56 px rail or the
 * sidebar's width (232 px, 204 px up to 1400 px). */
function gridColumns(rail: boolean): string {
  return rail ? "grid-cols-[56px_1fr]" : "grid-cols-[var(--sbw)_1fr]";
}

/** SidebarOverlay is the full sidebar opened over the content below
 * 1100 px (T064): no reflow, a shadow, and a click beside it closes it, as
 * navigation and Esc do. */
function SidebarOverlay({ mode, children }: { mode: SidebarMode; children: ReactNode }) {
  if (!mode.overlay) return null;
  return (
    <>
      <div aria-hidden="true" className="fixed inset-0 z-40 bg-foreground/10" data-testid="sidebar-overlay-backdrop" onClick={mode.close} />
      <div className="fixed inset-y-0 left-0 z-50 w-[var(--sbw)] shadow-[0_8px_32px_rgba(0,0,0,.18)]" data-testid="sidebar-overlay">
        {children}
      </div>
    </>
  );
}

/** ShellPlaceholder is the frame while a project index's document is
 * awaited after a switch (T038 WS-48): the sidebar with the workspace, the
 * breadcrumb from the workspace list and skeletons; nothing of the
 * previous index. */
export function ShellPlaceholder() {
  const hash = useHash();
  const route = parseRoute(hash);
  const mode = useSidebarMode(hash);
  const workspace = useWorkspace();
  return (
    <TooltipProvider delayDuration={200}>
      <div className={cn("grid h-screen", gridColumns(mode.rail))} aria-busy="true" data-testid="shell-placeholder">
        <Sidebar route={route} repository={undefined} status={{ kind: "loading" }} collapsed={mode.rail} onToggle={mode.toggle} />
        <SidebarOverlay mode={mode}>
          <Sidebar route={route} repository={undefined} status={{ kind: "loading" }} onToggle={mode.toggle} overlay />
        </SidebarOverlay>
        <div className="flex min-h-0 min-w-0 flex-col">
          <header className="flex h-[52px] flex-none items-center gap-2 border-b px-5 text-sm text-muted-foreground max-[1400px]:px-4">
            {workspace?.entry !== null && workspace?.entry !== undefined && <span>{workspace.entry.name}</span>}
            <Skeleton className="ml-auto h-8 w-64" />
          </header>
          <main className="relative flex min-h-0 flex-1 flex-col gap-3.5 px-5 py-4 max-[1400px]:px-4 max-[1400px]:py-3">
            <Skeleton className="h-9 w-80" />
            <Skeleton className="h-28" />
            <Skeleton className="min-h-0 flex-1" />
            <OpeningNote />
          </main>
        </div>
      </div>
    </TooltipProvider>
  );
}

/** FirstRunPage is the page without an index to view (T038 S11, WS-51):
 * greyed sections, the Workspace list and the add card. choose is the
 * state with entries but no current one. */
export function FirstRunPage() {
  const hash = useHash();
  const route = parseRoute(hash);
  const workspace = useWorkspace();
  const choose = (workspace?.workspace.entries.length ?? 0) > 0;
  // "New to Graphvein? Start here" opens Help's first page (T045), which
  // this page shows itself: Help needs no index.
  const help = helpHash("start-here");
  const onHelp = route.kind === "section" && route.section.id === "help";
  // Settings needs no index for this machine's Storage (T068 part 3).
  const onSettings = route.kind === "section" && route.section.id === "settings";
  useHelpShortcut();
  const mode = useSidebarMode(hash);
  return (
    <TooltipProvider delayDuration={200}>
      <div className={cn("grid h-screen", gridColumns(mode.rail))}>
        <Sidebar route={route} repository={undefined} status={{ kind: "loading" }} collapsed={mode.rail} noIndex onToggle={mode.toggle} />
        <SidebarOverlay mode={mode}>
          <Sidebar route={route} repository={undefined} status={{ kind: "loading" }} noIndex onToggle={mode.toggle} overlay />
        </SidebarOverlay>
        <div className="flex min-h-0 min-w-0 flex-col">
          <header className="flex h-[52px] flex-none items-center border-b px-5 text-sm font-medium max-[1400px]:px-4" data-testid="top-bar">
            {onHelp ? "Help" : onSettings ? "Settings" : choose ? "Workspace" : "Add a folder"}
          </header>
          <main className="flex min-h-0 flex-1 flex-col gap-3.5 overflow-auto px-5 py-4 max-[1400px]:px-4 max-[1400px]:py-3">
            {onHelp ? (
              <HelpSection status={{ kind: "loading" }} noIndex />
            ) : onSettings ? (
              <Suspense fallback={<Skeleton className="h-28" />}>
                <MachineSettings />
              </Suspense>
            ) : (
              <>
                <UnknownLinkNotice />
                <FirstRunCard choose={choose} helpHref={help} />
              </>
            )}
          </main>
        </div>
      </div>
    </TooltipProvider>
  );
}

/**
 * AppShell is the whole page. fetcher, client and schemaClient replace the
 * status, API section and schema requests in tests, ideFetcher the editor
 * preset list, uiFetcher the repository's UI defaults (T025b). The shell owns the single useStatus instance so every part
 * of the frame shows the same answer, the single schema summary request
 * (the sidebar's Schema tag and the Schema section read the same answer;
 * T018b1 section 2a), and the /sets request, so it is made once per index
 * run however often the Schema section is left and shown again. It also
 * owns the re-index state (runClient replaces its requests in tests), which
 * reads the status view and asks useStatus for a check on a run switch.
 */
export function AppShell({
  fetcher,
  client,
  ideFetcher,
  uiFetcher,
  schemaClient = httpSchemaClient,
  runClient = httpIndexRunClient,
  changesClient,
  architectureClient,
  configClient,
  searchFetcher,
  unreadFetcher,
}: {
  fetcher?: StatusFetcher;
  client?: ApiClient;
  ideFetcher?: IdeFetcher;
  uiFetcher?: UiFetcher;
  schemaClient?: SchemaClient;
  runClient?: IndexRunClient;
  changesClient?: ChangesClient;
  architectureClient?: ArchitectureClient;
  configClient?: ConfigClient;
  searchFetcher?: SearchFetcher;
  unreadFetcher?: UnreadFetcher;
}) {
  const hash = useHash();
  const project = useProject();
  const route = projectRoute(hash, project !== null);
  const section = route.kind === "section" ? route.section.id : null;
  // One sidebar width on every section (T064): the user's choice, else the
  // window's width; graph views no longer fold it to the rail.
  const mode = useSidebarMode(hash);
  const result = useStatus(fetcher);
  const { view } = result;
  const indexRun = useIndexRun(view, result.check, runClient);
  const reindexModel = deriveReindex({ status: view, ...indexRun });
  // One re-index state (T038 WS-52): when the workspace list sees the
  // viewed index's job start or change (the add flow, another tab), the
  // top bar asks for the run at once instead of at its next poll.
  const workspace = useWorkspace();
  const listJob = workspace?.entry?.job ?? null;
  const listJobKey = listJob === null ? "" : `${listJob.id}:${listJob.state}`;
  const { poll } = indexRun;
  useEffect(() => {
    if (listJobKey !== "") poll();
  }, [listJobKey, poll]);
  const switched = useRunSwitchNotice(view);
  const summary = useSchemaSummary(schemaClient, view);
  const sets = useShellSchemaSets(schemaClient, view, summary, section === "schema");
  const schemaTag = schemaEntryTag(summary.state.kind === "ok" ? summary.state.data : null);
  // A graphvein.yaml that does not load: every view but Settings (which
  // fixes it), Help and the project overview says so. Read again on a new run
  // and on every section change, so a fix made in Settings lifts it.
  const runKey = view.kind === "ok" ? (view.status.index.last_run?.finished_at ?? "") : "";
  const problem = useConfigProblem(configClient, project?.repository ?? null, `${runKey}\u0000${section ?? ""}`);
  const gated = problem !== null && route.kind === "section" && section !== "settings" && section !== "help" && section !== "project" ? route.section.label : null;
  // "?" opens Help from every view (T045), never while typing.
  useHelpShortcut();
  // Jump to (T040): Ctrl+K / Cmd+K anywhere, or the sidebar's box.
  const [jumpOpen, setJumpOpen] = useState(false);
  const toggleJump = useCallback(() => {
    setJumpOpen((o) => !o);
  }, []);
  const openJump = useCallback(() => {
    setJumpOpen(true);
  }, []);
  useJumpShortcut(toggleJump);
  return (
    <TooltipProvider delayDuration={200}>
      <EditorProvider {...(ideFetcher !== undefined ? { fetcher: ideFetcher } : {})}>
        <LabelProvider {...(uiFetcher !== undefined ? { fetcher: uiFetcher } : {})}>
        <ReportProvider version={versionLine(view)} {...(uiFetcher !== undefined ? { fetcher: uiFetcher } : {})}>
        <CompareProvider status={view} onStale={result.check} {...(changesClient !== undefined ? { client: changesClient } : {})}>
          <MarkersProvider>
          <JumpToDialog open={jumpOpen} onOpenChange={setJumpOpen} served={result.repository} {...(searchFetcher !== undefined ? { searchFetcher } : {})} />
          <Frame route={route} hash={hash} mode={mode} schemaTag={schemaTag}>
            {{
              sidebarProps: { repository: result.repository, status: view, onJump: openJump },
              // The top bar is the single-repository one in project mode too:
              // the project lives in the sidebar (T022 MR-UI).
              top: <TopBar route={route} result={result} reindex={{ model: reindexModel, start: indexRun.start, dismiss: indexRun.dismiss }} />,
              content: (
                <>
                  <UnknownLinkNotice />
                  {view.kind === "loading" && <OpeningNote />}
                  <NoCheckoutNote />
                  {view.kind === "failed" && <ServerBanner error={view.error} checking={result.checking} onRetry={result.check} />}
                  {switched.notice !== null && <SwitchNotice notice={switched.notice} onDismiss={switched.dismiss} />}
                  {gated !== null && problem !== null ? (
                    <ConfigErrorScreen problem={problem} view={gated} />
                  ) : section === "project" ? (
                    <ProjectSection />
                  ) : section === "schema" ? (
                    <SchemaSection status={view} summary={summary} sets={sets} client={schemaClient} />
                  ) : section === "changes" ? (
                    <CatalogueLoaderContext.Provider value={(signal) => schemaClient.assumptions({ signal })}>
                      <ChangesSection status={view} />
                    </CatalogueLoaderContext.Provider>
                  ) : section === "explorer" ? (
                    <Suspense fallback={<Skeleton className="h-28" />}>
                      <ExplorerSection status={view} />
                    </Suspense>
                  ) : section === "settings" ? (
                    <Suspense fallback={<Skeleton className="h-28" />}>
                      <SettingsSection status={view} reindex={{ model: reindexModel, start: indexRun.start }} />
                    </Suspense>
                  ) : section === "help" ? (
                    <HelpSection status={view} client={client} />
                  ) : section === "unread" ? (
                    <UnreadSection status={view} schema={summary.state.kind === "ok" ? summary.state.data : null} {...(unreadFetcher !== undefined ? { fetcher: unreadFetcher } : {})} />
                  ) : section === "overview" ? (
                    <OverviewSection
                      status={view}
                      summary={summary}
                      {...(client !== undefined ? { client } : {})}
                      {...(architectureClient !== undefined ? { architectureClient } : {})}
                      {...(unreadFetcher !== undefined ? { unreadFetcher } : {})}
                    />
                  ) : section === "architecture" ? (
                    <ArchitectureSection status={view} client={architectureClient} />
                  ) : route.kind === "section" ? (
                    <ApiSection status={view} client={client} />
                  ) : (
                    <NotFound hash={route.hash} />
                  )}
                </>
              ),
            }}
          </Frame>
          </MarkersProvider>
        </CompareProvider>
        </ReportProvider>
        </LabelProvider>
      </EditorProvider>
    </TooltipProvider>
  );
}

/** versionLine is the version a wrong-fact report names: server,
 * extractors and contract, from an ok status answer only. */
export function versionLine(view: StatusView): string {
  if (view.kind !== "ok") return "";
  const s = view.status;
  const contract = s.index.last_run !== null ? ` · contract ${String(s.index.last_run.contract_version)}` : "";
  return `graphvein ${s.server_version} · ${s.extractor.current}${contract}`;
}

interface FrameSlots {
  sidebarProps: { repository: string | undefined; status: StatusView; onJump: () => void };
  top: ReactNode;
  content: ReactNode;
}

/** Frame is the grid inside the providers: it reads the compare state for
 * the Changes entry's count and its group list. */
function Frame({ route, hash, mode, schemaTag, children }: { route: Route; hash: string; mode: SidebarMode; schemaTag: EntryTag | null; children: FrameSlots }) {
  const compare = useCompare();
  const tags: Record<string, EntryTag> = {};
  if (schemaTag !== null) tags.schema = schemaTag;
  const cs = compare.summary.state;
  if (compare.choice.on && cs.kind === "ok") {
    tags.changes = { text: formatCount(cs.data.summary.total), tone: "muted", title: `${String(cs.data.summary.total)} changes against the compare chip's base` };
  }
  let sub: SubEntries | undefined;
  if (route.kind === "section" && route.section.id === "changes" && compare.choice.on && cs.kind === "ok") {
    const current = parseChangesParams(hash).group ?? defaultGroup(cs.data);
    sub = {
      section: "changes",
      label: "Change groups",
      entries: groupEntries(cs.data.summary.groups, cs.data.summary.not_extracted).map((e) => ({
        id: e.id,
        label: e.label,
        text: e.text,
        muted: e.count === null,
        href: groupHash(e.id),
        current: e.id === current,
      })),
    };
  }
  return (
    <div className={cn("grid h-screen", gridColumns(mode.rail))}>
      <Sidebar route={route} repository={children.sidebarProps.repository} status={children.sidebarProps.status} collapsed={mode.rail} tags={tags} sub={sub} onJump={children.sidebarProps.onJump} onToggle={mode.toggle} />
      <SidebarOverlay mode={mode}>
        <Sidebar route={route} repository={children.sidebarProps.repository} status={children.sidebarProps.status} tags={tags} sub={sub} onJump={children.sidebarProps.onJump} onToggle={mode.toggle} overlay />
      </SidebarOverlay>
      <div className="flex min-h-0 min-w-0 flex-col">
        {children.top}
        <main {...{ [SCROLL_ATTR]: "main" }} className="relative flex min-h-0 flex-1 flex-col gap-3.5 overflow-auto px-5 py-4 max-[1400px]:px-4 max-[1400px]:py-3">
          {children.content}
        </main>
      </div>
    </div>
  );
}
