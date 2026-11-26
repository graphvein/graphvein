// The Help page (T045): the guide in docs/guide, rendered from the pages
// the build parsed (guide.ts), with an anchor on every heading
// (#/help?at=<slug>, so "Copy link to this view" can address a heading)
// and the examples of Start here: the guide's fixed illustrations, each
// followed by a live case of the index being viewed when it has one
// (examples.ts). No tour and no pop-ups: the page is read like any other
// section.

import { Link as LinkIcon } from "lucide-react";
import { Fragment, useEffect } from "react";

import { httpClient, type ApiClient } from "@/api/endpoints";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiRequest } from "@/hooks/useApiRequest";
import type { StatusView } from "@/hooks/useStatus";
import { cn } from "@/lib/utils";
import { helpHash, parseHashQuery, useHash } from "@/router";
import { loadExamples, type Examples } from "@/sections/help/examples";
import { GuideExampleChip } from "@/components/precision/ProvChip";
import { GUIDE } from "@/sections/help/guide";
import { plainText, type Block, type ExampleKind, type FixedExample, type GuidePage, type Inline } from "@/sections/help/markdown";

/** headingId is the element id of a guide heading. */
export function headingId(slug: string): string {
  return `help-${slug}`;
}

/** docPath is the repository path of a relative link to another public
 * document ("../cli/mcp.md" from docs/guide is "docs/cli/mcp.md"); null
 * for any other link. */
export function docPath(href: string): string | null {
  if (!href.endsWith(".md") || href.includes(":") || href.startsWith("/") || href.startsWith("#")) return null;
  const parts = ["docs", "guide"];
  for (const seg of href.split("/")) {
    if (seg === "..") {
      if (parts.length === 0) return null;
      parts.pop();
    } else if (seg !== "." && seg !== "") parts.push(seg);
  }
  return parts.join("/");
}

const LINK = "font-medium text-brand-text underline-offset-4 hover:underline";

function InlineView({ c, keys = false }: { c: readonly Inline[]; keys?: boolean }) {
  return (
    <>
      {c.map((i, n) => {
        switch (i.t) {
          case "text":
            return <Fragment key={n}>{i.v}</Fragment>;
          case "code":
            return keys ? (
              <kbd key={n} className="mr-1 inline-block rounded-[4px] border border-b-2 bg-muted px-[5px] font-mono text-[12px] leading-5 font-medium text-foreground/80">
                {i.v}
              </kbd>
            ) : (
              <code key={n} className="rounded-[4px] bg-muted px-1 font-mono text-[0.92em]">
                {i.v}
              </code>
            );
          case "strong":
            return (
              <strong key={n} className="font-semibold">
                <InlineView c={i.c} keys={keys} />
              </strong>
            );
          case "link": {
            if (i.href.startsWith("#/")) {
              return (
                <a key={n} href={i.href} className={LINK}>
                  <InlineView c={i.c} keys={keys} />
                </a>
              );
            }
            // A public document of the repository: the app does not serve
            // docs/, so the page names the file instead of linking to it.
            const path = docPath(i.href);
            return (
              <span key={n}>
                <InlineView c={i.c} keys={keys} />
                {path !== null && (
                  <>
                    {" "}
                    (<code className="rounded-[4px] bg-muted px-1 font-mono text-[0.92em]">{path}</code>)
                  </>
                )}
              </span>
            );
          }
        }
      })}
    </>
  );
}

function Heading({ block, current }: { block: Extract<Block, { t: "heading" }>; current: boolean }) {
  const text = plainText(block.c);
  const Tag = block.level === 1 ? "h2" : block.level === 2 ? "h3" : "h4";
  return (
    <Tag
      id={headingId(block.slug)}
      tabIndex={-1}
      data-current={current ? "true" : undefined}
      className={cn(
        "group -mx-2 flex scroll-mt-3 items-baseline gap-1.5 rounded-md px-2 py-0.5 outline-none",
        block.level === 1 ? "mt-6 text-[20px] font-semibold tracking-tight first:mt-0" : block.level === 2 ? "mt-4 text-[16px] font-semibold" : "mt-3 text-[14.5px] font-semibold",
        current && "bg-brand-soft",
      )}
    >
      <span className="min-w-0 [overflow-wrap:anywhere]">
        <InlineView c={block.c} />
      </span>
      <a
        href={helpHash(block.slug)}
        aria-label={`Link to “${text}”`}
        title="Link to this heading"
        className="flex-none self-center rounded-sm p-0.5 text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-foreground focus-visible:opacity-100"
        data-testid="heading-link"
      >
        <LinkIcon aria-hidden="true" className="size-3.5" />
      </a>
    </Tag>
  );
}

/** ExamplesState is the live examples' request: "none" without an index
 * (nothing is requested), "failed" when the endpoint list request fails. */
type ExamplesState = { kind: "none" } | { kind: "failed" } | { kind: "loading" } | { kind: "ok"; examples: Examples };

/** LabelChip is an example's label in the shared provenance chip (T041),
 * so it looks as it does in the views. */
function LabelChip({ kind, text }: { kind: ExampleKind; text: string }) {
  return <GuideExampleChip kind={kind} text={text} />;
}

const EXAMPLE_ROW = "min-h-6 [overflow-wrap:anywhere]";

/** ExampleLine is one label's example box: the guide's fixed example,
 * marked "Example" (never from the repository), then, when the index
 * being viewed has a case (HP-1..HP-3), an "In your index:" line with its
 * link. While the examples load, a placeholder of the same height holds
 * that line's place (HP-4). Without a case nothing more is shown; with an
 * index (reserve) the line's place is kept, so the box has the same height
 * before and after the examples load: it says "none of this kind yet" only
 * when the answer proves it (the list answered; for Inferred, every called
 * route was read), else it stays empty (a failed request, or Inferred
 * searched only in the first called routes). */
function ExampleLine({ kind, fixed, state, reserve }: { kind: ExampleKind; fixed: FixedExample; state: ExamplesState; reserve: boolean }) {
  const ex = state.kind === "ok" ? state.examples[kind] : null;
  const noneProven = state.kind === "ok" && (kind !== "inferred" || state.examples.inferredSearchedAll);
  return (
    <div className="grid gap-1 rounded-md border border-dashed px-2.5 py-1.5 text-[13px]" data-testid={`help-example-${kind}`}>
      <div className={EXAMPLE_ROW} data-testid="help-example-fixed" title="An illustration, not taken from your repository">
        <span className="mr-1.5 rounded-[3px] bg-muted px-1 align-middle text-[10.5px] font-semibold tracking-wide text-muted-foreground uppercase">Example</span>
        <code className="rounded-[4px] bg-muted px-1 font-mono text-[12.5px]">{fixed.subject}</code> <LabelChip kind={kind} text={fixed.label} />
        {fixed.note.length > 0 && (
          <span className="text-muted-foreground">
            {" "}
            <InlineView c={fixed.note} />
          </span>
        )}
      </div>
      {state.kind === "loading" ? (
        <div className={cn(EXAMPLE_ROW, "flex items-center")} data-testid="help-example-pending">
          <Skeleton className="h-4 w-64 max-w-full" />
        </div>
      ) : ex !== null ? (
        <div className={EXAMPLE_ROW} data-testid="help-example-index">
          In your index:{" "}
          <a href={ex.href} className={cn(LINK, ex.row !== "HP-3" && "font-mono text-[13px]")} data-row={ex.row}>
            {ex.subject}
          </a>{" "}
          <LabelChip kind={kind} text={ex.label} />
          {ex.detail !== "" && <span className="text-muted-foreground"> {ex.detail}</span>}
        </div>
      ) : (
        reserve && (
          <div className={cn(EXAMPLE_ROW, "text-muted-foreground")} aria-hidden={noneProven ? undefined : "true"} data-testid="help-example-empty">
            {noneProven ? "In your index: none of this kind yet" : ""}
          </div>
        )
      )}
    </div>
  );
}

function BlockView({ block, at, examples }: { block: Block; at: string | null; examples: ExamplesState }) {
  switch (block.t) {
    case "heading":
      return <Heading block={block} current={block.slug === at} />;
    case "paragraph":
      return (
        <p className="leading-relaxed">
          <InlineView c={block.c} />
        </p>
      );
    case "list": {
      const List = block.ordered ? "ol" : "ul";
      return (
        <List className={cn("grid gap-1 pl-5 leading-relaxed", block.ordered ? "list-decimal" : "list-disc")}>
          {block.items.map((item, n) => (
            <li key={n}>
              <InlineView c={item} />
            </li>
          ))}
        </List>
      );
    }
    case "table":
      return (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-[13.5px]">
            <thead>
              <tr>
                {block.head.map((cell, n) => (
                  <th key={n} className="border-b px-2 py-1.5 text-left font-medium text-muted-foreground first:pl-0">
                    <InlineView c={cell} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, r) => (
                <tr key={r}>
                  {row.map((cell, n) => (
                    <td key={n} className="border-b px-2 py-1.5 align-top first:pl-0">
                      <InlineView c={cell} keys={n === 0} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "example":
      return <ExampleLine kind={block.kind} fixed={block.fixed} state={examples} reserve={examples.kind !== "none"} />;
  }
}

function PageView({ page, at, examples }: { page: GuidePage; at: string | null; examples: ExamplesState }) {
  return (
    <section className="grid gap-2.5" data-testid="help-page" data-file={page.file}>
      {page.blocks.map((b, n) => (
        <BlockView key={n} block={b} at={at} examples={examples} />
      ))}
    </section>
  );
}

/** HelpSection is the Help page. client replaces the API section's
 * requests in tests; guide replaces the parsed pages. */
export function HelpSection({
  status,
  client = httpClient,
  guide = GUIDE,
  noIndex = false,
}: {
  status: StatusView;
  client?: ApiClient | undefined;
  guide?: readonly GuidePage[];
  /** No index to view (the workspace's first-run page, T038): nothing is
   * requested and no live example is shown (HP-4). */
  noIndex?: boolean;
}) {
  const hash = useHash();
  const at = parseHashQuery(hash).get("at");
  const run = !noIndex && status.kind === "ok" && status.status.index.last_run !== null ? status.status.index.last_run.finished_at : null;
  // HP-5: the examples belong to one index run and are requested again
  // for a newer one; without an index there is nothing to pick from.
  const request = useApiRequest(run === null ? null : JSON.stringify(["help-examples", run]), (signal) => loadExamples(client, signal));
  const examples: ExamplesState =
    !noIndex && (status.kind === "loading" || status.kind === "checking" || request.state.kind === "loading")
      ? { kind: "loading" }
      : request.state.kind === "ok"
        ? { kind: "ok", examples: request.state.data }
        : run === null
          ? { kind: "none" }
          : { kind: "failed" };

  useEffect(() => {
    if (at === null) return;
    const el = document.getElementById(headingId(at));
    if (el === null) return;
    if (typeof el.scrollIntoView === "function") el.scrollIntoView({ block: "start" });
    el.focus({ preventScroll: true });
  }, [at]);

  const titles = guide.map((p) => p.blocks[0]).filter((b): b is Extract<Block, { t: "heading" }> => b?.t === "heading");
  return (
    <div className="mx-auto grid w-full max-w-[760px] min-w-0 gap-5 pb-10 text-[14px]" data-testid="help-section">
      <header className="grid gap-2">
        <h1 className="m-0 text-[22px] font-semibold tracking-tight">Help</h1>
        <nav aria-label="On this page">
          <ul className="flex flex-wrap gap-x-4 gap-y-1 text-[13.5px]">
            {titles.map((t) => (
              <li key={t.slug}>
                <a href={helpHash(t.slug)} className={LINK}>
                  {plainText(t.c)}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      </header>
      {guide.map((p) => (
        <PageView key={p.file} page={p} at={at} examples={examples} />
      ))}
    </div>
  );
}
