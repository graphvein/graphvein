// The try-it dialogs (T029 part B): the production warning, adding an
// environment through the guarded edit (TI-30), the environment settings
// (TI-48: pins, CA file, system proxy, limits) and a value typed for the
// session (TI-12). An edit always shows its preview (the diff of the file)
// and writes only on one explicit confirmation with the preview's hash.

import { KeyRound, ShieldAlert, TriangleAlert } from "lucide-react";
import { useState, type ReactNode } from "react";

import type { EditAnswer, EnvLimits, Environment, Environments, Pin, TryItClient } from "@/api/tryit";
import { Button } from "@/components/ui/button";
import { useApiRequest } from "@/hooks/useApiRequest";
import { cn } from "@/lib/utils";
import { useProject } from "@/shell/project";
import { bytesText, limitText, suggestedName } from "@/sections/explorer/tryit/model";
import { DiffBlock, Modal } from "@/sections/explorer/tryit/Modal";
import { rovingKey } from "@/sections/explorer/tryit/roving";
import { editFile, errorText, type AddPrefill } from "@/sections/explorer/tryit/TryIt";

const FIELD = "h-8 rounded-md border bg-background px-2 text-[13px]";

function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: ReactNode }) {
  return (
    <label className="grid gap-1 text-[12.5px]">
      <span className="text-muted-foreground">{label}</span>
      {children}
      {hint !== undefined && <span className="text-[11.5px] text-muted-foreground">{hint}</span>}
    </label>
  );
}

/** Preview shows what an edit would write: the change, problems and the
 * diff. */
function Preview({ edit }: { edit: EditAnswer }) {
  return (
    <div className="grid gap-1.5" data-testid="edit-preview">
      <p className="text-[12.5px]">
        <b className="font-semibold">Preview:</b> {edit.change !== "" ? edit.change : `changes ${edit.file}`}
      </p>
      {!edit.valid && (
        <ul className="list-disc pl-5 text-[12.5px] text-unmatched">
          {edit.problems.map((p) => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      )}
      <DiffBlock diff={edit.diff} />
    </div>
  );
}

/** EnvChip is an environment's label in its colour: production red, the
 * rest neutral. */
export function LabelChip({ env, className }: { env: Pick<Environment, "label" | "production">; className?: string }) {
  return (
    <span
      className={cn("inline-flex h-5 flex-none items-center rounded-full px-2 text-[11.5px] font-semibold", env.production ? "bg-unmatched text-background" : env.label === "staging" ? "bg-dynamic-soft text-dynamic" : "bg-linked-soft text-linked", className)}
      data-testid="env-label"
      data-production={env.production ? "true" : "false"}
    >
      {env.label}
    </span>
  );
}

/** ProductionDialog is the one warning shown when switching to an
 * environment labelled production (TI-10). In project mode it lists every
 * member the project environment covers with its base URL: onConfirm
 * receives exactly that list ({member: base URL}, null while it is still
 * being read), and the acknowledgement carries to another member only
 * when that member and its base URL were on it. reason says why the
 * warning is shown again. */
export function ProductionDialog({ env, client, reason, onCancel, onConfirm }: { env: Environment; client: TryItClient; reason?: string | null; onCancel: () => void; onConfirm: (listed: Record<string, string> | null) => void }) {
  const project = useProject();
  const names = project !== null && env.source === "project" ? project.project.members.map((m) => m.repository) : [];
  const members = useApiRequest(names.length > 0 ? JSON.stringify(["prod-members", env.id, names]) : null, async (signal) => {
    const out: { repository: string; url: string }[] = [];
    for (const n of names) {
      try {
        const e = await client.memberEnvironments(n, { signal });
        const m = e.environments.find((x) => x.id === env.id);
        if (m !== undefined) out.push({ repository: n, url: m.baseUrl });
      } catch (err) {
        if (signal.aborted) throw err;
      }
    }
    return out;
  });
  return (
    <Modal title={`Switch to ${env.name}, labelled ${env.label}?`} tone="danger" onClose={onCancel} testId="production-dialog">
      {reason != null && (
        <p className="rounded-md border border-dynamic/50 bg-dynamic-soft px-3 py-2 text-[12.5px]" data-testid="production-reason">
          {reason}
        </p>
      )}
      <div className="flex items-start gap-2">
        <ShieldAlert aria-hidden="true" className="mt-0.5 size-5 flex-none text-unmatched" />
        <div className="grid gap-1.5">
          <p>
            You labelled <b className="font-semibold">{env.name}</b> ({env.baseUrl}) <b className="font-semibold">{env.label}</b>. Requests there act on real data:
            even a GET can change data when the server does so on read.
          </p>
          <p className="text-muted-foreground">
            While it is selected every operation shows the production marker. POST, PUT, PATCH and DELETE still ask before each send. Switching to another environment ends this acknowledgement.
          </p>
        </div>
      </div>
      {names.length > 0 && (
        <div className="grid gap-1 rounded-md border px-3 py-2" data-testid="production-members">
          <span className="text-[12px] font-semibold">This project environment covers</span>
          {members.state.kind === "ok" ? (
            <ul className="grid gap-0.5 text-[12.5px]">
              {members.state.data.map((m) => (
                <li key={m.repository}>
                  <b className="font-medium">{m.repository}</b> <span className="font-mono text-[12px] text-muted-foreground">{m.url}</span>
                </li>
              ))}
            </ul>
          ) : (
            <span className="text-[12px] text-muted-foreground">reading the members…</span>
          )}
          <span className="text-[11.5px] text-muted-foreground">One acknowledgement for all of them: the server records it for each member when you work there.</span>
        </div>
      )}
      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" onClick={onCancel} data-autofocus>
          Cancel
        </Button>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => {
            onConfirm(names.length > 0 && members.state.kind === "ok" ? Object.fromEntries(members.state.data.map((m) => [m.repository, m.url])) : null);
          }}
          data-testid="production-confirm"
        >
          Switch to {env.label}
        </Button>
      </div>
    </Modal>
  );
}

type EditStep = { kind: "form" } | { kind: "busy" } | { kind: "preview"; edit: EditAnswer } | { kind: "failed"; message: string };

const LABELS = ["local", "staging", "production"];

function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return "";
  }
}

/** AddEnvironmentDialog adds an environment from a URL the user typed or a
 * suggestion they picked (TI-30): preview with the diff, then one
 * explicit "Allow and save". Nothing is dialable before it is written. */
export function AddEnvironmentDialog({ envs, client, prefill, onCancel, onApplied }: { envs: Environments; client: TryItClient; prefill: AddPrefill; onCancel: () => void; onApplied: (a: EditAnswer) => void }) {
  const targets = envs.files.filter((f) => f.writable);
  const [url, setUrl] = useState(prefill.url);
  const [name, setName] = useState(prefill.name ?? suggestedName(prefill.url));
  const [label, setLabel] = useState(prefill.label ?? "local");
  const [target, setTarget] = useState(targets[0]?.target ?? "repository");
  const [authOn, setAuthOn] = useState(false);
  const [header, setHeader] = useState("Authorization");
  const [variable, setVariable] = useState("");
  const [prefix, setPrefix] = useState("Bearer ");
  const [step, setStep] = useState<EditStep>({ kind: "form" });
  const file = editFile(envs, target);
  const host = hostOf(url);
  const body = (apply: boolean, resultHash: string) => ({
    target,
    base_hash: file?.hash ?? "absent",
    environment: { name, ...(target === "repository" || !envs.environments.some((e) => e.source === "project" && e.name === name) ? { label } : {}), base_url: url.trim(), ...(authOn && variable !== "" ? { auth: { header, env: variable, prefix } } : {}) },
    apply,
    result_hash: resultHash,
  });
  const reset = () => {
    setStep({ kind: "form" });
  };
  const preview = () => {
    setStep({ kind: "busy" });
    client.edit(body(false, ""), envs.token).then(
      (edit) => {
        setStep({ kind: "preview", edit });
      },
      (err: unknown) => {
        setStep({ kind: "failed", message: errorText(err) });
      },
    );
  };
  const apply = (edit: EditAnswer) => {
    setStep({ kind: "busy" });
    client.edit({ ...body(true, edit.resultHash), base_hash: edit.baseHash }, envs.token).then(onApplied, (err: unknown) => {
      setStep({ kind: "failed", message: errorText(err) });
    });
  };
  return (
    <Modal title={host !== "" ? `Allow requests to ${host}?` : "Add an environment"} tone="warn" wide onClose={onCancel} testId="add-environment">
      <p className="text-[12.5px]">
        {prefill.source !== undefined && prefill.source !== null ? (
          <>
            Suggested from {/:\d+$/.test(prefill.source) ? <span className="font-mono">{prefill.source}</span> : prefill.source}: nothing proves a server you mean to use answers there.
          </>
        ) : (
          <>Typed by you. </>
        )}{" "}
        Requests go through Graphvein&apos;s proxy (egress purpose <span className="font-mono">try-it</span>), only to this origin, and only after it is written to {file?.file ?? "graphvein.yaml"}.
      </p>
      <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,1fr)] gap-3 max-[700px]:grid-cols-1">
        <Field label="Base URL">
          <input
            className={cn(FIELD, "font-mono")}
            value={url}
            onChange={(e) => {
              setUrl(e.target.value);
              if (name === "" || name === suggestedName(url)) setName(suggestedName(e.target.value));
              reset();
            }}
            placeholder="https://api.staging.example.com"
            aria-label="Base URL"
            data-testid="add-url"
          />
        </Field>
        <Field label="Name">
          <input
            className={cn(FIELD, "font-mono")}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              reset();
            }}
            aria-label="Environment name"
          />
        </Field>
      </div>
      <div className="flex flex-wrap items-center gap-3 text-[12.5px]">
        <span className="text-muted-foreground">Label</span>
        <div
          role="radiogroup"
          aria-label="Label"
          className="inline-flex rounded-md bg-muted p-0.5"
          onKeyDown={(e) => {
            // A radio group checks the radio the arrow keys move to.
            rovingKey(e, "[role=radio]")?.click();
          }}
        >
          {LABELS.map((l, i) => (
            <button
              key={l}
              type="button"
              role="radio"
              aria-checked={label === l}
              tabIndex={label === l || (!LABELS.includes(label) && i === 0) ? 0 : -1}
              onClick={() => {
                setLabel(l);
                reset();
              }}
              className={cn("h-7 rounded-[5px] px-3", label === l ? "bg-background font-medium shadow-xs" : "text-muted-foreground")}
            >
              {l}
            </button>
          ))}
        </div>
        <input
          className={cn(FIELD, "w-[120px]")}
          value={LABELS.includes(label) ? "" : label}
          onChange={(e) => {
            setLabel(e.target.value === "" ? "local" : e.target.value);
            reset();
          }}
          placeholder="other label"
          aria-label="Other label"
        />
        {targets.length > 1 && (
          <label className="flex items-center gap-1.5">
            <span className="text-muted-foreground">Write to</span>
            <select
              className={FIELD}
              value={target}
              onChange={(e) => {
                setTarget(e.target.value);
                reset();
              }}
            >
              {targets.map((t) => (
                <option key={t.target} value={t.target}>
                  {t.file} ({t.target === "project" ? "every member" : "this repository"})
                </option>
              ))}
            </select>
          </label>
        )}
      </div>
      {label.toLowerCase() === "production" || label.toLowerCase() === "prod" ? (
        <p className="text-[12px] text-unmatched">A production label asks once when you switch to it and marks every operation while it is selected.</p>
      ) : null}
      <label className="flex items-center gap-2 text-[12.5px]">
        <input
          type="checkbox"
          checked={authOn}
          onChange={(e) => {
            setAuthOn(e.target.checked);
            reset();
          }}
        />
        Send an auth header from an environment variable of the server
      </label>
      {authOn && (
        <div className="grid grid-cols-3 gap-3 max-[700px]:grid-cols-1">
          <Field label="Header">
            <input className={cn(FIELD, "font-mono")} value={header} onChange={(e) => { setHeader(e.target.value); reset(); }} aria-label="Auth header" />
          </Field>
          <Field label="Variable (a name, never a value)">
            <input className={cn(FIELD, "font-mono")} value={variable} onChange={(e) => { setVariable(e.target.value); reset(); }} placeholder="API_TOKEN" aria-label="Auth variable" />
          </Field>
          <Field label="Prefix">
            <input className={cn(FIELD, "font-mono")} value={prefix} onChange={(e) => { setPrefix(e.target.value); reset(); }} aria-label="Auth prefix" />
          </Field>
        </div>
      )}
      {step.kind === "preview" && <Preview edit={step.edit} />}
      {step.kind === "failed" && (
        <p role="alert" className="text-[12.5px] text-unmatched">
          {step.message}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" onClick={onCancel} data-autofocus>
          Cancel
        </Button>
        {step.kind === "preview" && step.edit.valid ? (
          <Button
            size="sm"
            onClick={() => {
              apply(step.edit);
            }}
            data-testid="add-apply"
          >
            Allow and save
          </Button>
        ) : (
          <Button size="sm" variant="secondary" onClick={preview} disabled={step.kind === "busy" || url.trim() === "" || name === ""} data-testid="add-preview">
            Preview the change
          </Button>
        )}
      </div>
    </Modal>
  );
}

/** SettingsPreset pre-fills the settings dialog from a refusal: a
 * certificate to pin, a limit to raise. note explains why it opened. */
export interface SettingsPreset {
  addPin?: Pin;
  replacePins?: boolean;
  limit?: { limit: string; value: number };
  note?: ReactNode;
}

function limitInput(v: number | null): string {
  return v === null ? "" : String(v);
}

/** SettingsDialog edits an environment's options (TI-48): TLS pins, a CA
 * file, the system proxy and the limits, through the guarded edit. */
export function SettingsDialog({ env, envs, client, preset, onCancel, onApplied }: { env: Environment; envs: Environments; client: TryItClient; preset: SettingsPreset | null; onCancel: () => void; onApplied: () => void }) {
  const initialPins = (() => {
    const pins = preset?.replacePins === true ? [] : [...(env.options.tls?.pins ?? [])];
    if (preset?.addPin !== undefined && !pins.some((p) => p.sha256 === preset.addPin?.sha256)) pins.push(preset.addPin);
    return pins;
  })();
  const own: EnvLimits = env.options.limits ?? { request_bytes: null, response_bytes: null, timeout_seconds: null };
  const raised = (k: keyof EnvLimits) => (preset?.limit?.limit === k ? preset.limit.value : own[k]);
  const [pins, setPins] = useState<Pin[]>(initialPins);
  const [caFile, setCaFile] = useState(env.options.tls?.ca_file ?? "");
  const [proxy, setProxy] = useState(env.options.use_system_proxy);
  const [limits, setLimits] = useState({ request_bytes: limitInput(raised("request_bytes")), response_bytes: limitInput(raised("response_bytes")), timeout_seconds: limitInput(raised("timeout_seconds")) });
  const [step, setStep] = useState<EditStep>({ kind: "form" });
  const file = editFile(envs, env.source === "project" ? "project" : "repository");
  const reset = () => {
    setStep({ kind: "form" });
  };
  const num = (s: string): number | null => (s.trim() === "" ? null : Number(s));
  const options = () => {
    const l = { request_bytes: num(limits.request_bytes), response_bytes: num(limits.response_bytes), timeout_seconds: num(limits.timeout_seconds) };
    const anyLimit = l.request_bytes !== null || l.response_bytes !== null || l.timeout_seconds !== null;
    return {
      name: env.name,
      tls: pins.length > 0 || caFile.trim() !== "" ? { pins, ca_file: caFile.trim() === "" ? null : caFile.trim() } : null,
      use_system_proxy: proxy,
      limits: anyLimit ? l : null,
    };
  };
  const body = (apply: boolean, base: string, resultHash: string) => ({ target: env.source === "project" ? "project" : "repository", base_hash: base, options: options(), apply, result_hash: resultHash });
  const preview = () => {
    setStep({ kind: "busy" });
    client.edit(body(false, file?.hash ?? "absent", ""), envs.token).then(
      (edit) => {
        setStep({ kind: "preview", edit });
      },
      (err: unknown) => {
        setStep({ kind: "failed", message: errorText(err) });
      },
    );
  };
  const apply = (edit: EditAnswer) => {
    setStep({ kind: "busy" });
    client.edit(body(true, edit.baseHash, edit.resultHash), envs.token).then(onApplied, (err: unknown) => {
      setStep({ kind: "failed", message: errorText(err) });
    });
  };
  const ceilings = envs.ceilings;
  const limitRow = (k: keyof typeof limits, title: string) => (
    <Field label={title} hint={`now ${limitText(k, env.effectiveLimits[k])}; at most ${limitText(k, ceilings[k])}; empty: the default`}>
      <input
        className={cn(FIELD, "font-mono", preset?.limit?.limit === k && "border-brand")}
        inputMode="numeric"
        value={limits[k]}
        onChange={(e) => {
          setLimits({ ...limits, [k]: e.target.value.replace(/[^0-9]/g, "") });
          reset();
        }}
        aria-label={title}
        data-testid={`limit-${k}`}
      />
    </Field>
  );
  return (
    <Modal title={`Settings of ${env.name}`} wide onClose={onCancel} testId="environment-settings">
      {preset?.note !== undefined && <div className="rounded-md border border-dynamic/40 bg-dynamic-soft px-3 py-2 text-[12.5px]">{preset.note}</div>}
      <p className="text-[12px] text-muted-foreground">
        Written to {file?.file ?? (env.source === "project" ? "graphvein-project.yaml" : "graphvein.yaml")} through a preview. Changing the trusted certificates or the proxy makes it a new destination: it is selected again
        {env.production ? ", with the production warning" : ""}.
      </p>
      <div className="grid gap-1.5">
        <span className="text-[12.5px] font-semibold">Trusted certificates (pins) for {env.origin}</span>
        {pins.length === 0 ? (
          <span className="text-[12px] text-muted-foreground">none: only certificates the system roots{caFile !== "" ? " or the CA file" : ""} trust are accepted</span>
        ) : (
          <ul className="grid gap-1" data-testid="pin-list">
            {pins.map((p) => (
              <li key={p.sha256} className={cn("flex flex-wrap items-center gap-2 rounded-md border px-2 py-1 font-mono text-[11.5px]", p.sha256 === preset?.addPin?.sha256 && "border-brand bg-brand-soft")}>
                <span className="min-w-0 [overflow-wrap:anywhere]">{p.sha256}</span>
                {p.origin !== null && <span className="text-muted-foreground">{p.origin}</span>}
                {p.sha256 === preset?.addPin?.sha256 && <span className="font-sans text-[11px] text-brand-text">new</span>}
                <button
                  type="button"
                  className="ml-auto font-sans text-[11.5px] text-muted-foreground underline"
                  onClick={() => {
                    setPins(pins.filter((x) => x !== p));
                    reset();
                  }}
                >
                  remove
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
      <div className="grid grid-cols-2 gap-3 max-[700px]:grid-cols-1">
        <Field label="CA file (PEM, a path relative to the repository or absolute)" hint="trusted in addition to the system roots, for this environment only">
          <input
            className={cn(FIELD, "font-mono")}
            value={caFile}
            onChange={(e) => {
              setCaFile(e.target.value);
              reset();
            }}
            placeholder="certs/rootCA.pem"
            aria-label="CA file"
          />
        </Field>
        <label className="flex items-start gap-2 self-end text-[12.5px]">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={proxy}
            onChange={(e) => {
              setProxy(e.target.checked);
              reset();
            }}
            aria-label="Use the system proxy"
          />
          <span>
            Use the system proxy (HTTPS_PROXY, HTTP_PROXY, NO_PROXY of the server)
            {proxy && env.baseUrl.startsWith("http://") && <span className="block text-[11.5px] text-dynamic">An http:// environment passes the proxy in clear, headers and the auth value included.</span>}
          </span>
        </label>
      </div>
      <div className="grid grid-cols-3 gap-3 max-[700px]:grid-cols-1">
        {limitRow("request_bytes", "Request body (bytes)")}
        {limitRow("response_bytes", "Response read (bytes)")}
        {limitRow("timeout_seconds", "Time limit (seconds)")}
      </div>
      {step.kind === "preview" && <Preview edit={step.edit} />}
      {step.kind === "failed" && (
        <p role="alert" className="text-[12.5px] text-unmatched">
          {step.message}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" onClick={onCancel} data-autofocus>
          Cancel
        </Button>
        {step.kind === "preview" && step.edit.valid ? (
          <Button
            size="sm"
            onClick={() => {
              apply(step.edit);
            }}
            data-testid="settings-apply"
          >
            Save {env.name}
          </Button>
        ) : (
          <Button size="sm" variant="secondary" onClick={preview} disabled={step.kind === "busy"} data-testid="settings-preview">
            Preview the change
          </Button>
        )}
      </div>
    </Modal>
  );
}

/** SessionAuthDialog takes a value for this server session (TI-12): kept
 * in server memory only, never shown again, cleared from the bar. */
export function SessionAuthDialog({ env, client, token, onCancel, onSaved }: { env: Environment; client: TryItClient; token: string; onCancel: () => void; onSaved: () => void }) {
  const [header, setHeader] = useState(env.auth.header ?? "Authorization");
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  const prefix = env.auth.prefix ?? "";
  const save = () => {
    const v = value;
    setValue("");
    // The prefix is the environment's; a typed value replaces the whole
    // header value, so it is added here as the variable would get it.
    client.auth({ environment: env.id, value: `${prefix}${v}`, ...(env.auth.header === null ? { header } : {}) }, token).then(onSaved, (err: unknown) => {
      setError(errorText(err));
    });
  };
  return (
    <Modal title={`A value for ${env.name}, for this session`} onClose={onCancel} testId="session-auth">
      <div className="flex items-start gap-2 text-[12.5px]">
        <KeyRound aria-hidden="true" className="mt-0.5 size-4 flex-none text-muted-foreground" />
        <p>Kept in the server&apos;s memory only: never written, logged or shown again. It wins over the environment variable and ends when you clear it or the server stops.</p>
      </div>
      {env.auth.header === null && (
        <Field label="Header">
          <input className={cn(FIELD, "font-mono")} value={header} onChange={(e) => { setHeader(e.target.value); }} aria-label="Header" />
        </Field>
      )}
      <Field label={`${env.auth.header ?? header}${prefix !== "" ? ` (sent as “${prefix}” + the value)` : ""}`}>
        <input
          type="password"
          autoComplete="off"
          className={cn(FIELD, "font-mono")}
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
          }}
          aria-label="Value"
          data-testid="session-value"
        />
      </Field>
      {error !== null && (
        <p role="alert" className="text-[12.5px] text-unmatched">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" onClick={onCancel} data-autofocus>
          Cancel
        </Button>
        <Button size="sm" onClick={save} disabled={value === ""} data-testid="session-save">
          Keep for this session
        </Button>
      </div>
    </Modal>
  );
}

/** WarnNote is a short amber note with an icon. */
export function WarnNote({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <p className="flex items-start gap-1.5 text-[12px] text-dynamic" data-testid={testId}>
      <TriangleAlert aria-hidden="true" className="mt-px size-3.5 flex-none" />
      <span>{children}</span>
    </p>
  );
}

export { bytesText };
