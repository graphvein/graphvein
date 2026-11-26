// The precision chips of a change item (T020, Variant A): the item's
// scope label and its file's label, and a changed field's label when it
// belongs to another family than the item (a `table_access` field is
// code-to-table). Every label is the item's `precision` from
// /changes/items; nothing is joined in the UI.

import type { ChangeItem } from "@/api/changes";
import type { ScopeLabel } from "@/api/endpoints";
import { PrecisionChip } from "@/components/precision/PrecisionChip";
import { fileChip, scopeChip } from "@/components/precision/words";

/** KIND_FAMILY is the popover column of a row D label, by record kind. */
const KIND_FAMILY: Record<string, string> = {
  route: "routes",
  exposes_route: "routes",
  client_call: "client_calls",
  calls_route: "client_calls",
  ipc_command: "ipc_commands",
  exposes_command: "ipc_commands",
  ipc_call: "ipc_calls",
  invokes_command: "ipc_calls",
  server_action: "server_actions",
  exposes_action: "server_actions",
  action_call: "action_calls",
  calls_action: "action_calls",
  calls: "code_calls",
  table: "schema_other",
  column: "schema_other",
  db_index: "schema_other",
  migration: "schema_other",
};

function columnOf(label: ScopeLabel | null, kind: string): string | null {
  if (label !== null && label.row !== null && label.row !== "D") return label.row;
  return KIND_FAMILY[kind] ?? null;
}

/** ItemPrecisionChips are the item's scope chip ("table facts · Precise")
 * and file chip ("typescript · Best effort"). */
export function ItemPrecisionChips({ item }: { item: ChangeItem }) {
  const p = item.precision;
  if (p === null) return null;
  const language = p.file.language;
  return (
    <>
      <PrecisionChip state={scopeChip(p.scope, p.scope_side)} scope={`${item.kind} facts`} focus={{ column: columnOf(p.scope, item.kind) }} testId="item-scope-precision" />
      {language !== null && <PrecisionChip state={fileChip(p.file)} scope={language} focus={{ language }} testId="item-file-precision" />}
    </>
  );
}

function same(a: ScopeLabel | null, b: ScopeLabel | null): boolean {
  return a !== null && b !== null && a.label === b.label && a.row === b.row && a.reason === b.reason;
}

/** FieldPrecisionChip is a changed field's chip when its label differs
 * from the item's scope label (shown once in the header otherwise). */
export function FieldPrecisionChip({ item, field }: { item: ChangeItem; field: string }) {
  const p = item.precision;
  if (p === null) return null;
  const label = p.fields[field] ?? null;
  if (label === null || same(label, p.scope)) return null;
  return (
    <PrecisionChip
      state={scopeChip(label, p.scope_side)}
      scope={field}
      focus={{ column: columnOf(label, item.kind) }}
      className="ml-1.5"
      testId="field-precision"
    />
  );
}
