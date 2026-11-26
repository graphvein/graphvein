// Add a folder (T038 S1-S10; ADR 0041 revision 1): a typed path is checked
// by the server (POST /api/v0/workspace/check reads only metadata, the
// configuration files and the folder's store), confirmed by the user with
// what would happen ("Index <path>? Graphvein will read every file in
// it."), then added (POST /api/v0/workspace/add with the one-time confirm
// value), which starts its run. Progress comes from the workspace list's
// entry job (the run's own counts, no estimate) and shows on the entry's
// row too; the dialog can be closed while it runs. When the run commits
// the dialog offers "Open" and never switches by itself.
//
// Claim rows WS-44 (progress), WS-45 (confirmation, with the registered
// entries a folder holds), WS-46 (refusals), WS-55 (Index it for a
// registered folder without an index), WS-56 (added, not indexed).

import { Check, Circle, ClipboardCopy, FolderPlus, LoaderCircle, SquareTerminal, TriangleAlert } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { ApiError } from "@/api/client";
import { PHASES, type Job } from "@/api/indexRun";
import type { CheckResult, WorkspaceEntry } from "@/api/workspace";
import { Button } from "@/components/ui/button";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { Modal } from "@/sections/explorer/tryit/Modal";
import { useWorkspace, type WorkspaceState } from "@/shell/workspace/WorkspaceRoot";
import { errorWords, jobProgress, jobRunning, needsIndex, phaseWord } from "@/shell/workspace/words";

/** INDEX_COMMAND is the generic terminal command shown before a path is
 * typed. */
export const INDEX_COMMAND = "graphvein index <folder>";

/** quotePath quotes a path for bash and Git Bash, as the server's
 * `command` does. */
export function quotePath(path: string): string {
  return `'${path.replaceAll("'", `'\\''`)}'`;
}

/** REFUSAL_HEADINGS words each check refusal (WS-46); an unknown code is
 * shown with the server's code (WS-69). */
export const REFUSAL_HEADINGS: Record<string, string> = {
  path_not_absolute: "Type the full path of the folder",
  path_not_found: "No folder at this path",
  not_a_directory: "This path is a file, not a folder",
  filesystem_root: "The filesystem root cannot be added",
  home_directory: "Your home folder cannot be added",
  config_directory: "A configuration folder cannot be added",
  inside_index_store: "This path is inside an index or git folder",
  inside_registered: "This folder is inside a folder already in the workspace",
  invalid_manifest: "graphvein-project.yaml in this folder does not load",
  invalid_config: "graphvein.yaml in this folder does not load",
  path_unreadable: "The path cannot be read",
  registry_error: "The workspace registry cannot be read",
};

export function refusalHeading(code: string): string {
  return REFUSAL_HEADINGS[code] ?? `Refused: ${code}`;
}

type Step =
  | { kind: "path" }
  | { kind: "checking"; path: string }
  | { kind: "checked"; check: CheckResult }
  | { kind: "adding"; check: CheckResult }
  | { kind: "running"; id: string; name: string; started: Job | null }
  | { kind: "error"; path: string; error: ApiError | Error; during: "check" | "add" }
  // The entry is in the workspace but its run did not start (WS-56): an
  // add answered with run_error, or a run request refused.
  | { kind: "notStarted"; id: string; name: string; code: string; message: string; added: boolean };

function copyText(text: string, done: (ok: boolean) => void): void {
  try {
    navigator.clipboard.writeText(text).then(
      () => {
        done(true);
      },
      () => {
        done(false);
      },
    );
  } catch {
    done(false);
  }
}

/** CommandLine shows a terminal command with a Copy button. */
export function CommandLine({ command }: { command: string }) {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  return (
    <div className="flex min-h-10 items-center gap-2 rounded-md border bg-muted/60 px-2.5 py-1.5" data-testid="index-command">
      <SquareTerminal aria-hidden="true" className="size-4 flex-none text-muted-foreground" />
      <code className="min-w-0 flex-1 font-mono text-[12.5px] [overflow-wrap:anywhere] select-all">{command}</code>
      <Button
        size="xs"
        variant="outline"
        onClick={() => {
          copyText(command, (ok) => {
            setState(ok ? "done" : "failed");
          });
        }}
        aria-label="Copy the command"
      >
        {state === "done" ? <Check aria-hidden="true" /> : <ClipboardCopy aria-hidden="true" />}
        {state === "done" ? "Copied" : state === "failed" ? "Select and copy" : "Copy"}
      </Button>
    </div>
  );
}

/** PathForm is the path step (dialog S1 and the first-run card S11). */
export function PathForm({ initial = "", onSubmit, busy = false, autoFocus = true, footer }: { initial?: string; onSubmit: (path: string) => void; busy?: boolean; autoFocus?: boolean; footer?: (submit: ReactNode) => ReactNode }) {
  const [path, setPath] = useState(initial);
  const trimmed = path.trim();
  const submit = (
    <Button type="submit" disabled={trimmed === "" || busy} data-testid="add-continue">
      {busy && <LoaderCircle aria-hidden="true" className="animate-spin" />}
      {busy ? "Checking…" : "Continue"}
    </Button>
  );
  return (
    <form
      className="grid gap-3"
      onSubmit={(ev) => {
        ev.preventDefault();
        if (trimmed !== "" && !busy) onSubmit(trimmed);
      }}
    >
      <label className="grid gap-1.5">
        <span className="text-[13px] font-medium">Folder path on this machine</span>
        <input
          type="text"
          value={path}
          onChange={(ev) => {
            setPath(ev.target.value);
          }}
          autoFocus={autoFocus}
          spellCheck={false}
          autoComplete="off"
          placeholder="/home/you/src/my-app"
          className="h-10 rounded-md border bg-background px-3 font-mono text-[13px] outline-none focus-visible:ring-2 focus-visible:ring-ring"
          data-testid="add-path"
        />
        <span className="text-[12px] leading-snug text-muted-foreground">
          The full path of a repository folder, or of a project folder that holds <code className="font-mono">graphvein-project.yaml</code>. Graphvein checks the path, then asks you to confirm before it reads anything.
        </span>
      </label>
      <div className="grid gap-1.5">
        <span className="text-[12px] text-muted-foreground">Or run it in a terminal; the folder then appears here once served:</span>
        <CommandLine command={trimmed === "" ? INDEX_COMMAND : `graphvein index ${quotePath(trimmed)}`} />
      </div>
      {footer === undefined ? <div className="flex justify-end">{submit}</div> : footer(submit)}
    </form>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="m-0 min-w-0 [overflow-wrap:anywhere]">{children}</dd>
    </>
  );
}

/** indexWords words what the folder's store holds (WS-45). */
export function indexWords(index: CheckResult["index"]): string {
  if (index === null) return "not read";
  if (index.state === "present") return index.last_run === null ? "an index is stored; this run replaces it" : `an index from ${formatShortTime(index.last_run)} is stored; this run replaces it`;
  if (index.state === "absent") return "no index yet";
  if (index.state === "unknown") return `could not be read${index.error !== null ? `: ${index.error}` : ""}`;
  return index.state;
}

/** storeWords says where the index is written (WS-45): the SQLite path,
 * or Postgres named by its variable, never the URL. */
export function storeWords(store: CheckResult["store"]): ReactNode {
  if (store === null) return "not reported";
  if (store.kind === "sqlite") return <code className="font-mono">{store.path ?? "the folder's .graphvein/index.db"}</code>;
  if (store.kind === "postgres") return <>Postgres from <code className="font-mono">{store.env ?? "GRAPHVEIN_POSTGRES_URL"}</code></>;
  return store.kind;
}

/** Confirmation is step 2 (S2, S10): what the check found. */
export function Confirmation({ check }: { check: CheckResult }) {
  const project = check.kind === "project";
  return (
    <div className="grid gap-3" data-testid="add-confirm">
      <p className="m-0 text-[13.5px]">
        <b className="font-semibold">Graphvein will read every file in it.</b> Files your <code className="font-mono">.gitignore</code> and <code className="font-mono">.graphveinignore</code> exclude are skipped.
      </p>
      <dl className="m-0 grid grid-cols-[150px_1fr] gap-x-4 gap-y-1.5 text-[13px]">
        <Row label={project ? "Project name" : "Repository name"}>
          <b className="font-semibold">{check.name}</b>
          {project && <span className="text-muted-foreground"> (from graphvein-project.yaml)</span>}
        </Row>
        <Row label="Folder">
          <code className="font-mono">{check.resolved}</code>
          {check.resolved !== check.path && <span className="block text-[12px] text-muted-foreground">typed as {check.path}</span>}
        </Row>
        {project && (
          <Row label="Members">
            <ul className="m-0 grid gap-0.5 p-0" data-testid="add-members">
              {(check.members ?? []).map((m) => (
                <li key={m.repository} className="list-none">
                  <b className="font-medium">{m.repository}</b> <code className="font-mono text-[12px]">{m.path ?? "(no path)"}</code>
                  {!m.checkout && <span className="text-dynamic"> · no checkout at this path: not indexed</span>}
                </li>
              ))}
            </ul>
          </Row>
        )}
        <Row label="Index written to">{project ? "each member's own index, as graphvein index writes it" : storeWords(check.store)}</Row>
        <Row label="Stored index">{indexWords(check.index)}</Row>
        <Row label="Files">counted by the scan when it starts; no estimate before</Row>
      </dl>
      {check.contains.length > 0 && (
        <div className="grid gap-1 rounded-md border border-dynamic/40 bg-dynamic/5 px-3 py-2 text-[13px]" data-testid="add-contains">
          <p className="m-0 font-medium">
            This folder holds {check.contains.length === 1 ? "an entry" : `${String(check.contains.length)} entries`} already in the workspace:
          </p>
          <ul className="m-0 grid gap-0.5 p-0">
            {check.contains.map((e) => (
              <li key={e.id} className="list-none">
                <b className="font-medium">{e.name}</b> <span className="text-muted-foreground">({e.kind === "project" ? "project" : e.kind === "repository" ? "repository" : e.kind})</span> <code className="font-mono text-[12px]">{e.path}</code>
              </li>
            ))}
          </ul>
          <p className="m-0 text-[12.5px] text-muted-foreground">Its index reads their files again into an index of its own; their entries and indexes stay as they are.</p>
        </div>
      )}
      <p className="m-0 text-[12.5px] text-muted-foreground">
        Graphvein never changes your files; it only adds its <code className="font-mono">.graphvein</code> folder. Nothing is sent over the network.
      </p>
    </div>
  );
}

/** Phases is the run's progress (S3): the phases passed, the current one
 * with the run's own counts, the rest ahead; no estimate (WS-44). */
export function Phases({ job }: { job: Job }) {
  const known = (PHASES as readonly string[]).includes(job.phase ?? "starting");
  const at = known ? (PHASES as readonly string[]).indexOf(job.phase ?? "starting") : -1;
  const done = !jobRunning(job);
  const p = jobProgress(job);
  return (
    <div className="grid gap-1.5" data-testid="add-phases">
      {!known && (
        <p className="m-0 text-[13px]" data-testid="add-phase-unknown">
          <LoaderCircle aria-hidden="true" className="mr-1.5 inline size-3.5 animate-spin text-brand" />
          {phaseWord(job.phase)}
        </p>
      )}
      {known && (
        <ol className="m-0 grid gap-1 p-0">
          {PHASES.filter((ph) => ph !== "starting" && ph !== "finishing").map((ph) => {
            const i = (PHASES as readonly string[]).indexOf(ph);
            const state = done && job.state === "committed" ? "done" : i < at ? "done" : i === at ? "now" : "ahead";
            return (
              <li key={ph} className={cn("grid list-none grid-cols-[20px_110px_1fr] items-center gap-2 text-[13px]", state === "ahead" && "text-muted-foreground")} data-phase={ph} data-state={state}>
                {state === "done" ? <Check aria-hidden="true" className="size-4 text-linked" /> : state === "now" ? <LoaderCircle aria-hidden="true" className="size-4 animate-spin text-brand" /> : <Circle aria-hidden="true" className="size-3 justify-self-center opacity-40" />}
                <span>{phaseWord(ph)}</span>
                {state === "now" && p.fraction !== null && (
                  <span className="flex items-center gap-2">
                    <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-accent">
                      <span className="block h-full rounded-full bg-brand" style={{ width: `${String(Math.round(p.fraction * 100))}%` }} />
                    </span>
                    <span className="font-mono text-[12px] tabular-nums" data-testid="add-count">
                      {job.progress?.done} of {job.progress?.total} {job.progress?.unit}
                    </span>
                  </span>
                )}
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}

function Footer({ left, children }: { left?: ReactNode; children: ReactNode }) {
  return (
    <div className="-mx-4 -mb-3 flex min-h-[60px] items-center gap-2 border-t px-4 py-3">
      <div className="min-w-0 flex-1 text-[12.5px] text-muted-foreground">{left}</div>
      {children}
    </div>
  );
}

/** findEntry is the list's entry for an id. */
function findEntry(state: WorkspaceState, id: string): WorkspaceEntry | undefined {
  return state.workspace.entries.find((e) => e.id === id);
}

/** AddFolderDialog is the dialog; it renders while state.addRequest is
 * set (the root keeps it open across a switch). */
export function AddFolderDialog() {
  const state = useWorkspace();
  if (state === null || state.addRequest === null) return null;
  return <AddFlow state={state} initialPath={state.addRequest.path ?? ""} />;
}

function AddFlow({ state, initialPath }: { state: WorkspaceState; initialPath: string }) {
  const [step, setStep] = useState<Step>(initialPath === "" ? { kind: "path" } : { kind: "checking", path: initialPath });
  const [copied, setCopied] = useState<"idle" | "done" | "failed">("idle");
  const started = useRef(false);
  const { client, workspace } = state;

  const runCheck = (path: string) => {
    setStep({ kind: "checking", path });
    client.check(path, workspace.token).then(
      (check) => {
        setStep({ kind: "checked", check });
      },
      (err: unknown) => {
        setStep({ kind: "error", path, error: err instanceof Error ? err : new Error(String(err)), during: "check" });
      },
    );
  };
  // A path handed over by the first-run card is checked at once.
  useEffect(() => {
    if (started.current || initialPath === "") return;
    started.current = true;
    runCheck(initialPath);
    // runCheck is stable for the dialog's life.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialPath]);

  const add = (check: CheckResult) => {
    if (check.resolved === null || check.confirm === null) return;
    setStep({ kind: "adding", check });
    client.add(check.resolved, check.confirm, workspace.token).then(
      (res) => {
        state.watch(res.entry.id);
        if (res.runError !== null) {
          setStep({ kind: "notStarted", id: res.entry.id, name: res.entry.name, code: res.runError.code, message: res.runError.message, added: true });
          return;
        }
        setStep({ kind: "running", id: res.entry.id, name: res.entry.name, started: res.job });
      },
      (err: unknown) => {
        setStep({ kind: "error", path: check.path, error: err instanceof Error ? err : new Error(String(err)), during: "add" });
      },
    );
  };
  // reindex starts the run of an entry already in the workspace; a
  // refusal says the entry stays and was not indexed, never that it was
  // not added (T038 review).
  const reindex = (id: string, name: string, added = false) => {
    client.run(id).then(
      () => {
        state.watch(id);
        setStep({ kind: "running", id, name, started: null });
      },
      (err: unknown) => {
        const e = errorWords(err instanceof Error ? err : new Error(String(err)));
        setStep({ kind: "notStarted", id, name, code: e.code, message: e.message, added });
      },
    );
  };
  const open = (id: string) => {
    state.closeAdd();
    state.switchTo(id);
  };
  const close = state.closeAdd;

  let title: ReactNode = "Add a folder";
  let body: ReactNode;
  let footer: ReactNode;
  switch (step.kind) {
    case "path":
    case "checking":
      body = (
        <PathForm
          initial={step.kind === "checking" ? step.path : ""}
          busy={step.kind === "checking"}
          onSubmit={runCheck}
          footer={(submit) => (
            <Footer>
              <Button variant="outline" onClick={close}>
                Cancel
              </Button>
              {submit}
            </Footer>
          )}
        />
      );
      footer = null;
      break;
    case "checked": {
      const c = step.check;
      if (c.outcome === "ready") {
        title = (
          <>
            Index <code className="font-mono [overflow-wrap:anywhere]">{c.resolved}</code>?
          </>
        );
        body = <Confirmation check={c} />;
        footer = (
          <Footer
            left={
              c.command !== null && (
                <button
                  type="button"
                  className="inline-flex items-center gap-1.5 font-medium text-brand-text hover:underline"
                  onClick={() => {
                    copyText(c.command ?? "", (ok) => {
                      setCopied(ok ? "done" : "failed");
                    });
                  }}
                  data-testid="add-copy-command"
                >
                  <SquareTerminal aria-hidden="true" className="size-4" />
                  {copied === "done" ? "Command copied" : copied === "failed" ? `Copy by hand: ${c.command}` : "Copy the command instead"}
                </button>
              )
            }
          >
            <Button variant="outline" onClick={close} data-autofocus="">
              Cancel
            </Button>
            <Button
              onClick={() => {
                add(c);
              }}
              data-testid="add-index"
            >
              Index {c.name}
            </Button>
          </Footer>
        );
      } else if (c.outcome === "registered" && c.entry !== null) {
        const id = c.entry;
        const entry = findEntry(state, id);
        const empty = entry !== undefined && needsIndex(entry);
        title = "Already in the workspace";
        body = (
          <p className="m-0" data-testid="add-registered">
            <code className="font-mono">{c.resolved}</code> is already in the workspace as <b className="font-semibold">{entry?.name ?? c.name ?? id}</b>
            {empty ? ", not indexed yet" : ""}. Nothing was read.
          </p>
        );
        footer = (
          <Footer left={c.command !== null ? <code className="font-mono text-[12px]">{c.command}</code> : null}>
            <Button
              variant="outline"
              onClick={() => {
                reindex(id, entry?.name ?? id);
              }}
              data-testid="add-reindex"
            >
              {empty ? "Index it" : "Re-index it"}
            </Button>
            <Button
              onClick={() => {
                open(id);
              }}
              data-autofocus=""
            >
              Open it
            </Button>
          </Footer>
        );
      } else {
        const r = c.refusal;
        title = "Add a folder";
        body = (
          <div className="grid gap-2" data-testid="add-refused" data-code={r?.code ?? c.outcome}>
            <p className="m-0 flex items-start gap-2 font-semibold text-unmatched">
              <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 flex-none" />
              {r !== null ? refusalHeading(r.code) : `Refused: ${c.outcome}`}
            </p>
            {r !== null && <p className="m-0 text-[13px]">{r.message}</p>}
            <p className="m-0 font-mono text-[12px] text-muted-foreground [overflow-wrap:anywhere]">{c.path}</p>
            <p className="m-0 text-[12.5px] text-muted-foreground">Nothing was read or indexed.</p>
          </div>
        );
        const inside = r?.code === "inside_registered" && r.entry !== null ? r.entry : null;
        footer = (
          <Footer>
            {inside !== null && (
              <Button
                variant="outline"
                onClick={() => {
                  open(inside);
                }}
              >
                Open {findEntry(state, inside)?.name ?? inside}
              </Button>
            )}
            <Button
              onClick={() => {
                setStep({ kind: "path" });
              }}
              data-autofocus=""
            >
              Change the path
            </Button>
          </Footer>
        );
      }
      break;
    }
    case "adding":
      title = (
        <>
          Index <code className="font-mono">{step.check.resolved}</code>?
        </>
      );
      body = <Confirmation check={step.check} />;
      footer = (
        <Footer>
          <Button disabled>
            <LoaderCircle aria-hidden="true" className="animate-spin" />
            Starting…
          </Button>
        </Footer>
      );
      break;
    case "running": {
      const entry = findEntry(state, step.id);
      const job = entry?.job ?? step.started;
      const ended = job !== null && !jobRunning(job);
      title = ended ? (job.state === "committed" ? `Indexed ${step.name}` : `Indexing ${step.name} did not finish`) : `Indexing ${step.name}`;
      body = (
        <div className="grid gap-3" data-testid="add-progress">
          {job === null ? (
            <p className="m-0 text-[13px]">
              <LoaderCircle aria-hidden="true" className="mr-1.5 inline size-3.5 animate-spin text-brand" />
              Waiting for the run to report…
            </p>
          ) : (
            <Phases job={job} />
          )}
          {job !== null && job.state === "failed" && (
            <p className="m-0 text-[13px] text-unmatched" data-testid="add-failed">
              {job.error?.code ?? "failed"}: {job.error?.message ?? "the run failed"}
            </p>
          )}
          {job !== null && job.state === "committed" && (
            <p className="m-0 text-[13px]" data-testid="add-done">
              The run committed{job.run !== null ? ` at ${formatShortTime(job.run.finished_at)}` : ""}. {step.name} is in the workspace; open it to see its facts.
            </p>
          )}
          {!ended && <p className="m-0 text-[12.5px] text-muted-foreground">Counts come from the run itself; there is no time estimate. You can close this window: indexing goes on and the Workspace list shows its progress. Until the run commits, nothing of {step.name} is shown.</p>}
        </div>
      );
      footer = ended ? (
        <Footer>
          <Button variant="outline" onClick={close}>
            Close
          </Button>
          {job.state === "committed" && (
            <Button
              onClick={() => {
                open(step.id);
              }}
              data-autofocus=""
              data-testid="add-open"
            >
              Open {step.name}
            </Button>
          )}
        </Footer>
      ) : (
        <Footer left="There is no cancel; stopping graphvein serve stops the run.">
          <Button variant="outline" onClick={close} data-autofocus="">
            Close, keep indexing
          </Button>
        </Footer>
      );
      break;
    }
    case "notStarted":
      title = step.added ? `Added ${step.name} to the workspace; indexing did not start` : `Indexing ${step.name} did not start`;
      body = (
        <div className="grid gap-2" data-testid="add-not-started" data-code={step.code}>
          <p className="m-0 flex items-start gap-2 font-semibold text-unmatched">
            <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 flex-none" />
            {step.code}
          </p>
          <p className="m-0 text-[13px]">{step.message}</p>
          <p className="m-0 text-[12.5px] text-muted-foreground">
            {step.name} is in the workspace; nothing of it is indexed yet. Try again now, or later with Index it on its row in the Workspace list.
          </p>
        </div>
      );
      footer = (
        <Footer>
          <Button variant="outline" onClick={close}>
            Close
          </Button>
          <Button
            onClick={() => {
              reindex(step.id, step.name, step.added);
            }}
            data-autofocus=""
            data-testid="add-retry"
          >
            Try again
          </Button>
        </Footer>
      );
      break;
    case "error": {
      const e = errorWords(step.error);
      const guard = step.error instanceof ApiError && step.error.status === 403;
      title = guard ? "Refused by the server" : step.during === "add" ? "The folder was not added" : "The path could not be checked";
      body = (
        <div className="grid gap-2" data-testid="add-error" data-code={e.code}>
          <p className="m-0 flex items-start gap-2 font-semibold text-unmatched">
            <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 flex-none" />
            {e.code}
          </p>
          <p className="m-0 text-[13px]">{e.message}</p>
          <p className="m-0 text-[12.5px] text-muted-foreground">
            {guard
              ? "The server accepts this request only from the page it served on this machine, with its session token. Reload the page and try again. Nothing was read."
              : e.code === "confirm_required" || e.code === "path_changed"
                ? "The confirmation expired or the folder changed since the check. Check the path again; nothing was indexed."
                : "Nothing was indexed."}
          </p>
        </div>
      );
      footer = (
        <Footer>
          <Button variant="outline" onClick={close}>
            Close
          </Button>
          <Button
            onClick={() => {
              runCheck(step.path);
            }}
            data-autofocus=""
          >
            Check again
          </Button>
        </Footer>
      );
      break;
    }
  }
  return (
    <Modal title={title} onClose={close} testId="add-dialog">
      <div className="min-h-[270px]">{body}</div>
      {footer}
    </Modal>
  );
}

/** FirstRunCard is the page without an index (S11, WS-51): the path step
 * and the command. helpHref renders the "New to Graphvein?" link only
 * when a Help page exists. */
export function FirstRunCard({ helpHref = null, choose = false }: { helpHref?: string | null; choose?: boolean }) {
  const state = useWorkspace();
  return (
    <div className="grid flex-1 place-items-center p-6" data-testid="first-run">
      <section className="grid w-[min(560px,100%)] gap-4 rounded-xl border bg-card p-6 shadow-sm">
        <div className="flex items-center gap-3">
          <span className="grid size-10 place-items-center rounded-lg bg-[linear-gradient(135deg,hsl(245_70%_58%),hsl(270_70%_60%))] text-white">
            <FolderPlus aria-hidden="true" className="size-5" />
          </span>
          <h1 className="m-0 text-xl font-semibold">{choose ? "Choose an index or add a folder" : "Add a folder"}</h1>
        </div>
        <p className="m-0 text-[14px] leading-relaxed text-muted-foreground">
          {choose
            ? "This server has no current index. Choose one in the Workspace list, or type a folder to index; nothing is read until you confirm, and nothing leaves this machine."
            : "Graphvein maps a repository on this machine, from its architecture down to the exact line. Type the folder to index; nothing is read until you confirm, and nothing leaves this machine."}
        </p>
        <PathForm
          autoFocus={false}
          onSubmit={(path) => {
            state?.openAdd({ path });
          }}
        />
        {helpHref !== null && (
          <a href={helpHref} className="text-[13px] font-medium text-brand-text hover:underline" data-testid="first-run-help">
            New to Graphvein? Start here
          </a>
        )}
      </section>
    </div>
  );
}
