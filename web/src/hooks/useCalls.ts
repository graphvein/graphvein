// useCalls loads the calls behind a gap sheet (D4).

import type { ApiClient, CallsParams, CallsResponse } from "@/api/endpoints";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";
import { useEditor, withEditor } from "@/hooks/useEditor";

/** useCalls requests the calls of params, or nothing when params is null
 * (no sheet open, or the never-called sheet, which needs no request). */
export function useCalls(client: ApiClient, params: CallsParams | null, reload: number): RequestResult<CallsResponse> {
  const { requestEditor } = useEditor();
  const key = params === null ? null : JSON.stringify(["calls", params, reload, requestEditor ?? null]);
  return useApiRequest(
    key,
    (signal) => {
      if (params === null) return Promise.reject(new Error("no calls requested"));
      return client.calls({ ...params, ...withEditor(requestEditor) }, { signal });
    },
    requestEditor === undefined,
  );
}
