// useEndpointDetail loads the selected endpoint (D3).

import type { ApiClient, EndpointDetail, Scope } from "@/api/endpoints";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";
import { useEditor, withEditor } from "@/hooks/useEditor";

/** useEndpointDetail requests the endpoint id, or nothing when id is null. */
export function useEndpointDetail(
  client: ApiClient,
  id: string | null,
  scope: Scope,
  root: string | null,
  reload: number,
): RequestResult<EndpointDetail> {
  // The editor is part of the key, so a new choice reloads the links
  // instead of showing the old editor's links under the new label.
  const { requestEditor } = useEditor();
  const key = id === null ? null : JSON.stringify(["endpoint", id, scope, root, reload, requestEditor ?? null]);
  return useApiRequest(
    key,
    (signal) => {
      if (id === null) return Promise.reject(new Error("no endpoint selected"));
      return client.endpoint({ id, scope, root, ...withEditor(requestEditor) }, { signal });
    },
    requestEditor === undefined,
  );
}
