// Project mode in the shell (T022 B9; P8: one project per server, with a
// repository filter). ProjectRoot asks the server whether it serves a
// project; a single-repository server renders the shell exactly as before.
// Under `serve --project` the selected member is named on every request
// (api/client.ts withRepository), and the whole shell (sidebar included)
// remounts when it changes, so no section shows one member's data
// under another member's name.
//
// The selection is never silent: without a stored choice the first member
// is selected and the sidebar's member list says so beside it ("shown by
// default: choose a repository", ProjectMembers.tsx).

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import { setRequestRepository } from "@/api/client";
import { fetchProject, type Project, type ProjectFetcher } from "@/api/project";
import { readLocation, writeLocation } from "@/shell/workspace/location";
import { useWorkspace } from "@/shell/workspace/WorkspaceRoot";

/** REPOSITORY_KEY prefixes the localStorage key of the chosen member, per
 * project name (localStorage is per server address already). */
export const REPOSITORY_KEY = "graphvein.repository.v1";

function storageKey(project: string): string {
  return `${REPOSITORY_KEY}.${project}`;
}

/** readChoice returns the stored member of project when it is still a
 * member; anything else reads as no choice. Blocked storage too. */
export function readChoice(project: Project): string | null {
  try {
    const v = window.localStorage.getItem(storageKey(project.project));
    return v !== null && project.members.some((m) => m.repository === v) ? v : null;
  } catch {
    return null;
  }
}

function writeChoice(project: string, repository: string): void {
  try {
    window.localStorage.setItem(storageKey(project), repository);
  } catch {
    // Blocked storage: the choice lasts for this page.
  }
}

/** PROJECT_POLL_MS is how often the project document is asked again, the
 * status's cadence. */
export const PROJECT_POLL_MS = 30_000;

/** ProjectState is what the shell knows in project mode. */
export interface ProjectState {
  project: Project;
  /** The member every request names. */
  repository: string;
  /** False while the first member is shown by default (no choice made). */
  explicit: boolean;
  select: (repository: string) => void;
  /** Requests /api/v0/project again (after a re-index, or on demand). */
  refresh: () => void;
}

const ProjectContext = createContext<ProjectState | null>(null);

/** useProject is the project state, or null on a single-repository
 * server (and in component tests). */
export function useProject(): ProjectState | null {
  return useContext(ProjectContext);
}

/** ProjectProvider holds the project document and the selected member. It
 * sets the request repository during render, before any section below it
 * renders and requests. */
export function ProjectProvider({ initial, fetcher = fetchProject, children }: { initial: Project; fetcher?: ProjectFetcher; children: (repository: string) => ReactNode }) {
  const [project, setProject] = useState(initial);
  // A member named by the page URL (a copied link or a workspace switch,
  // T038) wins over the stored choice.
  // Only under a workspace list does the page URL carry the member.
  const inWorkspace = useWorkspace() !== null;
  const [choice, setChoice] = useState<string | null>(() => {
    const named = inWorkspace ? readLocation().member : null;
    return named !== null && initial.members.some((m) => m.repository === named) ? named : readChoice(initial);
  });
  const [tick, setTick] = useState(0);
  const first = project.members[0]?.repository ?? "";
  const repository = choice !== null && project.members.some((m) => m.repository === choice) ? choice : first;
  // Every request below names this member (api/client.ts).
  setRequestRepository(repository);
  // The URL names the shown member, so a copied link opens it (WS-50).
  useEffect(() => {
    if (!inWorkspace) return;
    const loc = readLocation();
    if (loc.member !== repository) writeLocation({ ws: loc.ws, member: repository }, window.location.hash, "replace");
  }, [repository, inWorkspace]);
  useEffect(() => {
    if (tick === 0) return;
    const controller = new AbortController();
    fetcher({ signal: controller.signal })
      .then((probe) => {
        if (probe.kind === "project") setProject(probe.project);
      })
      .catch(() => {
        // The member list keeps the last answer; the sections report failures.
      });
    return () => {
      controller.abort();
    };
  }, [tick, fetcher]);
  // Freshness and the link pass change with re-indexes (this server's or
  // another process's): ask again at the status cadence while the tab is
  // visible, and when it becomes visible.
  useEffect(() => {
    const tickIfVisible = () => {
      if (document.visibilityState === "visible") setTick((t) => t + 1);
    };
    const timer = window.setInterval(tickIfVisible, PROJECT_POLL_MS);
    document.addEventListener("visibilitychange", tickIfVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", tickIfVisible);
    };
  }, []);
  const select = useCallback(
    (name: string) => {
      writeChoice(project.project, name);
      setChoice(name);
    },
    [project.project],
  );
  const refresh = useCallback(() => {
    setTick((t) => t + 1);
  }, []);
  const value = useMemo<ProjectState>(() => ({ project, repository, explicit: choice === repository, select, refresh }), [project, repository, choice, select, refresh]);
  return <ProjectContext.Provider value={value}>{children(repository)}</ProjectContext.Provider>;
}

/** ProjectRoot is the page: it asks whether the server serves a project
 * and renders shell(null) for a single-repository server (or when the
 * question cannot be answered: the shell's own status request then shows
 * the failure), or the project provider around shell(repository). */
export function ProjectRoot({
  fetcher = fetchProject,
  shell,
  single = false,
  placeholder,
}: {
  fetcher?: ProjectFetcher;
  shell: (repository: string | null) => ReactNode;
  /** single: the workspace list already says the index is a repository,
   * so the shell renders at once without asking (T038). */
  single?: boolean;
  /** placeholder is shown while the question is open (the workspace's
   * skeleton shell); a blank page otherwise. */
  placeholder?: ReactNode;
}) {
  const [probe, setProbe] = useState<{ kind: "loading" } | { kind: "single" } | { kind: "project"; project: Project }>(single ? { kind: "single" } : { kind: "loading" });
  useEffect(() => {
    if (single) return;
    const controller = new AbortController();
    fetcher({ signal: controller.signal })
      .then((p) => {
        setProbe(p);
      })
      .catch(() => {
        if (!controller.signal.aborted) setProbe({ kind: "single" });
      });
    return () => {
      controller.abort();
    };
  }, [fetcher, single]);
  if (probe.kind === "loading") return placeholder ?? <div aria-busy="true" className="h-screen bg-background" />;
  if (probe.kind === "single") {
    setRequestRepository(null);
    return shell(null);
  }
  if (probe.project.members.length === 0) {
    return <p className="p-6 text-sm">Project {probe.project.project} has no members.</p>;
  }
  return (
    <ProjectProvider initial={probe.project} fetcher={fetcher}>
      {(repository) => shell(repository)}
    </ProjectProvider>
  );
}
