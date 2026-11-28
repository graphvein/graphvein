// The API section's title row: transport filter with counts, "Include
// tests" (scope all instead of production), the source-root filter and, at
// the right end, the time of the index the data comes from. A transport
// with neither endpoints nor calls is a dashed "none found" tab (T061 D3)
// whose tooltip names what the reader looks for.

import type { TransportSummary } from "@/api/endpoints";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { manifestTitle, noneFound, noneFoundTitle, rootLabel } from "@/sections/api/coverage";
import type { TransportFilter } from "@/sections/api/params";
import { TRANSPORT_BAR } from "@/sections/api/parts";

export interface RootOption {
  root: string;
  total: number;
  title: string;
}

/** rootOptions merges the roots of the given transports (a root may call
 * both) in byte order with "." first, as the server sorts them. */
export function rootOptions(transports: TransportSummary[]): RootOption[] {
  const m = new Map<string, RootOption>();
  for (const t of transports) {
    for (const r of t.roots) {
      const prev = m.get(r.root);
      if (prev === undefined) m.set(r.root, { root: r.root, total: r.total, title: manifestTitle(r) });
      else prev.total += r.total;
    }
  }
  return [...m.values()].sort((a, b) =>
    a.root === b.root ? 0 : a.root === "." ? -1 : b.root === "." ? 1 : a.root < b.root ? -1 : 1,
  );
}

export interface ToolbarProps {
  transports: TransportSummary[] | null;
  transport: TransportFilter;
  onTransport: (t: TransportFilter) => void;
  tests: boolean;
  onTests: (on: boolean) => void;
  root: string | null;
  onRoot: (root: string | null) => void;
  /** Roots of the transports shown (all transports when "All"). */
  roots: RootOption[];
  finishedAt: string | null;
  /** The scope the counts were read in (the endpoints answer's). */
  scope: string;
}

/** Toolbar renders the title row. */
export function Toolbar(props: ToolbarProps) {
  const counts = new Map(props.transports?.map((t) => [t.transport, t.endpoint_count]) ?? []);
  const none = new Set(props.transports?.filter(noneFound).map((t) => t.transport) ?? []);
  const total = props.transports?.reduce((n, t) => n + t.endpoint_count, 0);
  const options: { value: TransportFilter; label: string; n: number | undefined }[] = [
    { value: "all", label: "All", n: total },
    { value: "http", label: "HTTP", n: counts.get("http") },
    { value: "tauri_ipc", label: "Tauri IPC", n: counts.get("tauri_ipc") },
    { value: "server_action", label: "Server actions", n: counts.get("server_action") },
  ];
  const known = props.root === null || props.roots.some((r) => r.root === props.root);
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
      <h1 id="api-title" className="m-0 text-[22px] font-semibold tracking-tight compact:text-xl">
        API
      </h1>
      <div role="group" aria-label="Transport" className="inline-flex h-[34px] items-center gap-0.5 rounded-lg bg-muted p-[3px] text-muted-foreground">
        {options.map((o) => {
          const on = props.transport === o.value;
          const empty = o.value !== "all" && none.has(o.value);
          return (
            <button
              key={o.value}
              type="button"
              aria-pressed={on}
              title={empty && o.value !== "all" ? noneFoundTitle(o.value, props.scope) : undefined}
              data-none-found={empty ? "true" : undefined}
              className={cn(
                "inline-flex h-7 items-center gap-1.5 rounded-md px-[11px] text-[13px] font-medium whitespace-nowrap max-[1299px]:px-2",
                empty && "border border-dashed border-border px-2.5 font-normal text-muted-foreground/90 max-[1299px]:px-2",
                on && "bg-background text-foreground shadow-[0_1px_3px_rgba(0,0,0,.1),0_1px_2px_rgba(0,0,0,.06)]",
              )}
              onClick={() => {
                props.onTransport(o.value);
              }}
            >
              {o.value !== "all" && !empty && <i aria-hidden="true" className={cn("size-[7px] rounded-[2px]", TRANSPORT_BAR[o.value])} />}
              {o.label}
              {empty ? (
                <span className="text-xs">none found</span>
              ) : (
                o.n !== undefined && <span className="text-xs text-muted-foreground tabular-nums">{o.n}</span>
              )}
            </button>
          );
        })}
      </div>
      <label className="inline-flex h-8 cursor-pointer items-center gap-2 rounded-md border border-input px-2.5 text-[13px] font-medium select-none has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring">
        <input
          type="checkbox"
          className="size-3.5 accent-brand"
          checked={props.tests}
          onChange={(e) => {
            props.onTests(e.target.checked);
          }}
        />
        Include tests
      </label>
      <label className="inline-flex h-8 items-center text-[13px] text-muted-foreground">
        <span className="sr-only">Source root</span>
        <select
          className="h-8 max-w-[260px] rounded-md max-[1299px]:max-w-[150px] border border-input bg-background px-2 font-mono text-[12.5px] text-foreground"
          value={props.root ?? ""}
          onChange={(e) => {
            props.onRoot(e.target.value === "" ? null : e.target.value);
          }}
        >
          <option value="">All roots</option>
          {props.roots.map((r) => (
            <option key={r.root} value={r.root} title={r.title}>
              {rootLabel(r.root)} ({r.total})
            </option>
          ))}
          {!known && props.root !== null && <option value={props.root}>{rootLabel(props.root)} (no calls)</option>}
        </select>
      </label>
      {props.finishedAt !== null && (
        <span className="ml-auto text-[12.5px] whitespace-nowrap text-muted-foreground" title={props.finishedAt} data-testid="index-time">
          <span className="max-[1299px]:hidden">Data from the index of</span>
          <span className="hidden max-[1299px]:inline">Index of</span> {formatShortTime(props.finishedAt)}
        </span>
      )}
    </div>
  );
}
