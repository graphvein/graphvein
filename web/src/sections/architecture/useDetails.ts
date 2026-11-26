// The expanded content of components (GET /api/v0/architecture/component)
// for the expanded card, the selected link's two ends and the selected
// card. Several ids at once, each requested once per index reload and
// editor choice; a new reload drops every held answer.

import { useEffect, useRef, useState } from "react";

import type { ArchitectureClient, ComponentResponse } from "@/api/architecture";
import { useEditor, withEditor } from "@/hooks/useEditor";

export type DetailState = { kind: "loading" } | { kind: "ok"; data: ComponentResponse } | { kind: "failed"; error: string };

/** useDetails returns the state per requested component id. retry asks
 * again for the failed ones. */
export function useDetails(client: ArchitectureClient, ids: readonly string[], reload: number): { get: (id: string) => DetailState; retry: () => void } {
  const { requestEditor } = useEditor();
  const scope = JSON.stringify([reload, requestEditor ?? null]);
  const [held, setHeld] = useState<{ scope: string; map: Map<string, DetailState> }>({ scope, map: new Map() });
  const [attempt, setAttempt] = useState(0);
  const inflight = useRef(new Set<string>());
  // The scope of the latest render: an answer of an older scope is dropped.
  const current = useRef(scope);
  useEffect(() => {
    current.current = scope;
  });
  const map = held.scope === scope ? held.map : new Map<string, DetailState>();
  const wanted = ids.filter((id) => !map.has(id) || (attempt > 0 && map.get(id)?.kind === "failed"));
  const key = JSON.stringify([scope, wanted, attempt]);
  useEffect(() => {
    if (requestEditor === undefined) return;
    const req = JSON.parse(key) as [string, string[], number];
    const [sc, ids2] = req;
    for (const id of ids2) {
      const tag = `${sc} ${id}`;
      if (inflight.current.has(tag)) continue;
      inflight.current.add(tag);
      const done = (state: DetailState) => {
        inflight.current.delete(tag);
        if (current.current !== sc) return;
        setHeld((h) => {
          const m = new Map(h.scope === sc ? h.map : []);
          m.set(id, state);
          return { scope: sc, map: m };
        });
      };
      client.component({ id, ...withEditor(requestEditor) }, {}).then(
        (data) => {
          done({ kind: "ok", data });
        },
        (err: unknown) => {
          done({ kind: "failed", error: err instanceof Error ? err.message : String(err) });
        },
      );
    }
  }, [key, client, requestEditor]);
  return {
    get: (id) => map.get(id) ?? { kind: "loading" },
    retry: () => {
      setAttempt((n) => n + 1);
    },
  };
}
