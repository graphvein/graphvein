// The response of a try-it send (T029 part B, OX-22): status, time,
// sizes, the request as the server built it, the response headers with
// the server's redaction (TI-19), and the body as text in a <pre>: as
// received, or JSON re-indented token for token (never re-serialised). A
// cut body says which limit cut it and offers to raise it; a redirect
// shows its target and how to follow it once (TI-46). Nothing of the
// response is rendered as HTML.

import { ArrowRight, EyeOff, Scissors, TriangleAlert } from "lucide-react";
import { useState } from "react";

import type { Environment, Prepared, Sent } from "@/api/tryit";
import { Button } from "@/components/ui/button";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { bytesText, LIMIT_NAME, limitText, prettyBody, type BodyView } from "@/sections/explorer/tryit/model";

const HEAD = "text-[11.5px] font-semibold tracking-[.06em] text-muted-foreground uppercase";

const REDACTED = "[redacted]";

/** authFrom says where the auth header's value came from (prepare's
 * auth_source: "env" or "session"), never the value. */
export function authFrom(p: Prepared, env: Environment | null): string {
  if (p.authSource === "session") return "a value typed for this session";
  const variable = env?.auth.env ?? null;
  return variable !== null ? `from $${variable}` : "from the environment's variable";
}

function statusTone(status: number): string {
  if (status >= 500) return "text-unmatched";
  if (status >= 400) return "text-dynamic";
  if (status >= 300) return "text-http";
  return "text-linked";
}

/** ResponseView shows one answer. onRaise opens the environment settings
 * with the limit that cut the body; onFollow follows a redirect once. */
export function ResponseView({ sent, prepared, env, onRaise, onFollow }: { sent: Sent; prepared: Prepared | null; env: Environment | null; onRaise: (limit: string, value: number) => void; onFollow: (() => void) | null }) {
  const style = useLabelStyle();
  const contentType = sent.headers.find((h) => h.name.toLowerCase() === "content-type")?.value ?? null;
  const view: BodyView = sent.bodyEncoding === "hex" ? { text: sent.body, json: false, indented: false, note: null, duplicateKeys: [] } : prettyBody(sent.body, contentType);
  const [asReceived, setAsReceived] = useState(false);
  const shown = view.indented && !asReceived ? view.text : sent.body;
  const cut = sent.truncated && sent.truncatedReason !== null ? label(style, "truncation", { reason: sent.truncatedReason, limit: sent.truncatedLimit?.limit ?? null }) : null;
  const tl = sent.truncatedLimit;
  const raiseTo = tl !== null ? Math.min(tl.ceiling, tl.value * 4) : 0;
  const follow = sent.redirectFollow;
  return (
    <div className="grid min-w-0 content-start gap-3" data-testid="tryit-response" aria-live="polite">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span className={cn("text-[18px] font-semibold tabular-nums", statusTone(sent.status))} data-testid="response-status">
          {sent.status} {sent.statusText}
        </span>
        <span className="text-[12.5px] text-muted-foreground tabular-nums" data-testid="response-meta">
          {sent.durationMs} ms · {bytesText(sent.bodyBytes)}
          {prepared !== null && prepared.bodyBytes > 0 ? ` · sent ${bytesText(prepared.bodyBytes)}` : ""} · through Graphvein&apos;s proxy{prepared?.proxy != null ? ` and ${prepared.proxy}` : ""}
        </span>
        {sent.remembered && <span className="text-[11.5px] text-muted-foreground">not asking again for {env?.name ?? "this environment"} until you switch</span>}
      </div>
      <div className="min-w-0 rounded-md border bg-muted/40 px-2.5 py-1.5 font-mono text-[12px] [overflow-wrap:anywhere]" data-testid="response-request">
        <b>{sent.method}</b> {sent.url}
      </div>
      {prepared?.proxyCleartext === true && (
        <p className="flex items-start gap-1.5 text-[12px] text-dynamic" data-testid="proxy-cleartext">
          <TriangleAlert aria-hidden="true" className="mt-px size-3.5 flex-none" />
          Sent through {prepared.proxy} over http://: the proxy saw this request in clear, headers and the auth value included.
        </p>
      )}
      {cut !== null && (
        <div className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-2 gap-y-1.5 rounded-md border border-dynamic/50 bg-dynamic-soft px-2.5 py-1.5 text-[12.5px]" data-testid="response-truncated" title={cut.title}>
          <Scissors aria-hidden="true" className="mt-0.5 size-3.5 flex-none text-dynamic" />
          <span>
            <b className="font-semibold">Truncated:</b> {cut.text}
            {tl !== null ? ` (${LIMIT_NAME[tl.limit] ?? tl.limit} ${limitText(tl.limit, tl.value)}, at most ${limitText(tl.limit, tl.ceiling)})` : ""}. Only the part received is shown.
          </span>
          {tl !== null && tl.value < tl.ceiling && (
            <Button
              size="xs"
              variant="outline"
              className="col-start-2 justify-self-start"
              onClick={() => {
                onRaise(tl.limit, raiseTo);
              }}
              data-testid="raise-limit"
            >
              Raise to {limitText(tl.limit, raiseTo)}…
            </Button>
          )}
        </div>
      )}
      {sent.redirect !== null && (
        <div className="grid gap-1.5 rounded-md border border-http/40 bg-http-soft px-2.5 py-2 text-[12.5px]" data-testid="response-redirect">
          <div className="flex flex-wrap items-center gap-1.5">
            <ArrowRight aria-hidden="true" className="size-3.5 flex-none" />
            <b className="font-semibold">Redirect, not followed:</b>
            <span className="font-mono text-[12px] [overflow-wrap:anywhere]">{follow?.url ?? sent.redirect}</span>
          </div>
          {follow !== null && (
            <>
              <p className="text-muted-foreground">
                A browser would send {follow.method}
                {follow.keepsBody ? " with the same body" : ""} to {follow.sameOrigin ? "the same origin" : <span className="font-mono">{follow.origin}</span>}.
                {follow.environment === null && follow.suggestedEnvironment !== null ? " No environment holds it yet." : ""}
              </p>
              {onFollow !== null && (
                <Button size="xs" variant="outline" className="justify-self-start" onClick={onFollow} data-testid="redirect-follow">
                  {follow.environment !== null ? `Follow once in ${follow.environment.replace(/^[a-z]+:/, "")}` : "Add this origin as an environment and follow"}
                </Button>
              )}
            </>
          )}
        </div>
      )}
      {prepared !== null && (
        <div className="grid gap-1">
          <div className={HEAD}>Request headers sent</div>
          <table className="w-full text-[12px]" data-testid="request-headers">
            <tbody>
              {prepared.authHeader !== null && (
                <tr className="border-t first:border-t-0">
                  <td className="w-[38%] py-1 pr-3 align-top font-mono font-semibold">{prepared.authHeader.toLowerCase()}</td>
                  <td className="py-1 align-top">
                    <span className="inline-flex items-center gap-1 rounded-[5px] bg-muted px-1.5 text-muted-foreground" data-testid="redacted">
                      <EyeOff aria-hidden="true" className="size-3" />
                      redacted · {authFrom(prepared, env)}
                    </span>
                  </td>
                </tr>
              )}
              {prepared.headers.map((h) => (
                <tr key={h} className="border-t first:border-t-0">
                  <td className="w-[38%] py-1 pr-3 align-top font-mono font-semibold [overflow-wrap:anywhere]">{h.toLowerCase()}</td>
                  <td className="py-1 align-top text-muted-foreground">as typed</td>
                </tr>
              ))}
              {prepared.authHeader === null && prepared.headers.length === 0 && (
                <tr>
                  <td className="py-1 text-muted-foreground">{prepared.sendWithoutAuth ? "none: sent without auth, as you chose" : "none typed; no auth configured"}</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
      <div className="grid gap-1">
        <div className={HEAD}>Response headers</div>
        {sent.headers.length === 0 ? (
          <span className="text-[12px] text-muted-foreground">none</span>
        ) : (
          <table className="w-full text-[12px]" data-testid="response-headers">
            <tbody>
              {sent.headers.map((h, i) => (
                <tr key={`${h.name}-${String(i)}`} className="border-t first:border-t-0">
                  <td className="w-[38%] py-1 pr-3 align-top font-mono font-semibold [overflow-wrap:anywhere]">{h.name.toLowerCase()}</td>
                  <td className="py-1 align-top font-mono [overflow-wrap:anywhere]">
                    {h.value === REDACTED ? (
                      <span className="inline-flex items-center gap-1 rounded-[5px] bg-muted px-1.5 text-muted-foreground" title="redacted by the server: the value never reaches the browser" data-testid="redacted">
                        <EyeOff aria-hidden="true" className="size-3" />
                        redacted
                      </span>
                    ) : (
                      h.value
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <div className="grid gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <div className={HEAD} data-testid="response-body-head">
            Body{view.json ? (view.indented && !asReceived ? " · JSON, re-indented" : " · JSON, as received") : sent.bodyEncoding === "hex" ? " · not text: hex of the first 4 KiB" : contentType !== null ? ` · ${contentType}` : ""}
          </div>
          {view.indented && (
            <Button
              size="xs"
              variant="ghost"
              onClick={() => {
                setAsReceived(!asReceived);
              }}
              aria-pressed={asReceived}
              data-testid="body-as-received"
            >
              {asReceived ? "Re-indent" : "Show as received"}
            </Button>
          )}
        </div>
        {view.indented && !asReceived && <p className="text-[11.5px] text-muted-foreground">Only whitespace changed: every number, string and key is shown exactly as the server sent it, in its order.</p>}
        {view.note !== null && (
          <p className="text-[11.5px] text-muted-foreground" data-testid="body-note">
            Body {view.note}.
          </p>
        )}
        {view.duplicateKeys.length > 0 && (
          <p className="flex items-start gap-1.5 text-[12px] text-dynamic" data-testid="body-duplicate-keys">
            <TriangleAlert aria-hidden="true" className="mt-px size-3.5 flex-none" />
            <span>
              A key repeats inside one object: <span className="font-mono">{view.duplicateKeys.map((k) => JSON.stringify(k)).join(", ")}</span>. Both are shown; many JSON readers keep only the last.
            </span>
          </p>
        )}
        {sent.body === "" ? (
          <span className="text-[12px] text-muted-foreground">empty ({bytesText(sent.bodyBytes)})</span>
        ) : (
          <pre className="max-h-[480px] overflow-auto rounded-md border bg-code-bg px-2.5 py-2 font-mono text-[12px] leading-[1.5] whitespace-pre-wrap [overflow-wrap:anywhere]" data-testid="response-body">
            {shown}
          </pre>
        )}
      </div>
    </div>
  );
}
