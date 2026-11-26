// The section pieces the Explorer's expanded operation and the API
// section's route detail share (T029): a section's heading, its state line
// and its items, each with its provenance chip. Both places show a shape
// with the same words and the same rules: "Not read by Graphvein" for a
// section not read, "None, proven" only with its evidence, a partial
// section with what it may miss, and the names a security scheme reads.
// Nothing here reads a document or a route: callers pass the views
// (model.ts) and a field reader for the schemas to expand.

import { CircleSlash, TriangleAlert } from "lucide-react";

import type { ShapeSchema } from "@/api/routeShape";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { ProvenanceChip, RowTag } from "@/sections/explorer/chips";
import { notReadName, SECTION_TITLE, type ItemView, type SectionStatus, type SectionView } from "@/sections/explorer/model";

export const HEAD = "text-[11.5px] font-semibold tracking-[.06em] text-muted-foreground uppercase";

/** StateLine says what a section claims as a whole. Only provably_none
 * says "none", and only with its evidence. */
export function StateLine({ view }: { view: SectionView }) {
  const style = useLabelStyle();
  const s: SectionStatus = view.status;
  const stored = s.kind === "not_read" || s.kind === "provably_none" || s.kind === "none_besides_scheme" || s.kind === "partial" || s.kind === "complete";
  const l = stored ? label(style, "sectionState", { state: s.kind, section: view.key }) : null;
  if (s.kind === "not_read") {
    return (
      <div
        data-testid="section-not-read"
        title={`${l?.title ?? ""}; x-graphvein.not_read: ${notReadName(view.key)}. Absence here is not a claim that there is none.`}
        className="flex items-center gap-2 rounded-md border border-dashed bg-[repeating-linear-gradient(135deg,transparent_0_6px,hsl(var(--muted))_6px_12px)] px-2.5 py-1.5 text-[12.5px] text-muted-foreground"
      >
        <CircleSlash aria-hidden="true" className="size-3.5 flex-none" />
        <span>
          {l?.text}
          {s.note !== undefined && <span data-testid="section-note">: {s.note}</span>}
        </span>
      </div>
    );
  }
  if (s.kind === "provably_none") {
    return (
      <div data-testid="section-none" className="flex flex-wrap items-center gap-2 text-[12.5px]" title={l?.title}>
        <span className="font-medium">{l?.text}</span>
        {s.evidence !== null && <ProvenanceChip p={s.evidence} />}
        {s.note !== undefined && <span data-testid="section-note" className="text-muted-foreground">{s.note}</span>}
      </div>
    );
  }
  if (s.kind === "none_besides_scheme") {
    return (
      <div data-testid="section-none-besides-scheme" className="flex flex-wrap items-center gap-2 text-[12.5px]" title={`${l?.title ?? ""}; proven empty apart from what ${s.schemes.join(", ")} reads (see Security)`}>
        <span className="font-medium">{l?.text}</span>
        <span className="text-[11.5px] text-muted-foreground">({s.schemes.join(", ")})</span>
        {s.evidence !== null && <ProvenanceChip p={s.evidence} />}
      </div>
    );
  }
  if (s.kind === "partial") {
    return (
      <div data-testid="section-partial" className="flex items-start gap-1.5 text-[12px] text-dynamic" title={l?.title}>
        <TriangleAlert aria-hidden="true" className="mt-px size-3.5 flex-none" />
        <span>
          <span className="font-medium">{l?.text}</span>
          {s.reason !== null && <span className="text-muted-foreground">: {s.reason}</span>}
        </span>
      </div>
    );
  }
  if (s.kind === "complete") {
    return (
      <div data-testid="section-complete" className="flex flex-wrap items-center gap-2 text-[12px] text-muted-foreground" title={l?.title}>
        <span>{l?.text}</span>
        {s.evidence !== null && <ProvenanceChip p={s.evidence} />}
        {s.note !== undefined && <span data-testid="section-note">{s.note}</span>}
      </div>
    );
  }
  if (s.kind === "template") {
    return (
      <div data-testid="section-template" className="text-[12px] text-muted-foreground">
        Names from the path template; their types are not read.
      </div>
    );
  }
  if (s.kind === "no_template_params") {
    return (
      <div data-testid="section-no-template-params" className="text-[12px] text-muted-foreground">
        The path has no parameters.
      </div>
    );
  }
  return (
    <div data-testid="section-declared" className="text-[12px] text-muted-foreground">
      Written from the declared FastAPI security dependencies.
    </div>
  );
}

/** Presence says whether an item must be sent; unknown is said, never
 * left blank. */
export function Presence({ value }: { value: ItemView["presence"] }) {
  if (value === null) return null;
  if (value === "required") return <span className="text-[11.5px] font-semibold text-unmatched">required</span>;
  if (value === "optional") return <span className="text-[11.5px] text-muted-foreground">optional</span>;
  return (
    <span className="text-[11.5px] text-muted-foreground italic" title="whether it must be sent is not proven">
      required? not read
    </span>
  );
}

/** FieldLine is one field of an expanded schema: required null is "not
 * read", never "optional". */
export interface FieldLine {
  name: string;
  type: string;
  required: boolean | null;
}

/** FieldsOf reads the fields of an item's schema to expand, or null. */
export type FieldsOf = (item: ItemView) => FieldLine[] | null;

/** shapeFields lists a stored schema's fields one level deep, resolving a
 * model reference (and an array's items) through the shape's models. */
export function shapeFields(schema: ShapeSchema, models: readonly { name: string; schema: ShapeSchema }[]): FieldLine[] | null {
  let s: ShapeSchema | undefined = schema;
  if (s.type === "ref") s = models.find((m) => m.name === schema.ref)?.schema;
  if (s?.type === "array" && s.items !== undefined) return shapeFields(s.items, models);
  if (s?.fields === undefined) return null;
  return s.fields.map((f) => ({ name: f.name, type: f.schema.type === "ref" ? (f.schema.ref ?? "ref") : f.schema.type, required: f.presence === "required" ? true : f.presence === "optional" ? false : null }));
}

/** SchemaFields shows an object's fields in a small code-like block. */
function SchemaFields({ item, fieldsOf }: { item: ItemView; fieldsOf: FieldsOf }) {
  if (item.schema === null) return null;
  const fields = fieldsOf(item);
  if (fields === null || fields.length === 0) return null;
  return (
    <div className="mt-1 w-fit max-w-full min-w-[min(260px,100%)] rounded-md border bg-code-bg px-2.5 py-1.5 font-mono text-[11.5px] leading-[1.55]" data-testid="schema-fields">
      {fields.map((f) => (
        <div key={f.name} className="flex flex-wrap gap-x-2">
          <span className="text-brand-text [overflow-wrap:anywhere]">&quot;{f.name}&quot;</span>
          {/* A type can be a long spec reference without a break
           * (docs/openapi/openapi.json#/components/schemas/...): it wraps
           * anywhere, so it never widens the section past its column. */}
          <span className="[overflow-wrap:anywhere]" data-testid="schema-field-type">
            {f.type}
          </span>
          {f.required === true && <span className="text-unmatched">required</span>}
          {f.required === null && <span className="text-muted-foreground italic">required? not read</span>}
        </div>
      ))}
    </div>
  );
}

/** ItemRow is one item with its provenance chip. narrow stacks the chip
 * under the item (the API section's detail pane). Wide, the chip column
 * keeps its one-line width while it fits; when the row is short of space
 * it gives way first, down to the chip's own width, so the "x-graphvein
 * only" tag wraps under the chip before the item's fields wrap (T029). */
function ItemRow({ item, fieldsOf, narrow }: { item: ItemView; fieldsOf: FieldsOf; narrow: boolean }) {
  const style = useLabelStyle();
  const access = item.access !== null && item.access !== "declared" ? label(style, "shapeAccess", item.access) : null;
  return (
    <li
      className={cn("items-start gap-x-3 gap-y-1 border-t px-2 py-1.5 first:border-t-0", narrow ? "grid grid-cols-1" : "flex max-[1100px]:grid max-[1100px]:grid-cols-1", !item.inDocument && item.readBy === undefined && "bg-muted/35")}
      data-testid="section-item"
      data-in-document={item.readBy !== undefined ? "scheme" : item.inDocument ? "true" : "false"}
    >
      <div className="min-w-0 flex-[1_1_auto]">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="font-mono text-[12.5px] font-semibold [overflow-wrap:anywhere]">{item.name}</span>
          {item.readBy !== undefined ? (
            <span className="text-[12px] text-muted-foreground" data-testid="read-by-scheme">
              read by the security scheme <span className="font-mono text-foreground">{item.readBy}</span> (see Security)
            </span>
          ) : (
            <span className="font-mono text-[12px] text-http [overflow-wrap:anywhere]">{item.type}</span>
          )}
          <Presence value={item.presence} />
          {access !== null && (
            <span className="text-[11.5px] text-muted-foreground" title={access.title}>
              {access.text}
            </span>
          )}
          {item.notes.map((n) => (
            <span key={n} className="rounded bg-muted px-1 font-mono text-[11px] text-muted-foreground [overflow-wrap:anywhere]">
              {n}
            </span>
          ))}
        </div>
        <SchemaFields item={item} fieldsOf={fieldsOf} />
      </div>
      <div className={cn("flex flex-[0_1000_auto] flex-wrap items-center gap-1", narrow ? "justify-start" : "justify-end max-[1100px]:justify-start")} data-testid="item-chips">
        <ProvenanceChip p={item.provenance} />
        {item.readBy === undefined && !item.inDocument && item.row !== null && item.why !== null && <RowTag row={item.row} why={item.why} />}
      </div>
    </li>
  );
}

/** SectionBlock is one section's heading, state and items. bodyPresence
 * is the Request body heading's presence (null: none written). */
export function SectionBlock({ view, fieldsOf, sub, bodyPresence = null, narrow = false }: { view: SectionView; fieldsOf: FieldsOf; sub?: boolean | undefined; bodyPresence?: ItemView["presence"]; narrow?: boolean }) {
  const head = <h4 className={sub === true ? "text-[12.5px] font-semibold" : HEAD}>{SECTION_TITLE[view.key]}</h4>;
  return (
    <section className="grid gap-1.5" aria-label={SECTION_TITLE[view.key]} data-testid={`section-${view.key}`} data-state={view.status.kind}>
      {view.key === "body" && bodyPresence !== null ? (
        <div className="flex flex-wrap items-baseline gap-2" data-testid="body-presence">
          {head}
          <Presence value={bodyPresence} />
        </div>
      ) : (
        head
      )}
      {view.items.length > 0 && (
        <ul className="rounded-md border" aria-label={`${SECTION_TITLE[view.key]} items`}>
          {view.items.map((item, i) => (
            <ItemRow key={`${item.name}-${String(i)}`} item={item} fieldsOf={fieldsOf} narrow={narrow} />
          ))}
        </ul>
      )}
      {/* The state line says what the items do not: "not read", proven
       * none, what a partial section may miss. */}
      <StateLine view={view} />
    </section>
  );
}
