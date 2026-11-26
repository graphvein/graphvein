// Manifest editing in the Project view (T022 B10; ADR 0075 "Manifest
// editing from the UI"). graphvein-project.yaml stays the source of truth:
// every edit here (consumes entries and selectors, default scope, link
// scopes, repository names, accepting a suggestion) is previewed as the
// server's diff of the file and written only after a confirm. Adding a
// member by folder stays in the CLI (graphvein project add). The embedded
// UI has little room left (static_test.go), so this file is kept small.

import { useCallback, useEffect, useState } from "react";

import { fetchJSON } from "@/api/client";
import { INDEX_RUN_PATH } from "@/api/indexRun";
import {
  fetchManifest,
  fetchSuggestions,
  postEdit,
  selectText,
  type ConsumeEntry,
  type EditResult,
  type Manifest,
  type Op,
  type Selector,
  type Suggestions,
} from "@/api/manifest";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";

const FIELDS = ["bases", "origins", "path_prefixes"] as const;
const BTN =
  "h-7 rounded-md border px-2 text-[12.5px] font-medium hover:bg-accent disabled:opacity-50";
const IN =
  "h-7 min-w-0 rounded-md border bg-background px-2 font-mono text-[12.5px]";
const ROW = "flex flex-wrap items-center gap-1.5";
const MONO = "font-mono text-[12.5px]";

/** list reads a comma-separated input into a selector list. */
const list = (s: string) =>
  s
    .split(",")
    .map((v) => v.trim())
    .filter((v) => v !== "");

/** at writes an endpoint: repository, repository/scope, or its default
 * scope. */
const at = (repo: string, scope?: string, def?: boolean) =>
  def === true
    ? `${repo} (default scope)`
    : scope !== undefined && scope !== ""
      ? `${repo}/${scope}`
      : repo;
const sel = (s: Partial<Selector>) =>
  selectText(s) !== "" ? ` (select: ${selectText(s)})` : "";

function Field({
  name,
  value,
  set,
  wide,
}: {
  name: string;
  value: string;
  set: (v: string) => void;
  wide?: boolean;
}) {
  return (
    <input
      aria-label={name}
      placeholder={name}
      className={wide === true ? `${IN} w-[236px]` : IN}
      value={value}
      onChange={(e) => {
        set(e.target.value);
      }}
    />
  );
}

/** Pick is a select of options (value, text); the first, empty, names
 * what to pick. */
function Pick({
  name,
  value,
  set,
  options,
}: {
  name: string;
  value: string;
  set: (v: string) => void;
  options: string[][];
}) {
  return (
    <select
      aria-label={name}
      className={IN}
      value={value}
      onChange={(e) => {
        set(e.target.value);
      }}
    >
      {[["", `${name}…`], ...options].map(([v = "", t]) => (
        <option key={v} value={v}>
          {t ?? v}
        </option>
      ))}
    </select>
  );
}

/** Selectors edits a selector as three comma-separated lists. */
function Selectors({
  value,
  set,
}: {
  value: Partial<Selector>;
  set: (s: Partial<Selector>) => void;
}) {
  const style = useLabelStyle();
  return FIELDS.map((k) => {
    const l = label(style, "selectorField", k);
    return (
      <span key={k} title={l.title}>
        <Field
          name={l.text}
          value={(value[k] ?? []).join(", ")}
          set={(v) => {
            set({ ...value, [k]: list(v) });
          }}
        />
      </span>
    );
  });
}

/** ManifestEditor lists the manifest's declarations with their editors
 * and the suggestions; onWritten runs after a write (the project document
 * is read again). */
export function ManifestEditor({ onWritten }: { onWritten: () => void }) {
  const style = useLabelStyle();
  const [m, setM] = useState<Manifest | null>(null);
  const [error, setError] = useState("");
  const [pending, setPending] = useState<{ ops: Op[]; r: EditResult } | null>(
    null,
  );
  const [written, setWritten] = useState<EditResult | null>(null);
  // T022 review: the old names whose stored index the user confirmed
  // deleting, for the open preview.
  const [confirmed, setConfirmed] = useState<string[]>([]);
  const [sugg, setSugg] = useState<Suggestions | null>(null);
  const [busy, setBusy] = useState(false);
  // The one open inline editor: a selector (owner and target) or a rename.
  const [open, setOpen] = useState("");
  const [draft, setDraft] = useState<Partial<Selector>>({});
  const [name, setName] = useState("");
  const [add, setAdd] = useState({ owner: "", provider: "", select: {} });
  const [scope, setScope] = useState({ member: "", name: "", path: "" });

  const load = useCallback(() => {
    fetchManifest().then(setM, (e: unknown) => {
      setError(String(e));
    });
  }, []);
  useEffect(load, [load]);
  const run = (p: Promise<unknown>) => {
    setBusy(true);
    setError("");
    p.catch((e: unknown) => {
      setError(String(e));
    }).finally(() => {
      setBusy(false);
    });
  };
  if (m === null)
    return error !== "" ? (
      <p className="m-0 text-[13px] text-dynamic">{error}</p>
    ) : null;

  const preview = (ops: Op[]) => {
    run(
      postEdit(m, ops).then((r) => {
        setPending({ ops, r });
        setConfirmed([]);
        setWritten(null);
      }),
    );
  };
  const b = (op: string, on: () => void, off = false) => {
    const l = label(style, "manifestOp", op);
    return (
      <button
        type="button"
        className={BTN}
        title={l.title}
        disabled={busy || off}
        onClick={on}
      >
        {l.text}
      </button>
    );
  };
  const go = (ops: Op[]) => (
    <button
      type="button"
      className={BTN}
      disabled={busy}
      onClick={() => {
        preview(ops);
      }}
    >
      Preview
    </button>
  );
  const owners = m.members.flatMap((x) => [
    { member: x.repository, scope: "", list: x.consumes },
    ...x.scopes.map((s) => ({
      member: x.repository,
      scope: s.name,
      list: s.consumes,
    })),
  ]);
  const providers = m.members.flatMap((x) => [
    x.repository,
    ...(x.scopes.length > 0 ? [`${x.repository}#`] : []),
    ...x.scopes.map((s) => `${x.repository}/${s.name}`),
  ]);
  const entry = (o: (typeof owners)[number], c: ConsumeEntry) => {
    const key = `${o.member}/${o.scope}:${at(c.repository, c.scope, c.default_scope)}`;
    const base = {
      member: o.member,
      ...(o.scope !== "" ? { scope: o.scope } : {}),
    };
    const target = {
      repository: c.repository,
      ...(c.scope !== "" ? { scope: c.scope } : {}),
      ...(c.default_scope ? { default_scope: true } : {}),
    };
    const scoped =
      c.scope === "" &&
      (m.members.find((x) => x.repository === c.repository)?.scopes.length ??
        0) > 0;
    return (
      <li key={key} className={ROW} data-testid="manifest-consume">
        <span className={MONO}>
          {at(o.member, o.scope)} consumes{" "}
          {at(c.repository, c.scope, c.default_scope)}
          {sel(c.select)}
        </span>
        {b("set_select", () => {
          setOpen(open === key ? "" : key);
          setDraft(c.select);
        })}
        {scoped &&
          b("set_default_scope", () => {
            preview([
              {
                op: "set_default_scope",
                ...base,
                repository: c.repository,
                value: !c.default_scope,
              },
            ]);
          })}
        {b("remove_consume", () => {
          preview([{ op: "remove_consume", ...base, target }]);
        })}
        {open === key && (
          <span className={`${ROW} w-full pl-3`}>
            <Selectors value={draft} set={setDraft} />
            {go([{ op: "set_select", ...base, target, select: draft }])}
          </span>
        )}
      </li>
    );
  };

  return (
    <section className="grid gap-3 rounded-lg border bg-card px-4 py-3 text-[13px]">
      <h2 className="m-0 text-[13.5px] font-semibold">
        Edit declarations{" "}
        <small className="font-normal text-muted-foreground">
          shown as a diff of {m.file}, written on confirm; add members with
          graphvein project add
        </small>
      </h2>
      {error !== "" && (
        <p role="alert" className="m-0 text-dynamic">
          {error}
        </p>
      )}
      <ul className="m-0 grid list-none gap-1.5 p-0">
        {owners.flatMap((o) => o.list.map((c) => entry(o, c)))}
      </ul>
      <div className={ROW}>
        <Pick
          name="consumer"
          value={add.owner}
          set={(v) => {
            setAdd({ ...add, owner: v });
          }}
          options={owners.map((o) => [at(o.member, o.scope)])}
        />
        consumes
        <Pick
          name="provider"
          value={add.provider}
          set={(v) => {
            setAdd({ ...add, provider: v });
          }}
          options={providers.map((p) => [
            p,
            p.replace("#", " (default scope)"),
          ])}
        />
        <Selectors
          value={add.select}
          set={(s) => {
            setAdd({ ...add, select: s });
          }}
        />
        {b(
          "add_consume",
          () => {
            const [member = "", s] = add.owner.split("/");
            const [repository = "", ps] = add.provider
              .replace("#", "")
              .split("/");
            preview([
              {
                op: "add_consume",
                member,
                ...(s !== undefined ? { scope: s } : {}),
                consume: {
                  repository,
                  ...(ps !== undefined ? { scope: ps } : {}),
                  ...(add.provider.endsWith("#")
                    ? { default_scope: true }
                    : {}),
                  select: add.select,
                },
              },
            ]);
          },
          add.owner === "" || add.provider === "",
        )}
      </div>
      <ul className="m-0 grid list-none gap-1.5 p-0">
        {m.members.map((x) => (
          <li key={x.repository} className={ROW}>
            <b className={MONO}>{x.repository}</b>
            {x.scopes.map((s) => (
              <span key={s.name} className={`${ROW} ${MONO}`}>
                scope {s.name} ({s.path})
                {b("remove_scope", () => {
                  preview([
                    { op: "remove_scope", member: x.repository, name: s.name },
                  ]);
                })}
              </span>
            ))}
            {x.repository_source === "manifest" ? (
              b("rename_repository", () => {
                setOpen(`=${x.repository}`);
                setName(x.repository);
              })
            ) : (
              <span className="text-muted-foreground">
                name from its graphvein.yaml
              </span>
            )}
            {open === `=${x.repository}` && (
              <>
                <Field name="New name" value={name} set={setName} />
                {go([
                  {
                    op: "rename_repository",
                    member: x.repository,
                    to: name.trim(),
                  },
                ])}
              </>
            )}
          </li>
        ))}
      </ul>
      <div className={ROW}>
        <Pick
          name="member"
          value={scope.member}
          set={(v) => {
            setScope({ ...scope, member: v });
          }}
          options={m.members
            .filter((x) => x.checkout)
            .map((x) => [x.repository])}
        />
        <Field
          name="scope name"
          value={scope.name}
          set={(v) => {
            setScope({ ...scope, name: v });
          }}
        />
        <Field
          wide
          name="folder inside the member"
          value={scope.path}
          set={(v) => {
            setScope({ ...scope, path: v });
          }}
        />
        {b(
          "add_scope",
          () => {
            preview([{ op: "add_scope", ...scope }]);
          },
          scope.member === "" || scope.name === "" || scope.path === "",
        )}
      </div>
      <div className="grid gap-1.5">
        <div className={ROW}>
          <b>Suggestions</b>
          <span className="text-muted-foreground">
            from env, Compose and proxy files; applied only when accepted
          </span>
          <button
            type="button"
            className={BTN}
            disabled={busy}
            onClick={() => {
              run(fetchSuggestions().then(setSugg));
            }}
          >
            Read suggestions
          </button>
        </div>
        {sugg?.proposals?.length === 0 && (
          <p className="m-0 text-muted-foreground">No proposals.</p>
        )}
        {(sugg?.proposals ?? []).map((p) => (
          <div
            key={p.number}
            className="rounded-md border px-2.5 py-1.5"
            data-testid="proposal"
          >
            <div className={ROW}>
              <span className={MONO}>
                {p.number}. {at(p.member, p.scope)} consumes{" "}
                {at(p.provider, p.provider_scope)}
                {sel(p.select)}
              </span>
              {p.declared ? (
                <span className="text-muted-foreground">already declared</span>
              ) : (
                b("accept_suggestion", () => {
                  preview([{ op: "accept_suggestion", number: p.number }]);
                })
              )}
            </div>
            <ul className="m-0 list-none p-0 pl-3 font-mono text-[11.5px] text-muted-foreground">
              {p.evidence.map((e) => (
                <li key={`${e.member}${e.file}${String(e.line)}${e.key}`}>
                  {e.member !== "" ? e.member : "(project folder)"}: {e.file}
                  {e.line > 0 ? `:${String(e.line)}` : ""} {e.key}={e.value} (
                  {e.row})
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
      {pending !== null && (
        <div
          role="dialog"
          aria-label="Manifest diff preview"
          className="grid gap-2 rounded-lg border-2 p-3"
        >
          <b>Preview of {m.file}</b>
          <ul className="m-0 pl-4">
            {pending.r.changes.map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
          <pre
            className="m-0 max-h-[360px] overflow-auto rounded-md border p-2 font-mono text-[12px]"
            data-testid="manifest-diff"
          >
            {pending.r.diff.split("\n").map((l, i) => (
              <div
                key={i}
                className={
                  /^\+[^+]/.test(l)
                    ? "bg-linked-soft"
                    : /^-[^-]/.test(l)
                      ? "bg-unmatched-soft"
                      : ""
                }
              >
                {l || " "}
              </div>
            ))}
          </pre>
          {pending.r.problems.map((p) => (
            <p key={p} role="alert" className="m-0 text-dynamic">
              Not valid, so it cannot be written: {p}
            </p>
          ))}
          {pending.r.valid && (
            <p className="m-0 text-muted-foreground">
              After writing: {pending.r.reindex.reason}.
            </p>
          )}
          {(pending.r.retiring ?? []).map((rt) => (
            <div
              key={rt.repository}
              role="alert"
              className="grid gap-1 rounded-md border border-unmatched-bar bg-unmatched-soft px-2.5 py-2"
              data-testid="retiring"
            >
              <span>
                This rename deletes the stored index of <b>{rt.repository}</b> (
                {rt.nodes} nodes) from the store shared by all projects
                {rt.projects.length > 0
                  ? ` (stored link passes of ${rt.projects.join(", ")} list it)`
                  : ""}
                . It cannot be undone.
              </span>
              {rt.standalone && (
                <span className="font-medium">
                  {rt.repository} was indexed on its own: no stored link pass
                  lists it, so this may be the store&apos;s only copy.
                </span>
              )}
              <label className="flex items-center gap-1.5">
                <input
                  type="checkbox"
                  checked={confirmed.includes(rt.repository)}
                  onChange={(e) => {
                    const on = e.currentTarget.checked;
                    setConfirmed((c) =>
                      on
                        ? [...c, rt.repository]
                        : c.filter((x) => x !== rt.repository),
                    );
                  }}
                />
                {rt.confirm}
              </label>
            </div>
          ))}
          <div className={ROW}>
            <button
              type="button"
              className={`${BTN} bg-primary text-primary-foreground`}
              disabled={
                busy ||
                !pending.r.valid ||
                pending.r.diff === "" ||
                (pending.r.retiring ?? []).some(
                  (rt) => !confirmed.includes(rt.repository),
                )
              }
              onClick={() => {
                run(
                  postEdit(
                    m,
                    pending.ops,
                    pending.r.result_hash,
                    confirmed,
                  ).then((r) => {
                    setWritten(r);
                    setPending(null);
                    setOpen("");
                    setSugg(null);
                    load();
                    onWritten();
                  }),
                );
              }}
            >
              Write {m.file}
            </button>
            <button
              type="button"
              className={BTN}
              onClick={() => {
                setPending(null);
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
      {written !== null && (
        <div
          role="status"
          className={`${ROW} rounded-lg border border-dynamic-bar bg-dynamic-soft px-3 py-2`}
        >
          Written. The project links are stale until linked again.
          <button
            type="button"
            className={BTN}
            onClick={() => {
              run(
                fetchJSON(INDEX_RUN_PATH, {
                  method: "POST",
                  body:
                    written.reindex.kind === "link_pass"
                      ? { link_pass_only: true }
                      : {},
                }).then(() => {
                  setWritten(null);
                  onWritten();
                }),
              );
            }}
          >
            {written.reindex.kind === "link_pass"
              ? "Run the link pass"
              : "Re-index the project"}
          </button>
        </div>
      )}
    </section>
  );
}
