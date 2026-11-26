// The banner above the content when the last status request failed. The
// text names the kind of failure, so an error answer from a running server
// is not reported as an unreachable one.

import { CircleAlert, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { ApiError } from "@/api/client";

/** bannerText explains a failed status request to the user. */
export function bannerText(error: Error, origin: string): string {
  if (error instanceof ApiError && error.kind === "http") {
    return `The Graphvein server at ${origin} answered with an error (${error.message}).`;
  }
  if (error instanceof ApiError && error.kind === "format") {
    return `The Graphvein server at ${origin} sent a status this UI cannot read (${error.message}).`;
  }
  return `Cannot reach the Graphvein server at ${origin}. Is graphvein serve still running?`;
}

interface ServerBannerProps {
  error: Error;
  checking: boolean;
  onRetry: () => void;
}

/** ServerBanner explains a failed status request and offers Retry. */
export function ServerBanner({ error, checking, onRetry }: ServerBannerProps) {
  return (
    <div role="alert" className="flex items-center gap-3 rounded-lg border border-stale/55 bg-stale/10 px-3 py-2 text-[13px]">
      <CircleAlert aria-hidden="true" className="size-4 flex-none" />
      <p className="min-w-0 flex-1">{bannerText(error, window.location.origin)}</p>
      <Button variant="outline" size="sm" onClick={onRetry} disabled={checking}>
        <RefreshCw aria-hidden="true" className={checking ? "animate-spin" : undefined} />
        Retry
      </Button>
    </div>
  );
}
