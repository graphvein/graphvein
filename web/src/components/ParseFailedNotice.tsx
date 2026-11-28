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

/** MemberParseFailed is one project member's count of files not parsed. */
export interface MemberParseFailed {
  repository: string;
  count: number;
}

/** projectParseFailedText is the notice's sentence for the project-wide
 * Couldn't read view (T078): it names each member with files not parsed,
 * in the given order; "" when none has any. */
export function projectParseFailedText(members: MemberParseFailed[]): string {
  const failed = members.filter((m) => m.count > 0);
  if (failed.length === 0) return "";
  const total = failed.reduce((n, m) => n + m.count, 0);
  const parts = failed.map((m) => `${m.count === 1 ? "1 file" : `${m.count.toLocaleString("en-US")} files`} of ${m.repository}`);
  const last = parts.pop() ?? "";
  const named = parts.length === 0 ? last : `${parts.join(", ")} and ${last}`;
  return `Counts are incomplete: ${named} could not be parsed (parse_failed), so ${total === 1 ? "its" : "their"} facts are missing.`;
}

/** ProjectParseFailedNotice is ParseFailedNotice for the project-wide
 * Couldn't read view: nothing when no member has a file not parsed. */
export function ProjectParseFailedNotice({ members, className }: { members: MemberParseFailed[]; className?: string }) {
  const text = projectParseFailedText(members);
  if (text === "") return null;
  return (
    <div role="status" className={cn("text-[13px] text-dynamic", className)} data-testid="parse-failed-notice">
      {text} They are listed under Files not read.
    </div>
  );
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
