// The empty and refusal states of a comparison (T019b brief item 7, V3
// mockup): one pure function from the error (or the ref job's failure)
// to the page, so the chip, the section and their tests read the same
// words. The server's error code decides; a code this UI does not know is
// shown with its message, never as "nothing changed".

import { ApiError } from "@/api/client";
import type { ChangeRun, Retention } from "@/api/changes";

/** Action buttons a refusal page offers. */
export type RefusalAction = "pick" | "branch" | "retry" | "previous";

export interface Refusal {
  /** The server's code, or "first_run" for the first run's base_unknown. */
  code: string;
  title: string;
  body: string;
  /** The chip's short words ("expired", "unknown", ...). */
  chip: string;
  actions: RefusalAction[];
}

export interface RefusalContext {
  base: string;
  baseText: string;
  runs: readonly ChangeRun[] | null;
  retention: Retention | null;
}

/** refusalOf maps a failed summary request (or a failed ref job's code
 * and message) to the refusal page. It offers only actions that can work
 * (T019 Revision 5, R5-6): no "previous run" while the journal holds one
 * run, no branch comparison when git is not there (git_unavailable) or the
 * latest run recorded no commit (made outside git). */
export function refusalOf(err: Error | { code: string; message: string }, ctx: RefusalContext): Refusal {
  const r = refusalPage(err, ctx);
  const firstRun = ctx.runs !== null && ctx.runs.length <= 1;
  const noGit = r.code === "git_unavailable" || (ctx.runs !== null && ctx.runs[0] !== undefined && ctx.runs[0].commit === null);
  const actions = r.actions.filter((a) => !(a === "previous" && firstRun) && !(a === "branch" && noGit));
  if (r.code === "first_run" && noGit) {
    return { ...r, actions, body: "The change journal starts with the next index run: when Graphvein re-indexes, it keeps the old state of every fact that changed. This index has one run, so there is nothing to compare against yet. Re-index from the top bar." };
  }
  return { ...r, actions };
}

function refusalPage(err: Error | { code: string; message: string }, ctx: RefusalContext): Refusal {
  const code = err instanceof ApiError ? (err.code ?? (err.kind === "http" ? `http_${String(err.status ?? "")}` : err.kind)) : "code" in err ? err.code : "error";
  const message = err.message;
  switch (code) {
    case "base_unknown":
      if (ctx.base === "previous" && (ctx.runs === null || ctx.runs.length <= 1)) {
        return {
          code: "first_run",
          title: "No comparison yet",
          body: "The change journal starts with the next index run: when Graphvein re-indexes, it keeps the old state of every fact that changed. This index has one run, so there is nothing to compare against yet. Re-index from the top bar, or compare with a branch.",
          chip: "no base yet",
          actions: ["branch"],
        };
      }
      return {
        code,
        title: "Base not known",
        body: `${ctx.baseText} names no run, pin or journal entry this index holds (a deleted pin reads the same). Pick another base.`,
        chip: "unknown",
        actions: ["pick", "previous"],
      };
    case "base_expired": {
      const keep = ctx.retention === null ? "" : ` (the journal keeps ${String(ctx.retention.journal_runs)} runs)`;
      return {
        code,
        title: "Base run no longer covered",
        body: `${ctx.baseText} is older than the journal or pin overlay still covers${keep}. Graphvein does not answer a comparison partially. Pick a newer run, a pin, or compare against a branch.`,
        chip: "expired",
        actions: ["pick", "branch"],
      };
    }
    case "reindex_needed":
      return {
        code,
        title: "Re-index before comparing branches",
        body: "The current index was built by another extractor version than this Graphvein, or it recorded no commit (made outside git). A branch comparison would report extractor differences as changes. Re-index the working tree first, from the top bar.",
        chip: "re-index needed",
        actions: ["pick"],
      };
    case "git_unavailable":
      return {
        code,
        title: "git not found",
        body: "Branch comparisons read refs with your local git, and git is not installed for the server or the root is not a git repository. Run and pin comparisons still work.",
        chip: "git not found",
        actions: ["previous", "pick"],
      };
    case "ref_unknown":
      return {
        code,
        title: "Ref not known or not indexed",
        body: `${ctx.baseText}: ${message}. A branch comparison needs both commits indexed first; pick Branch in the compare menu to index them.`,
        chip: "ref unknown",
        actions: ["branch", "previous"],
      };
    case "ref_objects_missing":
      return {
        code,
        title: "Objects for the ref are missing",
        body: "The commit resolves, but this clone does not have all its objects (a partial or shallow clone). Graphvein never fetches: run git fetch yourself, then try again.",
        chip: "objects missing",
        actions: ["retry", "previous"],
      };
    case "index_changed":
      return {
        code,
        title: "The index changed while loading",
        body: "An index run committed during the comparison, twice. Retry when indexing has finished.",
        chip: "index changed",
        actions: ["retry"],
      };
    default:
      return { code, title: "Could not load the comparison", body: message, chip: "failed", actions: ["retry", "pick"] };
  }
}
