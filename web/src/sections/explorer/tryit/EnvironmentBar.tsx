// The Explorer's page-wide environment bar (T029 part B, variant B, the
// developer's decision): the selected environment with its label, its
// base URL and auth, the production marker while a production-labelled
// environment is selected, and a picker with the configured environments,
// "Add a URL" and the base URL suggestions (each with its source kind and
// file:line, used only when picked: TI-31..TI-41, OX-24).

import { Popover } from "radix-ui";
import { ChevronDown, KeyRound, Plus, Settings2, ShieldAlert, X } from "lucide-react";
import { useState } from "react";

import type { Environment, Suggestion } from "@/api/tryit";
import { Button } from "@/components/ui/button";
import { useApiRequest } from "@/hooks/useApiRequest";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { LabelChip } from "@/sections/explorer/tryit/dialogs";
import { rovingKey } from "@/sections/explorer/tryit/roving";
import { useTryIt } from "@/sections/explorer/tryit/TryIt";

/** AuthLine says where the selected environment's auth comes from, never
 * a value: the variable (set or not), or a value typed for the session. */
function AuthLine({ env }: { env: Environment }) {
  const t = useTryIt();
  const style = useLabelStyle();
  const a = env.auth;
  const l = label(style, "tryAuth", { header: a.header, env: a.env, set: a.envSet, session: a.sessionValue, prefix: a.prefix });
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-[12.5px]" data-testid="env-auth">
      <span className="text-muted-foreground">Auth</span>
      <span className={cn("inline-flex h-6 items-center gap-1 rounded-md border px-1.5", a.env !== null && !a.envSet && !a.sessionValue && "border-dynamic/50 text-dynamic")} title={l.title}>
        <KeyRound aria-hidden="true" className="size-3.5" />
        <span className="font-mono text-[12px]">{l.text}</span>
      </span>
      {a.sessionValue ? (
        <Button
          size="xs"
          variant="ghost"
          onClick={() => {
            t?.clearSessionAuth(env);
          }}
          data-testid="clear-session-auth"
        >
          <X aria-hidden="true" />
          Clear the session value
        </Button>
      ) : (
        <Button
          size="xs"
          variant="ghost"
          onClick={() => {
            t?.openSessionAuth(env);
          }}
          data-testid="enter-session-auth"
        >
          Enter a value for this session
        </Button>
      )}
    </div>
  );
}

function SuggestionRow({ s, onUse }: { s: Suggestion; onUse: () => void }) {
  const style = useLabelStyle();
  const l = label(style, "suggestionSource", { kind: s.sourceKind, row: s.row });
  return (
    <li className={cn("grid gap-1 rounded-md border px-2.5 py-2", s.file === null && "border-dashed")} data-testid="suggestion" data-kind={s.sourceKind}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-[12.5px] font-semibold">{s.url}</span>
        <Button size="xs" variant="outline" onClick={onUse} data-testid="suggestion-use">
          Use…
        </Button>
      </div>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px]">
        <span className="font-medium" title={l.title}>
          {l.text}
        </span>
        {s.file !== null ? (
          <span className="rounded-[5px] border px-1.5 font-mono text-[11px] text-muted-foreground">
            {s.file}
            {s.line !== null ? `:${String(s.line)}` : ""}
          </span>
        ) : (
          <span className="text-[11.5px] text-muted-foreground italic">no file</span>
        )}
      </div>
      <p className="text-[11.5px] text-muted-foreground">{s.reason}</p>
    </li>
  );
}

/** Picker is the popover with the environments, Add a URL and the
 * suggestions. */
function Picker({ onClose }: { onClose: () => void }) {
  const t = useTryIt();
  const [url, setUrl] = useState("");
  const suggestions = useApiRequest(t === null ? null : "suggestions", (signal) => t?.client.suggestions({ signal }) ?? Promise.reject(new Error("no try-it")));
  if (t === null || t.envs === null) return null;
  const envs = t.envs;
  return (
    <div className="grid grid-cols-2 gap-4 max-[1000px]:grid-cols-1" data-testid="environment-picker">
      <div className="grid content-start gap-2">
        <div className="text-[11.5px] font-semibold tracking-[.06em] text-muted-foreground uppercase">Your environments</div>
        {envs.configError !== null && <p className="text-[12px] text-unmatched">This member&apos;s graphvein.yaml was not read: {envs.configError}</p>}
        {envs.environments.length === 0 ? (
          <p className="text-[12.5px] text-muted-foreground" data-testid="no-environments">
            None configured yet in {envs.files.map((f) => f.file).join(" or ")}. Add a URL below or pick a suggestion.
          </p>
        ) : (
          <ul
            className="grid gap-1"
            role="listbox"
            aria-label="Environments"
            onKeyDown={(e) => {
              // Arrows, Home and End move between options; Enter or Space
              // selects (a production switch still shows its warning).
              rovingKey(e, "[role=option]");
            }}
          >
            {envs.environments.map((e, i) => (
              <li key={e.id}>
                <button
                  type="button"
                  role="option"
                  aria-selected={e.selected}
                  tabIndex={e.selected || (t.selected === null && i === 0) ? 0 : -1}
                  className={cn("grid w-full min-w-0 gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-accent", e.selected && "bg-brand-soft")}
                  onClick={() => {
                    onClose();
                    t.select(e);
                  }}
                  data-testid="environment-option"
                  data-id={e.id}
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <LabelChip env={e} />
                    <b className="font-semibold whitespace-nowrap">{e.name}</b>
                    <span className="min-w-0 truncate font-mono text-[12px]" title={e.baseUrl}>
                      {e.baseUrl}
                    </span>
                  </span>
                  <span className="flex flex-wrap gap-x-2 text-[11px] text-muted-foreground">
                    <span>
                      {e.file}:{e.line}
                      {e.source === "project" ? ` · project environment, base URL for ${envs.repository}` : ""}
                    </span>
                    {e.auth.env !== null && (
                      <span>
                        {e.auth.header} from {`$${e.auth.env}`} · {e.auth.envSet ? "set" : "not set"}
                      </span>
                    )}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
        {t.selected !== null && (
          <Button
            size="xs"
            variant="ghost"
            className="justify-self-start"
            onClick={() => {
              onClose();
              t.select(null);
            }}
          >
            Select none
          </Button>
        )}
        <form
          className="mt-2 grid gap-1.5 border-t pt-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (url.trim() === "") return;
            onClose();
            t.openAdd({ url: url.trim() });
          }}
        >
          <label className="text-[12.5px] font-semibold" htmlFor="tryit-add-url">
            Add a URL
          </label>
          <div className="flex gap-2">
            <input
              id="tryit-add-url"
              className="h-8 min-w-0 flex-1 rounded-md border bg-background px-2 font-mono text-[12.5px]"
              value={url}
              onChange={(e) => {
                setUrl(e.target.value);
              }}
              placeholder="https://api.staging.example.com"
              data-testid="picker-url"
            />
            <Button size="sm" type="submit" variant="secondary" disabled={url.trim() === ""}>
              <Plus aria-hidden="true" />
              Add…
            </Button>
          </div>
          <span className="text-[11.5px] text-muted-foreground">Written to a file after you see the change; nothing is sent before.</span>
        </form>
      </div>
      <div className="grid content-start gap-2">
        <div className="flex flex-wrap items-baseline gap-x-2 text-[11.5px] font-semibold tracking-[.06em] text-muted-foreground uppercase">
          Suggested from the repository
          <span className="font-normal tracking-normal normal-case">used only after you pick one</span>
        </div>
        {suggestions.state.kind === "ok" ? (
          suggestions.state.data.suggestions.length === 0 ? (
            <p className="text-[12.5px] text-muted-foreground">No base URL is suggested: no published port, router, spec server or listen call was read.</p>
          ) : (
            <ul className="grid max-h-[420px] gap-1.5 overflow-auto" data-testid="suggestions">
              {suggestions.state.data.suggestions.map((s, i) => (
                <SuggestionRow
                  key={`${s.url}-${s.sourceKind}-${String(i)}`}
                  s={s}
                  onUse={() => {
                    onClose();
                    t.openAdd({ url: s.url, source: s.file !== null ? `${s.file}${s.line !== null ? `:${String(s.line)}` : ""}` : "a framework default" });
                  }}
                />
              ))}
            </ul>
          )
        ) : suggestions.state.kind === "failed" ? (
          <p className="text-[12.5px] text-unmatched">The suggestions could not be read: {suggestions.state.error.message}</p>
        ) : (
          <p className="text-[12.5px] text-muted-foreground">Reading the suggestions…</p>
        )}
      </div>
    </div>
  );
}

/** EnvironmentBar is the page-wide bar. openAtStart opens the picker on
 * the first render (a Settings link to the try it environments). */
export function EnvironmentBar({ openAtStart = false }: { openAtStart?: boolean }) {
  const t = useTryIt();
  const [open, setOpen] = useState(openAtStart);
  if (t === null) return null;
  const env = t.selected;
  const production = env?.production === true;
  return (
    <div
      className={cn("grid gap-1.5 rounded-lg border bg-card px-3 py-2", production && "border-unmatched/60 bg-unmatched-soft/50")}
      data-testid="environment-bar"
      data-production={production ? "true" : "false"}
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex min-w-0 items-center gap-2">
          <span className="text-[13px] text-muted-foreground">Environment</span>
          <Popover.Root open={open} onOpenChange={setOpen}>
            <Popover.Trigger asChild>
              <button type="button" className="flex h-8 max-w-[560px] min-w-0 items-center gap-2 rounded-md border bg-background px-2 text-[13px]" data-testid="environment-trigger">
                {env !== null ? (
                  <>
                    <LabelChip env={env} />
                    <b className="font-semibold">{env.name}</b>
                    <span className="min-w-0 truncate font-mono text-[12px]">{env.baseUrl}</span>
                  </>
                ) : t.envs === null ? (
                  <span className="text-muted-foreground">{t.loadError !== null ? "not read" : "reading…"}</span>
                ) : t.envs.environments.length === 0 ? (
                  <span className="text-muted-foreground">none configured: add one</span>
                ) : (
                  <span className="text-muted-foreground">none selected</span>
                )}
                <ChevronDown aria-hidden="true" className="size-4 flex-none" />
              </button>
            </Popover.Trigger>
            <Popover.Portal>
              <Popover.Content
                align="start"
                sideOffset={6}
                collisionPadding={12}
                className="z-50 max-h-(--radix-popover-content-available-height) w-[min(980px,calc(100vw-24px))] overflow-auto rounded-[calc(var(--radius)+2px)] border bg-popover p-4 text-[13px] text-popover-foreground shadow-[0_10px_30px_rgba(0,0,0,.16),0_2px_6px_rgba(0,0,0,.08)] outline-none"
              >
                <Picker
                  onClose={() => {
                    setOpen(false);
                  }}
                />
              </Popover.Content>
            </Popover.Portal>
          </Popover.Root>
        </div>
        {env !== null && <AuthLine env={env} />}
        {env !== null && (
          <Button
            size="xs"
            variant="ghost"
            className="ml-auto"
            onClick={() => {
              t.openSettings(env);
            }}
            data-testid="environment-settings-open"
          >
            <Settings2 aria-hidden="true" />
            Settings
          </Button>
        )}
      </div>
      {env?.production === true && (
        <div className="flex items-center gap-2 text-[12.5px] font-medium text-unmatched" data-testid="production-marker">
          <ShieldAlert aria-hidden="true" className="size-4 flex-none" />
          Production: {env.name} ({env.label}) is selected. Requests act on real data; even a GET can change it.
        </div>
      )}
      {env !== null && env.options.use_system_proxy && env.baseUrl.startsWith("http://") && (
        <p className="text-[12px] text-dynamic" data-testid="proxy-cleartext-bar">
          Through the system proxy over http://: the proxy sees each request in clear, headers and the auth value included.
        </p>
      )}
      {t.notice !== null && (
        <p role="alert" className="text-[12.5px] text-unmatched">
          {t.notice}
        </p>
      )}
      {t.loadError !== null && (
        <p role="alert" className="text-[12.5px] text-unmatched">
          The environments could not be read: {t.loadError}
        </p>
      )}
    </div>
  );
}
