// The "Report a wrong fact" flag (T040, ADR 0089). Rendered only while
// the public-repository switch is on (GET /api/v0/ui report_url); placed
// on the endpoint detail header, caller rows and schema column rows, never
// on list rows and never inside a tooltip. The icon is muted and shown on
// hover of its row (group/fact) or on keyboard focus; a label beside it
// appears on hover and focus. A click opens a preview of the prefilled
// issue (variant B): the text carries paths and names from the user's
// possibly private code to a public site, so it is shown before leaving.

import { Popover } from "radix-ui";
import { Copy, ExternalLink, Flag } from "lucide-react";
import { useId, useState } from "react";

import { copyText } from "@/lib/clipboard";
import { cn } from "@/lib/utils";
import { issueLink, reportHost, shownText, useReport, type WrongFact } from "@/shell/report";

/** FLAG_LABEL is the flag's accessible name and hover label. */
export const FLAG_LABEL = "Report a wrong fact";

/** FLAG_WIDTH_PX is the width a fixed table column adds for the flag. */
export const FLAG_WIDTH_PX = 26;

/** WrongFactFlag renders the flag for fact, or nothing while the switch is
 * off. The row it sits in carries the Tailwind group "group/fact" so the
 * flag shows on hover of the row; hover and focus change opacity only, so
 * no box moves. */
export function WrongFactFlag({ fact, className }: { fact: () => WrongFact; className?: string }) {
  const report = useReport();
  const [open, setOpen] = useState(false);
  const titleId = useId();
  const [copied, setCopied] = useState<"no" | "yes" | "failed">("no");
  if (report.url === null) return null;
  const base = report.url;
  const f = open ? fact() : null;
  const link = f !== null ? issueLink(base, f, report.version) : null;
  const github = reportHost(base).split("/")[0] === "github.com";
  const site = github ? "GitHub" : reportHost(base);
  const openLabel = github ? "Open on GitHub" : "Open on the issue form";
  return (
    <Popover.Root
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        setCopied("no");
      }}
    >
      <span className={cn("relative inline-flex", className)} data-testid="wrong-fact-flag">
        <Popover.Trigger asChild>
          <button
            type="button"
            aria-label={FLAG_LABEL}
            className={cn(
              "peer grid size-6 flex-none place-items-center rounded-md text-muted-foreground opacity-0 transition-opacity outline-none",
              "group-hover/fact:opacity-100 hover:bg-accent hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring",
              open && "opacity-100",
            )}
            onClick={(e) => {
              // A caller row opens its peek on click: the flag does not.
              e.stopPropagation();
            }}
            onKeyDown={(e) => {
              // Enter and Space activate the flag, not the row's peek.
              if (e.key === "Enter" || e.key === " ") e.stopPropagation();
            }}
          >
            <Flag aria-hidden="true" className="size-3.5" />
          </button>
        </Popover.Trigger>
        {/* The label is the flag's own, not a tooltip: it sits beside the
            icon, takes no layout space and shows on hover and focus. */}
        <span
          aria-hidden="true"
          className="pointer-events-none absolute top-1/2 right-full mr-1 -translate-y-1/2 rounded-[4px] border bg-popover px-1.5 py-0.5 text-[11.5px] whitespace-nowrap text-popover-foreground opacity-0 shadow-sm transition-opacity peer-hover:opacity-100 peer-focus-visible:opacity-100"
        >
          {FLAG_LABEL}
        </span>
      </span>
      <Popover.Portal>
        <Popover.Content
          role="dialog"
          aria-labelledby={titleId}
          side="bottom"
          align="end"
          sideOffset={6}
          collisionPadding={8}
          data-testid="wrong-fact-preview"
          className="z-50 grid w-[min(460px,calc(100vw-16px))] gap-2.5 rounded-[calc(var(--radius)+2px)] border bg-popover p-3.5 text-[13px] leading-snug text-popover-foreground shadow-[0_10px_30px_rgba(0,0,0,.16),0_2px_6px_rgba(0,0,0,.08)] outline-none"
          onClick={(e) => {
            e.stopPropagation();
          }}
          onKeyDown={(e) => {
            e.stopPropagation();
          }}
        >
          {f !== null && link !== null && (
            <>
              <h3 id={titleId} className="m-0 text-[14px] font-semibold">
                Report a wrong fact on {site}
              </h3>
              <p className="m-0 text-muted-foreground">
                The text below travels to {site} in the link when you press {openLabel}. To edit or remove anything private first, use Copy text instead and paste your edited text into a new issue.
              </p>
              <pre className="m-0 max-h-[300px] overflow-auto rounded-md border bg-muted/50 px-2.5 py-2 font-mono text-[12px] leading-[1.5] whitespace-pre-wrap [overflow-wrap:anywhere]" data-testid="wrong-fact-text">
                {shownText(f, report.version)}
              </pre>
              {link.shortened && (
                <p className="m-0 text-[12.5px] text-muted-foreground" data-testid="wrong-fact-shortened">
                  Too long for the link: the form gets a shortened text. Copy text has all of it.
                </p>
              )}
              <div className="flex flex-wrap items-center gap-2">
                <a
                  href={link.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex h-8 items-center gap-1.5 rounded-md bg-brand px-3 text-[13px] font-medium text-brand-foreground hover:opacity-90"
                  onClick={() => {
                    setOpen(false);
                  }}
                >
                  <ExternalLink aria-hidden="true" className="size-3.5" />
                  {openLabel}
                </a>
                <button
                  type="button"
                  className="inline-flex h-8 items-center gap-1.5 rounded-md border px-3 text-[13px] font-medium hover:bg-accent"
                  onClick={() => {
                    void copyText(shownText(f, report.version)).then((ok) => {
                      setCopied(ok ? "yes" : "failed");
                    });
                  }}
                >
                  <Copy aria-hidden="true" className="size-3.5" />
                  Copy text
                </button>
                <span role="status" className="text-[12.5px] text-muted-foreground">
                  {copied === "yes" ? "Copied" : copied === "failed" ? "Could not copy: select the text above" : ""}
                </span>
              </div>
              <div className="text-[12px] text-muted-foreground">{reportHost(base)}</div>
            </>
          )}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
