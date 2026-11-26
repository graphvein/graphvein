// Small presentational pieces shared by the API section: badges, paths,
// IDE links, highlighted text and attribute readers for call nodes. Class
// names are written out in full so Tailwind finds them in the source.

import {
  ArrowLeftRight,
  Ban,
  CircleDashed,
  CircleHelp,
  CircleX,
  FlaskConical,
  Globe,
  Layers,
  Link2,
  Puzzle,
  Split,
  Zap,
  type LucideIcon,
} from "lucide-react";
import type { ReactNode } from "react";

import { CATEGORY_LABEL, CATEGORY_TONE, TRANSPORT_LABEL, categoryOf, rawLabel, type Category } from "@/api/categories";
import type { EdgeRef, IdeLink, IdeStatus, PresentedNode, Transport } from "@/api/endpoints";
import { EditorLink } from "@/components/EditorLink";
import { IdeCaveat, IdeMark } from "@/components/IdeParts";
import { EdgeChip, FactChip } from "@/components/precision/ProvChip";
import { useEditor } from "@/hooks/useEditor";
import { ideLabel, presentIde } from "@/lib/ide";
import { cn } from "@/lib/utils";

export const BADGE = "inline-flex items-center gap-[5px] whitespace-nowrap rounded-md border border-transparent px-[7px] text-xs leading-5 font-semibold";

const TRANSPORT_CLASS: Record<Transport, string> = {
  http: "bg-http-soft text-http",
  tauri_ipc: "bg-ipc-soft text-ipc",
  server_action: "bg-action-soft text-action",
};

/** TRANSPORT_BAR is the accent colour of a transport (card tops, group
 * header edges, swatches). */
export const TRANSPORT_BAR: Record<Transport, string> = { http: "bg-http-bar", tauri_ipc: "bg-ipc-bar", server_action: "bg-action-bar" };

/** TransportBadge is M5's transport tag with its colour swatch. */
export function TransportBadge({ transport }: { transport: Transport }) {
  return (
    <span className={cn(BADGE, TRANSPORT_CLASS[transport])}>
      <span aria-hidden="true" className="size-[7px] rounded-[2px] bg-current" />
      {TRANSPORT_LABEL[transport]}
    </span>
  );
}

export type Tone = "linked" | "across" | "unmatched" | "dynamic" | "wrapper" | "other" | "never";

export const TONE_CLASS: Record<Tone, string> = {
  linked: "bg-linked-soft text-linked",
  // Linked across repositories (project scope): the brand colour, apart
  // from "linked" inside the repository.
  across: "bg-brand-soft text-brand-text",
  unmatched: "bg-unmatched-soft text-unmatched",
  dynamic: "bg-dynamic-soft text-dynamic",
  // Wrapper is never told by colour alone: a dashed border marks it.
  wrapper: "bg-wrapper-soft text-wrapper border-dashed border-wrapper/70",
  other: "bg-other-soft text-other",
  never: "bg-never-soft text-never font-medium",
};

export const BAR_CLASS: Record<Tone, string> = {
  linked: "bg-linked-bar",
  across: "bg-brand",
  unmatched: "bg-unmatched-bar",
  dynamic: "bg-dynamic-bar",
  wrapper: "bg-wrapper-bar",
  other: "bg-other-bar",
  never: "bg-never-bar",
};

export const CATEGORY_ICON: Record<Category | "never", LucideIcon> = {
  linked: Link2,
  linked_across: ArrowLeftRight,
  unmatched: CircleX,
  dynamic: Zap,
  wrapper: Layers,
  ambiguous: Split,
  ambiguous_across: Split,
  test_only: FlaskConical,
  unproven: CircleHelp,
  plugin: Puzzle,
  global: Globe,
  other: CircleDashed,
  never: Ban,
};

/** CategoryBadge shows a call's category with its label and icon; the
 * tooltip names the raw status. */
export function CategoryBadge({ transport, raw }: { transport: Transport; raw: string }) {
  const category = categoryOf(transport, raw);
  const Icon = CATEGORY_ICON[category];
  const label = category === "other" ? `Other: ${rawLabel(raw)}` : CATEGORY_LABEL[category];
  return (
    <span
      className={cn(BADGE, TONE_CLASS[CATEGORY_TONE[category]], "max-w-full")}
      title={`Status: ${rawLabel(raw)}`}
      data-category={category}
    >
      <Icon aria-hidden="true" className="size-3 flex-none" />
      <span className="truncate">{label}</span>
    </span>
  );
}

/** statusOf reads a call node's raw status; "" when missing or not a
 * string, as the server counts it. */
export function statusOf(node: PresentedNode): string {
  const s = node.attributes.status;
  return typeof s === "string" ? s : "";
}

const METHOD_CLASS: Record<string, string> = {
  GET: "text-m-get",
  POST: "text-m-post",
  PUT: "text-m-put",
  PATCH: "text-m-patch",
  DELETE: "text-m-delete",
};

/** Method renders an HTTP method in its colour ("?" when unknown). */
export function Method({ method, className }: { method: string | null; className?: string }) {
  return (
    <span
      className={cn(
        "font-mono text-[11px] font-semibold tracking-[.02em]",
        method === null ? "text-muted-foreground" : (METHOD_CLASS[method] ?? "text-foreground"),
        className,
      )}
    >
      {method ?? "?"}
    </span>
  );
}

/** PathText highlights {param} segments of a route template. */
export function PathText({ path }: { path: string }) {
  const parts = path.split(/(\{[^}]+\})/);
  return (
    <>
      {parts.map((p, i) =>
        p.startsWith("{") && p.endsWith("}") ? (
          <span key={i} className="text-muted-foreground">
            {p}
          </span>
        ) : (
          p
        ),
      )}
    </>
  );
}

/** WrappedPath shows a file path that wraps only between segments, with
 * an optional :line. */
export function WrappedPath({ file, line, highlight }: { file: string; line?: number | null; highlight?: string }) {
  const segs = file.split("/");
  return (
    <span className="font-mono text-xs [overflow-wrap:anywhere]">
      {segs.map((seg, i) => (
        <span key={i} className="inline-block max-w-full [overflow-wrap:anywhere]">
          <Highlight text={seg} query={highlight ?? ""} />
          {i < segs.length - 1 ? "/" : null}
          {i === segs.length - 1 && line !== undefined && line !== null ? (
            <span className="text-muted-foreground">:{line}</span>
          ) : null}
        </span>
      ))}
    </span>
  );
}

/** Highlight marks every case-insensitive occurrence of query in text with
 * <mark> elements (no HTML strings). */
export function Highlight({ text, query }: { text: string; query: string }) {
  if (query === "") return <>{text}</>;
  const lower = text.toLowerCase();
  const q = query.toLowerCase();
  const out: ReactNode[] = [];
  let i = 0;
  let n = 0;
  for (;;) {
    const j = lower.indexOf(q, i);
    if (j === -1) break;
    if (j > i) out.push(text.slice(i, j));
    out.push(
      <mark key={n++} className="rounded-[2px] bg-brand-soft text-inherit">
        {text.slice(j, j + q.length)}
      </mark>,
    );
    i = j + q.length;
  }
  out.push(text.slice(i));
  return <>{out}</>;
}

/** ideLabel names the editor of an IDE link from its scheme (lib/ide). */
export { ideLabel };

/** IdeButton opens a node in the editor through the server-built link; it
 * is disabled with the reason when there is no link. Its label, title and
 * accessible name come from the response (presentIde). */
export function IdeButton({ ide, status, label, place }: { ide: IdeLink | null; status: IdeStatus; label?: boolean; place: string }) {
  const view = presentIde(status, ide, useEditor());
  const base =
    "inline-flex h-6 flex-none items-center gap-1 whitespace-nowrap rounded-md px-[7px] text-xs font-medium [&_svg]:size-3.5";
  if (view.href === null) {
    return (
      <button type="button" disabled title={view.title} aria-label={view.name} className={cn(base, "cursor-not-allowed text-muted-foreground opacity-60")}>
        <IdeMark view={view} />
        {label === true ? view.text : null}
      </button>
    );
  }
  return (
    <EditorLink
      href={view.href}
      place={place}
      label={view.text}
      detail={view.title}
      aria-label={view.name}
      data-testid="ide-button"
      className={cn(
        base,
        label === true ? "border border-input bg-background shadow-xs hover:bg-accent" : "hover:bg-accent",
      )}
    >
      <IdeMark view={view} />
      {label === true ? view.text : null}
      <IdeCaveat view={view} />
    </EditorLink>
  );
}

/** EdgeConfidence shows how sure a stored edge is in the applied label
 * style, as the shared provenance chip (T041), with the other style in its
 * tooltip. block puts it on a line of its own (one per edge in a cell). */
export function EdgeConfidence({ edge, size, block = false }: { edge: EdgeRef; size?: "md" | "sm" | undefined; block?: boolean }) {
  return block ? (
    <div>
      <EdgeChip edge={edge} size={size} />
    </div>
  ) : (
    <EdgeChip edge={edge} size={size} />
  );
}

/** FactBadge is an endpoint header's fact chip (T041): "Fact · 1.0" like
 * every other fact chip, as the shared provenance chip. */
export function FactBadge() {
  return <FactChip />;
}

/** FileStateChip marks a node whose file is not as indexed. */
export function FileStateChip({ state }: { state: string | null }) {
  if (state === null || state === "unchanged") return null;
  const text = state === "changed" ? "changed" : state === "missing" ? "missing" : `file ${state}`;
  return (
    <span
      className="rounded-[4px] border border-dynamic/45 px-[5px] text-[11px] leading-4 font-semibold text-dynamic"
      title={state === "missing" ? "The file cannot be read now" : "Changed since the last index; the line may be outdated"}
    >
      {text}
    </span>
  );
}

function attr(node: PresentedNode, key: string): unknown {
  return node.attributes[key];
}

/** isTestScope says whether a node is in test scope. */
export function isTestScope(node: PresentedNode): boolean {
  return attr(node, "scope") === "test";
}

/** viaWrapper says whether a call reaches its endpoint through a wrapper
 * function (http `wrapper`, ipc `wrapper_identity`). */
export function viaWrapper(node: PresentedNode): boolean {
  const w = node.kind === "ipc_call" ? attr(node, "wrapper_identity") : attr(node, "wrapper");
  return typeof w === "string" && w !== "";
}

/** callExpression is the call's source spelling: the URL expression of an
 * HTTP call, the command argument of an invoke. */
export function callExpression(node: PresentedNode): string {
  if (node.kind === "client_call") {
    const u = attr(node, "url_expression");
    return typeof u === "string" ? u : node.name;
  }
  return node.name.startsWith("INVOKE ") ? node.name.slice("INVOKE ".length) : node.name;
}

/** callMethod is the HTTP method or the invoke form of a call. */
export function callMethod(node: PresentedNode): string | null {
  const v = node.kind === "client_call" ? attr(node, "method") : attr(node, "form");
  return typeof v === "string" ? v : null;
}

/** conditionalText explains a conditional call, or null. Several targets
 * of one command name differ by build configuration; otherwise a runtime
 * condition chooses. */
export function conditionalText(node: PresentedNode): string | null {
  if (node.kind === "client_call") {
    const paths = attr(node, "paths");
    const any = Array.isArray(paths) && paths.some((p) => typeof p === "object" && p !== null && (p as Record<string, unknown>).conditional === true);
    return any ? "may call, depending on a runtime condition" : null;
  }
  if (attr(node, "conditional") !== true) return null;
  // A server action use's alternatives are the actions a runtime condition
  // chooses between (T024b R1).
  if (node.kind === "action_call") return "may submit to, depending on a runtime condition";
  const alts = attr(node, "alternatives");
  if (Array.isArray(alts) && alts.length > 1) {
    const commands = new Set(alts.map((a) => (typeof a === "object" && a !== null ? (a as Record<string, unknown>).command : undefined)));
    if (commands.size === 1) return "depending on build configuration";
  }
  return "may call, depending on a runtime condition";
}

/** callerName is the owner's qualified name, else the file's base name. */
export function callerName(owner: { kind: string; qualified_name: string } | null, node: PresentedNode): string {
  const base = node.file.split("/").pop() ?? node.file;
  if (owner === null || owner.qualified_name === "") return base;
  // A call at module level is contained by its file node, whose qualified
  // name is the path shown below it anyway.
  if (owner.kind === "file") return `${base} (top level)`;
  return owner.qualified_name;
}

/** StackedBar draws counts as one bar; segments are decorative (the
 * numbers are always shown next to it). */
export function StackedBar({ parts, className }: { parts: { tone: Tone; count: number }[]; className?: string }) {
  return (
    <div aria-hidden="true" className={cn("flex h-1.5 gap-px overflow-hidden rounded-full bg-muted", className)}>
      {parts
        .filter((p) => p.count > 0)
        .map((p, i) => (
          <div key={i} className={BAR_CLASS[p.tone]} style={{ flexGrow: p.count, flexBasis: 0 }} />
        ))}
    </div>
  );
}
