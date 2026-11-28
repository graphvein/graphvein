// GET /api/v0/store for the first-run line and Settings' Storage section
// (T068 part 3). It needs no index. A reload after a move or a new
// default folder keeps the last answer on screen until the new one
// arrives, so the section does not flash a skeleton.

import { useCallback, useEffect, useState } from "react";

import {
  httpStoreClient,
  type StoreClient,
  type StoreSettings,
} from "@/api/store";
import { useWorkspace } from "@/shell/workspace/WorkspaceRoot";

export type StoreSettingsView =
  | { kind: "loading" }
  | { kind: "ok"; settings: StoreSettings }
  | { kind: "failed"; error: Error };

/** useStoreClient is the workspace's storage client (tests set it), else
 * the HTTP one. */
export function useStoreClient(): StoreClient {
  return useWorkspace()?.storeClient ?? httpStoreClient;
}

/** useStoreSettings reads GET /api/v0/store once and on reload. */
export function useStoreSettings(client: StoreClient): {
  view: StoreSettingsView;
  reload: () => void;
} {
  const [view, setView] = useState<StoreSettingsView>({ kind: "loading" });
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    client.get({ signal: controller.signal }).then(
      (settings) => {
        if (!controller.signal.aborted) setView({ kind: "ok", settings });
      },
      (err: unknown) => {
        if (controller.signal.aborted) return;
        setView({
          kind: "failed",
          error: err instanceof Error ? err : new Error(String(err)),
        });
      },
    );
    return () => {
      controller.abort();
    };
  }, [client, tick]);
  const reload = useCallback(() => {
    setTick((t) => t + 1);
  }, []);
  return { view, reload };
}
