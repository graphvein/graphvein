// The "Not in the document" fold of the Explorer (T029, rows OG-45..OG-47,
// OG-90..OG-93): routes without a usable path, with the unknown part
// visible and an "Enter the missing part" action that edits
// explorer.path_values in graphvein.yaml through the guarded edit
// (preview with diff, then apply); the values set by you with their use;
// paths OpenAPI cannot express; conflicts no single operation can stand
// for; ANY-method routes.

import { Check, PencilLine, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { ApiError } from "@/api/client";
import type { ExplorerClient, MountHop, OpenApiDocument, PathValueEdit, PathValues, Unresolved } from "@/api/openapi";
import { Button } from "@/components/ui/button";
import { useApiRequest } from "@/hooks/useApiRequest";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { MethodBadge, PathKey } from "@/sections/explorer/chips";
import { unknownPathText } from "@/sections/explorer/model";

const HEAD = "text-[11.5px] font-semibold tracking-[.06em] text-muted-foreground uppercase";

/** keyWord says what a path value key supplies. */
export function keyWord(key: string): string {
  if (key.startsWith("nextjs_base_path:")) return `basePath of the Next.js project ${key.slice("nextjs_base_path:".length)}`;
  if (key.startsWith("mount:")) return "the missing prefix of every route below this hop (one value for all of them)";
  return "the whole path of this route";
}

/** suggestValue pre-fills the editor: the route's own pattern, which the
 * user completes with the part Graphvein could not read; "/" (no prefix)
 * for a hop. */
export function suggestValue(u: Unresolved, key: string): string {
  if (key.startsWith("nextjs_base_path:")) return "";
  if (key.startsWith("mount:")) return "/";
  return u.pattern.startsWith("/") ? u.pattern : `/${u.pattern}`;
}

/** joinMount places a path below a hop as the generator does (row
 * OG-95): the known prefix above the hop, the value ("/" adds nothing),
 * then the path below it. "" below the hop is the prefix itself; "/" is
 * the prefix with a trailing slash, except for Express. */
export function joinMount(above: string, value: string, path: string, framework = ""): string {
  const p = above.replace(/\/$/, "") + (value === "/" ? "" : value);
  if (path === "") return p === "" ? "/" : p;
  if (path === "/") return p === "" ? "/" : framework === "express" ? p : `${p}/`;
  return p + path;
}

/** mountMeaning says what a value for a hop states, by what it covers. */
function MountMeaning({ mount }: { mount: MountHop }) {
  const hop = (
    <>
      <span className="font-mono">{mount.hop}</span> ({mount.file}:{mount.line})
    </>
  );
  const above = mount.above.filter((a) => a !== "");
  return (
    <p className="text-[12px] text-muted-foreground" data-testid="mount-meaning">
      {mount.covers === "hop" && (
        <>
          Your value is only the part {hop} adds to the path
          {above.length > 0 ? (
            <>
              : Graphvein read the prefix above it, <span className="font-mono">{above.join(", ")}</span>, and keeps it fixed.
            </>
          ) : (
            ": the hop is at the root."
          )}{" "}
          Each route below it is served at {above.length > 0 ? "that prefix, then " : ""}your value, then its path below the hop. Use / when the hop adds nothing.
        </>
      )}
      {mount.covers === "nothing" && (
        <>Graphvein read that {hop} adds no prefix: the routes below it are served at the paths shown. The only value is /, which states that it passes requests on unchanged (Graphvein did not prove that).</>
      )}
      {mount.covers === "root" && (
        <>
          Your value is the whole prefix from the root: the routers above {hop} are not proven either, so it includes everything above the hop. Each route below it is served at your value, then its path below the hop. Use / when nothing is added.
        </>
      )}
      {mount.shadowingNotChecked && " Whether another pattern beside the hop answers some of these paths first is not checked (shown on each operation)."}
    </p>
  );
}

/** MountPreview shows where some routes below a hop land with a value. */
function MountPreview({ mount, value }: { mount: MountHop; value: string }) {
  if (!value.startsWith("/")) return null;
  if (mount.covers === "nothing" && value !== "/") {
    return (
      <p className="text-[12px] text-unmatched" data-testid="mount-preview">
        This hop adds no unread prefix: only / is accepted.
      </p>
    );
  }
  return (
    <div className="grid gap-0.5 text-[12px]" data-testid="mount-preview">
      <span className="text-muted-foreground">
        With this value, {mount.routes} route{mount.routes === 1 ? "" : "s"} below the hop {mount.routes === 1 ? "is" : "are"} placed, for example:
      </span>
      {mount.examples.map((e) => (
        <span key={`${e.method} ${e.above} ${e.path}`} className="font-mono">
          {e.method} {joinMount(e.above, value, e.path, mount.framework)}
        </span>
      ))}
    </div>
  );
}

function errorText(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  return err instanceof Error ? err.message : String(err);
}

type Step = { kind: "edit" } | { kind: "busy" } | { kind: "preview"; edit: PathValueEdit; value: string; key: string } | { kind: "failed"; message: string };

/** Editor enters one missing part: choose the key, type the value, see the
 * diff of graphvein.yaml, apply. Nothing is written before Apply. */
export function Editor({
  keys,
  suggest,
  values,
  client,
  onApplied,
  onClose,
}: {
  keys: string[];
  suggest: (key: string) => string;
  values: PathValues;
  client: ExplorerClient;
  onApplied: () => void;
  onClose: () => void;
}) {
  const current = (k: string) => values.values.find((v) => v.key === k);
  const initial = (k: string) => current(k)?.value ?? suggest(k);
  const [key, setKey] = useState(keys[0] ?? "");
  const [value, setValue] = useState(() => initial(keys[0] ?? ""));
  const mount = values.mounts.find((m) => m.key === key);
  const existing = current(key);
  const [step, setStep] = useState<Step>({ kind: "edit" });
  const preview = () => {
    setStep({ kind: "busy" });
    client.editPathValues({ base_hash: values.hash, operations: [{ op: "set", key, value }] }, values.token).then(
      (edit) => {
        setStep({ kind: "preview", edit, value, key });
      },
      (err: unknown) => {
        setStep({ kind: "failed", message: errorText(err) });
      },
    );
  };
  const apply = (edit: PathValueEdit, k: string, v: string) => {
    setStep({ kind: "busy" });
    client.editPathValues({ base_hash: edit.baseHash, operations: [{ op: "set", key: k, value: v }], apply: true, result_hash: edit.resultHash }, values.token).then(
      () => {
        onApplied();
      },
      (err: unknown) => {
        setStep({ kind: "failed", message: errorText(err) });
      },
    );
  };
  return (
    <div className="grid gap-2 rounded-md border bg-card p-3" data-testid="path-value-editor">
      <div className="flex flex-wrap items-center gap-2 text-[12.5px]">
        <label className="flex items-center gap-1.5">
          <span className="text-muted-foreground">Key</span>
          <select
            value={key}
            onChange={(e) => {
              setKey(e.target.value);
              setValue(initial(e.target.value));
              setStep({ kind: "edit" });
            }}
            className="h-8 max-w-[420px] rounded-md border bg-background px-2 font-mono text-[12px]"
          >
            {keys.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>
        </label>
        <span className="text-[11.5px] text-muted-foreground">supplies {keyWord(key)}</span>
      </div>
      {existing !== undefined && (
        <p className="text-[12.5px] text-dynamic" data-testid="path-value-existing">
          Already set by you to <span className="font-mono">{existing.value}</span>
          {existing.status !== "used" && existing.why !== null ? `, not used: ${existing.why}` : ""}
        </p>
      )}
      <label className="grid gap-1 text-[12.5px]">
        <span className="text-muted-foreground">Value</span>
        <input
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            setStep({ kind: "edit" });
          }}
          placeholder={key.startsWith("nextjs_base_path:") ? "/docs" : key.startsWith("mount:") ? "/" : "/api/…"}
          className="h-8 rounded-md border bg-background px-2 font-mono text-[12.5px]"
          aria-label="Path value"
          data-testid="path-value-input"
        />
      </label>
      {mount !== undefined && <MountMeaning mount={mount} />}
      {mount !== undefined && <MountPreview mount={mount} value={value} />}
      <p className="text-[11.5px] text-muted-foreground">
        Written to {values.file} under explorer.path_values. It changes only the Explorer, try-it and the generated document, never the index, and is always shown as set by you.
      </p>
      {step.kind === "failed" && (
        <p className="text-[12.5px] text-unmatched" role="alert">
          {step.message}
        </p>
      )}
      {step.kind === "preview" && (
        <div className="grid gap-1.5" data-testid="path-value-preview">
          <div className={HEAD}>Preview</div>
          {step.edit.changes.map((c) => (
            <p key={c} className="text-[12.5px]">
              {c}
            </p>
          ))}
          {step.edit.problems.map((p) => (
            <p key={p} className="text-[12.5px] text-unmatched">
              {p}
            </p>
          ))}
          <pre className="max-h-64 overflow-auto rounded-md border bg-code-bg p-2 font-mono text-[11.5px] leading-[1.5]" data-testid="path-value-diff">
            {step.edit.diff.split("\n").map((line, i) => (
              <span key={i} className={cn("block", line.startsWith("+") && !line.startsWith("+++") && "bg-linked-soft text-linked", line.startsWith("-") && !line.startsWith("---") && "bg-unmatched-soft text-unmatched")}>
                {line === "" ? " " : line}
              </span>
            ))}
          </pre>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        {step.kind === "preview" ? (
          <Button
            size="sm"
            disabled={!step.edit.valid}
            onClick={() => {
              apply(step.edit, step.key, step.value);
            }}
            data-testid="path-value-apply"
          >
            <Check aria-hidden="true" />
            Apply to {values.file}
          </Button>
        ) : (
          <Button size="sm" disabled={value.trim() === "" || step.kind === "busy" || !values.editable} onClick={preview} data-testid="path-value-preview-button">
            Preview the change
          </Button>
        )}
        <Button size="sm" variant="ghost" onClick={onClose}>
          <X aria-hidden="true" />
          Cancel
        </Button>
      </div>
    </div>
  );
}

function UnresolvedRow({ u, values, client, onApplied, open, setOpen, sameReason }: { u: Unresolved; values: PathValues | null; client: ExplorerClient; onApplied: () => void; open: boolean; setOpen: (open: boolean) => void; sameReason: boolean }) {
  const path = unknownPathText(u);
  const editable = values?.editable === true;
  return (
    <li className="grid gap-2 border-t px-3 py-2 first:border-t-0" data-testid="unresolved-row">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <MethodBadge method={u.method} />
        <span className="flex min-w-0 flex-wrap items-center gap-0.5 text-[13px]">
          <span className="rounded-[4px] border border-dashed border-dynamic/60 bg-dynamic-soft px-1 font-mono text-[12px] text-dynamic" title={u.unknown.text} data-testid="unknown-part">
            ‹{path.unknown}›
          </span>
          <PathKey path={path.rest} />
        </span>
        <span className="font-mono text-[11.5px] text-muted-foreground">
          {u.file}:{u.line}
        </span>
        <span className="ml-auto">
          {!open && (
            <Button
              size="xs"
              variant="outline"
              disabled={!editable}
              title={values === null ? "reading graphvein.yaml" : editable ? "set the part Graphvein could not read" : (values.reason ?? "graphvein.yaml cannot be edited here")}
              onClick={() => {
                setOpen(true);
              }}
              data-testid="enter-missing-part"
            >
              <PencilLine aria-hidden="true" />
              Enter the missing part
            </Button>
          )}
        </span>
      </div>
      <p className={cn("text-[12px] text-muted-foreground", sameReason && "sr-only")}>
        {u.unknown.text}
        {u.unknown.expression !== null && (
          <>
            {" "}
            (<span className="font-mono">{u.unknown.expression}</span>)
          </>
        )}
      </p>
      {open && values !== null && (
        <Editor
          keys={u.keys.length > 0 ? u.keys : [u.route]}
          suggest={(k) => suggestValue(u, k)}
          values={values}
          client={client}
          onApplied={() => {
            setOpen(false);
            onApplied();
          }}
          onClose={() => {
            setOpen(false);
          }}
        />
      )}
    </li>
  );
}

/** MissingPrefixes lists the hops where composition stopped (rows OG-94,
 * OG-95): one value per hop places every route below it. */
function MissingPrefixes({ values, client, onApplied, focus, onFocusDone }: { values: PathValues; client: ExplorerClient; onApplied: () => void; focus: string | null; onFocusDone: () => void }) {
  const mounts = values.mounts.filter((m) => m.routes > 0);
  const linked = focus !== null && mounts.some((m) => m.key === focus && !m.ambiguous && values.editable) ? focus : null;
  // A link from "What Graphvein couldn't read" opens that hop's editor.
  const [open, setOpen] = useState<string | null>(linked);
  const focusRef = useRef<HTMLLIElement>(null);
  useEffect(() => {
    const el = focusRef.current;
    if (focus !== null && el !== null && typeof el.scrollIntoView === "function") el.scrollIntoView({ block: "center" });
  }, [focus]);
  const close = () => {
    setOpen(null);
    if (focus !== null) onFocusDone();
  };
  const notFound = focus !== null && !mounts.some((m) => m.key === focus);
  if (mounts.length === 0) return notFound ? <LinkedPrefixAbsent onDismiss={onFocusDone} /> : null;
  return (
    <div className="grid gap-1.5" data-testid="missing-prefixes">
      {notFound && <LinkedPrefixAbsent onDismiss={onFocusDone} />}
      <div className={HEAD}>Missing prefixes ({mounts.length})</div>
      <p className="text-[12.5px] text-muted-foreground">Composition stopped at these hops: a prefix that is not a constant, or a router passed through a call Graphvein could not prove keeps the path. One value per hop places every route below it, marked as set by you.</p>
      <ul className="rounded-md border">
        {mounts.map((m) => {
          const set = values.values.find((v) => v.key === m.key);
          return (
            <li key={m.key} ref={m.key === focus ? focusRef : undefined} className="grid gap-2 border-t px-3 py-2 first:border-t-0" data-testid="mount-row" data-linked={m.key === focus ? "true" : undefined}>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="rounded-[4px] border border-dashed border-dynamic/60 bg-dynamic-soft px-1 font-mono text-[12px] text-dynamic [overflow-wrap:anywhere]">{m.hop}</span>
                <span className="font-mono text-[11.5px] text-muted-foreground">
                  {m.file}:{m.line}
                </span>
                <span className="text-[12px] text-muted-foreground">
                  {m.routes} route{m.routes === 1 ? "" : "s"} below
                </span>
                {set !== undefined && (
                  <span className="text-[12px] text-dynamic">
                    set by you to <span className="font-mono">{set.value}</span>
                    {set.status !== "used" && set.why !== null ? `, not used: ${set.why}` : ""}
                  </span>
                )}
                <span className="ml-auto">
                  {open !== m.key && (
                    <Button
                      size="xs"
                      variant="outline"
                      disabled={!values.editable || m.ambiguous}
                      title={m.ambiguous ? "another hop in the same function has the same text: a value cannot be tied to this one" : values.editable ? "one value for every route below this hop" : (values.reason ?? "graphvein.yaml cannot be edited here")}
                      onClick={() => {
                        setOpen(m.key);
                      }}
                      data-testid="set-missing-prefix"
                    >
                      <PencilLine aria-hidden="true" />
                      Set the missing prefix for all {m.routes} route{m.routes === 1 ? "" : "s"} under {m.hop.length > 48 ? `${m.hop.slice(0, 47)}…` : m.hop}
                    </Button>
                  )}
                </span>
              </div>
              {open === m.key && (
                <Editor
                  keys={[m.key]}
                  suggest={() => "/"}
                  values={values}
                  client={client}
                  onApplied={() => {
                    close();
                    onApplied();
                  }}
                  onClose={close}
                />
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

/** LinkedPrefixAbsent says that the missing prefix a link named is not
 * waiting for a value here: what the link pointed at is gone (a value was
 * set, or the index changed); nothing else is claimed. */
function LinkedPrefixAbsent({ onDismiss }: { onDismiss: () => void }) {
  return (
    <p role="status" className="flex flex-wrap items-center gap-x-3 text-[12.5px] text-muted-foreground" data-testid="linked-prefix-absent">
      <span>The missing prefix the link points at is not listed here now: a value may be set already, or the index changed since the link was made.</span>
      <button type="button" className="underline" onClick={onDismiss}>
        Dismiss
      </button>
    </p>
  );
}

/** UserValues lists the values set by you with their use and warnings. */
function UserValues({ doc }: { doc: OpenApiDocument }) {
  const style = useLabelStyle();
  if (doc.userValues.length === 0) return null;
  return (
    <div className="grid gap-1.5" data-testid="user-values">
      <div className={HEAD}>Set by you ({doc.userValues.length})</div>
      <ul className="rounded-md border">
        {doc.userValues.map((v) => {
          const l = label(style, "userValueStatus", v.status);
          return (
            <li key={v.key} className="grid gap-0.5 border-t px-3 py-1.5 text-[12.5px] first:border-t-0">
              <div className="flex flex-wrap items-center gap-2">
                <span className={cn("rounded-[5px] border px-1.5 text-[11.5px]", v.status === "used" ? "border-brand/40 bg-brand-soft text-brand-text" : "border-dynamic/40 bg-dynamic-soft text-dynamic")} title={l.title}>
                  {l.text}
                </span>
                <span className="font-mono">{v.value}</span>
                <span className="font-mono text-[11px] text-muted-foreground [overflow-wrap:anywhere]">{v.key}</span>
                {v.status === "used" && <span className="text-[11.5px] text-muted-foreground">{v.operations} operation{v.operations === 1 ? "" : "s"}</span>}
              </div>
              {v.why !== null && <p className="text-[12px] text-dynamic">Not used: {v.why}</p>}
              {v.warning !== null && <p className="text-[12px] text-muted-foreground">Warning: {v.warning}</p>}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

/** NotInDocument is the fold below the operations. */
export function NotInDocument({
  doc,
  client,
  reloadKey,
  onApplied,
  focusPrefix = null,
  onPrefixDone = () => undefined,
}: {
  doc: OpenApiDocument;
  client: ExplorerClient;
  reloadKey: string;
  onApplied: () => void;
  /** The missing prefix a link points at (prefix= in the hash). */
  focusPrefix?: string | null;
  onPrefixDone?: () => void;
}) {
  const [openRoute, setOpenRoute] = useState<string | null>(null);
  const needValues = doc.unresolved.length > 0;
  const values = useApiRequest(needValues ? JSON.stringify(["path-values", reloadKey]) : null, (signal) => client.pathValues({ signal }));
  const v = values.state.kind === "ok" ? values.state.data : null;
  const empty = doc.unresolved.length === 0 && doc.unrepresentable.length === 0 && doc.conflicts.length === 0 && doc.anyMethod.length === 0 && doc.userValues.length === 0;
  if (empty) return focusPrefix !== null ? <LinkedPrefixAbsent onDismiss={onPrefixDone} /> : null;
  return (
    <section className="grid gap-3 rounded-lg border bg-card p-3" aria-label="Not in the document" data-testid="not-in-document" id="not-in-document" tabIndex={-1}>
      <h3 className="text-[14px] font-semibold">Not in the document, and values set by you</h3>
      <UserValues doc={doc} />
      {doc.unresolved.length > 0 && (
        <div className="grid gap-1.5" data-testid="unresolved">
          <div className={HEAD}>Unknown path part ({doc.unresolved.length})</div>
          <p className="text-[12.5px] text-muted-foreground">These routes are registered in code, but part of their path is not read. Graphvein writes no guessed path; enter the part you know to add them to the Explorer, marked as set by you.</p>
          {values.state.kind === "failed" && <p className="text-[12.5px] text-unmatched">graphvein.yaml could not be read: {values.state.error.message}</p>}
          {v !== null && !v.editable && <p className="text-[12.5px] text-dynamic">Editing is not available: {v.reason ?? "no reason given"}</p>}
          {v !== null && (
            <MissingPrefixes
              key={focusPrefix ?? ""}
              focus={focusPrefix}
              onFocusDone={onPrefixDone}
              values={v}
              client={client}
              onApplied={() => {
                values.retry();
                onApplied();
              }}
            />
          )}
          <ul className="rounded-md border">
            {doc.unresolved.map((u, i) => (
              <UnresolvedRow
                key={u.route}
                u={u}
                sameReason={i > 0 && doc.unresolved[i - 1]?.unknown.text === u.unknown.text && u.unknown.expression === null && openRoute !== u.route}
                values={v}
                client={client}
                open={openRoute === u.route}
                setOpen={(open) => {
                  setOpenRoute(open ? u.route : null);
                }}
                onApplied={() => {
                  values.retry();
                  onApplied();
                }}
              />
            ))}
          </ul>
        </div>
      )}
      {doc.conflicts.length > 0 && (
        <div className="grid gap-1.5" data-testid="conflicts">
          <div className={HEAD}>Conflicts ({doc.conflicts.length})</div>
          <p className="text-[12.5px] text-muted-foreground">No single operation can stand for these routes, so none is picked.</p>
          <ul className="rounded-md border text-[12.5px]">
            {doc.conflicts.map((c, i) => (
              <li key={i} className="grid gap-0.5 border-t px-3 py-1.5 first:border-t-0">
                <span>
                  <b className="font-semibold">{c.reason.replace(/_/g, " ")}</b>
                  {c.method !== null && <span className="font-mono"> {c.method}</span>} <span className="font-mono">{c.keys.join(", ")}</span>
                </span>
                <span className="font-mono text-[11.5px] text-muted-foreground [overflow-wrap:anywhere]">{c.routes.join(" · ")}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {doc.unrepresentable.length > 0 && (
        <div className="grid gap-1.5" data-testid="unrepresentable">
          <div className={HEAD}>Paths OpenAPI 3.1 cannot express ({doc.unrepresentable.length})</div>
          <ul className="rounded-md border text-[12.5px]">
            {doc.unrepresentable.map((u) => (
              <li key={`${u.route}-${u.key}`} className="flex flex-wrap items-center gap-2 border-t px-3 py-1.5 first:border-t-0">
                <MethodBadge method={u.method} />
                <PathKey path={u.key} />
                <span className="text-muted-foreground">{u.reason.replace(/_/g, " ")}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {doc.anyMethod.length > 0 && (
        <div className="grid gap-1.5" data-testid="any-method">
          <div className={HEAD}>Any method ({doc.anyMethod.length})</div>
          <p className="text-[12.5px] text-muted-foreground">Routes that accept every method are listed, never expanded into eight operations.</p>
          <ul className="rounded-md border text-[12.5px]">
            {doc.anyMethod.map((a) => (
              <li key={a.route} className="flex flex-wrap items-center gap-2 border-t px-3 py-1.5 first:border-t-0">
                <MethodBadge method="ANY" />
                <PathKey path={a.key} />
                <span className="font-mono text-[11.5px] text-muted-foreground">
                  {a.file}:{a.line}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
