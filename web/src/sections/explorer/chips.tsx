// Small pieces of the Explorer (T029): the provenance chip every field
// carries (variant C of the T029 mockups; its kind is the shared provenance
// chip since T041), the method badge and the section
// state line. Labels come from the label table (lib/labels.ts, rows
// 59-64), so the Plain and Technical styles stay in step.

import type { ShapeProvenance } from "@/api/routeShape";
import { ShapeKindChip, useShapeKindTitle } from "@/components/precision/ProvChip";
import { useLabelStyle } from "@/hooks/useLabels";
import { ChipIdeButton } from "@/sections/explorer/chipLinks";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";

/** shortFile keeps the last two segments of a path for a chip; the
 * tooltip has the whole path. */
export function shortFile(file: string): string {
  const segs = file.split("/");
  return segs.length <= 2 ? file : segs.slice(-2).join("/");
}

/** chainText says the calls followed to reach a site, outermost first. */
export function chainText(p: ShapeProvenance): string {
  return p.chain.map((s) => `${s.function} (${s.file}:${String(s.line)})`).join(" → ");
}

/** ProvenanceChip shows how Graphvein knows one item: its kind (Fact, Read
 * in handler, Intended, Returned, Mapped), and file:line. The tooltip has
 * the rule, the confidence and the chain of calls followed. It ends in the
 * editor button when its route's links have one for file:line. */
export function ProvenanceChip({ p, className }: { p: ShapeProvenance; className?: string }) {
  const kindTitle = useShapeKindTitle(p);
  const chain = chainText(p);
  const title = `${kindTitle}\n${p.file}:${String(p.line)}${chain !== "" ? `\nvia ${chain}` : ""}`;
  return (
    <span
      title={title}
      data-testid="provenance-chip"
      data-kind={p.kind}
      className={cn("inline-flex h-5 max-w-full flex-none items-center gap-1.5 rounded-[5px] border border-border bg-background pr-1.5 pl-0.5 align-middle text-[11.5px] leading-none whitespace-nowrap text-foreground", className)}
    >
      <ShapeKindChip p={p} size="sm" />
      <span className="min-w-0 truncate font-mono text-[11px] text-foreground/80">
        {shortFile(p.file)}:{p.line}
      </span>
      {p.chain.length > 0 && <span className="font-mono text-[10.5px] text-foreground/70">+{p.chain.length} call{p.chain.length === 1 ? "" : "s"}</span>}
      <ChipIdeButton file={p.file} line={p.line} />
    </span>
  );
}

const METHOD_TONE: Record<string, string> = {
  GET: "bg-m-get",
  POST: "bg-m-post",
  PUT: "bg-m-put",
  PATCH: "bg-m-patch",
  DELETE: "bg-m-delete",
};

const METHOD_ROW: Record<string, string> = {
  GET: "border-m-get/30 bg-m-get/[.045]",
  POST: "border-m-post/30 bg-m-post/[.045]",
  PUT: "border-m-put/30 bg-m-put/[.045]",
  PATCH: "border-m-patch/30 bg-m-patch/[.045]",
  DELETE: "border-m-delete/30 bg-m-delete/[.045]",
};

/** methodRowTone is the border and background of an operation row. */
export function methodRowTone(method: string): string {
  return METHOD_ROW[method.toUpperCase()] ?? "border-border bg-muted/40";
}

/** MethodBadge is an operation's method in its colour, Swagger-style. */
export function MethodBadge({ method, className }: { method: string; className?: string }) {
  const m = method.toUpperCase();
  return (
    <span className={cn("inline-grid h-6 w-[64px] flex-none place-items-center rounded-[5px] font-mono text-[11.5px] font-semibold text-background", METHOD_TONE[m] ?? "bg-muted-foreground", className)} data-method={m}>
      {m}
    </span>
  );
}

/** PathKey shows an OpenAPI path key with its {parameters} marked. */
export function PathKey({ path }: { path: string }) {
  const parts = path.split(/(\{[^}]+\})/);
  return (
    <span className="font-mono [overflow-wrap:anywhere]">
      {parts.map((p, i) =>
        p.startsWith("{") && p.endsWith("}") ? (
          <span key={i} className="text-brand-text">
            {p}
          </span>
        ) : (
          <span key={i}>{p}</span>
        ),
      )}
    </span>
  );
}

/** RowTag is the technical style's claim row tag (OG-76 ...); plain hides
 * it and keeps it in the tooltip. */
export function RowTag({ row, why }: { row: string; why: string }) {
  const style = useLabelStyle();
  const l = label(style, "documentRow", { row, why });
  return (
    <span title={l.title} data-testid="row-tag" className="inline-flex h-5 items-center rounded-[5px] border border-dashed px-1.5 font-mono text-[10.5px] text-muted-foreground">
      {l.text !== "" ? l.text : "x-graphvein only"}
    </span>
  );
}
