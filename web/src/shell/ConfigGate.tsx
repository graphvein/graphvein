// The configuration gate (T039 ST-F, ADR 0088): graphvein serve starts on
// an index whose graphvein.yaml does not load, so the Settings page can show
// the decoder's message and fix the file. Every other view says the
// configuration does not load, naming the file and the message, instead of
// showing values Graphvein reads from a file it cannot read.

import { TriangleAlert } from "lucide-react";

import { httpConfigClient, type ConfigClient } from "@/api/config";
import { useConfig } from "@/sections/settings/useConfig";

/** ConfigProblem is a graphvein.yaml that does not load. */
export interface ConfigProblem {
  repository: string;
  file: string;
  message: string;
}

/** useConfigProblem reads GET /api/v0/config for member (null: a single
 * repository) whenever key changes (a new run, another section) and
 * returns the problem when the file does not load. A failed or pending
 * read is no problem known: the views answer as they would without it. */
export function useConfigProblem(client: ConfigClient = httpConfigClient, member: string | null, key: string): ConfigProblem | null {
  const { view } = useConfig(client, member, key);
  if (view.kind !== "ok" || view.config.config_error === null) return null;
  return { repository: view.config.repository, file: view.config.file, message: view.config.config_error };
}

/** ConfigErrorScreen stands in for a view while graphvein.yaml does not
 * load, and links to Settings, where the YAML view can fix it. */
export function ConfigErrorScreen({ problem, view }: { problem: ConfigProblem; view: string }) {
  return (
    <div role="alert" className="grid max-w-[760px] gap-2 rounded-lg border border-stale/50 px-4 py-3 text-[13px]" data-testid="config-error-screen">
      <p className="m-0 flex items-center gap-2 font-semibold">
        <TriangleAlert aria-hidden="true" className="size-4 flex-none text-stale" />
        The configuration of {problem.repository} does not load: {problem.file} has an error.
      </p>
      <pre className="m-0 rounded-md bg-code-bg px-2 py-1 font-mono text-[12px] whitespace-pre-wrap">{problem.message}</pre>
      <p className="m-0 text-muted-foreground">
        {view} is not shown while the file does not load: Graphvein reads none of its values until it does, and a re-index refuses it.
      </p>
      <p className="m-0">
        <a className="text-brand-text underline underline-offset-2" href="#/settings">
          Fix it in Settings
        </a>{" "}
        <span className="text-muted-foreground">(the YAML view checks the file before writing it), or in your editor.</span>
      </p>
    </div>
  );
}
