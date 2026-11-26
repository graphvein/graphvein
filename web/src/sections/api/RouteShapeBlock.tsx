// "Request and response" in the route detail's Overview tab (T029, rows
// OX-40..OX-43): the route's stored request/response shape with the
// Explorer's section pieces (sections/explorer/sections.tsx), so a section
// reads the same here and in the Explorer: provenance chips, "Not read by
// Graphvein", "None, proven" only with its evidence, "None besides the
// security scheme", the depth cap with where to raise it. A lazy chunk:
// the API section loads it only when an HTTP route's Overview is open.

import { ArrowUpRight, CircleSlash, Database } from "lucide-react";

import type { RouteModelTable } from "@/api/modelTables";
import { depthCapText, type RouteShape } from "@/api/routeShape";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { SECTION_TITLE, type ItemView, type SectionKey, type SectionView } from "@/sections/explorer/model";
import { ChipLinksProvider, type ChipLinks } from "@/sections/explorer/chipLinks";
import { HEAD, SectionBlock, shapeFields, type FieldLine } from "@/sections/explorer/sections";
import { depthCapChains, routeShapeViews, type RouteFacts } from "@/sections/api/shapeView";
import { DEFAULT_SCHEMA_PARAMS, groupKeyOf, schemaHash } from "@/sections/schema/params";

/** tableHash opens a model table in the Schema view. */
export function tableHash(t: RouteModelTable): string {
  return schemaHash({ ...DEFAULT_SCHEMA_PARAMS, set: groupKeyOf({ set: t.set, source: "models" }), sel: t.table });
}

/** classLabel writes a class node identity as the code names it: the
 * class and its file ("Item (app/dup/b.py)"), not the shape's internal
 * model name, which may carry a disambiguating suffix. */
export function classLabel(id: string): string {
  const colon = id.indexOf(":");
  const hash = id.lastIndexOf("#");
  if (colon < 0 || hash < colon) return id;
  return `${id.slice(hash + 1)} (${id.slice(colon + 1, hash)})`;
}

/** ModelTablesNote says which of the shape's models are also database
 * tables (T029 row MT-28): the same class declaration, proven by the
 * server, each with a link to the table in the Schema view. */
export function ModelTablesNote({ tables }: { tables: RouteModelTable[] | null }) {
  if (tables === null || tables.length === 0) return null;
  return (
    <ul className="m-0 grid gap-1 pl-0 text-[12.5px]" data-testid="shape-model-tables">
      {tables.map((t) => (
        <li key={`${t.model}|${t.table}`} className="flex list-none flex-wrap items-center gap-x-1.5 gap-y-0.5" title={`The shape's model ${t.model} and the table are the same declaration (${t.class}); row ${t.row}`} data-testid="shape-model-table">
          <Database aria-hidden="true" className="size-3.5 flex-none text-muted-foreground" />
          <span>
            The model <span className="font-mono">{classLabel(t.class)}</span> is also the database table
          </span>
          <a href={tableHash(t)} className="inline-flex items-center gap-0.5 font-mono font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]" data-testid="shape-model-table-link">
            {t.name ?? t.table}
            <ArrowUpRight aria-hidden="true" className="size-3" />
          </a>
          {t.tool !== null && <span className="text-muted-foreground">({t.tool})</span>}
        </li>
      ))}
    </ul>
  );
}

/** quiet says whether a section is shown only in the shared "not read"
 * line: not read and without an item (an item, such as a name a security
 * dependency reads, keeps the section on its own). */
function quiet(v: SectionView): boolean {
  return v.status.kind === "not_read" && v.items.length === 0;
}

/** NotReadLine names, in one hatched line, the sections Graphvein did not
 * read: the Explorer's "Not read by Graphvein" for each, kept compact.
 * Never "none". */
function NotReadLine({ views }: { views: SectionView[] }) {
  const style = useLabelStyle();
  const first = views[0];
  if (first === undefined) return null;
  const l = label(style, "sectionState", { state: "not_read", section: first.key });
  return (
    <div
      data-testid="shape-not-read"
      title={`${l.title}. Absence here is not a claim that there is none.`}
      className="flex items-start gap-2 rounded-md border border-dashed bg-[repeating-linear-gradient(135deg,transparent_0_6px,hsl(var(--muted))_6px_12px)] px-2.5 py-1.5 text-[12.5px] text-muted-foreground"
    >
      <CircleSlash aria-hidden="true" className="mt-0.5 size-3.5 flex-none" />
      <span>
        {l.text}: <span className="text-foreground/80">{views.map((v) => SECTION_TITLE[v.key]).join(", ")}</span>
      </span>
    </div>
  );
}

export interface RouteShapeBlockProps {
  shape: RouteShape | null;
  shapeError: string | null;
  facts: RouteFacts;
  /** The response's shape_links and ide status: each chip with a link
   * ends in its editor button; absent or null shows none. */
  links?: ChipLinks | null;
  /** The route detail's model_tables: models that are also tables. */
  modelTables?: RouteModelTable[] | null;
}

/** RouteShapeBlock is the block's content (the detail pane gives it its
 * heading). */
export function RouteShapeBlock({ shape, shapeError, facts, links = null, modelTables = null }: RouteShapeBlockProps) {
  const views = routeShapeViews(shape, facts);
  const fieldsOf = (item: ItemView): FieldLine[] | null => (item.schema?.kind === "shape" ? shapeFields(item.schema.schema, shape?.models ?? []) : null);
  const capped = depthCapChains(shape);
  const shown = (keys: SectionKey[]) => keys.map((k) => views[k]).filter((v) => !quiet(v));
  const params = shown(["path", "query", "header", "cookie"]);
  const left = [...params, ...shown(["body"])];
  const right = shown(["responses", "security"]);
  const notRead = (["path", "query", "header", "cookie", "body", "responses", "security"] as SectionKey[]).map((k) => views[k]).filter(quiet);
  return (
    <ChipLinksProvider value={links}>
    <div className="grid gap-3" data-testid="route-shape" data-shape={shape === null ? "none" : "stored"}>
      {shapeError !== null ? (
        <p className="m-0 rounded-md border border-stale/50 bg-stale/10 px-3 py-2 text-[12.5px]" data-testid="shape-error">
          <b className="font-semibold">The stored shape could not be read</b>: {shapeError}. Every section reads as not read.
        </p>
      ) : shape === null ? (
        <p className="m-0 text-[12.5px] text-muted-foreground" data-testid="shape-none">
          No request and response shape is stored for this route, so no section is read.
        </p>
      ) : null}
      {capped.length > 0 && (
        <p className="m-0 rounded-md border border-stale/50 bg-stale/10 px-3 py-2 text-[12.5px]" data-testid="shape-depth-cap">
          <b className="font-semibold">{shape !== null ? depthCapText(shape.follow.depth_limit) : ""}</b> ({capped.length} chain{capped.length === 1 ? "" : "s"}:{" "}
          {capped.map((c) => (c.at !== null ? `${c.at.function} ${c.at.file}:${String(c.at.line)}` : SECTION_TITLE[c.section])).join(", ")})
        </p>
      )}
      {(left.length > 0 || right.length > 0) && (
        <div className="@container">
          <div className="grid gap-x-5 gap-y-3 @[680px]:grid-cols-2">
            {left.length > 0 && (
              <div className="grid content-start gap-3">
                {params.length > 0 && <div className={HEAD}>Parameters</div>}
                {left.map((v) => (
                  <SectionBlock key={v.key} view={v} fieldsOf={fieldsOf} sub={v.key !== "body"} narrow />
                ))}
              </div>
            )}
            {right.length > 0 && (
              <div className="grid content-start gap-3">
                {right.map((v) => (
                  <SectionBlock key={v.key} view={v} fieldsOf={fieldsOf} narrow />
                ))}
              </div>
            )}
          </div>
        </div>
      )}
      <ModelTablesNote tables={modelTables} />
      <NotReadLine views={notRead} />
    </div>
    </ChipLinksProvider>
  );
}

export default RouteShapeBlock;
