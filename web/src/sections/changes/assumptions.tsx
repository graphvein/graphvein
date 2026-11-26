// The assumption texts on I8 paths (T019b part 2): an inferred reads_table
// or writes_table edge holds only if its `assumes` ids hold (T018h, ADR
// 0060). The impact panel shows each id with its title and text from the
// server's catalogue (GET /api/v0/schema/assumptions), and the id opens
// the Schema view's "why" popover (the same component and placement). An
// id the catalogue lacks is shown raw; a catalogue that did not load
// leaves the ids without texts.

import { createContext, useContext, useMemo, type ReactNode } from "react";

import { httpSchemaClient, type AssumptionCatalogue } from "@/api/schema";
import { useApiRequest } from "@/hooks/useApiRequest";
import { useLabel } from "@/hooks/useLabels";
import { catalogueView, SchemaWhyContext, type CatalogueView, type SchemaWhy } from "@/sections/schema/catalogue";
import { DEFAULT_SCHEMA_PARAMS, schemaHash } from "@/sections/schema/params";
import { PopoverContext, usePopoverHost } from "@/sections/schema/usePopover";
import { AssumptionChip, WhyPopoverHost } from "@/sections/schema/WhyPopover";

/** CatalogueLoader requests the catalogue; the shell passes its schema
 * client, tests their own. */
export type CatalogueLoader = (signal: AbortSignal) => Promise<AssumptionCatalogue>;

export const CatalogueLoaderContext = createContext<CatalogueLoader>((signal) => httpSchemaClient.assumptions({ signal }));

/** The catalogue source of the code-to-table assumptions. */
export const TABLE_ACCESS_SOURCE = "table_access";

/** ImpactCatalogue loads the catalogue once the panel shows an assumption
 * and provides the popover state to its children. */
export function ImpactCatalogue({ needed, children }: { needed: boolean; children: ReactNode }) {
  const load = useContext(CatalogueLoaderContext);
  const req = useApiRequest<AssumptionCatalogue>(needed ? "impact-assumptions" : null, load);
  const state = req.state;
  const catalogue = useMemo<CatalogueView>(() => (state.kind === "ok" ? catalogueView(state.data, null) : state.kind === "failed" ? { kind: "failed", message: state.error.message } : { kind: "loading" }), [state]);
  const why = useMemo<SchemaWhy>(
    () => ({
      catalogue,
      openEntry: (entry) => {
        window.location.hash = schemaHash({ ...DEFAULT_SCHEMA_PARAMS, tab: "assumptions", a: entry });
      },
      openMigration: () => undefined,
    }),
    [catalogue],
  );
  const popovers = usePopoverHost();
  return (
    <PopoverContext.Provider value={popovers}>
      <SchemaWhyContext.Provider value={why}>
        {children}
        <WhyPopoverHost />
      </SchemaWhyContext.Provider>
    </PopoverContext.Provider>
  );
}

/** AssumptionText is one `assumes` id on a path: the id (opens the
 * popover), then the catalogue's title and text. */
export function AssumptionText({ id }: { id: string }) {
  const { catalogue } = useContext(SchemaWhyContext);
  const a = catalogue.kind === "ok" ? catalogue.byId.get(id) : undefined;
  // T025 row 19 (lib/labels.ts): the technical style repeats the title
  // before the text, as the chip then shows the id.
  const lead = useLabel("assumptionLead", { id, title: a?.title });
  return (
    <span className="inline-flex min-w-0 basis-full items-baseline gap-1.5" data-testid="assumption" data-assumption={id}>
      <span className="flex-none rounded bg-dynamic-soft px-1 text-[10.5px] font-semibold text-dynamic">assumes</span>
      <span className="flex-none whitespace-nowrap">
        <AssumptionChip id={id} source={TABLE_ACCESS_SOURCE} testId="assumption-chip" />
      </span>
      {a !== undefined ? (
        <span className="min-w-0 [overflow-wrap:anywhere]" data-testid="assumption-text">
          {lead.text !== "" && <b className="font-semibold">{lead.text} </b>}
          <span className="text-muted-foreground">{a.text}</span>
        </span>
      ) : (
        <span className="text-muted-foreground italic">
          {catalogue.kind === "ok" ? "not in this build's catalogue" : catalogue.kind === "failed" ? "assumption text unavailable (the catalogue did not load)" : "loading the text"}
        </span>
      )}
    </span>
  );
}
