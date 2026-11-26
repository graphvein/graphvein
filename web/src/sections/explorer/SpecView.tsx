// The Explorer's Spec tab (T029): the generated document as the server
// serves it (the same bytes as `graphvein openapi`), as YAML or JSON with
// line numbers, its coverage beside it, and the download.

import { Download } from "lucide-react";
import { useState } from "react";

import type { ExplorerClient, OpenApiDocument } from "@/api/openapi";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiRequest } from "@/hooks/useApiRequest";
import { formatTime } from "@/lib/time";
import { cn } from "@/lib/utils";

/** downloadName is the saved file's name. */
export function downloadName(repository: string, format: "json" | "yaml"): string {
  return `${repository.replace(/[^A-Za-z0-9._-]/g, "_")}-openapi.${format}`;
}

/** saveDocument fetches the document in a format and hands it to the
 * browser as a download: the same bytes the server serves. */
export async function saveDocument(client: ExplorerClient, repository: string, format: "json" | "yaml"): Promise<void> {
  const text = await client.text(format, {});
  const blob = new Blob([text], { type: format === "json" ? "application/json" : "application/yaml" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = downloadName(repository, format);
  document.body.append(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/** DownloadButtons offers the document as JSON and YAML. */
export function DownloadButtons({ client, repository, compact }: { client: ExplorerClient; repository: string; compact?: boolean }) {
  const [error, setError] = useState<string | null>(null);
  const save = (format: "json" | "yaml") => {
    setError(null);
    saveDocument(client, repository, format).catch((err: unknown) => {
      setError(err instanceof Error ? err.message : String(err));
    });
  };
  return (
    <div className="flex flex-wrap items-center gap-1.5" data-testid="download">
      {compact !== true && <span className="text-[12.5px] text-muted-foreground">Download OpenAPI</span>}
      <Button
        size="sm"
        variant="outline"
        onClick={() => {
          save("json");
        }}
        title={`save ${downloadName(repository, "json")}`}
      >
        <Download aria-hidden="true" />
        JSON
      </Button>
      <Button
        size="sm"
        variant="outline"
        onClick={() => {
          save("yaml");
        }}
        title={`save ${downloadName(repository, "yaml")}`}
      >
        <Download aria-hidden="true" />
        YAML
      </Button>
      {error !== null && (
        <span role="alert" className="text-[12px] text-unmatched">
          {error}
        </span>
      )}
    </div>
  );
}

function Count({ n, text, dashed }: { n: number; text: string; dashed?: boolean }) {
  return (
    <span className={cn("inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-[12.5px]", dashed === true ? "border-dashed" : "bg-muted/50")}>
      <b className="font-semibold tabular-nums">{n}</b>
      {text}
    </span>
  );
}

/** SpecView is the Spec tab. */
export function SpecView({ doc, client, reloadKey }: { doc: OpenApiDocument; client: ExplorerClient; reloadKey: string }) {
  const [format, setFormat] = useState<"yaml" | "json">("yaml");
  const text = useApiRequest(JSON.stringify(["openapi-text", format, reloadKey]), (signal) => client.text(format, { signal }));
  const c = doc.counts;
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_300px] items-start gap-4 max-[1100px]:grid-cols-1" data-testid="spec-view">
      <div className="min-w-0 rounded-lg border bg-card">
        <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
          <div role="tablist" aria-label="Format" className="inline-flex rounded-md bg-muted p-0.5">
            {(["yaml", "json"] as const).map((f) => (
              <button
                key={f}
                role="tab"
                type="button"
                aria-selected={format === f}
                onClick={() => {
                  setFormat(f);
                }}
                className={cn("h-7 rounded-[5px] px-3 text-[12.5px]", format === f ? "bg-background font-medium shadow-xs" : "text-muted-foreground")}
              >
                {f.toUpperCase()}
              </button>
            ))}
          </div>
          <span className="font-mono text-[11.5px] text-muted-foreground">GET /api/v0/openapi?format={format}</span>
        </div>
        {text.state.kind === "ok" ? (
          <CodeBlock text={text.state.data} />
        ) : text.state.kind === "failed" ? (
          <p className="p-3 text-[13px] text-unmatched" role="alert">
            {text.state.error.message}
          </p>
        ) : (
          <div className="grid gap-2 p-3" aria-busy="true">
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-4 w-1/3" />
          </div>
        )}
      </div>
      <aside className="grid gap-3 rounded-lg border bg-card p-3" aria-label="Generated document">
        <h3 className="text-[14px] font-semibold">Generated document</h3>
        <div className="flex flex-wrap gap-1.5">
          <Count n={c.operations} text="operations" />
          <Count n={c.path_keys} text="paths" />
          <Count n={c.with_shape} text="with a stored shape" />
          {c.unresolved > 0 && <Count n={c.unresolved} text="unknown path part" dashed />}
          {c.conflicts > 0 && <Count n={c.conflicts} text="conflicts" dashed />}
          {c.unrepresentable > 0 && <Count n={c.unrepresentable} text="not expressible" dashed />}
          {c.set_by_you > 0 && <Count n={c.set_by_you} text="set by you" />}
          <Count n={doc.importedSpecs.length} text={doc.importedSpecs.length === 1 ? "committed spec (not merged)" : "committed specs (not merged)"} />
        </div>
        <p className="text-[12px] text-muted-foreground">
          {doc.index !== null ? `Generated from the index of ${formatTime(doc.index.finishedAt)} (${doc.index.extractor})` : "The repository is not indexed"}: what the code registers, not the document a running server answers. A field Graphvein did not read is absent and named in x-graphvein.not_read.
        </p>
        <div className="grid gap-1.5">
          <span className="text-[12.5px] font-medium">Download OpenAPI</span>
          <DownloadButtons client={client} repository={doc.repository} compact />
        </div>
        <p className="text-[11.5px] text-muted-foreground">The same bytes as graphvein openapi and GET /api/v0/openapi.</p>
      </aside>
    </div>
  );
}

/** CodeBlock shows text with line numbers: two pre blocks side by side,
 * so a long document stays two DOM nodes. */
function CodeBlock({ text }: { text: string }) {
  const lines = text.endsWith("\n") ? text.slice(0, -1).split("\n") : text.split("\n");
  const numbers = lines.map((_, i) => String(i + 1)).join("\n");
  return (
    <div className="flex max-h-[calc(100vh-260px)] min-h-64 overflow-auto bg-code-bg font-mono text-[12px] leading-[1.6]" data-testid="spec-text" data-lines={lines.length}>
      <pre aria-hidden="true" className="sticky left-0 flex-none border-r bg-code-bg px-2.5 py-2 text-right text-muted-foreground select-none">
        {numbers}
      </pre>
      <pre className="min-w-0 flex-1 px-3 py-2">{lines.join("\n")}</pre>
    </div>
  );
}
