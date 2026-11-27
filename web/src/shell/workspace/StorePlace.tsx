// Where a new index is kept, in the add flow (T068 part 3, ADR 0093,
// design record T063 D4): the first-run line, the confirmation's "Index
// kept in" row with Change…, and the chooser it opens (Graphvein's folder,
// inside the folder, or another folder the server checks). Every path
// shown comes from the server's answer; a typed folder is never shown as
// accepted before the server checked it.

import { Check, Database, LoaderCircle, TriangleAlert } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import {
  storePlaceWords,
  type StoreDoc,
  type StoreSettings,
} from "@/api/store";
import type { CheckResult, PlaceChoice } from "@/api/workspace";
import { useStoreClient, useStoreSettings } from "@/hooks/useStoreSettings";
import { cn } from "@/lib/utils";
import { settingsHash } from "@/router";

/** PlaceChip is a store's place as a chip ("outside the folder ·
 * default", "inside the folder", ...). */
export function PlaceChip({
  store,
  tone = "muted",
}: {
  store: StoreDoc;
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
      data-testid="store-place"
      data-place={store.place ?? ""}
    >
      {storePlaceWords(store)}
    </span>
  );
}

/** firstRunWords is the first-run line's sentence for GET /api/v0/store,
 * or null when it says nothing certain (failed, or no folder known). */
export function firstRunWords(
  s: StoreSettings,
): { text: string; path: string | null } | null {
  if (s.team_mode.set)
    return {
      text: `Indexes are kept in the Postgres database named by ${s.team_mode.env} in the environment of graphvein serve.`,
      path: null,
    };
  const dir = s.default_dir.path;
  if (dir === null) return null;
  if (s.default_dir.source === "env")
    return {
      text: `Indexes are kept in the folder ${s.default_dir.env} names, not in your repositories:`,
      path: dir,
    };
  if (s.default_dir.source === "stores_file")
    return {
      text: "Indexes are kept in the folder chosen in Settings, not in your repositories:",
      path: dir,
    };
  return {
    text: "Indexes are kept in Graphvein's own folder on this machine, not in your repositories:",
    path: dir,
  };
}

/** FirstRunStoreLine says where indexes go before anything is indexed
 * (T063 A1), with a link to Settings' Storage section. Nothing shows
 * until the server answered. */
export function FirstRunStoreLine() {
  const { view } = useStoreSettings(useStoreClient());
  if (view.kind !== "ok") return null;
  const words = firstRunWords(view.settings);
  if (words === null) return null;
  return (
    <p
      className="m-0 flex items-start gap-2.5 rounded-md border bg-muted/60 px-3 py-2.5 text-[13px] leading-snug"
      data-testid="first-run-store"
    >
      <Database
        aria-hidden="true"
        className="mt-0.5 size-4 flex-none text-muted-foreground"
      />
      <span className="min-w-0 [overflow-wrap:anywhere]">
        {words.text}{" "}
        {words.path !== null && (
          <code className="font-mono text-[12.5px]">{words.path}</code>
        )}
        {words.path !== null && "."}{" "}
        <a
          href={settingsHash("storage")}
          className="font-medium whitespace-nowrap text-brand-text hover:underline"
          data-testid="first-run-store-settings"
        >
          Change in Settings
        </a>
      </span>
    </p>
  );
}

/** KeptIn is the confirmation's "Index kept in" value: the file (or
 * Postgres), its place and, when the place can be chosen, Change…. */
export function KeptIn({
  store,
  places,
  onChange,
}: {
  store: StoreDoc | null;
  places: CheckResult["places"];
  onChange: (() => void) | null;
}) {
  if (store === null) return <>not reported</>;
  if (store.kind === "postgres") {
    return (
      <span className="grid gap-1" data-testid="add-kept-in">
        <span>
          Postgres from{" "}
          <code className="font-mono">
            {store.env ?? "GRAPHVEIN_POSTGRES_URL"}
          </code>
        </span>
        <span className="flex flex-wrap items-center gap-2">
          <PlaceChip store={store} />
        </span>
      </span>
    );
  }
  return (
    <span className="grid gap-1" data-testid="add-kept-in">
      <code className="font-mono">{store.path ?? "not known"}</code>
      <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <PlaceChip store={store} />
        {onChange !== null && places?.choosable === true && (
          <button
            type="button"
            className="font-medium text-brand-text hover:underline"
            onClick={onChange}
            data-testid="add-change-place"
          >
            Change…
          </button>
        )}
        {places !== null && !places.choosable && places.reason !== null && (
          <span className="text-[12px] text-muted-foreground">
            {places.reason}
          </span>
        )}
      </span>
      {store.place === "inside" && (
        <span className="text-[12px] text-muted-foreground">
          Git lists .graphvein/ as untracked unless the repository ignores it;
          Graphvein does not edit your ignore files.
        </span>
      )}
    </span>
  );
}

type PlaceId = "default" | "inside" | "folder";

type Probe =
  | { kind: "idle" }
  | { kind: "checking"; key: string }
  | { kind: "done"; key: string; check: CheckResult }
  | { kind: "failed"; key: string; message: string };

function keyOf(place: PlaceId, dir: string): string {
  return place === "folder" ? `folder\u0000${dir}` : place;
}

function choiceOf(place: PlaceId, dir: string): PlaceChoice {
  return place === "folder" ? { place: "folder", dir } : { place };
}

/** initialPlace is the place the confirmation showed. */
function initialPlace(check: CheckResult): PlaceId {
  const chosen = check.places?.chosen;
  if (chosen === "inside" || chosen === "folder" || chosen === "default")
    return chosen;
  if (check.store?.place === "inside") return "inside";
  if (check.store?.origin === "stores_file") return "folder";
  return "default";
}

/** PlaceChooser is Change… (T063 A3): three places for this index only.
 * Each choice is checked by the server (POST /api/v0/workspace/check with
 * store_place), and Index is enabled only for a ready answer of the
 * choice on screen, whose confirm value binds that index file. */
export function PlaceChooser({
  check,
  runCheck,
  footer,
}: {
  check: CheckResult;
  runCheck: (place: PlaceChoice) => Promise<CheckResult>;
  /** footer renders the dialog's buttons with the ready answer, or null. */
  footer: (ready: CheckResult | null) => ReactNode;
}) {
  const [place, setPlace] = useState<PlaceId>(initialPlace(check));
  const [dir, setDir] = useState("");
  const [probe, setProbe] = useState<Probe>(() => ({
    kind: "done",
    key: keyOf(initialPlace(check), ""),
    check,
  }));
  const seq = useRef(0);
  const name = check.name ?? "this folder";
  const trimmed = dir.trim();
  const key = keyOf(place, trimmed);

  const probeFor = (p: PlaceId, d: string) => {
    if (p === "folder" && d === "") {
      setProbe({ kind: "idle" });
      return;
    }
    const k = keyOf(p, d);
    const n = ++seq.current;
    setProbe({ kind: "checking", key: k });
    runCheck(choiceOf(p, d)).then(
      (c) => {
        if (n === seq.current) setProbe({ kind: "done", key: k, check: c });
      },
      (err: unknown) => {
        if (n === seq.current)
          setProbe({
            kind: "failed",
            key: k,
            message: err instanceof Error ? err.message : String(err),
          });
      },
    );
  };
  // A typed folder is checked when typing pauses.
  useEffect(() => {
    if (place !== "folder") return;
    const timer = window.setTimeout(() => {
      probeFor("folder", trimmed);
    }, 400);
    return () => {
      window.clearTimeout(timer);
    };
    // probeFor reads only its arguments and stable setters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [place, trimmed]);

  const current = probe.kind !== "idle" && probe.key === key ? probe : null;
  const answer = current?.kind === "done" ? current.check : null;
  const ready =
    answer !== null && answer.outcome === "ready" && answer.confirm !== null
      ? answer
      : null;
  const places = answer?.places ?? check.places;

  const pick = (p: PlaceId) => {
    setPlace(p);
    if (p !== "folder") probeFor(p, "");
  };

  const option = (p: PlaceId, title: string, body: ReactNode) => (
    <label
      className={cn(
        "grid cursor-pointer grid-cols-[20px_1fr] gap-x-3 gap-y-1 rounded-lg border px-3.5 py-3",
        place === p ? "border-brand bg-brand-soft/40" : "hover:bg-accent/50",
      )}
      data-testid={`place-${p}`}
    >
      <input
        type="radio"
        name="store-place"
        className="mt-0.5 size-4 accent-[hsl(var(--brand))]"
        checked={place === p}
        onChange={() => {
          pick(p);
        }}
      />
      <span className="text-[13.5px] font-semibold">{title}</span>
      <span className="col-start-2 grid min-w-0 gap-1 text-[12.5px] text-muted-foreground">
        {body}
      </span>
    </label>
  );

  return (
    <div className="grid gap-3" data-testid="add-place">
      {option(
        "default",
        "Graphvein's folder on this machine (default)",
        <>
          <code className="font-mono text-[12.5px] text-foreground [overflow-wrap:anywhere]">
            {places?.default ?? "not reported"}
          </code>
          <span>
            Nothing is written into the repository. Private to your user
            account.
          </span>
        </>,
      )}
      {option(
        "inside",
        "Inside the folder",
        <>
          <code className="font-mono text-[12.5px] text-foreground [overflow-wrap:anywhere]">
            {places?.inside ?? "not reported"}
          </code>
          <span>
            Git lists .graphvein/ as untracked unless the repository ignores it;
            Graphvein does not edit your ignore files.
          </span>
        </>,
      )}
      {option(
        "folder",
        "Another folder",
        <>
          <input
            type="text"
            value={dir}
            onChange={(e) => {
              setDir(e.target.value);
              if (place !== "folder") setPlace("folder");
            }}
            onFocus={() => {
              if (place !== "folder") setPlace("folder");
            }}
            spellCheck={false}
            autoComplete="off"
            placeholder="/data/graphvein"
            aria-label="Folder to keep the index in"
            className="h-9 rounded-md border bg-background px-3 font-mono text-[13px] text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
            data-testid="place-dir"
          />
          {place === "folder" && (
            <FolderVerdict probe={current} empty={trimmed === ""} />
          )}
        </>,
      )}
      {place !== "folder" &&
        (current === null ||
          current.kind !== "done" ||
          current.check.outcome !== "ready") && (
          <FolderVerdict probe={current} empty={false} />
        )}
      <p className="m-0 text-[12.5px] text-muted-foreground">
        Only the index of {name}. New indexes keep using the default; change it
        in Settings. Moving an index later is an explicit action in Settings.
      </p>
      {footer(ready)}
    </div>
  );
}

/** FolderVerdict is the server's answer for the chosen place. */
function FolderVerdict({
  probe,
  empty,
}: {
  probe: Probe | null;
  empty: boolean;
}) {
  if (empty)
    return (
      <span className="text-[12.5px]">
        Type the folder's absolute path; the server checks it before Index is
        enabled.
      </span>
    );
  if (probe === null || probe.kind === "idle" || probe.kind === "checking") {
    return (
      <span
        className="flex items-center gap-1.5 text-[12.5px]"
        data-testid="place-checking"
      >
        <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" />
        Checking…
      </span>
    );
  }
  if (probe.kind === "failed") {
    return (
      <span
        className="flex items-start gap-1.5 text-[12.5px] text-unmatched"
        data-testid="place-refused"
        data-code="request_failed"
      >
        <TriangleAlert
          aria-hidden="true"
          className="mt-0.5 size-3.5 flex-none"
        />
        The check failed: {probe.message}
      </span>
    );
  }
  const c = probe.check;
  if (c.outcome === "ready") {
    return (
      <span
        className="flex items-start gap-1.5 text-[12.5px] text-linked"
        data-testid="place-ready"
      >
        <Check aria-hidden="true" className="mt-0.5 size-3.5 flex-none" />
        <span className="min-w-0 [overflow-wrap:anywhere]">
          Checked:{" "}
          <code className="font-mono">
            {c.store?.path ?? c.places?.folder ?? "the index file"}
          </code>
          {c.store?.exists === false ? ", no index there yet" : ""}
        </span>
      </span>
    );
  }
  return (
    <span
      className="flex items-start gap-1.5 text-[12.5px] text-unmatched"
      data-testid="place-refused"
      data-code={c.refusal?.code ?? c.outcome}
    >
      <TriangleAlert aria-hidden="true" className="mt-0.5 size-3.5 flex-none" />
      <span className="min-w-0 [overflow-wrap:anywhere]">
        {c.refusal !== null ? c.refusal.message : `Refused: ${c.outcome}`}
      </span>
    </span>
  );
}
