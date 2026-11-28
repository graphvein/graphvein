// The Settings page (T039, variant A of the T039 mockups plus
// variant B's filter): one page with sections and a page index. Every key
// shows its ladder (this browser / repository / built-in) with the value
// in use in bold and an origin chip on each rung (row ST-49). Repository
// values are edited in place into a draft; the fixed save bar opens the
// server's preview (changes in words, unified diff, validity) and
// "Write graphvein.yaml" applies exactly the previewed result, only while
// the file still has the hash it was read with. After a write that
// changes index inputs, the notice names them and starts the shell's one
// re-index run. "Edit as YAML" edits the whole text through the same
// preview. Values and origins come only from GET /api/v0/config and this
// browser's storage: nothing is assumed. The Storage section (T068 part
// 3, StorageSection.tsx) comes first: it is this machine's, outside
// graphvein.yaml, and read from GET /api/v0/store.

import { ArrowLeft, CircleAlert, Code2, Info, Lock, RefreshCw, TriangleAlert } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";

import { ApiError } from "@/api/client";
import { httpConfigClient, type Config, type ConfigClient, type ConfigKey, type EditRequest, type EditResult, type SettingValue } from "@/api/config";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { editorName, useEditor } from "@/hooks/useEditor";
import { DepthSetting } from "@/components/DepthSetting";
import { useStoredDepth } from "@/hooks/useDepth";
import { useLabels } from "@/hooks/useLabels";
import { OFF } from "@/lib/editorChoice";
import { formatShortTime } from "@/lib/time";
import { parseHashQuery, replaceHash, settingsHash, useHash } from "@/router";
import { cn } from "@/lib/utils";
import type { StatusView } from "@/hooks/useStatus";
import { explorerEditHash } from "@/sections/explorer/params";
import { DiffBlock, Modal } from "@/sections/explorer/tryit/Modal";
import {
  appliesWords,
  draftSummary,
  draftValue,
  fieldOf,
  FILTERS,
  inUseWords,
  keptWords,
  keyShown,
  ladder,
  lineFields,
  linesOf,
  parseLines,
  operations,
  SECTIONS,
  shortHash,
  withDraft,
  type BrowserChoice,
  type Draft,
  type Filter,
  type Rung,
  type SectionId,
} from "@/sections/settings/model";
import { useConfig } from "@/sections/settings/useConfig";
import { EditorControl } from "@/shell/EditorPicker";
import { LabelSetting } from "@/shell/LabelControl";
import { useProject } from "@/shell/project";
import { useWorkspace } from "@/shell/workspace/WorkspaceRoot";
import { STORAGE_ANCHOR, StorageSection } from "@/sections/settings/StorageSection";
import { PHASE_LABELS, slotLabel, type ReindexModel } from "@/shell/reindexModel";

/** SettingsReindex is the shell's one re-index run (the same run the
 * top-bar control shows). */
export interface SettingsReindex {
  model: ReindexModel;
  start: () => void;
}

/** chip styles of the three origins (the legend under the title). */
const CHIP = {
  browser: "bg-fresh-soft text-fresh border-fresh/30",
  repository: "bg-brand-soft text-brand-text border-brand/30",
  builtin: "bg-muted text-muted-foreground border-transparent",
} as const;

function Chip({ tone, children, title }: { tone: keyof typeof CHIP; children: ReactNode; title?: string }) {
  return (
    <span title={title} className={cn("inline-flex h-[22px] flex-none items-center rounded-md border px-1.5 text-[12px] font-medium whitespace-nowrap", CHIP[tone])}>
      {children}
    </span>
  );
}

/** The anchor id of a section. */
function anchor(id: SectionId | "file"): string {
  return `settings-${id}`;
}

/** The anchor id of a key's row (#/settings?at=ide.editor). */
function rowAnchor(key: string): string {
  return `settings-row-${key}`;
}

/** LINK_TEMPLATE is the ide key that, when graphvein.yaml sets it, builds
 * every editor link instead of the editor (ST-2, ST-12). It has no key
 * row, so the Editor section shows it read-only, edited in the YAML view. */
const LINK_TEMPLATE = "ide.link_template";

/** useSettingsAnchor scrolls to the section or key row the hash names
 * (#/settings?at=editor, #/settings?at=ide.editor) once it is rendered,
 * and focuses its heading or row, so a link from the editor chooser or the
 * Help guide lands on it. */
function useSettingsAnchor(ready: boolean): void {
  const at = parseHashQuery(useHash()).get("at");
  useEffect(() => {
    if (!ready || at === null) return;
    const el = document.getElementById(anchor(at as SectionId)) ?? document.getElementById(rowAnchor(at));
    if (el === null) return;
    if (typeof el.scrollIntoView === "function") el.scrollIntoView({ block: "start" });
    el.focus({ preventScroll: true });
  }, [at, ready]);
}

/** useBrowserChoices reads the per-browser rungs of ui.labels and
 * ide.editor (their stored keys are unchanged since they left the sidebar)
 * and of the two walk depths (T042, the keys of the depth switches). */
function useBrowserChoices(): Record<string, BrowserChoice> {
  const labels = useLabels();
  const editor = useEditor();
  const [impact] = useStoredDepth("impact");
  const [chain] = useStoredDepth("chain");
  const labelText = labels.source === "browser" ? (labels.style === "technical" ? "technical" : "plain") : null;
  const editorText = editor.choice === null ? null : editor.choice === OFF ? "no editor links" : editorName(editor, editor.choice);
  return {
    "ui.labels": { text: labelText },
    "ide.editor": { text: editorText },
    "ui.impact_depth": { text: impact.depth === null ? null : String(impact.depth) },
    "ui.chain_depth": { text: chain.depth === null ? null : String(chain.depth) },
  };
}

/** SettingsSection is the page. client replaces the requests in tests. */
export function SettingsSection({ status, reindex, client = httpConfigClient }: { status: StatusView; reindex?: SettingsReindex; client?: ConfigClient }) {
  const project = useProject();
  const member = project?.repository ?? null;
  // A committed run changes index_settings: read the file again then.
  const runKey = status.kind === "ok" ? (status.status.index.last_run?.finished_at ?? "") : "";
  const { view, reload } = useConfig(client, member, runKey);
  const browser = useBrowserChoices();
  const entryId = useWorkspace()?.entry?.id ?? null;
  const labels = useLabels();
  const editor = useEditor();
  const [draft, setDraft] = useState<Draft>({});
  const [draftFor, setDraftFor] = useState<string | null>(null);
  const [generation, setGeneration] = useState(0);
  const [filter, setFilter] = useState<Filter>("all");
  const [mode, setMode] = useState<"form" | "yaml">("form");
  const [yaml, setYaml] = useState<string | null>(null);
  const [preview, setPreview] = useState<{ request: EditRequest; state: PreviewState } | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [written, setWritten] = useState(false);

  const config = view.kind === "ok" ? view.config : null;
  useSettingsAnchor(config !== null && mode === "form");
  // A draft belongs to the file it was made against: a new hash (another
  // member, a reload after a write or a hand edit) starts clean.
  const fileKey = config === null ? null : `${config.repository}\u0000${config.hash ?? ""}`;
  if (fileKey !== null && fileKey !== draftFor) {
    setDraftFor(fileKey);
    setDraft({});
    setYaml(null);
    setGeneration((g) => g + 1);
  }

  const openPreview = (request: EditRequest) => {
    if (config === null) return;
    setPreview({ request, state: { kind: "loading" } });
    client.edit(config, request).then(
      (result) => {
        setPreview((p) => (p === null ? p : { ...p, state: { kind: "ok", result } }));
      },
      (err: unknown) => {
        handleRefusal(err);
      },
    );
  };

  const handleRefusal = (err: unknown) => {
    if (err instanceof ApiError && (err.code === "config_changed" || err.code === "preview_changed")) {
      // The file changed since it was read (or since the preview): nothing
      // was written; read it again and say so.
      setPreview(null);
      setNotice("graphvein.yaml changed on disk since this page read it. Nothing was written; the page shows the file as it is now, and your unsaved changes were dropped.");
      reload();
      return;
    }
    setPreview((p) => (p === null ? p : { ...p, state: { kind: "failed", message: err instanceof Error ? err.message : String(err) } }));
  };

  const apply = () => {
    if (config === null || preview === null || preview.state.kind !== "ok") return;
    const { result } = preview.state;
    setPreview({ ...preview, state: { kind: "applying", result } });
    client.edit(config, preview.request, result.result_hash).then(
      (done) => {
        setPreview(null);
        setNotice(null);
        setWritten(done.reindex_needed);
        setMode("form");
        // At-once keys apply now: the label and editor defaults are read
        // again from the server.
        // GET /api/v0/ui also carries the depth defaults (T042).
        if (done.changed_keys.some((k) => k.key.startsWith("ui."))) labels.refresh();
        // Any ide.* key (link_template and label are outside the form, so
        // only other_keys_changed tells) changes the editor defaults.
        if (done.other_keys_changed || done.changed_keys.some((k) => k.key.startsWith("ide."))) editor.retry();
        reload();
      },
      (err: unknown) => {
        handleRefusal(err);
      },
    );
  };

  if (view.kind === "loading") {
    return (
      <div className="grid gap-3" data-testid="settings-loading">
        <Skeleton className="h-9 w-56" />
        <StorageSection entryId={entryId} />
        <Skeleton className="h-40" />
      </div>
    );
  }
  if (view.kind === "failed") {
    return (
      <div className="grid gap-3">
        <StorageSection entryId={entryId} />
        <div role="alert" className="grid gap-2 rounded-lg border border-stale/40 p-4 text-sm" data-testid="settings-failed">
          <p className="font-medium">The settings could not be read: {view.error.message}</p>
          <div>
            <Button variant="outline" size="sm" onClick={reload}>
              Try again
            </Button>
          </div>
        </div>
      </div>
    );
  }
  const cfg = view.config;
  const summary = draftSummary(cfg, draft);
  const typedEdits = cfg.editable && cfg.config_error === null;

  return (
    <div className="flex min-h-full flex-col gap-3" data-testid="settings-page">
      <Header config={cfg} mode={mode} onMode={setMode} />
      {notice !== null && (
        <p role="status" className="flex items-start gap-2 rounded-lg border border-dynamic/40 bg-dynamic-soft px-3 py-2 text-[13px]" data-testid="settings-reloaded">
          <Info aria-hidden="true" className="mt-0.5 size-4 flex-none" />
          {notice}
        </p>
      )}
      {!cfg.editable && cfg.reason !== null && (
        <p className="flex items-start gap-2 rounded-lg border px-3 py-2 text-[13px] text-muted-foreground" data-testid="settings-readonly">
          <Lock aria-hidden="true" className="mt-0.5 size-4 flex-none" />
          {cfg.reason}
        </p>
      )}
      {cfg.config_error !== null && <ConfigErrorBox config={cfg} onYaml={() => { setMode("yaml"); }} />}
      <ReindexNotice config={cfg} written={written} reindex={reindex} />
      {mode === "yaml" ? (
        <YamlView
          config={cfg}
          readAt={view.readAt}
          text={yaml ?? cfg.text ?? ""}
          onText={setYaml}
          onBack={() => { setMode("form"); }}
          onPreview={() => { openPreview({ text: yaml ?? cfg.text ?? "" }); }}
        />
      ) : (
        <>
          <Legend filter={filter} onFilter={setFilter} />
          <div className="grid grid-cols-[minmax(0,1fr)_200px] items-start gap-6 max-[1100px]:grid-cols-1">
            <div className="grid min-w-0 gap-4">
              <StorageSection entryId={entryId} />
              <Form
                key={generation}
                config={cfg}
                browser={browser}
                draft={draft}
                filter={filter}
                typedEdits={typedEdits}
                written={written}
                onDraft={(key, next) => { setDraft((d) => withDraft(d, key, next)); }}
              />
            </div>
            <PageIndex config={cfg} filter={filter} draft={draft} browser={browser} />
          </div>
          <SaveBar
            count={summary.count}
            words={appliesWords(summary)}
            onDiscard={() => {
              setDraft({});
              setGeneration((g) => g + 1);
            }}
            onReview={() => { openPreview({ operations: operations(cfg, draft) }); }}
          />
        </>
      )}
      {preview !== null && <PreviewDialog config={cfg} readAt={view.readAt} state={preview.state} onClose={() => { setPreview(null); }} onApply={apply} />}
    </div>
  );
}

type PreviewState = { kind: "loading" } | { kind: "ok"; result: EditResult } | { kind: "applying"; result: EditResult } | { kind: "failed"; message: string };

function Header({ config, mode, onMode }: { config: Config; mode: "form" | "yaml"; onMode: (m: "form" | "yaml") => void }) {
  const project = useProject();
  return (
    <div className="grid gap-1.5">
      <div className="flex flex-wrap items-center gap-2.5">
        <h1 className="m-0 text-[22px] font-semibold tracking-tight">Settings</h1>
        {project === null ? (
          <span className="rounded-md border px-1.5 py-0.5 text-[12px] text-muted-foreground">repository {config.repository}</span>
        ) : (
          <label className="flex items-center gap-1.5 text-[13px] text-muted-foreground">
            Scope
            <select
              className="h-7 rounded-md border border-input bg-background px-1.5 text-[13px] text-foreground"
              value={project.repository}
              data-testid="settings-scope"
              onChange={(e) => {
                project.select(e.target.value);
              }}
            >
              {project.project.members.map((m) => (
                <option key={m.repository} value={m.repository}>
                  {m.repository}: graphvein.yaml
                </option>
              ))}
            </select>
          </label>
        )}
        <span className="flex-1" />
        {mode === "form" && (
          <Button variant="outline" size="sm" onClick={() => { onMode("yaml"); }} disabled={config.text === null && config.exists !== false} data-testid="settings-yaml-open">
            <Code2 aria-hidden="true" />
            Edit as YAML
          </Button>
        )}
      </div>
      <p className="m-0 text-[13px] text-muted-foreground">
        graphvein.yaml is the source of truth for the repository settings: the form reads it and writes it through a previewed edit. Values from this browser apply only here. Storage is this machine's and is not in graphvein.yaml.
        {project !== null && " In a project each member has its own graphvein.yaml; the member select is the one in the sidebar."}
      </p>
    </div>
  );
}

function Legend({ filter, onFilter }: { filter: Filter; onFilter: (f: Filter) => void }) {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-[13px] text-muted-foreground">
      <span className="flex flex-wrap items-center gap-1.5">
        Where a value comes from:
        <Chip tone="browser">This browser</Chip>
        <Chip tone="repository">graphvein.yaml:line</Chip>
        <Chip tone="builtin">Built-in default</Chip>
      </span>
      <span className="flex-1" />
      <div role="radiogroup" aria-label="Show" className="inline-grid grid-cols-3 gap-0.5 rounded-md border border-input bg-background p-0.5" data-testid="settings-filter">
        {FILTERS.map((f) => (
          <button
            key={f.id}
            type="button"
            role="radio"
            aria-checked={filter === f.id}
            data-testid={`settings-filter-${f.id}`}
            onClick={() => { onFilter(f.id); }}
            className={cn("h-7 rounded-[5px] px-2.5 text-[12.5px] whitespace-nowrap", filter === f.id ? "bg-brand-soft font-semibold text-brand-text" : "hover:bg-accent hover:text-foreground")}
          >
            {f.label}
          </button>
        ))}
      </div>
    </div>
  );
}

/** visibleKeys groups the shown form keys by section. */
function visibleKeys(config: Config, filter: Filter, draft: Draft, browser: Record<string, BrowserChoice>): Map<SectionId, ConfigKey[]> {
  const out = new Map<SectionId, ConfigKey[]>();
  for (const k of config.keys) {
    if (!keyShown(filter, k, browser[k.key] ?? null, draft[k.key] !== undefined)) continue;
    const s = fieldOf(k.key).section;
    out.set(s, [...(out.get(s) ?? []), k]);
  }
  return out;
}

/** shownSections lists the sections the filter keeps, in page order. The
 * read-only sections (path values, try-it, other keys) show under "All". */
function shownSections(config: Config, filter: Filter, draft: Draft, browser: Record<string, BrowserChoice>): SectionId[] {
  const keys = visibleKeys(config, filter, draft, browser);
  return SECTIONS.map((s) => s.id).filter((id) => {
    if ((keys.get(id) ?? []).length > 0) return true;
    if (filter !== "all") return false;
    if (id === "explorer" || id === "tryit") return true;
    if (id === "indexing") return true; // .graphveinignore
    if (id === "other") return config.other_keys.some((o) => o.edited_by === null && o.key !== LINK_TEMPLATE) || config.project !== null;
    return false;
  });
}

function Form({
  config,
  browser,
  draft,
  filter,
  typedEdits,
  written,
  onDraft,
}: {
  config: Config;
  browser: Record<string, BrowserChoice>;
  draft: Draft;
  filter: Filter;
  typedEdits: boolean;
  written: boolean;
  onDraft: (key: ConfigKey, next: SettingValue) => void;
}) {
  const keys = visibleKeys(config, filter, draft, browser);
  const sections = shownSections(config, filter, draft, browser);
  const pending = config.index_settings?.state === "changed" ? config.index_settings.changed_keys : [];
  return (
    <div className="grid min-w-0 gap-4" data-testid="settings-form">
      {sections.length === 0 && <p className="text-[13px] text-muted-foreground">No key matches this filter.</p>}
      {sections.map((id) => {
        const meta = SECTIONS.find((s) => s.id === id);
        return (
          <section key={id} id={anchor(id)} tabIndex={-1} aria-labelledby={`${anchor(id)}-h`} className="min-w-0 scroll-mt-4 rounded-lg border bg-card outline-none" data-testid={anchor(id)}>
            <header className="flex flex-wrap items-baseline gap-x-2.5 border-b px-4 py-2.5">
              <h2 id={`${anchor(id)}-h`} className="m-0 text-[15px] font-semibold">
                {meta?.title ?? id}
              </h2>
              <span className="text-[12.5px] text-muted-foreground">{meta?.note}</span>
            </header>
            <div className="grid divide-y px-4">
              {(keys.get(id) ?? []).map((k) => (
                <KeyRow
                  key={k.key}
                  config={config}
                  k={k}
                  browser={browser[k.key] ?? null}
                  draft={draft}
                  typedEdits={typedEdits}
                  notIndexed={written && pending.includes(k.key)}
                  onDraft={onDraft}
                />
              ))}
              {id === "editor" && filter === "all" && <LinkTemplateRow config={config} />}
              {id === "indexing" && filter === "all" && <IgnoreRow config={config} />}
              {id === "explorer" && <ElsewhereRow config={config} keyName="explorer.path_values" editor="the Explorer's path values" href={explorerEditHash("path-values")} />}
              {id === "tryit" && <ElsewhereRow config={config} keyName="tryit.environments" editor="the Explorer's try it environments" href={explorerEditHash("environments")} />}
              {id === "other" && <OtherKeys config={config} />}
            </div>
          </section>
        );
      })}
    </div>
  );
}

/** browserControl is the per-browser rung's control. */
function browserControl(key: string): ReactNode {
  if (key === "ui.labels") return <LabelSetting labelledBy="settings-ui.labels-label" />;
  if (key === "ui.impact_depth") return <DepthSetting kind="impact" labelledBy="settings-ui.impact_depth-label" />;
  if (key === "ui.chain_depth") return <DepthSetting kind="chain" labelledBy="settings-ui.chain_depth-label" />;
  if (key === "ide.editor") {
    return (
      <div className="w-[260px] max-w-full">
        <EditorControl browserRung />
      </div>
    );
  }
  return null;
}

function KeyRow({
  config,
  k,
  browser,
  draft,
  typedEdits,
  notIndexed,
  onDraft,
}: {
  config: Config;
  k: ConfigKey;
  browser: BrowserChoice;
  draft: Draft;
  typedEdits: boolean;
  notIndexed: boolean;
  onDraft: (key: ConfigKey, next: SettingValue) => void;
}) {
  const field = fieldOf(k.key);
  const rungs = ladder(k, browser);
  const drafted = draft[k.key] !== undefined;
  const editable = typedEdits && k.editable;
  return (
    <div id={rowAnchor(k.key)} tabIndex={-1} className="grid scroll-mt-4 grid-cols-[minmax(0,5fr)_minmax(0,7fr)] gap-x-6 gap-y-2 py-3.5 outline-none max-[1100px]:grid-cols-1" data-testid="settings-key" data-key={k.key}>
      <div className="grid content-start gap-0.5">
        <span id={`settings-${k.key}-label`} className="text-[14px] font-medium">
          {field.label}
        </span>
        <code className="text-[12px] text-muted-foreground">{k.key}</code>
        <span className="text-[12.5px] text-muted-foreground">{field.help}</span>
        <span className="text-[12px] text-muted-foreground">
          {k.applies === "at_once" ? "Applies at once." : config.reindex.scope === "project" ? "Applies at the next project index run." : "Applies at the next index run."}
        </span>
      </div>
      <div className="grid min-w-0 grid-cols-[96px_minmax(0,1fr)] content-start items-center gap-x-3 gap-y-1.5 text-[13px]">
        {rungs.map((r) => (
          <RungRow key={r.scope} rung={r}>
            {r.scope === "browser" ? (
              <span className="flex flex-wrap items-center gap-2">
                {browserControl(k.key)}
                {r.set && <Chip tone="browser">This browser</Chip>}
              </span>
            ) : r.scope === "repository" ? (
              <span className="flex min-w-0 flex-wrap items-center gap-2">
                {editable ? <ValueEditor k={k} value={draftValue(draft, k)} label={`${field.label} in graphvein.yaml`} onChange={(v) => { onDraft(k, v); }} /> : <RungText rung={r} />}
                {r.unknown && <span className="text-[12px] text-muted-foreground">{config.config_error !== null ? "graphvein.yaml does not load" : (config.reason ?? "not known here")}</span>}
                {r.line !== null && (
                  <Chip tone="repository" title={`${config.file}, line ${String(r.line)}`}>
                    graphvein.yaml:{String(r.line)}
                  </Chip>
                )}
                {drafted && <span className="text-[12px] font-medium text-brand-text" data-testid="settings-unsaved-mark">unsaved</span>}
                {notIndexed && !drafted && (
                  <span className="inline-flex items-center gap-1 text-[12px] text-dynamic" data-testid="settings-not-indexed">
                    <RefreshCw aria-hidden="true" className="size-3" />
                    written, not in the index yet
                  </span>
                )}
                {!k.editable && k.reason !== null && <span className="text-[12px] text-muted-foreground">{k.reason}</span>}
              </span>
            ) : (
              <span className="flex items-center gap-2">
                <RungText rung={r} />
                {r.inUse && <Chip tone="builtin">Built-in default</Chip>}
              </span>
            )}
          </RungRow>
        ))}
        <span className="col-span-2 text-[12.5px] text-muted-foreground" data-testid="settings-in-use">
          In use: <b className="font-semibold text-foreground">{inUseWords(rungs)}</b>
        </span>
      </div>
    </div>
  );
}

const SCOPE_WORDS: Record<Rung["scope"], string> = { browser: "This browser", repository: "Repository", builtin: "Built-in" };

function RungRow({ rung, children }: { rung: Rung; children: ReactNode }) {
  return (
    <>
      <span className="text-muted-foreground" data-testid={`rung-${rung.scope}`}>
        {SCOPE_WORDS[rung.scope]}
      </span>
      <span className="min-w-0" data-in-use={rung.inUse ? "true" : undefined}>
        {children}
      </span>
    </>
  );
}

function RungText({ rung }: { rung: Rung }) {
  return (
    <span
      data-testid={`rung-${rung.scope}-value`}
      className={cn(
        "break-words",
        rung.inUse ? "font-semibold text-foreground" : "text-muted-foreground",
        // A value that is set but overridden reads as crossed out.
        rung.set && !rung.inUse && "line-through decoration-muted-foreground/60",
      )}
    >
      {rung.text}
    </span>
  );
}

/** ValueEditor edits a repository value; an empty value means "not set
 * in graphvein.yaml". */
function ValueEditor({ k, value, label, onChange }: { k: ConfigKey; value: SettingValue; label: string; onChange: (v: SettingValue) => void }) {
  const input = "h-8 rounded-md border border-input bg-background px-2 text-[13px]";
  switch (k.kind) {
    case "enum":
      return (
        <select aria-label={label} className={input} value={typeof value === "string" ? value : ""} onChange={(e) => { onChange(e.target.value === "" ? null : e.target.value); }}>
          <option value="">not set</option>
          {k.choices.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
          {/* A value the server's choices do not list stays visible. */}
          {typeof value === "string" && value !== "" && !k.choices.includes(value) && <option value={value}>{value}</option>}
        </select>
      );
    case "boolean":
      return (
        <select aria-label={label} className={input} value={typeof value === "boolean" ? String(value) : ""} onChange={(e) => { onChange(e.target.value === "" ? null : e.target.value === "true"); }}>
          <option value="">not set</option>
          <option value="true">true</option>
          <option value="false">false</option>
        </select>
      );
    case "integer":
      return <IntegerEditor k={k} value={value} label={label} onChange={onChange} />;
    case "list":
    case "map":
      return <LinesEditor k={k} value={value} label={label} onChange={onChange} />;
  }
}

function IntegerEditor({ k, value, label, onChange }: { k: ConfigKey; value: SettingValue; label: string; onChange: (v: SettingValue) => void }) {
  const [text, setText] = useState(typeof value === "number" ? String(value) : "");
  const n = Number(text);
  const valid = text === "" || (Number.isInteger(n) && (k.min === null || n >= k.min) && (k.max === null || n <= k.max));
  return (
    <span className="flex items-center gap-2">
      <input
        aria-label={label}
        inputMode="numeric"
        placeholder="not set"
        aria-invalid={!valid}
        className="h-8 w-24 rounded-md border border-input bg-background px-2 text-[13px] tabular-nums aria-invalid:border-stale"
        value={text}
        onChange={(e) => {
          const t = e.target.value.trim();
          setText(t);
          const v = Number(t);
          if (t === "") onChange(null);
          else if (Number.isInteger(v) && (k.min === null || v >= k.min) && (k.max === null || v <= k.max)) onChange(v);
        }}
      />
      {!valid && k.min !== null && k.max !== null && <span className="text-[12px] text-stale">a whole number from {String(k.min)} to {String(k.max)}</span>}
    </span>
  );
}

function LinesEditor({ k, value, label, onChange }: { k: ConfigKey; value: SettingValue; label: string; onChange: (v: SettingValue) => void }) {
  const kind = k.kind === "map" ? "map" : "list";
  const [text, setText] = useState(linesOf(value, kind));
  const parsed = parseLines(text, kind);
  return (
    <span className="grid w-full max-w-[420px] gap-1">
      <textarea
        aria-label={label}
        aria-invalid={parsed === undefined}
        rows={Math.min(6, Math.max(2, text.split("\n").length))}
        placeholder={kind === "map" ? "not set (one “path: base” per line)" : "not set (one pattern per line)"}
        className="w-full resize-y rounded-md border border-input bg-background px-2 py-1 font-mono text-[12.5px] aria-invalid:border-stale"
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          const v = parseLines(e.target.value, kind);
          if (v !== undefined) onChange(v);
        }}
      />
      {parsed === undefined && <span className="text-[12px] text-stale">Each line needs “key: value”.</span>}
    </span>
  );
}

function IgnoreRow({ config }: { config: Config }) {
  const { ignore } = config;
  return (
    <div className="grid grid-cols-[minmax(0,5fr)_minmax(0,7fr)] gap-x-6 gap-y-2 py-3.5 max-[1100px]:grid-cols-1" data-testid="settings-ignore">
      <div className="grid content-start gap-0.5">
        <span className="text-[14px] font-medium">Ignored paths</span>
        <code className="text-[12px] text-muted-foreground">{ignore.file}</code>
        <span className="text-[12.5px] text-muted-foreground">Read by the scanner at the repository root. Shown read-only: edit the file itself.</span>
      </div>
      <div className="min-w-0 text-[13px]">
        {ignore.exists === null ? (
          <span className="text-muted-foreground">unknown: not readable here</span>
        ) : !ignore.exists ? (
          <span className="text-muted-foreground">no {ignore.file} at the root</span>
        ) : (
          <pre className="max-h-40 overflow-auto rounded-md border bg-code-bg px-2 py-1 font-mono text-[12px]">{ignore.text}</pre>
        )}
      </div>
    </div>
  );
}

function ElsewhereRow({ config, keyName, editor, href }: { config: Config; keyName: string; editor: string; href: string }) {
  const entry = config.other_keys.find((o) => o.key === keyName);
  return (
    <div className="flex flex-wrap items-center gap-2 py-3.5 text-[13px]" data-testid={`settings-elsewhere-${keyName}`}>
      <code className="text-[12px] text-muted-foreground">{keyName}</code>
      {config.config_error !== null || config.exists === null ? (
        <span className="text-muted-foreground">unknown</span>
      ) : entry !== undefined ? (
        <Chip tone="repository">graphvein.yaml:{String(entry.line)}</Chip>
      ) : (
        <span className="text-muted-foreground">not set in graphvein.yaml</span>
      )}
      <span className="text-muted-foreground">
        Edited in{" "}
        <a className="text-brand-text underline underline-offset-2" href={href}>
          {editor}
        </a>
        , which checks what this form cannot (registrations, pins, base URLs).
      </span>
    </div>
  );
}

/** LinkTemplateRow shows ide.link_template in the Editor section when
 * graphvein.yaml sets it: while set it builds every editor link (the
 * Editor for links row says so in its repository rung). It is edited in
 * the YAML view only; nothing is shown when the file does not set it. */
function LinkTemplateRow({ config }: { config: Config }) {
  const entry = config.other_keys.find((o) => o.key === LINK_TEMPLATE);
  if (entry === undefined || config.config_error !== null) return null;
  return (
    <div id={rowAnchor(LINK_TEMPLATE)} tabIndex={-1} className="flex scroll-mt-4 flex-wrap items-center gap-2 py-3.5 text-[13px] outline-none" data-testid="settings-link-template">
      <span className="font-medium">Link template</span>
      <code className="text-[12px] text-muted-foreground">{LINK_TEMPLATE}</code>
      <Chip tone="repository">graphvein.yaml:{String(entry.line)}</Chip>
      <span className="text-muted-foreground">Builds every editor link while it is set; edited in the YAML view.</span>
    </div>
  );
}

function OtherKeys({ config }: { config: Config }) {
  // ide.link_template shows in the Editor section instead.
  const plain = config.other_keys.filter((o) => o.edited_by === null && o.key !== LINK_TEMPLATE);
  return (
    <div className="grid gap-3 py-3.5 text-[13px]" data-testid="settings-other-keys">
      {plain.length > 0 && (
        <div className="grid gap-1.5">
          <span className="text-muted-foreground">Keys graphvein.yaml sets that this form does not edit; the YAML view edits them, and every form write keeps them.</span>
          <ul className="flex flex-wrap gap-1.5">
            {plain.map((o) => (
              <li key={o.key} className="inline-flex items-center gap-1.5 rounded-md border px-1.5 py-0.5">
                <code className="text-[12px]">{o.key}</code>
                <span className="text-[12px] text-brand-text">:{String(o.line)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {config.project !== null && (
        <div className="grid gap-1.5" data-testid="settings-project-environments">
          <span className="font-medium">Project try it environments ({config.project.file}, read-only here)</span>
          {config.project.error !== null ? (
            <span className="text-stale">{config.project.file} does not load: {config.project.error}</span>
          ) : config.project.environments.length === 0 ? (
            <span className="text-muted-foreground">none declared</span>
          ) : (
            <ul className="grid gap-1">
              {config.project.environments.map((e) => (
                <li key={e.name} className="flex flex-wrap items-center gap-2">
                  <b className="font-medium">{e.label}</b>
                  <code className="text-[12px] text-muted-foreground">{e.name}</code>
                  <Chip tone="repository">
                    {config.project?.file}:{String(e.line)}
                  </Chip>
                  <span className="text-muted-foreground">{e.base_urls.map((b) => `${b.repository} → ${b.base_url}`).join(", ") || "no base URLs"}</span>
                  {!e.for_member && <span className="text-muted-foreground">(no base URL for this member)</span>}
                </li>
              ))}
            </ul>
          )}
          <span className="text-muted-foreground">
            Edited in{" "}
            <a className="text-brand-text underline underline-offset-2" href={explorerEditHash("environments")}>
              the Explorer's try it environments
            </a>{" "}
            (target project).
          </span>
        </div>
      )}
    </div>
  );
}

function PageIndex({ config, filter, draft, browser }: { config: Config; filter: Filter; draft: Draft; browser: Record<string, BrowserChoice> }) {
  const sections = shownSections(config, filter, draft, browser);
  return (
    <nav aria-label="On this page" className="sticky top-0 grid gap-0.5 text-[13px] max-[1100px]:hidden" data-testid="settings-index">
      <span className="px-2 pb-1 text-[12px] font-medium text-muted-foreground">On this page</span>
      <a href={settingsHash("storage")} onClick={(e) => { e.preventDefault(); replaceHash(settingsHash("storage")); document.getElementById(STORAGE_ANCHOR)?.scrollIntoView({ block: "start" }); }} className="rounded-md px-2 py-1 text-muted-foreground hover:bg-accent hover:text-foreground">
        Storage
      </a>
      {sections.map((id) => (
        <a key={id} href={settingsHash(id)} onClick={(e) => { e.preventDefault(); replaceHash(settingsHash(id)); document.getElementById(anchor(id))?.scrollIntoView({ block: "start" }); }} className="rounded-md px-2 py-1 text-muted-foreground hover:bg-accent hover:text-foreground">
          {SECTIONS.find((s) => s.id === id)?.title ?? id}
        </a>
      ))}
    </nav>
  );
}

/** SaveBar is the fixed bar under the form: the same height in every state
 * (measured: no layout shift between idle, unsaved and the preview). */
function SaveBar({ count, words, onDiscard, onReview }: { count: number; words: string; onDiscard: () => void; onReview: () => void }) {
  return (
    <div className="sticky bottom-0 z-10 -mx-5 mt-auto flex h-[52px] flex-none items-center gap-3 border-t bg-background px-5 text-[13px] max-[1400px]:-mx-4 max-[1400px]:px-4" data-testid="settings-savebar">
      {count === 0 ? (
        <span className="flex min-w-0 items-center gap-2 text-muted-foreground" data-testid="settings-savebar-idle">
          <Info aria-hidden="true" className="size-4 flex-none" />
          {/* Two lines fit the bar's 52 px, so a narrow window wraps the
              sentence instead of cutting it. */}
          <span className="line-clamp-2 leading-[1.3]">No unsaved changes. Browser choices apply at once; changes to graphvein.yaml are previewed before they are written.</span>
        </span>
      ) : (
        <>
          <span aria-hidden="true" className="size-2 flex-none rounded-full bg-brand" />
          <b className="font-semibold whitespace-nowrap" data-testid="settings-unsaved-count">
            {count === 1 ? "1 change" : `${String(count)} changes`} to graphvein.yaml
          </b>
          <span className="min-w-0 truncate text-muted-foreground">{words}</span>
          <span className="flex-1" />
          <Button variant="outline" size="sm" onClick={onDiscard}>
            Discard
          </Button>
          <Button size="sm" onClick={onReview} data-testid="settings-review">
            Review and save…
          </Button>
        </>
      )}
    </div>
  );
}

function PreviewDialog({ config, readAt, state, onClose, onApply }: { config: Config; readAt: string; state: PreviewState; onClose: () => void; onApply: () => void }) {
  const result = state.kind === "ok" || state.kind === "applying" ? state.result : null;
  return (
    <Modal title="Review the change to graphvein.yaml" onClose={onClose} wide testId="settings-preview">
      {state.kind === "loading" && <Skeleton className="h-40" />}
      {state.kind === "failed" && (
        <p role="alert" className="flex items-start gap-2 text-stale">
          <CircleAlert aria-hidden="true" className="mt-0.5 size-4 flex-none" />
          The server refused the edit: {state.message}. Nothing was written.
        </p>
      )}
      {result !== null && (
        <>
          <div className="grid gap-1">
            <h3 className="m-0 text-[13px] font-semibold">What changes</h3>
            {result.changes.length === 0 ? (
              <p className="m-0 text-muted-foreground">No change to the file.</p>
            ) : (
              <ul className="m-0 grid gap-0.5 pl-4" data-testid="settings-preview-changes">
                {result.changes.map((c) => (
                  <li key={c}>
                    <ChangeWords text={c} />
                  </li>
                ))}
              </ul>
            )}
            {result.changed_keys.length > 0 && (
              <ul className="m-0 grid gap-0.5 pl-4 text-[12.5px]">
                {result.changed_keys.map((c) => (
                  <li key={c.key} className={c.applies === "at_once" ? "text-muted-foreground" : "text-dynamic"}>
                    <code>{c.key}</code> {c.applies === "at_once" ? "applies at once" : "applies at the next index run"}
                  </li>
                ))}
              </ul>
            )}
            {result.other_keys_changed && <p className="m-0 text-[12.5px] text-dynamic">Keys outside the form change too; when they apply depends on the key.</p>}
          </div>
          <DiffBlock diff={result.diff} />
          {result.valid ? (
            <p className="m-0 flex items-start gap-2 rounded-md border border-brand/30 bg-brand-soft px-3 py-2" data-testid="settings-preview-valid">
              <Info aria-hidden="true" className="mt-0.5 size-4 flex-none" />
              <span>
                {["Valid.", keptWords(result)].filter((w) => w !== "").join(" ")} Written only if graphvein.yaml still has the content read at {formatShortTime(readAt)} ({shortHash(config.hash)}); otherwise nothing is written and the page reads it again.
              </span>
            </p>
          ) : (
            <div role="alert" className="grid gap-1 rounded-md border border-stale/50 px-3 py-2" data-testid="settings-preview-invalid">
              <p className="m-0 flex items-center gap-2 font-medium text-stale">
                <TriangleAlert aria-hidden="true" className="size-4" />
                The result does not load; it cannot be written.
              </p>
              {result.problems.map((p) => (
                <pre key={p} className="m-0 font-mono text-[12px] whitespace-pre-wrap">
                  {p}
                </pre>
              ))}
            </div>
          )}
        </>
      )}
      <div className="flex items-center gap-2 border-t pt-3">
        <span className="text-[12px] text-muted-foreground">Loopback only · session token</span>
        <span className="flex-1" />
        <Button variant="outline" size="sm" onClick={onClose} data-autofocus>
          Back
        </Button>
        <Button size="sm" onClick={onApply} disabled={result === null || !result.valid || result.changes.length === 0 || state.kind === "applying"} data-testid="settings-write">
          {state.kind === "applying" ? "Writing…" : "Write graphvein.yaml"}
        </Button>
      </div>
    </Modal>
  );
}

/** ChangeWords shows a change line of the server with its `quoted`
 * values and items as code. */
function ChangeWords({ text }: { text: string }) {
  const parts = text.split("`");
  // An odd number of backticks leaves the text as it is.
  if (parts.length % 2 === 0) return <>{text}</>;
  return (
    <>
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <code key={i} className="rounded-[4px] bg-code-bg px-1 font-mono text-[12px]">
            {part}
          </code>
        ) : (
          part
        ),
      )}
    </>
  );
}

function ConfigErrorBox({ config, onYaml }: { config: Config; onYaml: () => void }) {
  return (
    <div role="alert" className="grid gap-2 rounded-lg border border-stale/50 px-4 py-3 text-[13px]" data-testid="settings-config-error">
      <p className="m-0 flex items-center gap-2 font-semibold">
        <TriangleAlert aria-hidden="true" className="size-4 text-stale" />
        {config.file} does not load, so no repository value is shown and the form cannot edit it.
      </p>
      <pre className="m-0 rounded-md bg-code-bg px-2 py-1 font-mono text-[12px] whitespace-pre-wrap">{config.config_error}</pre>
      <p className="m-0 text-muted-foreground">Graphvein reads none of its values until it loads. Fix it in the YAML view (the preview checks it) or in your editor.</p>
      {config.editable && (
        <div>
          <Button variant="outline" size="sm" onClick={onYaml}>
            <Code2 aria-hidden="true" />
            Fix in the YAML view
          </Button>
        </div>
      )}
    </div>
  );
}

/** ReindexNotice says which written values the index does not hold yet
 * (index_settings.state "changed") and runs the shell's re-index. */
function ReindexNotice({ config, written, reindex }: { config: Config; written: boolean; reindex: SettingsReindex | undefined }) {
  const s = config.index_settings;
  const slot = reindex?.model.slot;
  const running = slot !== undefined && (slot.kind === "running" || slot.kind === "pending" || slot.kind === "checking" || slot.kind === "elsewhere");
  if (s === null || (s.state !== "changed" && !running)) return null;
  const names = s.changed_keys.map((k) => fieldOf(k).label.toLowerCase());
  const n = s.changed_keys.length;
  const progress = slot?.kind === "running" && slot.job?.state === "running" ? slot.job.progress : null;
  return (
    <div role="status" className="grid gap-1.5 rounded-lg border border-brand/30 bg-brand-soft/60 px-4 py-3 text-[13px]" data-testid="settings-reindex-notice">
      <p className="m-0 flex items-start gap-2">
        <RefreshCw aria-hidden="true" className={cn("mt-0.5 size-4 flex-none", running && "animate-spin")} />
        <span>
          {written && <b className="font-semibold">graphvein.yaml written. </b>}
          {running ? (
            <>
              Re-indexing{n > 0 ? ` so ${names.join(", ")} apply` : ""}: <b className="font-semibold">{slotLabel(slot)}</b>
              {slot.kind === "running" && slot.job?.phase != null && ` · ${(PHASE_LABELS[slot.job.phase] ?? slot.job.phase).toLowerCase()}`}. The views keep showing the current index until it commits.
            </>
          ) : (
            <>
              <b className="font-semibold">{n === 1 ? "1 value is" : `${String(n)} values are`} not in the index yet:</b> {names.join(", ")}. {config.reindex.scope === "project" ? "A project re-index applies them." : "A re-index applies them."}
            </>
          )}
        </span>
      </p>
      <div className="flex h-8 items-center gap-3 pl-6">
        {running ? (
          <>
            <span className="h-1.5 w-[180px] overflow-hidden rounded-full bg-muted" aria-hidden="true">
              {progress !== null && progress.total > 0 && <span className="block h-full bg-brand" style={{ width: `${String(Math.round((100 * progress.done) / progress.total))}%` }} />}
            </span>
            <span className="text-[12px] text-muted-foreground">also shown on Re-index in the top bar</span>
          </>
        ) : reindex !== undefined && slot?.kind !== "unavailable" ? (
          <Button size="sm" onClick={reindex.start} data-testid="settings-reindex">
            Re-index now
          </Button>
        ) : (
          <span className="text-[12px] text-muted-foreground">{slot?.kind === "unavailable" ? slot.reason || "Re-index is not available here." : "Run graphvein index to apply them."}</span>
        )}
      </div>
    </div>
  );
}

function YamlView({ config, readAt, text, onText, onBack, onPreview }: { config: Config; readAt: string; text: string; onText: (t: string) => void; onBack: () => void; onPreview: () => void }) {
  const fields = useMemo(() => lineFields(config), [config]);
  const lines = text.split("\n");
  const asRead = text === (config.text ?? "");
  return (
    <div className="grid gap-2.5" data-testid="settings-yaml">
      <div className="flex flex-wrap items-center gap-2.5 text-[13px]">
        <Button variant="outline" size="sm" onClick={onBack}>
          <ArrowLeft aria-hidden="true" />
          Back to the form
        </Button>
        <b className="font-semibold">graphvein.yaml</b>
        <span className="text-muted-foreground">
          {config.exists === false ? "no file yet" : `${String(lines.length)} lines`} · read {formatShortTime(readAt)} · {shortHash(config.hash)}
        </span>
        <span className="flex-1" />
        <Button size="sm" onClick={onPreview} disabled={!config.editable || asRead} data-testid="settings-yaml-preview">
          Preview change…
        </Button>
      </div>
      <div className="overflow-x-auto rounded-lg border bg-code-bg">
        <div className="grid min-w-fit grid-cols-[auto_150px_minmax(480px,1fr)] py-2 font-mono text-[12.5px] leading-[22px]">
          <div aria-hidden="true" className="grid content-start pr-3 pl-3 text-right text-muted-foreground select-none">
            {lines.map((_, i) => (
              <span key={i}>{String(i + 1)}</span>
            ))}
          </div>
          <div aria-hidden="true" className="grid content-start pr-2 font-sans select-none" data-testid="settings-yaml-fields">
            {lines.map((_, i) => {
              const f = asRead ? fields.get(i + 1) : undefined;
              return (
                <span key={i} className="h-[22px] truncate">
                  {f !== undefined && (
                    <span className={cn("rounded px-1 py-px text-[11.5px]", f.tone === "field" ? "bg-brand-soft text-brand-text" : f.tone === "editor" ? "bg-fresh-soft text-fresh" : "bg-muted text-muted-foreground")}>
                      {f.text}
                    </span>
                  )}
                </span>
              );
            })}
          </div>
          <textarea
            aria-label="graphvein.yaml"
            spellCheck={false}
            wrap="off"
            readOnly={!config.editable}
            rows={Math.max(lines.length, 4)}
            className="resize-none overflow-hidden bg-transparent pr-3 font-mono text-[12.5px] leading-[22px] outline-none"
            value={text}
            onChange={(e) => {
              onText(e.target.value);
            }}
          />
        </div>
      </div>
      <p className="m-0 text-[12.5px] text-muted-foreground">
        {asRead
          ? "The middle column names the form field each line feeds. Built-in defaults are not written to the file; the form shows them."
          : "Edited: the field names show for the file as read. Preview validates the text and shows the diff before anything is written."}
      </p>
    </div>
  );
}

