// The expanded operation of the Explorer (T029, variant B with variant C's
// provenance chips): handler and path facts, markers, parameters, request
// body, responses and security, each section with the state the document
// proves. Nothing here reads more than the generated document.

import { ArrowUpRight, Lock, Play } from "lucide-react";
import { useState, type ReactNode } from "react";

import type { DocOperation, OpenApiDocument, OpenApiSchema } from "@/api/openapi";
import { depthCapText } from "@/api/routeShape";
import { EdgeChip } from "@/components/precision/ProvChip";
import { Button } from "@/components/ui/button";
import { useLabelStyle } from "@/hooks/useLabels";
import { confidenceNumber, label, RANK_NOTE } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { LinkDifferences } from "@/sections/api/LinkDifferences";
import { ChipLinksProvider, useRouteChipLinks, type ChipLinks } from "@/sections/explorer/chipLinks";
import { ProvenanceChip, shortFile } from "@/sections/explorer/chips";
import { HEAD, SectionBlock, shapeFields, type FieldLine } from "@/sections/explorer/sections";
import { TryItPanel } from "@/sections/explorer/tryit/TryItPanel";
import { useTryIt } from "@/sections/explorer/tryit/TryIt";
import { authLines, handlerName, requestBodyPresence, sectionView, templateProvenance, type ItemView, type SectionKey, type SectionView } from "@/sections/explorer/model";

/** apiHash is the API section link of a route: its callers and impact. */
export function apiHash(route: string): string {
  return `#/api?sel=${encodeURIComponent(route)}&tab=callers`;
}

/** openapiFields lists an object schema's fields, resolving one $ref. */
function openapiFields(schema: OpenApiSchema, doc: OpenApiDocument): FieldLine[] | null {
  let s = schema;
  if (typeof s.$ref === "string") {
    const name = s.$ref.slice(s.$ref.lastIndexOf("/") + 1);
    const target = doc.schemas[name];
    if (target === undefined) return null;
    s = target;
  }
  if (typeof s.items === "object" && s.items !== null && typeof (s.items as OpenApiSchema).$ref === "string") return openapiFields(s.items as OpenApiSchema, doc);
  const props = s.properties;
  if (typeof props !== "object" || props === null) return null;
  const required = Array.isArray(s.required) ? s.required.filter((r): r is string => typeof r === "string") : [];
  return Object.entries(props as Record<string, OpenApiSchema>).map(([name, f]) => ({ name, type: typeText(f), required: required.includes(name) }));
}

function typeText(s: OpenApiSchema): string {
  if (typeof s.$ref === "string") return s.$ref.slice(s.$ref.lastIndexOf("/") + 1);
  const t = s.type;
  if (Array.isArray(t)) return t.map(String).join(" | ");
  if (typeof t === "string") return t === "array" && typeof s.items === "object" && s.items !== null ? `${typeText(s.items as OpenApiSchema)}[]` : t;
  if (Array.isArray(s.enum)) return s.enum.map((v) => JSON.stringify(v)).join(" | ");
  if (Array.isArray(s.anyOf)) return s.anyOf.map((x) => typeText(x as OpenApiSchema)).join(" | ");
  return "no constraint";
}

/** Section is one section of the operation, with the document's schemas
 * (a written Schema Object, or a stored shape kept in x-graphvein) to
 * expand. */
export function Section({ view, op, doc, sub }: { view: SectionView; op: DocOperation; doc: OpenApiDocument; sub?: boolean }) {
  const fieldsOf = (item: ItemView): FieldLine[] | null => {
    if (item.schema === null) return null;
    return item.schema.kind === "openapi" ? openapiFields(item.schema.schema, doc) : shapeFields(item.schema.schema, op.shape?.models ?? []);
  };
  return <SectionBlock view={view} fieldsOf={fieldsOf} sub={sub} bodyPresence={view.key === "body" ? requestBodyPresence(op) : null} />;
}

function AuthWritten({ op }: { op: DocOperation }) {
  const lines = authLines(op);
  if (lines.length === 0) return null;
  return (
    <div className="grid gap-1.5" data-testid="auth-written">
      <div className="text-[12.5px] font-semibold">Auth as written</div>
      <ul className="grid gap-1 text-[12.5px]">
        {lines.map((l) => (
          <li key={l.attribute} className={cn("rounded-md border px-2 py-1.5", l.unknown && "border-dashed")} data-attribute={l.attribute}>
            <div className="flex items-center gap-1.5">
              <Lock aria-hidden="true" className="size-3.5 flex-none text-muted-foreground" />
              <span className="font-medium">{l.title}</span>
              {l.entries.length === 0 && <span className="text-muted-foreground">none read</span>}
            </div>
            {l.entries.length > 0 && (
              <ul className="mt-1 grid gap-0.5 pl-5">
                {l.entries.slice(0, 8).map((e, i) => (
                  <li key={i} className="[overflow-wrap:anywhere]">
                    <span className="font-mono text-[12px]">{e.text}</span>
                    {e.detail !== null && <span className="text-[11.5px] text-muted-foreground"> · {e.detail}</span>}
                  </li>
                ))}
                {l.entries.length > 8 && <li className="text-[11.5px] text-muted-foreground">and {l.entries.length - 8} more</li>}
              </ul>
            )}
          </li>
        ))}
      </ul>
      <p className="text-[11.5px] text-muted-foreground">What the code declares, not proof that the route is protected. An empty list means none was read, never “public”.</p>
    </div>
  );
}

function Fact({ name, children }: { name: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[12.5px]">
      <span className="text-muted-foreground">{name}</span>
      {children}
    </div>
  );
}

/** OperationDetail is the expanded operation. */
export function OperationDetail({ op, doc }: { op: DocOperation; doc: OpenApiDocument }) {
  const style = useLabelStyle();
  const views = Object.fromEntries((["path", "query", "header", "cookie", "body", "responses", "security"] as SectionKey[]).map((k) => [k, sectionView(op, k, doc)])) as Record<SectionKey, SectionView>;
  const handler = handlerName(op);
  const resolution = op.resolution !== "literal" ? label(style, "resolution", op.resolution) : null;
  const userAssumption = op.assumptions.find((a) => a.id === "set_by_you");
  const tryit = useTryIt();
  const [trying, setTrying] = useState(false);
  const links = useRouteChipLinks(op.route);
  return (
    <ChipLinksProvider value={links}>
    <div className="grid gap-4 border-t px-4 pt-3 pb-4 max-[1100px]:px-3" data-testid="operation-detail">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="grid min-w-0 gap-1.5">
          <Fact name="Handler">
            {handler !== null ? <span className="font-mono font-medium">{handler}</span> : <span className="text-muted-foreground italic">not stored</span>}
            <ProvenanceChip p={templateProvenance(op)} />
            <span className="text-[11.5px] text-muted-foreground">{op.framework}</span>
          </Fact>
          {resolution !== null && (
            <Fact name="Path key">
              <span className={cn("rounded-[5px] border px-1.5 text-[12px]", op.resolution === "set_by_you" ? "border-brand/40 bg-brand-soft text-brand-text" : "border-dynamic/35 bg-dynamic-soft text-dynamic")} title={resolution.title} data-testid="resolution">
                {resolution.text}
              </span>
              {userAssumption !== undefined && (
                <span className="font-mono text-[11.5px] text-muted-foreground">
                  {userAssumption.from}: {userAssumption.value}
                </span>
              )}
              {op.resolution !== "set_by_you" && <span className="font-mono text-[11.5px] text-muted-foreground">as registered: {op.template}</span>}
            </Fact>
          )}
          {op.assumptions.filter((a) => a.id !== "set_by_you").length > 0 && (
            <Fact name="Assumes">
              {op.assumptions
                .filter((a) => a.id !== "set_by_you")
                .map((a) => (
                  <span key={`${a.id}-${a.from}`} className="rounded-[5px] border px-1.5 text-[12px]" title={a.text ?? `assumption ${a.id} (${a.from})`}>
                    {a.text ?? a.id}
                  </span>
                ))}
            </Fact>
          )}
          {op.documentedBy.length > 0 && (
            <Fact name="Documented by">
              {op.documentedBy.map((d) => (
                <span key={d.operation} className="inline-flex items-center gap-1.5 rounded-[5px] border border-border pr-1.5 pl-0.5 text-[11.5px]" title={`${d.operation}: linked by an inferred documents_route edge (${confidenceNumber(d.confidence)}); the committed spec is shown beside the code, never merged. ${RANK_NOTE}`} data-testid="documented-by">
                  <EdgeChip edge={{ kind: "documents_route", provenance: "inferred", confidence: d.confidence, match: null }} size="sm" testId="documented-by-tier" />
                  <span className="font-mono text-foreground/80">{shortFile(d.file)}</span>
                </span>
              ))}
            </Fact>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button asChild variant="outline" size="sm">
            <a href={apiHash(op.route)} data-testid="callers-link">
              Callers and impact in API section
              <ArrowUpRight aria-hidden="true" />
            </a>
          </Button>
          {tryit !== null && !trying && (
            <Button
              size="sm"
              onClick={() => {
                setTrying(true);
              }}
              data-testid="try-it-slot"
            >
              <Play aria-hidden="true" />
              Try it out
            </Button>
          )}
        </div>
      </div>
      {links !== null && <LinkedCalls links={links} />}
      <Markers op={op} doc={doc} />
      {trying && (
        <TryItPanel
          op={op}
          doc={doc}
          onClose={() => {
            setTrying(false);
          }}
        />
      )}
      <div className="grid grid-cols-2 gap-x-6 gap-y-4 max-[1100px]:grid-cols-1">
        <div className="grid content-start gap-3">
          <div className={HEAD}>Parameters</div>
          <Section view={views.path} op={op} doc={doc} sub />
          <Section view={views.query} op={op} doc={doc} sub />
          <Section view={views.header} op={op} doc={doc} sub />
          <Section view={views.cookie} op={op} doc={doc} sub />
          <Section view={views.body} op={op} doc={doc} />
        </div>
        <div className="grid content-start gap-3">
          <Section view={views.responses} op={op} doc={doc} />
          <Section view={views.security} op={op} doc={doc} />
          <AuthWritten op={op} />
        </div>
      </div>
      <p className="text-[11.5px] text-muted-foreground" data-testid="never-read">
        Not read by Graphvein for any operation: {op.notRead.filter((n) => ["operationId", "summary", "tags"].includes(n)).join(", ")}.
      </p>
    </div>
    </ChipLinksProvider>
  );
}

/** LinkedCalls is the operation's links area: the linked calls whose
 * request differs from what the route reads (T029 W4), each with its
 * differences; nothing when no linked call shows one. */
function LinkedCalls({ links }: { links: ChipLinks }) {
  const callers = links.callers ?? [];
  if (callers.length === 0) return null;
  return (
    <div className="grid gap-2 rounded-md border px-3 py-2" data-testid="linked-call-differences">
      <div className={HEAD}>Linked calls</div>
      <ul className="grid gap-3">
        {callers.map((c) => (
          <li key={`${c.name}:${c.file}`} className="grid gap-1.5">
            <div className="flex min-w-0 flex-wrap items-baseline gap-x-2 text-[12.5px]">
              <span className="font-mono font-medium [overflow-wrap:anywhere]">{c.name}</span>
              <span className="font-mono text-[11.5px] text-muted-foreground">
                {c.file}
                {c.line !== null ? `:${String(c.line)}` : ""}
              </span>
            </div>
            <LinkDifferences diffs={c.diffs} ide={links.status} />
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Markers lists what may make the operation behave otherwise than its
 * path says: shadowing routes, collapsed registrations, conflicts, a shape
 * that could not be read, chains cut at the depth limit. */
function Markers({ op, doc }: { op: DocOperation; doc: OpenApiDocument }) {
  const conflicts = doc.conflicts.filter((c) => c.routes.includes(op.route));
  const items: ReactNode[] = [];
  if (op.mayBeShadowed.length > 0) {
    items.push(
      <li key="shadow" data-testid="marker-shadowed">
        <b className="font-semibold">May be shadowed</b> by {op.mayBeShadowed.map((s) => `${s.method} ${s.key}`).join(", ")}: the framework answers with the first matching route, and which is registered first is not checked.
      </li>,
    );
  }
  if (op.shadowing === "not_checked") {
    items.push(
      <li key="shadowing" data-testid="marker-shadowing-not-checked">
        <b className="font-semibold">Shadowing not checked</b>: this path uses a prefix set by you below a hop Graphvein could not read, so whether another pattern beside the hop answers it first was not checked.
      </li>,
    );
  }
  if (op.equivalentRegistrations.length > 0) {
    items.push(
      <li key="eq" data-testid="marker-equivalent">
        <b className="font-semibold">{op.equivalentRegistrations.length} equivalent registrations</b>, written as one operation (the first, as the matcher picks it): {op.equivalentRegistrations.join(", ")}
      </li>,
    );
  }
  if (op.conditional) {
    items.push(
      <li key="cond">
        <b className="font-semibold">Conditional</b>: registered under a condition, so it may not exist at runtime.
      </li>,
    );
  }
  for (const c of conflicts) {
    items.push(
      <li key={`c-${c.reason}-${c.keys.join()}`} data-testid="marker-conflict">
        <b className="font-semibold">Conflict {c.reason.replace(/_/g, " ")}</b>: {c.keys.join(", ")}
      </li>,
    );
  }
  if (op.shapeError !== null) {
    items.push(
      <li key="se">
        <b className="font-semibold">The stored shape could not be read</b>: {op.shapeError}. Every section reads as not read.
      </li>,
    );
  }
  if (op.shape !== null && op.shape.depthCap.length > 0) {
    items.push(
      <li key="dc" data-testid="marker-depth-cap">
        <b className="font-semibold">{depthCapText(op.shape.depthLimit)}</b> ({op.shape.depthCap.length} chain{op.shape.depthCap.length === 1 ? "" : "s"}: {op.shape.depthCap.map((c) => (c.at !== null ? `${c.at.function} ${c.at.file}:${String(c.at.line)}` : c.section)).join(", ")})
      </li>,
    );
  }
  if (items.length === 0) return null;
  return <ul className="grid gap-1 rounded-md border border-stale/50 bg-stale/10 px-3 py-2 text-[12.5px]">{items}</ul>;
}
