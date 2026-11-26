// useEndpoints loads the coverage counts and the endpoint list (D2).

import type { ApiClient, EndpointsResponse, Scope } from "@/api/endpoints";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";

/** useEndpoints requests both transports for a scope and source root;
 * reload changes when the view must refetch for a new index (D9). */
export function useEndpoints(
  client: ApiClient,
  scope: Scope,
  root: string | null,
  reload: number,
): RequestResult<EndpointsResponse> {
  const key = JSON.stringify(["endpoints", scope, root, reload]);
  return useApiRequest(key, (signal) => client.endpoints({ scope, root }, { signal }));
}
