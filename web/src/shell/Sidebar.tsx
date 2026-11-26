// The section sidebar (M5 layout). Only available sections are links; the
// others are listed as plain text with a "Soon" badge (and "coming soon"
// for screen readers) so the frame shows where later views go.

import { Boxes, BookOpenText, Check, CircleHelp, Database, EyeOff, FileText, FolderGit2, GitCompareArrows, Settings, TextAlignStart, TriangleAlert, Workflow, type LucideIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import type { Status } from "@/api/status";
import type { StatusView } from "@/hooks/useStatus";
import { HELP_SECTION, SECTIONS, SETTINGS_SECTION, sectionHash, type Route } from "@/router";
import { cn } from "@/lib/utils";
import type { EntryTag } from "@/sections/schema/entry";
import { JumpToButton } from "@/shell/JumpTo";
import { MemberListView, MemberRailButton } from "@/shell/ProjectMembers";
import { useProject } from "@/shell/project";
import { WorkspaceRailButton, WorkspaceTree } from "@/shell/workspace/WorkspaceList";
import { useWorkspace } from "@/shell/workspace/WorkspaceRoot";

const ICONS: Record<string, LucideIcon> = {
  api: TextAlignStart,
  explorer: BookOpenText,
  architecture: Workflow,
  schema: Database,
  changes: GitCompareArrows,
  unread: EyeOff,
  review: Check,
};

const item = "flex h-8 items-center gap-2 whitespace-nowrap rounded-md px-2 text-sm";

// Logo is the Graphvein mark (T047 variant 1, "current vein, heavier,
// tapered"): the white glyph on the gradient tile, drawn over the whole
// 30 px tile in the favicon's 32-unit box (public/favicon.svg) so both
// read the same.
function Logo() {
  return (
    <div className="grid size-[30px] flex-none place-items-center rounded-lg bg-[linear-gradient(135deg,hsl(245_70%_58%),hsl(270_70%_60%))] text-white shadow-[0_1px_2px_hsl(245_60%_30%/.3)]" data-testid="logo">
      <svg aria-hidden="true" width="30" height="30" viewBox="0 0 32 32" fill="currentColor">
        <path d="M8.71 24.76L9.04 24.62 9.39 24.45 9.74 24.27 10.07 24.07 10.38 23.86 10.69 23.65 10.98 23.42 11.25 23.19 11.52 22.95 11.77 22.7 12.01 22.45 12.24 22.2 12.46 21.94 12.68 21.69 12.88 21.42 13.08 21.16 13.27 20.9 13.45 20.64 13.63 20.37 13.8 20.11 13.97 19.85 14.13 19.59 14.3 19.34 14.46 19.08 14.61 18.83 14.77 18.59 14.92 18.34 15.08 18.11 15.23 17.87 15.38 17.64 15.54 17.42 15.69 17.2 15.85 16.99 16.01 16.79 16.17 16.59 16.33 16.4 16.49 16.21 16.66 16.04 16.83 15.87 17.01 15.7 17.22 15.52 17.43 15.34 17.64 15.16 17.84 14.98 18.05 14.81 18.25 14.64 18.45 14.48 18.66 14.31 18.86 14.15 19.05 14 19.25 13.84 19.45 13.69 19.64 13.54 19.84 13.4 20.03 13.25 20.22 13.11 20.41 12.97 20.6 12.83 20.79 12.69 20.98 12.56 21.17 12.43 21.35 12.3 21.54 12.17 21.73 12.04 21.91 11.91 22.1 11.79 22.28 11.66 22.46 11.54 22.65 11.42 22.83 11.3 23.02 11.18 23.2 11.06 23.38 10.94 23.57 10.82 23.75 10.7 23.93 10.59 24.12 10.47 24.3 10.35 24.48 10.23 24.67 10.11A1.3 1.3 0 0 0 23.33 7.89L23.14 7.99 22.95 8.1 22.76 8.21 22.57 8.32 22.37 8.43 22.18 8.54 21.99 8.65 21.79 8.76 21.6 8.87 21.4 8.98 21.21 9.1 21.01 9.21 20.81 9.33 20.61 9.45 20.41 9.57 20.21 9.69 20.01 9.81 19.81 9.93 19.6 10.06 19.4 10.19 19.19 10.32 18.98 10.45 18.77 10.59 18.56 10.72 18.35 10.86 18.14 11 17.93 11.15 17.71 11.3 17.49 11.44 17.27 11.6 17.05 11.75 16.83 11.91 16.61 12.07 16.38 12.24 16.16 12.4 15.93 12.57 15.7 12.75 15.46 12.93 15.23 13.11 14.99 13.3 14.73 13.51 14.47 13.74 14.22 13.97 13.98 14.21 13.75 14.46 13.53 14.7 13.31 14.95 13.11 15.2 12.91 15.45 12.71 15.7 12.52 15.95 12.33 16.2 12.15 16.46 11.97 16.7 11.8 16.95 11.62 17.2 11.45 17.44 11.28 17.67 11.12 17.91 10.95 18.14 10.78 18.36 10.62 18.58 10.45 18.79 10.28 19 10.12 19.2 9.95 19.39 9.78 19.57 9.61 19.75 9.44 19.92 9.26 20.08 9.09 20.23 8.91 20.37 8.73 20.51 8.54 20.63 8.35 20.75 8.16 20.86 7.97 20.96 7.76 21.06 7.55 21.14 7.29 21.24A1.9 1.9 0 0 0 8.71 24.76Z" />
        <path d="M14.42 14.72L14.45 14.91 14.48 15.09 14.51 15.26 14.54 15.44 14.56 15.61 14.58 15.79 14.6 15.97 14.62 16.14 14.63 16.32 14.64 16.5 14.65 16.67 14.65 16.85 14.65 17.03 14.65 17.21 14.65 17.39 14.65 17.57 14.64 17.75 14.63 17.93 14.62 18.11 14.6 18.29 14.58 18.47 14.56 18.65 14.54 18.84 14.52 19.02 14.49 19.2 14.46 19.39 14.43 19.57 14.4 19.75 14.37 19.94 14.33 20.13 14.29 20.31 14.25 20.5 14.21 20.68 14.17 20.87 14.12 21.06 14.07 21.25 14.02 21.44 13.97 21.63 13.92 21.82 13.86 22.01A1.2 1.2 0 0 0 16.14 22.79L16.21 22.59 16.29 22.39 16.37 22.19 16.44 21.99 16.51 21.79 16.59 21.59 16.66 21.38 16.72 21.18 16.79 20.98 16.85 20.77 16.91 20.57 16.97 20.36 17.03 20.15 17.09 19.95 17.14 19.74 17.19 19.53 17.24 19.32 17.29 19.11 17.33 18.9 17.37 18.69 17.41 18.47 17.45 18.26 17.48 18.04 17.51 17.83 17.54 17.61 17.57 17.4 17.59 17.18 17.61 16.96 17.63 16.74 17.64 16.52 17.65 16.3 17.66 16.07 17.66 15.85 17.66 15.63 17.65 15.4 17.65 15.18 17.64 14.95 17.62 14.72 17.6 14.49 17.58 14.28A1.6 1.6 0 0 0 14.42 14.72Z" />
        <path d="M18.72 12.21L18.8 12.39 18.89 12.56 18.97 12.73 19.04 12.9 19.12 13.07 19.2 13.24 19.28 13.41 19.35 13.58 19.43 13.76 19.5 13.93 19.58 14.1 19.65 14.27 19.72 14.45 19.8 14.62 19.87 14.8 19.94 14.97 20.01 15.15 20.08 15.32 20.14 15.5 20.21 15.68 20.28 15.86 20.35 16.04 20.41 16.22 20.48 16.4 20.54 16.58 20.6 16.76 20.67 16.95 20.73 17.13 20.79 17.32 20.85 17.5 20.91 17.69 20.97 17.88 21.03 18.07 21.09 18.27 21.15 18.46 21.2 18.66 21.26 18.85 21.32 19.05 21.37 19.25 21.43 19.45A1.2 1.2 0 0 0 23.77 18.95L23.74 18.73 23.7 18.52 23.66 18.3 23.62 18.09 23.58 17.88 23.54 17.66 23.5 17.45 23.46 17.25 23.42 17.04 23.37 16.83 23.33 16.63 23.28 16.42 23.24 16.22 23.19 16.02 23.14 15.82 23.09 15.62 23.04 15.42 22.99 15.22 22.94 15.02 22.89 14.82 22.84 14.63 22.78 14.43 22.73 14.23 22.67 14.04 22.61 13.85 22.56 13.65 22.5 13.46 22.44 13.27 22.38 13.07 22.32 12.88 22.26 12.69 22.2 12.5 22.14 12.31 22.07 12.12 22.01 11.93 21.94 11.74 21.88 11.55 21.81 11.36 21.74 11.17 21.68 10.99A1.6 1.6 0 0 0 18.72 12.21Z" />
      </svg>
    </div>
  );
}

function Group({ children }: { children: string }) {
  return <div className="flex h-[30px] items-center px-2 text-xs font-medium text-muted-foreground">{children}</div>;
}

interface SidebarProps {
  route: Route;
  repository: string | undefined;
  /** The shell's status, for the version footer. Only an ok answer shows
   * versions: while loading or checking the values are skeletons, after a
   * failure "unavailable", so no value is claimed before the server said it
   * (T016i D5). */
  status: StatusView;
  /** Icon rail only (graph views): icons with their names as labels and
   * tooltips, nothing renamed. */
  collapsed?: boolean | undefined;
  /** Optional tags after a section's label, by section id (the Schema
   * entry's tag from the schema summary, D1 and round 3 Y). */
  tags?: Record<string, EntryTag> | undefined;
  /** Sub-entries under a section's link (the Changes groups, T019
   * Revision 2); shown only while that section is active, so the list
   * collapses when another section is. */
  sub?: SubEntries | undefined;
  /** noIndex greys every section (the first-run and choose states, T038
   * WS-51): there is no index whose facts a section could show. */
  noIndex?: boolean | undefined;
  /** Opens the jump-to palette (T040); no box without it. */
  onJump?: (() => void) | undefined;
}

/** PROJECT_ONLY is the greyed Project entry's tooltip for a single
 * repository: the entry stays so the list never moves on a switch (T038). */
const PROJECT_ONLY = "Project: only for a project (graphvein-project.yaml); this is a single repository";

/** SubEntries are links listed under one section's entry. */
export interface SubEntries {
  section: string;
  label: string;
  entries: { id: string; label: string; text: string; muted: boolean; href: string; current: boolean }[];
}

function SubList({ sub }: { sub: SubEntries }) {
  return (
    <ul aria-label={sub.label} className="grid gap-px py-0.5 pl-6" data-testid="sidebar-sub">
      {sub.entries.map((e) => (
        <li key={e.id}>
          <a
            href={e.href}
            aria-current={e.current ? "true" : undefined}
            className={cn("flex h-7 items-center gap-2 rounded-md px-2 text-[13px] whitespace-nowrap", e.current ? "bg-brand-soft font-medium text-brand-text" : "text-foreground hover:bg-accent")}
          >
            <span className="min-w-0 truncate">{e.label}</span>
            <span className={cn("ml-auto text-xs tabular-nums", e.muted ? "text-never italic" : "text-muted-foreground")}>{e.text}</span>
          </a>
        </li>
      ))}
    </ul>
  );
}

function TagView({ tag }: { tag: EntryTag }) {
  return (
    <span
      aria-hidden="true"
      title={tag.title}
      data-testid="section-tag"
      className={cn("ml-auto inline-flex items-center gap-1 text-xs whitespace-nowrap", tag.tone === "warn" ? "font-medium text-dynamic" : "text-muted-foreground")}
    >
      {tag.tone === "warn" && <TriangleAlert aria-hidden="true" className="size-3" />}
      {tag.text}
    </span>
  );
}

function Rail({ route, repository, tags, status, noIndex = false, onJump }: Pick<SidebarProps, "route" | "repository" | "tags" | "status" | "noIndex" | "onJump">) {
  const current = route.kind === "section" ? route.section.id : undefined;
  const project = useProject();
  const workspace = useWorkspace();
  return (
    <nav aria-label="Sections" className="flex min-w-0 flex-col items-center gap-1 border-r bg-sidebar py-2" data-testid="sidebar-rail">
      <div className="mb-1.5 p-1" title={repository === undefined ? "Graphvein" : `Graphvein · ${repository}`}>
        <Logo />
      </div>
      {onJump !== undefined && (
        <div className="mb-1">
          <JumpToButton onOpen={onJump} rail />
        </div>
      )}
      <ul className="grid gap-1">
        {project !== null && !noIndex ? (
          <li>
            <a
              href="#/project"
              aria-current={current === "project" ? "page" : undefined}
              aria-label={`Project ${project.project.project}`}
              title={`Project ${project.project.project}`}
              className={cn("grid size-9 place-items-center rounded-md", current === "project" ? "bg-brand-soft text-brand-text" : "hover:bg-accent")}
            >
              <Boxes aria-hidden="true" className="size-4" />
            </a>
          </li>
        ) : (
          <li className="grid size-9 place-items-center text-muted-foreground" title={noIndex ? "Project" : PROJECT_ONLY} data-testid="project-greyed">
            <Boxes aria-hidden="true" className="size-4 opacity-50" />
            <span className="sr-only">Project, not available</span>
          </li>
        )}
        {SECTIONS.map((section) => {
          const Icon = ICONS[section.id] ?? TextAlignStart;
          if (!section.available || noIndex) {
            return (
              <li key={section.id} className="grid size-9 place-items-center text-muted-foreground" title={`${section.label} (soon)`}>
                <Icon aria-hidden="true" className="size-4 opacity-60" />
                <span className="sr-only">{section.label}, coming soon</span>
              </li>
            );
          }
          const active = section.id === current;
          const tag = tags?.[section.id];
          const name = tag === undefined ? section.label : `${section.label}, ${tag.text}`;
          return (
            <li key={section.id}>
              <a
                href={sectionHash(section)}
                aria-current={active ? "page" : undefined}
                aria-label={name}
                title={tag === undefined ? section.label : `${name}: ${tag.title}`}
                className={cn("relative grid size-9 place-items-center rounded-md", active ? "bg-brand-soft text-brand-text" : "hover:bg-accent")}
              >
                <Icon aria-hidden="true" className="size-4" />
                {tag?.tone === "warn" && <span aria-hidden="true" className="absolute top-1 right-1 size-2 rounded-full bg-dynamic-bar" />}
              </a>
            </li>
          );
        })}
      </ul>
      {workspace !== null ? (
        <>
          <div className="my-0.5 h-px w-7 bg-border" />
          <WorkspaceRailButton state={workspace} opening={status.kind === "loading"} />
        </>
      ) : (
        project !== null && (
          <>
            <div className="my-0.5 h-px w-7 bg-border" />
            <MemberRailButton state={project} />
          </>
        )
      )}
      <div className="mt-auto grid justify-items-center gap-1.5">
        {noIndex ? (
          // Settings reads an index's graphvein.yaml: greyed like the
          // sections until there is one (first run, T038 x T039).
          <span aria-label="Settings, needs an index" title="Settings: needs an index" data-testid="rail-settings" className="grid size-9 place-items-center text-muted-foreground">
            <Settings aria-hidden="true" className="size-4 opacity-60" />
          </span>
        ) : (
          <a
            href={sectionHash(SETTINGS_SECTION)}
            aria-current={current === "settings" ? "page" : undefined}
            aria-label="Settings"
            title="Settings: graphvein.yaml and this browser's choices (editor for links, labels)"
            data-testid="rail-settings"
            className={cn("grid size-9 place-items-center rounded-md", current === "settings" ? "bg-brand-soft text-brand-text" : "text-muted-foreground hover:bg-accent")}
          >
            <Settings aria-hidden="true" className="size-4" />
          </a>
        )}
        {/* Help (T045) after Settings (T039); reachable without an index. */}
        <a
          href={sectionHash(HELP_SECTION)}
          aria-current={current === "help" ? "page" : undefined}
          aria-label="Help"
          aria-keyshortcuts="?"
          title="Help (?)"
          data-testid="rail-help"
          className={cn("grid size-9 place-items-center rounded-md", current === "help" ? "bg-brand-soft text-brand-text" : "text-muted-foreground hover:bg-accent")}
        >
          <CircleHelp aria-hidden="true" className="size-4" />
        </a>
        <div className="h-px w-7 bg-border" />
        <a href="/third-party-licenses.txt" aria-label="Third-party licences" title="Third-party licences" className="grid size-9 place-items-center rounded-md text-muted-foreground hover:bg-accent">
          <FileText aria-hidden="true" className="size-4" />
        </a>
      </div>
    </nav>
  );
}

/** FooterValue is one version in the footer: the value from an ok answer,
 * a skeleton while the first or a withdrawn answer is awaited, and
 * "unavailable" after a failure (the freshness badge's word). Values of an
 * earlier answer are never kept: another binary may answer next. */
function FooterValue({ status, pick }: { status: StatusView; pick: (s: Status) => string }) {
  if (status.kind === "ok") return <span className="font-mono">{pick(status.status)}</span>;
  if (status.kind === "failed") return <span>unavailable</span>;
  // A span, not the Skeleton div: it sits inside a line of text.
  return <span aria-hidden="true" className="inline-block h-3 w-14 animate-pulse rounded-md bg-accent align-middle" data-testid="version-skeleton" />;
}

/** Sidebar is the section navigation with the repository and versions. */
export function Sidebar({ route, repository, status, collapsed = false, tags, sub, noIndex = false, onJump }: SidebarProps) {
  const current = route.kind === "section" ? route.section.id : undefined;
  const project = useProject();
  const workspace = useWorkspace();
  if (collapsed) return <Rail route={route} repository={repository} tags={tags} status={status} noIndex={noIndex} onJump={onJump} />;
  // The logo's second line: the project and member, the repository, or
  // the first-run words.
  const subtitle = noIndex ? (workspace?.workspace.entries.length === 0 ? "no folder yet" : "no index chosen") : project !== null ? `${project.project.project} · ${project.repository}` : repository;
  return (
    <nav aria-label="Sections" className="flex h-screen max-h-screen min-w-0 flex-col overflow-y-auto border-r bg-sidebar p-2">
      <div className="mb-1.5 flex items-center gap-2.5 p-2">
        <Logo />
        <div className="min-w-0">
          <div className="text-sm leading-tight font-semibold">Graphvein</div>
          <div className="truncate text-xs text-muted-foreground">{subtitle ?? " "}</div>
        </div>
      </div>
      {onJump !== undefined && (
        <div className="px-0.5">
          <JumpToButton onOpen={onJump} />
        </div>
      )}
      <Group>Sections</Group>
      <ul className="grid gap-px">
        {project !== null && !noIndex ? (
          <li>
            <a href="#/project" aria-current={current === "project" ? "page" : undefined} className={cn(item, current === "project" ? "bg-brand-soft font-medium text-brand-text" : "hover:bg-accent")}>
              <Boxes aria-hidden="true" className="size-4" />
              Project
            </a>
          </li>
        ) : (
          // Always listed, greyed for a single repository, so the entries
          // below never move on a switch (T038, measured 33 px).
          <li className={cn(item, "text-muted-foreground")} title={noIndex ? undefined : PROJECT_ONLY} data-testid="project-greyed">
            <Boxes aria-hidden="true" className="size-4 opacity-60" />
            <span>
              Project
              <span className="sr-only">, {noIndex ? "no index" : "only for a project"}</span>
            </span>
          </li>
        )}
        {SECTIONS.map((section) => {
          const Icon = ICONS[section.id] ?? TextAlignStart;
          if (noIndex && section.available) {
            return (
              <li key={section.id} className={cn(item, "text-muted-foreground")}>
                <Icon aria-hidden="true" className="size-4 opacity-60" />
                <span>
                  {section.label}
                  <span className="sr-only">, needs an index</span>
                </span>
              </li>
            );
          }
          if (!section.available) {
            return (
              // A plain list item, not a disabled control: there is nothing
              // to activate yet, so it is not focusable and screen readers
              // read "Architecture, coming soon".
              <li key={section.id} className={cn(item, "text-muted-foreground")}>
                <Icon aria-hidden="true" className="size-4 opacity-60" />
                <span>
                  {section.label}
                  <span className="sr-only">, coming soon</span>
                </span>
                <Badge aria-hidden="true" variant="outline" className="ml-auto rounded-md px-[7px] text-muted-foreground">
                  Soon
                </Badge>
              </li>
            );
          }
          const active = section.id === current;
          const tag = tags?.[section.id];
          return (
            <li key={section.id}>
              <a
                href={sectionHash(section)}
                aria-current={active ? "page" : undefined}
                aria-label={tag === undefined ? undefined : `${section.label}, ${tag.text}`}
                title={tag?.title}
                className={cn(item, active ? "bg-brand-soft font-medium text-brand-text" : "hover:bg-accent")}
              >
                <Icon aria-hidden="true" className="size-4" />
                {section.label}
                {tag !== undefined && <TagView tag={tag} />}
              </a>
              {active && sub !== undefined && sub.section === section.id && <SubList sub={sub} />}
            </li>
          );
        })}
      </ul>
      {workspace !== null ? (
        <div className="mt-2 flex min-h-[132px] flex-1 flex-col" data-testid="sidebar-workspace">
          <WorkspaceTree state={workspace} opening={status.kind === "loading"} />
        </div>
      ) : (
        <div className="mt-2">
          <Group>Repository</Group>
        </div>
      )}
      {workspace !== null ? null : project !== null ? (
        // Project mode: the project's member list (T022 MR-UI).
        <MemberListView state={project} />
      ) : (
        <div className={item}>
          <FolderGit2 aria-hidden="true" className="size-4 text-muted-foreground" />
          {repository !== undefined ? <span className="truncate">{repository}</span> : <Skeleton className="h-4 w-24" />}
        </div>
      )}
      {/* Settings, then Help, sit at the bottom above the versions (T039,
          T045); the editor for links and the label style moved into
          Settings. */}
      <div className={cn("grid border-t px-0 pt-2 pb-2", workspace === null && "mt-auto")} data-testid="sidebar-settings">
        {noIndex ? (
          // Settings reads an index's graphvein.yaml: greyed like the
          // sections until there is one (first run, T038 x T039).
          <span className={cn(item, "text-muted-foreground")}>
            <Settings aria-hidden="true" className="size-4 opacity-60" />
            <span>
              Settings
              <span className="sr-only">, needs an index</span>
            </span>
          </span>
        ) : (
          <a
            href={sectionHash(SETTINGS_SECTION)}
            aria-current={current === "settings" ? "page" : undefined}
            title="graphvein.yaml and this browser's choices (editor for links, labels)"
            className={cn(item, current === "settings" ? "bg-brand-soft font-medium text-brand-text" : "hover:bg-accent")}
          >
            <Settings aria-hidden="true" className="size-4" />
            Settings
          </a>
        )}
        {/* Help stays a link without an index: Start here is for the first run. */}
        <a
          href={sectionHash(HELP_SECTION)}
          aria-current={current === "help" ? "page" : undefined}
          data-testid="sidebar-help"
          aria-keyshortcuts="?"
          title="Help: start here, the sections, questions and keyboard shortcuts (?)"
          className={cn(item, current === "help" ? "bg-brand-soft font-medium text-brand-text" : "hover:bg-accent")}
        >
          <CircleHelp aria-hidden="true" className="size-4" />
          Help
          <kbd aria-hidden="true" className="ml-auto rounded-[4px] border bg-muted px-1 font-mono text-[11px] leading-4 text-muted-foreground">
            ?
          </kbd>
        </a>
      </div>
      <div className="grid gap-0.5 border-t px-2 pt-2.5 pb-1 text-xs text-muted-foreground" data-testid="sidebar-versions">
        {/* Versions come from an index's status; without one (first run)
            nothing is claimed. */}
        {!noIndex && (status.kind === "loading" || status.kind === "checking") && <span className="sr-only">Checking the server</span>}
        {!noIndex && (
          <>
            <span>
              Extractors <FooterValue status={status} pick={(v) => v.extractor.current} />
            </span>
            <span>
              Server <FooterValue status={status} pick={(v) => v.server_version} />
            </span>
          </>
        )}
        <a href="/third-party-licenses.txt" className="w-fit underline-offset-4 hover:text-foreground hover:underline">
          Third-party licences
        </a>
      </div>
    </nav>
  );
}
