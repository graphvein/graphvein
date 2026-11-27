// Settings' Storage section (T068 part 3; ADR 0088 revision, ADR 0093;
// design record T063 D5, mockups A4-A9): where this machine keeps
// indexes. It is outside graphvein.yaml and needs no index: GET
// /api/v0/store answers without one, so the page without a folder shows
// it too (MachineSettings). Rows: the viewed entry's index (path, place,
// size, Move…, Copy path), the folder new indexes go to (Change…), team
// mode (set or not, never the URL) and the workspace list file. Move and
// Change… are dialogs whose typed folder the server checks first
// (POST /api/v0/store/check); the action then sends the check's one-time
// confirm value. Nothing here is assumed: a value the answer does not
// carry is said to be not known.

import {
  ArrowRight,
  Check,
  ClipboardCopy,
  FolderPlus,
  Info,
  LoaderCircle,
  SquareTerminal,
  TriangleAlert,
} from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { ApiError } from "@/api/client";
import {
  dirSourceWords,
  formatBytes,
  type MoveResult,
  type StoreCheck,
  type StoreClient,
  type StoreEntry,
  type StoreSettings,
} from "@/api/store";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useStoreClient, useStoreSettings } from "@/hooks/useStoreSettings";
import { copyText } from "@/lib/clipboard";
import { cn } from "@/lib/utils";
import { Modal } from "@/sections/explorer/tryit/Modal";
import { PlaceChip } from "@/shell/workspace/StorePlace";
import { useWorkspace } from "@/shell/workspace/WorkspaceRoot";

/** STORAGE_ANCHOR is the section's anchor (#/settings?at=storage). */
export const STORAGE_ANCHOR = "settings-storage";

function Tag({
  children,
  tone = "muted",
}: {
  children: ReactNode;
  tone?: "muted" | "warn";
}) {
  return (
    <span
      className={cn(
        "inline-flex h-[22px] flex-none items-center rounded-md border px-1.5 text-[12px] font-medium whitespace-nowrap",
        tone === "warn"
          ? "border-dynamic/40 bg-dynamic-soft text-dynamic"
          : "border-transparent bg-muted text-muted-foreground",
      )}
    >
      {children}
    </span>
  );
}

function CopyPath({ path, testId }: { path: string; testId: string }) {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  return (
    <Button
      variant="ghost"
      size="sm"
      onClick={() => {
        void copyText(path).then((ok) => {
          setState(ok ? "done" : "failed");
        });
      }}
      data-testid={testId}
    >
      {state === "done" ? (
        <Check aria-hidden="true" />
      ) : (
        <ClipboardCopy aria-hidden="true" />
      )}
      {state === "done"
        ? "Copied"
        : state === "failed"
          ? "Select and copy"
          : "Copy path"}
    </Button>
  );
}

/** StorageRow is one row: what it is (name, where it is stored, help),
 * then its value. */
function StorageRow({
  title,
  source,
  help,
  children,
  testId,
}: {
  title: string;
  source: string;
  help: ReactNode;
  children: ReactNode;
  testId: string;
}) {
  return (
    <div
      className="grid grid-cols-[minmax(0,5fr)_minmax(0,7fr)] gap-x-6 gap-y-2 py-3.5 max-[1100px]:grid-cols-1"
      data-testid={testId}
    >
      <div className="grid content-start gap-0.5">
        <span className="text-[14px] font-medium">{title}</span>
        <code className="text-[12px] text-muted-foreground">{source}</code>
        <span className="text-[12.5px] text-muted-foreground">{help}</span>
      </div>
      <div className="grid min-w-0 content-start gap-2 text-[13px]">
        {children}
      </div>
    </div>
  );
}

/** StorageSection is the section. entryId is the viewed workspace entry
 * (its index row); null shows the machine rows only. */
export function StorageSection({
  entryId,
  client: given,
}: {
  entryId: string | null;
  client?: StoreClient;
}) {
  const fallback = useStoreClient();
  const client = given ?? fallback;
  const workspace = useWorkspace();
  const token = workspace?.workspace.token ?? null;
  const { view, reload } = useStoreSettings(client);
  const [dialog, setDialog] = useState<
    { kind: "move"; entry: StoreEntry } | { kind: "default" } | null
  >(null);
  return (
    <section
      id={STORAGE_ANCHOR}
      tabIndex={-1}
      aria-labelledby={`${STORAGE_ANCHOR}-h`}
      className="min-w-0 scroll-mt-4 rounded-lg border bg-card outline-none"
      data-testid={STORAGE_ANCHOR}
    >
      <header className="flex flex-wrap items-baseline gap-x-2.5 border-b px-4 py-2.5">
        <h2
          id={`${STORAGE_ANCHOR}-h`}
          className="m-0 text-[15px] font-semibold"
        >
          Storage
        </h2>
        <span className="text-[12.5px] text-muted-foreground">
          where indexes are kept on this machine
        </span>
        <span className="flex-1" />
        <Tag>this machine · all folders</Tag>
      </header>
      {view.kind === "loading" && (
        <div className="grid gap-2 p-4" data-testid="storage-loading">
          <Skeleton className="h-12" />
          <Skeleton className="h-12" />
        </div>
      )}
      {view.kind === "failed" && (
        <div
          role="alert"
          className="grid gap-2 p-4 text-[13px]"
          data-testid="storage-failed"
        >
          <p className="m-0 font-medium">
            Where indexes are kept could not be read: {view.error.message}
          </p>
          <div>
            <Button variant="outline" size="sm" onClick={reload}>
              Try again
            </Button>
          </div>
        </div>
      )}
      {view.kind === "ok" && (
        <StorageRows
          settings={view.settings}
          entryId={entryId}
          token={token}
          onMove={(entry) => {
            setDialog({ kind: "move", entry });
          }}
          onDefault={() => {
            setDialog({ kind: "default" });
          }}
        />
      )}
      {dialog !== null &&
        token !== null &&
        view.kind === "ok" &&
        (dialog.kind === "move" ? (
          <MoveDialog
            entry={dialog.entry}
            settings={view.settings}
            client={client}
            token={token}
            onClose={(moved) => {
              setDialog(null);
              if (moved) {
                reload();
                workspace?.refresh();
              }
            }}
          />
        ) : (
          <DefaultDialog
            settings={view.settings}
            client={client}
            token={token}
            onClose={(changed) => {
              setDialog(null);
              if (changed) reload();
            }}
          />
        ))}
    </section>
  );
}

function StorageRows({
  settings,
  entryId,
  token,
  onMove,
  onDefault,
}: {
  settings: StoreSettings;
  entryId: string | null;
  token: string | null;
  onMove: (e: StoreEntry) => void;
  onDefault: () => void;
}) {
  const entry =
    entryId === null
      ? undefined
      : settings.entries.find((e) => e.id === entryId);
  const dd = settings.default_dir;
  const others = settings.sqlite_path.set
    ? settings.entries
        .filter((e) => e.id !== settings.sqlite_path.entry)
        .map((e) => e.name)
    : [];
  const noToken =
    token === null
      ? "This server has no workspace session token; change it with graphvein store."
      : null;
  return (
    <div className="grid divide-y px-4">
      {entry !== undefined && (
        <IndexRow
          entry={entry}
          settings={settings}
          disabledReason={noToken}
          onMove={onMove}
        />
      )}
      <StorageRow
        title="New indexes are kept in"
        source="stores.yaml · default_dir"
        help="Used for every folder added from now on. Indexes that exist stay where they are."
        testId="storage-default"
      >
        {settings.team_mode.set ? (
          <span className="text-muted-foreground">
            Not used in team mode: every index is in the Postgres database.
          </span>
        ) : (
          <>
            <span className="flex min-w-0 flex-wrap items-center gap-2">
              <code
                className="font-mono [overflow-wrap:anywhere]"
                data-testid="storage-default-path"
              >
                {dd.path ?? "not known"}
              </code>
              <Tag>{dirSourceWords(dd.source)}</Tag>
            </span>
            {dd.error !== null && (
              <span className="text-[12.5px] text-unmatched">{dd.error}</span>
            )}
            <span className="flex flex-wrap items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={!dd.editable || noToken !== null}
                onClick={onDefault}
                data-testid="storage-default-change"
              >
                Change…
              </Button>
              {dd.path !== null && (
                <CopyPath path={dd.path} testId="storage-default-copy" />
              )}
            </span>
            {!dd.editable && dd.reason !== null && (
              <span className="text-[12.5px] text-muted-foreground">
                {dd.reason}
              </span>
            )}
            {others.length > 0 && (
              <span
                className="text-[12.5px] text-muted-foreground"
                data-testid="storage-others"
              >
                {others.join(", ")} open from their own indexes while{" "}
                {settings.sqlite_path.env} is set.
              </span>
            )}
          </>
        )}
      </StorageRow>
      <StorageRow
        title="Team mode"
        source={settings.team_mode.env}
        help={
          <>
            One Postgres database for every folder. Set in the environment of{" "}
            <code className="font-mono">graphvein serve</code>; never here.
          </>
        }
        testId="storage-team"
      >
        <span className="flex items-center gap-2">
          <b className="font-semibold">
            {settings.team_mode.set ? "set" : "not set"}
          </b>
          <Tag>environment</Tag>
        </span>
        <span className="text-[12.5px] text-muted-foreground">
          {settings.team_mode.set
            ? "Every index is in the database it names; its URL is never shown."
            : "Indexes are SQLite files on this machine."}
        </span>
      </StorageRow>
      <StorageRow
        title="Workspace list"
        source="workspace.yaml"
        help="The folders the sidebar lists. Read-only here."
        testId="storage-registry"
      >
        <code className="font-mono [overflow-wrap:anywhere]">
          {settings.registry.path ?? "not known"}
        </code>
        {settings.registry.error !== null && (
          <span className="text-[12.5px] text-unmatched">
            {settings.registry.error}
          </span>
        )}
        {settings.stores_file.error !== null && (
          <span
            className="text-[12.5px] text-unmatched"
            data-testid="storage-stores-file-error"
          >
            stores.yaml ({settings.stores_file.path ?? "not known"}):{" "}
            {settings.stores_file.error}
          </span>
        )}
      </StorageRow>
    </div>
  );
}

/** indexSource is the row's second line: what chose the index's place. */
function indexSource(entry: StoreEntry): string {
  const s = entry.store;
  if (s === null) return "store";
  if (s.origin === "sqlite_path_env" || s.origin === "postgres")
    return s.env ?? s.origin;
  if (s.origin === "stores_file") return "stores.yaml";
  return "store";
}

function IndexRow({
  entry,
  settings,
  disabledReason,
  onMove,
}: {
  entry: StoreEntry;
  settings: StoreSettings;
  disabledReason: string | null;
  onMove: (e: StoreEntry) => void;
}) {
  const s = entry.store;
  const inside = s?.place === "inside";
  const help =
    s?.origin === "sqlite_path_env"
      ? `Set in the environment of this server run, for the folder named on its command line (${entry.folder}).`
      : s?.kind === "postgres"
        ? "Team mode: the index is in the Postgres database of the environment."
        : "Where this index is. Moving it is an explicit action; Graphvein never moves an index on its own.";
  const reason = !entry.movable ? entry.reason : disabledReason;
  return (
    <StorageRow
      title={`Index of ${entry.name}`}
      source={indexSource(entry)}
      help={help}
      testId="storage-index"
    >
      {s === null ? (
        <span className="text-muted-foreground">
          {entry.reason ?? "not known"}
        </span>
      ) : (
        <>
          <span className="flex min-w-0 flex-wrap items-center gap-2">
            {s.kind === "postgres" ? (
              <span>
                Postgres from{" "}
                <code className="font-mono">
                  {s.env ?? settings.team_mode.env}
                </code>
              </span>
            ) : (
              <code
                className="font-mono [overflow-wrap:anywhere]"
                data-testid="storage-index-path"
              >
                {s.path ?? "not known"}
              </code>
            )}
          </span>
          <span className="flex flex-wrap items-center gap-2">
            <PlaceChip
              store={s}
              tone={s.place === "environment" ? "warn" : "muted"}
            />
            {s.size !== null && <Tag>{formatBytes(s.size)}</Tag>}
            {s.kind !== "postgres" && !s.exists && s.error_code === null && (
              <Tag>no index yet</Tag>
            )}
          </span>
          {s.error_code !== null && (
            <span
              className="flex items-start gap-1.5 text-[12.5px] text-unmatched"
              data-testid="storage-index-error"
              data-code={s.error_code}
            >
              <TriangleAlert
                aria-hidden="true"
                className="mt-0.5 size-3.5 flex-none"
              />
              {s.error ?? s.error_code}
            </span>
          )}
          {inside && (
            <span className="text-[12.5px] text-muted-foreground">
              Kept inside the folder: Git lists .graphvein/ as untracked unless
              the repository ignores it.
            </span>
          )}
          {s.kind !== "postgres" && (
            <span className="flex flex-wrap items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={!entry.movable || disabledReason !== null}
                onClick={() => {
                  onMove(entry);
                }}
                data-testid="storage-move"
              >
                <ArrowRight aria-hidden="true" />
                {inside ? "Move out of the folder…" : "Move…"}
              </Button>
              {s.path !== null && (
                <CopyPath path={s.path} testId="storage-index-copy" />
              )}
              {reason !== null && (
                <span
                  className="text-[12.5px] text-muted-foreground"
                  data-testid="storage-move-reason"
                >
                  {reason}
                </span>
              )}
            </span>
          )}
        </>
      )}
    </StorageRow>
  );
}

type CheckState =
  | { kind: "idle" }
  | { kind: "checking"; dir: string }
  | { kind: "done"; dir: string; check: StoreCheck }
  | { kind: "failed"; dir: string; message: string };

/** useDirCheck checks a typed folder when typing pauses and keeps only
 * the answer of the folder on screen. */
function useDirCheck(
  dir: string,
  run: (dir: string) => Promise<StoreCheck>,
  auto: boolean,
): { state: CheckState; now: () => void } {
  const [state, setState] = useState<CheckState>({ kind: "idle" });
  const seq = useRef(0);
  const start = (d: string) => {
    const n = ++seq.current;
    setState({ kind: "checking", dir: d });
    run(d).then(
      (check) => {
        if (n === seq.current) setState({ kind: "done", dir: d, check });
      },
      (err: unknown) => {
        if (n === seq.current)
          setState({
            kind: "failed",
            dir: d,
            message: err instanceof Error ? err.message : String(err),
          });
      },
    );
  };
  const first = useRef(true);
  useEffect(() => {
    const isFirst = first.current;
    first.current = false;
    if (isFirst && !auto) return;
    const timer = window.setTimeout(
      () => {
        start(dir);
      },
      isFirst ? 0 : 400,
    );
    return () => {
      window.clearTimeout(timer);
    };
    // start reads only its argument and stable setters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dir]);
  return {
    state,
    now: () => {
      start(dir);
    },
  };
}

function Verdict({
  state,
  dir,
  ready,
}: {
  state: CheckState;
  dir: string;
  ready: ReactNode;
}) {
  if (state.kind === "idle") return null;
  if (state.dir !== dir || state.kind === "checking") {
    return (
      <p
        className="m-0 flex items-center gap-1.5 text-[12.5px]"
        data-testid="storage-checking"
      >
        <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" />
        Checking…
      </p>
    );
  }
  if (state.kind === "failed") {
    return (
      <p
        className="m-0 flex items-start gap-1.5 text-[12.5px] text-unmatched"
        data-testid="storage-refused"
        data-code="request_failed"
      >
        <TriangleAlert
          aria-hidden="true"
          className="mt-0.5 size-3.5 flex-none"
        />
        The check failed: {state.message}
      </p>
    );
  }
  if (state.check.outcome === "ready") {
    return (
      <p
        className="m-0 flex items-start gap-1.5 text-[12.5px] text-linked"
        data-testid="storage-ready"
      >
        <Check aria-hidden="true" className="mt-0.5 size-3.5 flex-none" />
        <span className="min-w-0 [overflow-wrap:anywhere]">{ready}</span>
      </p>
    );
  }
  const r = state.check.refusal;
  return (
    <p
      className="m-0 flex items-start gap-1.5 text-[12.5px] text-unmatched"
      data-testid="storage-refused"
      data-code={r?.code ?? state.check.outcome}
    >
      <TriangleAlert aria-hidden="true" className="mt-0.5 size-3.5 flex-none" />
      <span className="min-w-0 [overflow-wrap:anywhere]">
        {r !== null ? r.message : `Refused: ${state.check.outcome}`}
      </span>
    </p>
  );
}

function DialogFooter({
  left,
  children,
}: {
  left?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="-mx-4 -mb-3 mt-1 flex min-h-[60px] items-center gap-2 border-t px-4 py-3">
      <div className="min-w-0 flex-1 text-[12.5px] text-muted-foreground">
        {left}
      </div>
      {children}
    </div>
  );
}

function CommandLink({ command }: { command: string | null }) {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  if (command === null) return null;
  return (
    <button
      type="button"
      className="inline-flex items-center gap-1.5 font-medium text-brand-text hover:underline"
      onClick={() => {
        void copyText(command).then((ok) => {
          setState(ok ? "done" : "failed");
        });
      }}
      data-testid="storage-copy-command"
    >
      <SquareTerminal aria-hidden="true" className="size-4" />
      {state === "done"
        ? "Command copied"
        : state === "failed"
          ? `Copy by hand: ${command}`
          : "Copy the command instead"}
    </button>
  );
}

function errorText(err: unknown): { code: string; message: string } {
  if (err instanceof ApiError)
    return { code: err.code ?? err.kind, message: err.message };
  return {
    code: "error",
    message: err instanceof Error ? err.message : String(err),
  };
}

type MoveState =
  | { kind: "form" }
  | { kind: "moving" }
  | { kind: "done"; result: MoveResult }
  | { kind: "failed"; code: string; message: string };

/** MoveDialog is Move… (T063 A6): a typed destination the server checks,
 * then POST /api/v0/store/move with the check's confirm value. An empty
 * folder is the folder's place in Graphvein's folder. */
export function MoveDialog({
  entry,
  settings,
  client,
  token,
  onClose,
}: {
  entry: StoreEntry;
  settings: StoreSettings;
  client: StoreClient;
  token: string;
  onClose: (moved: boolean) => void;
}) {
  const [dir, setDir] = useState("");
  const [move, setMove] = useState<MoveState>({ kind: "form" });
  const trimmed = dir.trim();
  // A store away from Graphvein's folder is checked at once for the move
  // back into it (an empty destination).
  const away =
    entry.store?.place === "inside" || entry.store?.origin === "stores_file";
  const { state, now } = useDirCheck(
    trimmed,
    (d) => client.checkMove(entry.id, d, token),
    away,
  );
  const ready =
    state.kind === "done" &&
    state.dir === trimmed &&
    state.check.outcome === "ready" &&
    state.check.confirm !== null
      ? state.check
      : null;
  const plan = ready?.move ?? null;
  const done = move.kind === "done";

  const start = () => {
    if (ready === null || ready.confirm === null) return;
    setMove({ kind: "moving" });
    client.move(entry.id, trimmed, ready.confirm, token).then(
      (result) => {
        setMove({ kind: "done", result });
      },
      (err: unknown) => {
        setMove({ kind: "failed", ...errorText(err) });
      },
    );
  };

  let body: ReactNode;
  let footer: ReactNode;
  if (move.kind === "done") {
    const r = move.result;
    body = (
      <div className="grid gap-2" data-testid="storage-moved">
        <p className="m-0 flex items-start gap-2 font-medium">
          <Check
            aria-hidden="true"
            className="mt-0.5 size-4 flex-none text-linked"
          />
          <span className="min-w-0 [overflow-wrap:anywhere]">
            The index of {entry.name} is now{" "}
            <code className="font-mono">{r.to}</code>.
          </span>
        </p>
        <p className="m-0 text-[12.5px] text-muted-foreground">
          Copied {formatBytes(r.bytes)} with{" "}
          {r.runs === 1 ? "1 run" : `${String(r.runs)} runs`}; the copy was
          checked before the old files went. Deleted:{" "}
          {r.deleted.length === 0 ? "nothing" : r.deleted.join(", ")}.
        </p>
        {r.remaining.length > 0 && (
          <p
            className="m-0 flex items-start gap-1.5 text-[12.5px] text-unmatched"
            data-testid="storage-moved-remaining"
          >
            <TriangleAlert
              aria-hidden="true"
              className="mt-0.5 size-3.5 flex-none"
            />
            Not deleted: {r.remaining.join(", ")}. Both indexes are refused
            until these files are removed.
          </p>
        )}
        {r.warning !== null && (
          <p className="m-0 text-[12.5px] text-dynamic">{r.warning}</p>
        )}
      </div>
    );
    footer = (
      <DialogFooter>
        <Button
          onClick={() => {
            onClose(true);
          }}
          data-autofocus=""
        >
          Close
        </Button>
      </DialogFooter>
    );
  } else {
    body = (
      <div className="grid gap-3" data-testid="storage-move-form">
        <dl className="m-0 grid grid-cols-[110px_1fr] items-baseline gap-x-4 gap-y-2 text-[13px]">
          <dt className="text-muted-foreground">From</dt>
          <dd className="m-0 min-w-0 font-mono [overflow-wrap:anywhere]">
            {entry.store?.path ?? "not known"}
          </dd>
          <dt className="text-muted-foreground">To folder</dt>
          <dd className="m-0 grid min-w-0 gap-1">
            <input
              type="text"
              value={dir}
              onChange={(e) => {
                setDir(e.target.value);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") now();
              }}
              disabled={move.kind === "moving"}
              autoFocus
              spellCheck={false}
              autoComplete="off"
              placeholder={settings.default_dir.path ?? "/data/graphvein"}
              aria-label="Folder to move the index to"
              className="h-9 rounded-md border bg-background px-3 font-mono text-[13px] outline-none focus-visible:ring-2 focus-visible:ring-ring"
              data-testid="storage-move-dir"
            />
            <span className="text-[12px] text-muted-foreground">
              Empty: its place in Graphvein's folder
              {settings.default_dir.path !== null
                ? ` (${settings.default_dir.path})`
                : ""}
              .
            </span>
          </dd>
          {plan !== null && (
            <>
              <dt className="text-muted-foreground">Will be</dt>
              <dd
                className="m-0 min-w-0 font-mono [overflow-wrap:anywhere]"
                data-testid="storage-move-to"
              >
                {plan.to}
              </dd>
              <dt className="text-muted-foreground">Size</dt>
              <dd className="m-0">
                {formatBytes(plan.size)} (index.db; the -wal file is folded in
                first)
              </dd>
            </>
          )}
        </dl>
        <Verdict
          state={state}
          dir={trimmed}
          ready={
            plan !== null ? (
              <>
                Checked: the server can keep the index at{" "}
                {plan.to_default
                  ? "its place in Graphvein's folder"
                  : "this folder"}
                .
              </>
            ) : (
              "Checked."
            )
          }
        />
        <p className="m-0 flex items-start gap-2 rounded-md border bg-muted/60 px-3 py-2.5 text-[12.5px] leading-snug">
          <Info
            aria-hidden="true"
            className="mt-0.5 size-4 flex-none text-muted-foreground"
          />
          <span>
            The server closes this index while it copies and checks the copy
            (same runs, same row counts). Then the old{" "}
            <code className="font-mono">index.db</code>,{" "}
            <code className="font-mono">-wal</code>,{" "}
            <code className="font-mono">-shm</code> and{" "}
            <code className="font-mono">-lock</code> files are deleted. A
            re-index cannot start meanwhile; if one is running, Move waits for
            it to end. If anything fails, the old index is kept.
          </span>
        </p>
        {move.kind === "failed" && (
          <p
            role="alert"
            className="m-0 flex items-start gap-1.5 text-[12.5px] text-unmatched"
            data-testid="storage-move-failed"
            data-code={move.code}
          >
            <TriangleAlert
              aria-hidden="true"
              className="mt-0.5 size-3.5 flex-none"
            />
            <span>
              {move.message}
              {move.code === "move_failed"
                ? " The original index is kept."
                : move.code === "confirm_required" ||
                    move.code === "path_changed"
                  ? " Nothing was moved; check the folder again."
                  : " Nothing was moved."}
            </span>
          </p>
        )}
      </div>
    );
    footer = (
      <DialogFooter left={<CommandLink command={ready?.command ?? null} />}>
        <Button
          variant="outline"
          onClick={() => {
            onClose(false);
          }}
          disabled={move.kind === "moving"}
          data-autofocus=""
        >
          Cancel
        </Button>
        <Button
          disabled={ready === null || move.kind === "moving"}
          onClick={start}
          data-testid="storage-move-confirm"
        >
          {move.kind === "moving" && (
            <LoaderCircle aria-hidden="true" className="animate-spin" />
          )}
          {move.kind === "moving" ? "Moving…" : "Move index"}
        </Button>
      </DialogFooter>
    );
  }
  return (
    <Modal
      title={
        done
          ? `Moved the index of ${entry.name}`
          : `Move the index of ${entry.name}?`
      }
      onClose={() => {
        if (move.kind !== "moving") onClose(done);
      }}
      testId="storage-move-dialog"
    >
      {body}
      {footer}
    </Modal>
  );
}

type SetState =
  | { kind: "form" }
  | { kind: "saving" }
  | { kind: "failed"; code: string; message: string };

/** DefaultDialog is "New indexes are kept in" Change…: a typed folder the
 * server checks, then POST /api/v0/store/default. Empty is the built-in
 * folder. Indexes that exist stay where they are. */
export function DefaultDialog({
  settings,
  client,
  token,
  onClose,
}: {
  settings: StoreSettings;
  client: StoreClient;
  token: string;
  onClose: (changed: boolean) => void;
}) {
  const [dir, setDir] = useState(
    settings.default_dir.source === "stores_file"
      ? (settings.default_dir.path ?? "")
      : "",
  );
  const [save, setSave] = useState<SetState>({ kind: "form" });
  const trimmed = dir.trim();
  const { state, now } = useDirCheck(
    trimmed,
    (d) => client.checkDefault(d, token),
    false,
  );
  const ready =
    state.kind === "done" &&
    state.dir === trimmed &&
    state.check.outcome === "ready" &&
    state.check.confirm !== null
      ? state.check
      : null;
  const change = ready?.default_dir ?? null;
  const start = () => {
    if (ready === null || ready.confirm === null) return;
    setSave({ kind: "saving" });
    client.setDefault(trimmed, ready.confirm, token).then(
      () => {
        onClose(true);
      },
      (err: unknown) => {
        setSave({ kind: "failed", ...errorText(err) });
      },
    );
  };
  return (
    <Modal
      title="Where should new indexes be kept?"
      onClose={() => {
        if (save.kind !== "saving") onClose(false);
      }}
      testId="storage-default-dialog"
    >
      <div className="grid gap-3">
        <dl className="m-0 grid grid-cols-[110px_1fr] items-baseline gap-x-4 gap-y-2 text-[13px]">
          <dt className="text-muted-foreground">Now</dt>
          <dd className="m-0 min-w-0 [overflow-wrap:anywhere]">
            <code className="font-mono">
              {settings.default_dir.path ?? "not known"}
            </code>{" "}
            <span className="text-muted-foreground">
              ({dirSourceWords(settings.default_dir.source)})
            </span>
          </dd>
          <dt className="text-muted-foreground">Folder</dt>
          <dd className="m-0 grid min-w-0 gap-1">
            <input
              type="text"
              value={dir}
              onChange={(e) => {
                setDir(e.target.value);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") now();
              }}
              autoFocus
              spellCheck={false}
              autoComplete="off"
              placeholder="/data/graphvein"
              aria-label="Folder for new indexes"
              className="h-9 rounded-md border bg-background px-3 font-mono text-[13px] outline-none focus-visible:ring-2 focus-visible:ring-ring"
              data-testid="storage-default-dir"
            />
            <span className="text-[12px] text-muted-foreground">
              Empty: Graphvein's built-in folder for this machine.
            </span>
          </dd>
          {change !== null && (
            <>
              <dt className="text-muted-foreground">Will be</dt>
              <dd
                className="m-0 min-w-0 font-mono [overflow-wrap:anywhere]"
                data-testid="storage-default-to"
              >
                {change.to}
              </dd>
            </>
          )}
        </dl>
        <Verdict
          state={state}
          dir={trimmed}
          ready={
            change !== null && change.to_platform
              ? "Checked: the built-in folder."
              : "Checked: the server accepts this folder."
          }
        />
        <p className="m-0 text-[12.5px] text-muted-foreground">
          Used for every folder added from now on. Indexes that exist stay where
          they are; move one with Move… on its row.
        </p>
        {save.kind === "failed" && (
          <p
            role="alert"
            className="m-0 flex items-start gap-1.5 text-[12.5px] text-unmatched"
            data-testid="storage-default-failed"
            data-code={save.code}
          >
            <TriangleAlert
              aria-hidden="true"
              className="mt-0.5 size-3.5 flex-none"
            />
            {save.message} Nothing was changed.
          </p>
        )}
      </div>
      <DialogFooter>
        <Button
          variant="outline"
          onClick={() => {
            onClose(false);
          }}
          disabled={save.kind === "saving"}
          data-autofocus=""
        >
          Cancel
        </Button>
        <Button
          disabled={ready === null || save.kind === "saving"}
          onClick={start}
          data-testid="storage-default-confirm"
        >
          {save.kind === "saving" && (
            <LoaderCircle aria-hidden="true" className="animate-spin" />
          )}
          {save.kind === "saving" ? "Saving…" : "Use this folder"}
        </Button>
      </DialogFooter>
    </Modal>
  );
}

/** MachineSettings is Settings without an index (T063 A4): this
 * machine's Storage, and where the repository settings appear once a
 * folder is indexed. */
export function MachineSettings() {
  const workspace = useWorkspace();
  return (
    <div
      className="flex min-h-full flex-col gap-3"
      data-testid="settings-machine"
    >
      <div className="grid gap-1.5">
        <div className="flex flex-wrap items-center gap-2.5">
          <h1 className="m-0 text-[22px] font-semibold tracking-tight">
            Settings
          </h1>
          <span className="rounded-md border px-1.5 py-0.5 text-[12px] text-muted-foreground">
            this machine
          </span>
        </div>
        <p className="m-0 text-[13px] text-muted-foreground">
          No folder is indexed yet, so only this machine's settings are shown.
          Each repository's <code className="font-mono">graphvein.yaml</code>{" "}
          settings appear here once it is indexed.
        </p>
      </div>
      <div className="grid gap-4">
        <StorageSection entryId={null} />
        <section
          className="grid gap-2 rounded-lg border border-dashed p-4"
          data-testid="settings-repository-later"
        >
          <h2 className="m-0 text-[15px] font-semibold">Repository settings</h2>
          <p className="m-0 text-[13px] text-muted-foreground">
            Editor, labels and views, indexing and scan, matching, explorer path
            values, try-it environments and architecture are read from a
            repository's <code className="font-mono">graphvein.yaml</code>.
          </p>
          {workspace !== null && (
            <div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  workspace.openAdd();
                }}
              >
                <FolderPlus aria-hidden="true" />
                Add a folder
              </Button>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
