// The notice that a run's counts are incomplete because some files could
// not be parsed (T074): GET /api/v0/status index.parse_failed and GET
// /api/v0/architecture parse_failed count them. The Overview and the
// Couldn't read page read the status; the Architecture header reads its own
// answer. The files themselves are listed on the Couldn't read page under
// "Files not read".

import { cn } from "@/lib/utils";

/** parseFailedText is the notice's sentence for count files not parsed. */
export function parseFailedText(count: number): string {
  const files = count === 1 ? "1 file" : `${count.toLocaleString("en-US")} files`;
  return `Counts are incomplete: ${files} could not be parsed (parse_failed), so ${count === 1 ? "its" : "their"} facts are missing.`;
}

/** ParseFailedNotice renders the notice, or nothing when count is 0.
 * onPage is true on the Couldn't read page, which lists the files itself. */
export function ParseFailedNotice({ count, onPage = false, className }: { count: number; onPage?: boolean; className?: string }) {
  if (count <= 0) return null;
  return (
    <div role="status" className={cn("text-[13px] text-dynamic", className)} data-testid="parse-failed-notice">
      {parseFailedText(count)}{" "}
      {onPage ? (
        "They are listed under Files not read."
      ) : (
        <a href="#/unread" className="underline underline-offset-2">
          See Couldn&apos;t read
        </a>
      )}
    </div>
  );
}
