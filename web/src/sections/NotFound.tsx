// Shown for a hash that names no available section.

import { DEFAULT_HASH } from "@/router";

/** NotFound is shown for a hash that names no section, with a way back. */
export function NotFound({ hash }: { hash: string }) {
  return (
    <section aria-labelledby="nf-title" className="grid gap-3.5">
      <h1 id="nf-title" className="m-0 text-[22px] font-semibold tracking-tight">
        Page not found
      </h1>
      <div className="grid justify-items-center gap-2 rounded-xl border bg-card px-6 py-10 text-center text-sm text-muted-foreground shadow-[0_1px_2px_rgba(0,0,0,.04)]">
        <p className="m-0">
          There is no page at <span className="font-mono text-foreground">{hash}</span>.
        </p>
        <a href={DEFAULT_HASH} className="font-medium text-brand-text underline-offset-4 hover:underline">
          Go to the API section
        </a>
      </div>
    </section>
  );
}
