// useSchemaMigration loads one migration's detail (GET /api/v0/schema/migration).

import type { MigrationDetail, SchemaClient } from "@/api/schema";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";
import { useEditor, withEditor } from "@/hooks/useEditor";

/** useSchemaMigration requests the migration id, or nothing when id is null. */
export function useSchemaMigration(client: SchemaClient, id: string | null, run: string | null, reload: number): RequestResult<MigrationDetail> {
  // The editor is part of the key, so a new choice reloads the links
  // instead of showing the old editor's links under the new label.
  const { requestEditor } = useEditor();
  const key = id === null || run === null ? null : JSON.stringify(["schema-migration", id, run, reload, requestEditor ?? null]);
  return useApiRequest(
    key,
    (signal) => {
      if (id === null) return Promise.reject(new Error("no migration selected"));
      return client.migration({ id, ...withEditor(requestEditor) }, { signal });
    },
    requestEditor === undefined,
  );
}
