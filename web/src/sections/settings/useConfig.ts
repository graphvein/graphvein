// The Settings page's read of GET /api/v0/config (T039): the
// answer, when it was read (the preview says "written only if graphvein.yaml
// still has the content read at T"), and a reload. In project mode the
// request names the selected member (api/client.ts), so the read follows
// the member select.

import { useCallback, useEffect, useState } from "react";

import { httpConfigClient, type Config, type ConfigClient } from "@/api/config";

export type ConfigView = { kind: "loading" } | { kind: "ok"; config: Config; readAt: string } | { kind: "failed"; error: Error };

export interface ConfigResult {
  view: ConfigView;
  /** reload asks again; the last answer stays shown until the new one. */
  reload: () => void;
}

/** useConfig reads the configuration of member (null: a single
 * repository); a new member, or a new run (runKey), reads it again. */
export function useConfig(client: ConfigClient = httpConfigClient, member: string | null = null, runKey = ""): ConfigResult {
  const [view, setView] = useState<ConfigView>({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [loadedFor, setLoadedFor] = useState<string | null>(member);
  // A different member never shows the last member's values.
  if (loadedFor !== member) {
    setLoadedFor(member);
    setView({ kind: "loading" });
  }
  useEffect(() => {
    const controller = new AbortController();
    client.fetch({ signal: controller.signal }).then(
      (config) => {
        if (!controller.signal.aborted) setView({ kind: "ok", config, readAt: new Date().toISOString() });
      },
      (err: unknown) => {
        if (!controller.signal.aborted) setView({ kind: "failed", error: err instanceof Error ? err : new Error(String(err)) });
      },
    );
    return () => {
      controller.abort();
    };
  }, [client, member, runKey, attempt]);
  const reload = useCallback(() => {
    setAttempt((a) => a + 1);
  }, []);
  return { view, reload };
}
