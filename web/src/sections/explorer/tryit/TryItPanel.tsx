// "Try it out" inside an expanded operation (T029 part B, variant B:
// request left, response right). The request builder pre-fills what the
// document knows (path parameters, query and header names with their
// provenance, a JSON template from the body's schema, marked as a
// template); the server builds the URL and decides every guard. The flow
// of a send:
//
//   prepare (nothing is sent) -> auth missing?  dialog: send without auth,
//                                               or enter a value for the
//                                               session
//                             -> confirmation?  in place of Send: the exact
//                                               request and the reasons;
//                                               "don't ask again for this
//                                               environment until I switch"
//                             -> otherwise send (GET and HEAD outside
//                                production, or a remembered grant)
//
// A refusal the user can act on opens its dialog: an untrusted or changed
// certificate (pin it through the settings edit), a limit (raise it), a
// redirect to another origin (add it as an environment and follow).

import { ClipboardCopy, Play, Plus, ShieldAlert, Trash2, TriangleAlert, Upload } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { ApiError } from "@/api/client";
import type { DocOperation, OpenApiDocument } from "@/api/openapi";
import { authMissingDetails, certificateDetails, limitDetails, type CertificateDetails, type Environment, type Part, type Prepared, type Sent, type TryRequest } from "@/api/tryit";
import { Button } from "@/components/ui/button";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { ProvenanceChip } from "@/sections/explorer/chips";
import { LabelChip } from "@/sections/explorer/tryit/dialogs";
import { Modal } from "@/sections/explorer/tryit/Modal";
import { CopyButton } from "@/sections/explorer/tryit/CopyButton";
import { bodyTemplate, bytesText, curlCommand, fileToBase64, isMultipart, LIMIT_NAME, limitText, newRow, opensslFingerprint, pathParams, prefillRows, unavailableReason, type KeyValue } from "@/sections/explorer/tryit/model";
import { authFrom, ResponseView } from "@/sections/explorer/tryit/ResponseView";
import { errorText, useTryIt, type TryItContextValue } from "@/sections/explorer/tryit/TryIt";

const HEAD = "text-[11.5px] font-semibold tracking-[.06em] text-muted-foreground uppercase";
const INPUT = "h-8 min-w-0 rounded-md border bg-background px-2 font-mono text-[12.5px]";
const METHODS = ["GET", "PUT", "POST", "DELETE", "OPTIONS", "HEAD", "PATCH", "TRACE"];

type BodyMode = "none" | "raw" | "template" | "multipart";

interface MultipartRow {
  id: number;
  name: string;
  value: string;
  file: { name: string; type: string; data: string; size: number } | null;
}

type Flow =
  | { kind: "idle" }
  | { kind: "busy"; what: string }
  | { kind: "confirm"; req: TryRequest; prepared: Prepared; remember: boolean }
  | { kind: "auth-missing"; req: TryRequest; header: string; env: string }
  | { kind: "certificate"; details: CertificateDetails & { changed: boolean }; environment: string | null }
  | { kind: "error"; error: unknown; req: TryRequest | null }
  /** A confirmation (or the auth question) was prepared for an
   * environment that is no longer selected: it is closed, nothing sent. */
  | { kind: "dropped"; method: string; from: string; to: string | null };

/** KeyValues is the query or header table of the builder. */
function KeyValues({ title, rows, setRows, addText }: { title: string; rows: KeyValue[]; setRows: (r: KeyValue[]) => void; addText: string }) {
  const update = (id: number, patch: Partial<KeyValue>) => {
    setRows(rows.map((r) => (r.id === id ? { ...r, ...patch } : r)));
  };
  return (
    <div className="grid gap-1" data-testid={`builder-${title.toLowerCase()}`}>
      <div className="text-[12.5px] font-semibold">{title}</div>
      {rows.length > 0 && (
        <ul className="grid gap-1">
          {rows.map((r) => (
            <li key={r.id} className="grid grid-cols-[auto_minmax(0,1fr)_minmax(0,1.3fr)_auto] items-center gap-1.5 max-[1100px]:grid-cols-[auto_minmax(0,1fr)_minmax(0,1fr)_auto]" data-testid="builder-row">
              <input
                type="checkbox"
                checked={r.enabled}
                onChange={(e) => {
                  update(r.id, { enabled: e.target.checked });
                }}
                aria-label={`send ${r.name || "this row"}`}
              />
              <input
                className={INPUT}
                value={r.name}
                readOnly={r.provenance !== null}
                onChange={(e) => {
                  update(r.id, { name: e.target.value });
                }}
                placeholder="name"
                aria-label={`${title} name`}
              />
              <input
                className={INPUT}
                value={r.value}
                onChange={(e) => {
                  update(r.id, { value: e.target.value, enabled: true });
                }}
                placeholder={r.type ?? "value"}
                aria-label={`${title} value ${r.name}`}
              />
              <button
                type="button"
                className="rounded p-1 text-muted-foreground hover:bg-accent"
                aria-label={`remove ${r.name}`}
                onClick={() => {
                  setRows(rows.filter((x) => x.id !== r.id));
                }}
              >
                <Trash2 aria-hidden="true" className="size-3.5" />
              </button>
              {r.provenance !== null && (
                <div className="col-span-3 col-start-2 flex flex-wrap items-center gap-1.5 text-[11.5px] text-muted-foreground">
                  <ProvenanceChip p={r.provenance} />
                  <span>{r.type}</span>
                  <span>{r.required === true ? "required" : r.required === false ? "optional" : "required? not read"}</span>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      <Button
        size="xs"
        variant="ghost"
        className="justify-self-start"
        onClick={() => {
          setRows([...rows, newRow()]);
        }}
      >
        <Plus aria-hidden="true" />
        {addText}
      </Button>
    </div>
  );
}

/** Confirmation asks in place of Send (variant B): the exact request the
 * server prepared and why it asks. */
function Confirmation({ flow, env, onCancel, onSend, onRemember }: { flow: Extract<Flow, { kind: "confirm" }>; env: Environment | null; onCancel: () => void; onSend: () => void; onRemember: (v: boolean) => void }) {
  const style = useLabelStyle();
  const p = flow.prepared;
  const danger = p.method === "DELETE" || p.production;
  return (
    <div className={cn("grid gap-2 rounded-md border px-3 py-2.5 text-[12.5px]", danger ? "border-unmatched/60 bg-unmatched-soft" : "border-dynamic/50 bg-dynamic-soft")} role="group" aria-label="Confirm the request" data-testid="tryit-confirm">
      <div className="flex items-center gap-1.5 text-[13.5px] font-semibold">
        <TriangleAlert aria-hidden="true" className={cn("size-4", danger ? "text-unmatched" : "text-dynamic")} />
        Send {p.method} to {env !== null && <LabelChip env={env} />}
        {env?.name ?? p.environment}?
      </div>
      <div className="min-w-0 font-mono text-[12px] [overflow-wrap:anywhere]" data-testid="confirm-request">
        <b>{p.method}</b> {p.url}
      </div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-[12px]">
        <dt className="text-muted-foreground">Headers</dt>
        <dd className="font-mono [overflow-wrap:anywhere]">{p.headers.length > 0 ? p.headers.join(", ") : "none typed"}</dd>
        <dt className="text-muted-foreground">Auth</dt>
        <dd>
          {p.authHeader !== null ? (
            <>
              <span className="font-mono">{p.authHeader}</span> {authFrom(p, env)} (value never shown)
            </>
          ) : p.sendWithoutAuth ? (
            "sent without auth (your choice)"
          ) : (
            "none configured"
          )}
        </dd>
        <dt className="text-muted-foreground">Body</dt>
        <dd>
          {p.multipart !== null
            ? p.multipart.map((m) => `${m.name}${m.filename !== null ? ` (${m.filename}, ${bytesText(m.bytes)})` : ` (${bytesText(m.bytes)})`}`).join(", ")
            : p.bodyBytes > 0
              ? (
                <span className="flex flex-wrap items-center gap-x-1.5">
                  {bytesText(p.bodyBytes)} · sha256{" "}
                  <span className="font-mono text-[11.5px] break-all select-all" data-testid="confirm-sha256">
                    {p.bodySha256}
                  </span>
                  <CopyButton text={p.bodySha256} what="the body SHA-256" />
                </span>
              )
              : "none"}
        </dd>
        {p.proxy !== null && (
          <>
            <dt className="text-muted-foreground">Proxy</dt>
            <dd className={cn(p.proxyCleartext && "text-dynamic")}>
              {p.proxy}
              {p.proxyCleartext ? ": http://, the proxy sees the request in clear, auth value included" : ""}
            </dd>
          </>
        )}
      </dl>
      <ul className="grid gap-0.5" data-testid="confirm-reasons">
        {p.confirmationReasons.map((r) => {
          const l = label(style, "confirmationReason", r);
          return (
            <li key={r} className="flex items-center gap-1.5" title={l.title} data-reason={r}>
              <span className="size-1.5 flex-none rounded-full bg-current" aria-hidden="true" />
              {l.text}
            </li>
          );
        })}
      </ul>
      {p.production && <p className="font-medium text-unmatched">{p.environment} is labelled {p.label}: this acts on real data.</p>}
      {!p.freeForm && (
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={flow.remember}
            onChange={(e) => {
              onRemember(e.target.checked);
            }}
            data-testid="confirm-remember"
          />
          Don&apos;t ask again for {env?.name ?? "this environment"} until I switch
        </label>
      )}
      <div className="flex gap-2">
        <Button size="sm" variant="outline" onClick={onCancel} data-autofocus autoFocus>
          Cancel
        </Button>
        <Button size="sm" variant={danger ? "destructive" : "default"} onClick={onSend} data-testid="confirm-send">
          Send {p.method}
        </Button>
      </div>
    </div>
  );
}

/** Fingerprint writes a SHA-256 in openssl's form, wrapping only after a
 * colon so a hex pair is never split across lines. */
function Fingerprint({ sha256 }: { sha256: string }) {
  const pairs = opensslFingerprint(sha256).split(":");
  return (
    <>
      {pairs.map((p, i) => (
        <span key={i}>
          {p}
          {i < pairs.length - 1 && (
            <>
              :<wbr />
            </>
          )}
        </span>
      ))}
    </>
  );
}

/** CertificateDialog shows a certificate the server refused (TI-29) and
 * offers to trust it for this environment through the settings edit. */
function CertificateDialog({ d, env, onClose, onTrust }: { d: CertificateDetails & { changed: boolean }; env: Environment | null; onClose: () => void; onTrust: () => void }) {
  const c = d.certificate;
  return (
    <Modal title={d.changed ? `The certificate of ${d.origin} changed` : `${d.origin} presented a certificate that is not trusted`} tone={d.changed ? "danger" : "warn"} wide onClose={onClose} testId="certificate-dialog">
      {d.changed && (
        <p className="flex items-start gap-1.5 font-medium text-unmatched">
          <ShieldAlert aria-hidden="true" className="mt-0.5 size-4 flex-none" />
          You pinned another certificate for this origin. A changed certificate can mean the server was reissued, or that something else answers in its place. Trust it only if you know why it changed.
        </p>
      )}
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-[12.5px]" data-testid="certificate-details">
        <dt className="text-muted-foreground">Subject</dt>
        <dd className="font-mono [overflow-wrap:anywhere]">{c.subject}</dd>
        <dt className="text-muted-foreground">Issuer</dt>
        <dd className="font-mono [overflow-wrap:anywhere]">
          {c.issuer}
          {c.self_signed ? <span className="font-sans text-muted-foreground"> (self-signed)</span> : null}
        </dd>
        <dt className="text-muted-foreground">Names (SANs)</dt>
        <dd className="font-mono [overflow-wrap:anywhere]">{c.sans.length > 0 ? c.sans.join(", ") : "none"}</dd>
        <dt className="text-muted-foreground">Valid</dt>
        <dd className="font-mono">
          {c.not_before} to {c.not_after}
        </dd>
        <dt className="text-muted-foreground">SHA-256</dt>
        <dd className="flex flex-wrap items-center gap-x-1.5">
          <span className="font-mono text-[12px] select-all" data-testid="certificate-sha256">
            <Fingerprint sha256={c.sha256} />
          </span>
          <CopyButton text={opensslFingerprint(c.sha256)} what="the certificate SHA-256" testId="certificate-copy" />
        </dd>
        {d.changed && (
          <>
            <dt className="text-muted-foreground">Pinned before</dt>
            <dd className="grid font-mono text-[12px]" data-testid="certificate-old">
              {d.pinnedSha256.map((s) => (
                <span key={s}>
                  <Fingerprint sha256={s} />
                </span>
              ))}
            </dd>
          </>
        )}
        <dt className="text-muted-foreground">Why refused</dt>
        <dd className="[overflow-wrap:anywhere]">{d.reason}</dd>
      </dl>
      <p className="text-[12px] text-muted-foreground">
        Compare the SHA-256 with the server&apos;s own: <span className="font-mono">openssl x509 -noout -fingerprint -sha256 -in cert.pem</span> prints it in this form (upper-case pairs with colons). Trusting writes the fingerprint
        into {env?.name ?? "the environment"}&apos;s settings after a preview; it applies to {d.origin} only.
      </p>
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="outline" onClick={onClose} data-autofocus>
          Cancel
        </Button>
        <Button size="sm" variant={d.changed ? "destructive" : "default"} onClick={onTrust} disabled={env === null} data-testid="certificate-trust">
          Trust this certificate for this environment…
        </Button>
      </div>
    </Modal>
  );
}

/** ErrorBox shows a refusal with what the user can do about it. env is
 * the environment the refused request was prepared for; the selection
 * may have changed since, and the action offered depends on it. */
function ErrorBox({ error, t, env, onRetry, onRetryIn }: { error: unknown; t: TryItContextValue; env: Environment | null; onRetry: (() => void) | null; onRetryIn: ((env: Environment) => void) | null }) {
  const limit = limitDetails(error);
  const code = error instanceof ApiError ? error.code : undefined;
  return (
    <div role="alert" className="grid gap-1.5 rounded-md border border-unmatched/40 bg-unmatched-soft px-3 py-2 text-[12.5px]" data-testid="tryit-error" data-code={code}>
      <span className="[overflow-wrap:anywhere]">{errorText(error)}</span>
      {limit !== null && env !== null && (
        <span className="flex flex-wrap items-center gap-2">
          {LIMIT_NAME[limit.limit] ?? limit.limit} {limitText(limit.limit, limit.value)}, at most {limitText(limit.limit, limit.ceiling)}.
          {limit.value < limit.ceiling && (
            <Button
              size="xs"
              variant="outline"
              onClick={() => {
                t.openSettings(env, { limit: { limit: limit.limit, value: Math.min(limit.ceiling, limit.value * 4) }, note: `The ${LIMIT_NAME[limit.limit] ?? limit.limit} refused the request.` });
              }}
              data-testid="raise-limit"
            >
              Raise to {limitText(limit.limit, Math.min(limit.ceiling, limit.value * 4))}…
            </Button>
          )}
        </span>
      )}
      {(code === "environment_not_selected" || code === "production_acknowledgement_required") &&
        (env !== null && t.selected?.id === env.id ? (
          // The server lost the switch (another tab, a restart): select
          // the same environment again, through the warning.
          <Button
            size="xs"
            variant="outline"
            className="justify-self-start"
            onClick={() => {
              t.select(env, onRetry ?? undefined, true);
            }}
            data-testid="select-again"
          >
            Select {env.name} again
          </Button>
        ) : t.selected !== null && onRetryIn !== null ? (
          <span className="flex flex-wrap items-center gap-2" data-testid="switched">
            This request was prepared for {env?.name ?? "another environment"}; {t.selected.name} is selected now.
            <Button
              size="xs"
              variant="outline"
              onClick={() => {
                if (t.selected !== null) onRetryIn(t.selected);
              }}
              data-testid="prepare-in-selected"
            >
              Prepare it again for {t.selected.name}
            </Button>
          </span>
        ) : (
          <span data-testid="switched">No environment is selected now: choose one in the bar above.</span>
        ))}
      {code === "confirmation_required" && onRetry !== null && (
        <Button size="xs" variant="outline" className="justify-self-start" onClick={onRetry}>
          Prepare it again
        </Button>
      )}
    </div>
  );
}

/** TryItPanel is the Try it out area of one operation. */
export function TryItPanel({ op, doc, onClose }: { op: DocOperation; doc: OpenApiDocument; onClose: () => void }) {
  const t = useTryIt();
  const params = pathParams(op);
  const template = bodyTemplate(op, doc);
  const multipartOp = isMultipart(op);
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(params.map((p) => [p.name, ""])));
  const [query, setQuery] = useState<KeyValue[]>(() => prefillRows(op, doc, "query"));
  const [headers, setHeaders] = useState<KeyValue[]>(() => prefillRows(op, doc, "header"));
  const [mode, setMode] = useState<BodyMode>(() => (multipartOp ? "multipart" : template !== null ? "template" : op.method === "get" || op.method === "head" || op.method === "delete" ? "none" : "raw"));
  const [raw, setRaw] = useState(() => template?.text ?? "");
  const [contentType, setContentType] = useState(() => template?.contentType ?? "application/json");
  const [parts, setParts] = useState<MultipartRow[]>(() => [{ id: 1, name: "", value: "", file: null }]);
  const [freeForm, setFreeForm] = useState(false);
  const [ffPath, setFfPath] = useState(op.key.replace(/\{[^}]+\}/g, ""));
  const [ffMethod, setFfMethod] = useState(op.method.toUpperCase());
  const [flow, setFlow] = useState<Flow>({ kind: "idle" });
  const [response, setResponse] = useState<{ sent: Sent; prepared: Prepared | null; req: TryRequest } | null>(null);
  const [curl, setCurl] = useState<string | null>(null);
  const latest = useRef(t);
  useEffect(() => {
    latest.current = t;
  });
  // A confirmation binds to the environment it was prepared for: when the
  // selection changes while one is open, it is closed with a message
  // instead of being shown under the new environment's name.
  // Focus (keyboard users): opening the panel moves focus to its first
  // field; when a confirmation closes and focus fell to the page, it
  // returns to Send.
  const panelRef = useRef<HTMLDivElement>(null);
  const sendRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const panel = panelRef.current;
    if (panel === null) return;
    const field = panel.querySelector<HTMLElement>("[data-testid=tryit-request] :is(input, select, textarea)") ?? panel.querySelector<HTMLElement>("h4");
    field?.focus();
  }, []);
  const prevKind = useRef(flow.kind);
  const returnFocus = useRef(false);
  useEffect(() => {
    if (prevKind.current === "confirm" && flow.kind !== "confirm") returnFocus.current = true;
    prevKind.current = flow.kind;
    // While a send runs the button is disabled and cannot take focus.
    if (!returnFocus.current || flow.kind === "busy" || flow.kind === "confirm") return;
    returnFocus.current = false;
    const active = document.activeElement;
    if (active === null || active === document.body) sendRef.current?.focus();
  }, [flow.kind]);
  const selectedId = t?.selected?.id ?? null;
  const selectedName = t?.selected?.name ?? null;
  useEffect(() => {
    setFlow((f) => {
      if ((f.kind !== "confirm" && f.kind !== "auth-missing") || f.req.environment === selectedId) return f;
      const from = latest.current?.envs?.environments.find((e) => e.id === f.req.environment)?.name ?? f.req.environment.replace(/^[a-z]+:/, "");
      return { kind: "dropped", method: f.req.method, from, to: selectedName };
    });
  }, [selectedId, selectedName]);
  if (t === null) return null;
  const env = t.selected;
  /** envOf is the environment a request or response was made for, which
   * may no longer be the selected one. */
  const envOf = (id: string): Environment | null => t.envs?.environments.find((e) => e.id === id) ?? null;
  const unavailable = unavailableReason(t.envs?.environments ?? null, env);

  const multipart = (): Part[] =>
    parts
      .filter((p) => p.name !== "")
      .map((p) => (p.file !== null ? { name: p.name, filename: p.file.name, content_type: p.file.type || "application/octet-stream", data_base64: p.file.data } : { name: p.name, value: p.value }));

  const build = (envId: string): TryRequest => {
    const pairs = (rows: KeyValue[]) => rows.filter((r) => r.enabled && r.name !== "").map((r) => ({ name: r.name, value: r.value }));
    const req: TryRequest = {
      environment: envId,
      route: op.route,
      path: freeForm ? ffPath : op.key,
      method: freeForm ? ffMethod : op.method.toUpperCase(),
      query: pairs(query),
      headers: pairs(headers),
    };
    if (freeForm) req.free_form = true;
    else req.path_values = values;
    if (mode === "multipart") req.multipart = multipart();
    else if (mode !== "none" && raw !== "") {
      req.body = raw;
      if (contentType !== "") req.content_type = contentType;
    }
    return req;
  };

  const token = () => latest.current?.envs?.token ?? "";

  const fail = (error: unknown, req: TryRequest | null) => {
    const cert = certificateDetails(error);
    if (cert !== null) {
      setFlow({ kind: "certificate", details: cert, environment: req?.environment ?? null });
      return;
    }
    const missing = authMissingDetails(error);
    if (missing !== null && req !== null) {
      setFlow({ kind: "auth-missing", req, header: missing.header, env: missing.env });
      return;
    }
    setFlow({ kind: "error", error, req });
  };

  const send = (req: TryRequest, prepared: Prepared | null) => {
    setFlow({ kind: "busy", what: `Sending ${req.method}…` });
    t.client.send(req, token()).then(
      (sent) => {
        setResponse({ sent, prepared, req });
        setFlow({ kind: "idle" });
        if (req.remember === true || sent.remembered) void latest.current?.reload();
      },
      (err: unknown) => {
        fail(err, req);
      },
    );
  };

  /** start runs a request through prepare and the decisions it asks for. */
  const start = (req: TryRequest) => {
    setFlow({ kind: "busy", what: "Preparing…" });
    setCurl(null);
    t.client.prepare(req, token()).then(
      (p) => {
        if (p.authMissing !== null && req.send_without_auth !== true) {
          setFlow({ kind: "auth-missing", req, header: p.authMissing.header, env: p.authMissing.env });
          return;
        }
        if (p.acknowledgementRequired || !p.selected) {
          // The switch was not recorded (another tab switched): select
          // again, through the production warning when it is one.
          const e = latest.current?.envs?.environments.find((x) => x.id === req.environment);
          setFlow({ kind: "idle" });
          if (e !== undefined)
            latest.current?.select(e, () => {
              start(req);
            });
          return;
        }
        if (p.confirmationRequired) {
          setFlow({ kind: "confirm", req, prepared: p, remember: false });
          return;
        }
        send(req, p);
      },
      (err: unknown) => {
        fail(err, req);
      },
    );
  };

  const copyCurl = () => {
    if (env === null) return;
    const req = build(env.id);
    t.client.prepare(req, token()).then(
      (p) => {
        const text = curlCommand(p, env, { headers: req.headers ?? [], body: req.body ?? null, contentType: req.content_type ?? "", multipart: req.multipart ?? null });
        setCurl(text);
        // The text stays on the page when the clipboard is not available.
        try {
          navigator.clipboard.writeText(text).catch(() => undefined);
        } catch {
          // No clipboard (an insecure context or a test DOM).
        }
      },
      (err: unknown) => {
        fail(err, req);
      },
    );
  };

  /** follow sends a redirect's target once, as a free-form request in the
   * environment that holds it (TI-46): with its own confirmation and that
   * environment's production rules. */
  const followIn = (environmentId: string, sent: Sent, req: TryRequest) => {
    const f = sent.redirectFollow;
    if (f === null) return;
    const next: TryRequest = { environment: environmentId, route: op.route, path: f.path, method: f.method, free_form: true, query: f.query, headers: req.headers ?? [] };
    if (f.keepsBody) {
      if (req.multipart !== undefined) next.multipart = req.multipart;
      if (req.body !== undefined) next.body = req.body;
      if (req.content_type !== undefined) next.content_type = req.content_type;
    }
    start(next);
  };
  const onFollow =
    response?.sent.redirectFollow != null
      ? () => {
          const { sent, req } = response;
          const f = sent.redirectFollow;
          if (f === null) return;
          if (f.environment !== null) {
            const target = t.envs?.environments.find((e) => e.id === f.environment);
            if (target === undefined) return;
            if (target.selected) followIn(target.id, sent, req);
            else
              t.select(target, () => {
                followIn(target.id, sent, req);
              });
          } else if (f.suggestedEnvironment !== null) {
            t.openAdd({
              url: f.suggestedEnvironment.base_url,
              name: f.suggestedEnvironment.name,
              source: `the redirect of ${sent.method} ${sent.url}`,
              then: (id) => {
                followIn(id, sent, req);
              },
            });
          }
        }
      : null;

  const method = freeForm ? ffMethod : op.method.toUpperCase();
  const missing = !freeForm && params.some((p) => (values[p.name] ?? "") === "");
  const asks = freeForm || !(method === "GET" || method === "HEAD");
  const sendText = asks && env?.remembered !== true ? `Send ${method}…` : `Send ${method}`;

  return (
    <div ref={panelRef} className="grid gap-3 rounded-md border bg-background p-3" data-testid="tryit-panel">
      <div className="flex flex-wrap items-center gap-2">
        <h4 className="text-[14px] font-semibold outline-none" tabIndex={-1}>
          Try it out
        </h4>
        {env !== null && (
          <span className="flex min-w-0 flex-wrap items-center gap-1.5 text-[12.5px] text-muted-foreground" data-testid="tryit-env">
            in <LabelChip env={env} /> <b className="text-foreground">{env.name}</b> <span className="font-mono text-[12px]">{env.baseUrl}</span> · environment and auth from the bar above
          </span>
        )}
        <Button size="xs" variant="outline" className="ml-auto" onClick={onClose}>
          Close
        </Button>
      </div>
      {env?.production === true && (
        <p className="flex items-center gap-1.5 text-[12.5px] font-medium text-unmatched" data-testid="tryit-production">
          <ShieldAlert aria-hidden="true" className="size-4" />
          Production: requests act on real data.
        </p>
      )}
      {unavailable !== null ? (
        <div className="grid gap-2 rounded-md border border-dashed px-3 py-2 text-[12.5px]" data-testid="tryit-unavailable">
          {unavailable}
          {t.envs !== null && t.envs.environments.length === 0 && (
            <Button
              size="xs"
              variant="outline"
              className="justify-self-start"
              onClick={() => {
                t.openAdd({ url: "http://127.0.0.1:8000" });
              }}
            >
              Add an environment…
            </Button>
          )}
        </div>
      ) : (
        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-4 max-[1100px]:grid-cols-1">
          <div className="grid content-start gap-3" data-testid="tryit-request">
            <div className="grid gap-1.5">
              <div className={HEAD}>Path</div>
              {!freeForm && params.length === 0 && <span className="text-[12px] text-muted-foreground">The path has no parameters.</span>}
              {!freeForm &&
                params.map((p) => (
                  <label key={p.name} className="grid grid-cols-[minmax(90px,auto)_minmax(0,1fr)] items-center gap-x-2 gap-y-0.5 text-[12.5px]">
                    <span>
                      <span className="font-mono font-semibold">{p.name}</span> <span className="text-[11.5px] font-semibold text-unmatched">required</span>
                    </span>
                    <input
                      className={INPUT}
                      value={values[p.name] ?? ""}
                      onChange={(e) => {
                        setValues({ ...values, [p.name]: e.target.value });
                      }}
                      placeholder={p.type}
                      aria-label={`path value ${p.name}`}
                      data-testid={`path-value-${p.name}`}
                    />
                    <span className="col-start-2 flex flex-wrap items-center gap-1.5 text-[11.5px] text-muted-foreground">
                      <ProvenanceChip p={p.provenance} />
                      {p.type}
                    </span>
                  </label>
                ))}
              <label className="flex items-center gap-2 text-[12.5px]">
                <input
                  type="checkbox"
                  checked={freeForm}
                  onChange={(e) => {
                    setFreeForm(e.target.checked);
                  }}
                  data-testid="free-form-toggle"
                />
                Free-form path (any path under the base URL; always asks before sending)
              </label>
              {freeForm && (
                <div className="flex gap-2">
                  <select
                    className={cn(INPUT, "w-[96px]")}
                    value={ffMethod}
                    onChange={(e) => {
                      setFfMethod(e.target.value);
                    }}
                    aria-label="Free-form method"
                  >
                    {METHODS.map((m) => (
                      <option key={m}>{m}</option>
                    ))}
                  </select>
                  <input
                    className={cn(INPUT, "flex-1")}
                    value={ffPath}
                    onChange={(e) => {
                      setFfPath(e.target.value);
                    }}
                    aria-label="Free-form path"
                    data-testid="free-form-path"
                  />
                </div>
              )}
            </div>
            <KeyValues title="Query" rows={query} setRows={setQuery} addText="Add a query parameter" />
            <KeyValues title="Headers" rows={headers} setRows={setHeaders} addText="Add a header" />
            {env !== null && env.auth.header !== null && (
              <p className="text-[11.5px] text-muted-foreground">
                <span className="font-mono">{env.auth.header}</span> is set by the server from the environment&apos;s auth; its value never reaches this page.
              </p>
            )}
            <div className="grid gap-1.5">
              <div className="flex flex-wrap items-center gap-2">
                <span className={HEAD}>Body</span>
                <select
                  className={cn(INPUT, "font-sans")}
                  value={mode}
                  onChange={(e) => {
                    const m = e.target.value as BodyMode;
                    setMode(m);
                    if (m === "template" && template !== null) {
                      setRaw(template.text);
                      setContentType(template.contentType);
                    }
                  }}
                  aria-label="Body"
                  data-testid="body-mode"
                >
                  <option value="none">no body</option>
                  <option value="raw">raw text</option>
                  {template !== null && <option value="template">JSON template</option>}
                  <option value="multipart">multipart form (fields and files)</option>
                </select>
              </div>
              {mode === "template" && template !== null && (
                <p className="text-[11.5px] text-muted-foreground" data-testid="body-template-note">
                  <span className="font-semibold text-dynamic">Template</span> from the {template.kind === "fact" ? "documented" : template.kind} type <span className="font-mono text-foreground">{template.schemaName}</span>
                  {template.provenance !== null && <ProvenanceChip p={template.provenance} className="mx-1" />}
                  with placeholder values to edit: not an example the server is known to accept.
                </p>
              )}
              {(mode === "raw" || mode === "template") && (
                <>
                  <input
                    className={INPUT}
                    value={contentType}
                    onChange={(e) => {
                      setContentType(e.target.value);
                    }}
                    placeholder="Content-Type"
                    aria-label="Content-Type"
                  />
                  <textarea
                    className="min-h-[120px] rounded-md border bg-background px-2 py-1.5 font-mono text-[12px] leading-[1.5]"
                    value={raw}
                    onChange={(e) => {
                      setRaw(e.target.value);
                    }}
                    aria-label="Request body"
                    data-testid="body-text"
                  />
                </>
              )}
              {mode === "multipart" && (
                <ul className="grid gap-1.5" data-testid="multipart">
                  {parts.map((p) => (
                    <li key={p.id} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_auto] items-center gap-1.5">
                      <input
                        className={INPUT}
                        value={p.name}
                        onChange={(e) => {
                          setParts(parts.map((x) => (x.id === p.id ? { ...x, name: e.target.value } : x)));
                        }}
                        placeholder="field name"
                        aria-label="Part name"
                      />
                      {p.file !== null ? (
                        <span className="min-w-0 truncate text-[12px]">
                          <Upload aria-hidden="true" className="mr-1 inline size-3.5" />
                          {p.file.name} ({bytesText(p.file.size)})
                        </span>
                      ) : (
                        <div className="flex min-w-0 gap-1.5">
                          <input
                            className={cn(INPUT, "flex-1")}
                            value={p.value}
                            onChange={(e) => {
                              setParts(parts.map((x) => (x.id === p.id ? { ...x, value: e.target.value } : x)));
                            }}
                            placeholder="text value"
                            aria-label="Part value"
                          />
                          <label className="inline-flex h-8 cursor-pointer items-center gap-1 rounded-md border px-2 text-[12px]">
                            <Upload aria-hidden="true" className="size-3.5" />
                            File…
                            <input
                              type="file"
                              className="sr-only"
                              aria-label="Choose a file"
                              data-testid="part-file"
                              onChange={(e) => {
                                const f = e.target.files?.[0];
                                if (f === undefined) return;
                                void fileToBase64(f).then((data) => {
                                  setParts((cur) => cur.map((x) => (x.id === p.id ? { ...x, file: { name: f.name, type: f.type, data, size: f.size }, name: x.name === "" ? "file" : x.name } : x)));
                                });
                              }}
                            />
                          </label>
                        </div>
                      )}
                      <button
                        type="button"
                        className="rounded p-1 text-muted-foreground hover:bg-accent"
                        aria-label="remove part"
                        onClick={() => {
                          setParts(parts.filter((x) => x.id !== p.id));
                        }}
                      >
                        <Trash2 aria-hidden="true" className="size-3.5" />
                      </button>
                    </li>
                  ))}
                  <Button
                    size="xs"
                    variant="ghost"
                    className="justify-self-start"
                    onClick={() => {
                      setParts([...parts, { id: Date.now(), name: "", value: "", file: null }]);
                    }}
                  >
                    <Plus aria-hidden="true" />
                    Add a field or file
                  </Button>
                </ul>
              )}
            </div>
            {flow.kind === "confirm" ? (
              <Confirmation
                flow={flow}
                env={envOf(flow.req.environment)}
                onCancel={() => {
                  setFlow({ kind: "idle" });
                }}
                onRemember={(v) => {
                  setFlow({ ...flow, remember: v });
                }}
                onSend={() => {
                  send({ ...flow.req, confirmation: flow.prepared.confirmation ?? "", ...(flow.remember ? { remember: true } : {}) }, flow.prepared);
                }}
              />
            ) : (
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  ref={sendRef}
                  onClick={() => {
                    if (env !== null) start(build(env.id));
                  }}
                  disabled={env === null || flow.kind === "busy" || missing}
                  variant={method === "DELETE" ? "destructive" : "default"}
                  data-testid="tryit-send"
                >
                  <Play aria-hidden="true" />
                  {sendText}
                </Button>
                <Button variant="outline" size="sm" onClick={copyCurl} disabled={env === null} data-testid="copy-curl">
                  <ClipboardCopy aria-hidden="true" />
                  Copy as curl
                </Button>
                <span className="text-[12px] text-muted-foreground">
                  {flow.kind === "busy"
                    ? flow.what
                    : missing
                      ? "Enter every required path value."
                      : env?.production === true
                        ? "production: acknowledged when you switched"
                        : method === "GET" || method === "HEAD"
                          ? `${method} is sent without asking`
                          : env?.remembered === true
                            ? `not asking again for ${env.name} until you switch`
                            : "asks before sending"}
                </span>
              </div>
            )}
            {curl !== null && (
              <pre className="overflow-auto rounded-md border bg-code-bg px-2.5 py-1.5 font-mono text-[11.5px] leading-[1.5] whitespace-pre-wrap [overflow-wrap:anywhere]" data-testid="curl-text">
                {curl}
              </pre>
            )}
            {flow.kind === "dropped" && (
              <p role="status" className="rounded-md border border-dynamic/50 bg-dynamic-soft px-3 py-2 text-[12.5px]" data-testid="confirm-dropped">
                The confirmation for {flow.method} to {flow.from} was closed because {flow.to !== null ? `you switched to ${flow.to}` : "no environment is selected now"}. Nothing was sent.
                {flow.to !== null ? ` Press Send to prepare it for ${flow.to}.` : ""}
              </p>
            )}
            {flow.kind === "error" && (
              <ErrorBox
                error={flow.error}
                t={t}
                env={flow.req !== null ? envOf(flow.req.environment) : env}
                onRetry={
                  flow.req !== null
                    ? () => {
                        const r = flow.req;
                        if (r !== null) start({ ...r, confirmation: undefined, remember: undefined });
                      }
                    : null
                }
                onRetryIn={
                  flow.req !== null
                    ? (target) => {
                        const r = flow.req;
                        if (r !== null) start({ ...r, environment: target.id, confirmation: undefined, remember: undefined });
                      }
                    : null
                }
              />
            )}
          </div>
          <div className="min-w-0">
            {response !== null ? (
              <ResponseView
                sent={response.sent}
                prepared={response.prepared}
                env={envOf(response.req.environment)}
                onRaise={(limit, value) => {
                  const of = envOf(response.req.environment);
                  if (of !== null) t.openSettings(of, { limit: { limit, value }, note: `The response was cut at the ${LIMIT_NAME[limit] ?? limit}.` });
                }}
                onFollow={onFollow}
              />
            ) : (
              <p className="rounded-md border border-dashed px-3 py-6 text-center text-[12.5px] text-muted-foreground">The response appears here. Nothing is sent until you press Send.</p>
            )}
          </div>
        </div>
      )}
      {flow.kind === "auth-missing" && env !== null && (
        <Modal
          title={`No value for ${flow.header}`}
          tone="warn"
          testId="auth-missing"
          onClose={() => {
            setFlow({ kind: "idle" });
          }}
        >
          <p>
            {env.name} takes <span className="font-mono">{flow.header}</span> from the variable <span className="font-mono">${flow.env}</span>, which is not set for the server.
          </p>
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                setFlow({ kind: "idle" });
              }}
              data-autofocus
            >
              Cancel
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                start({ ...flow.req, send_without_auth: true, confirmation: undefined });
              }}
              data-testid="send-without-auth"
            >
              Send without auth
            </Button>
            <Button
              size="sm"
              onClick={() => {
                const req = flow.req;
                setFlow({ kind: "idle" });
                t.openSessionAuth(env, () => {
                  start({ ...req, send_without_auth: undefined, confirmation: undefined });
                });
              }}
              data-testid="enter-value"
            >
              Enter a value for this session…
            </Button>
          </div>
        </Modal>
      )}
      {flow.kind === "certificate" && (
        <CertificateDialog
          d={flow.details}
          env={flow.environment !== null ? envOf(flow.environment) : null}
          onClose={() => {
            setFlow({ kind: "idle" });
          }}
          onTrust={() => {
            const d = flow.details;
            const of = flow.environment !== null ? envOf(flow.environment) : null;
            setFlow({ kind: "idle" });
            if (of === null) return;
            const env = of;
            t.openSettings(env, {
              addPin: { origin: env.source === "project" ? d.origin : null, sha256: d.certificate.sha256 },
              note: d.changed ? (
                <span>
                  <b>Changed certificate.</b> The new fingerprint is added beside the pinned one{d.pinnedSha256.length === 1 ? "" : "s"}; remove the old pin if the server was reissued.
                </span>
              ) : (
                <span>Trusting the certificate of {d.origin} for {env.name} only.</span>
              ),
            });
          }}
        />
      )}
    </div>
  );
}
