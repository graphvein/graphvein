// useSchemaTable loads one table's detail (GET /api/v0/schema/table).

import type { SchemaClient, TableDetail } from "@/api/schema";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";
import { useEditor, withEditor } from "@/hooks/useEditor";

/** useSchemaTable requests the table id, or nothing when id is null. */
export function useSchemaTable(client: SchemaClient, id: string | null, run: string | null, reload: number): RequestResult<TableDetail> {
  // The editor is part of the key, so a new choice reloads the links
  // instead of showing the old editor's links under the new label.
  const { requestEditor } = useEditor();
  const key = id === null || run === null ? null : JSON.stringify(["schema-table", id, run, reload, requestEditor ?? null]);
  return useApiRequest(
    key,
    (signal) => {
      if (id === null) return Promise.reject(new Error("no table selected"));
      return client.table({ id, ...withEditor(requestEditor) }, { signal });
    },
    requestEditor === undefined,
  );
}
