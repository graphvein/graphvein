// A modal dialog for try-it's decisions (production, missing auth, an
// untrusted certificate, edits with their preview). It is a native
// <dialog> opened with showModal(): top layer, inert background, Esc
// closes it. Radix Dialog's scroll lock injects <style> elements the
// page's CSP blocks (as the API section's gap sheet notes).

import { X } from "lucide-react";
import { useEffect, useEffectEvent, useRef, type ReactNode } from "react";

import { cn } from "@/lib/utils";

/** Modal renders an open dialog; mounting opens it, unmounting closes it
 * and returns focus to the element that had it. tone colours the frame:
 * "danger" for production and certificate warnings. */
export function Modal({ title, onClose, children, tone, wide, testId }: { title: ReactNode; onClose: () => void; children: ReactNode; tone?: "danger" | "warn"; wide?: boolean; testId?: string }) {
  const ref = useRef<HTMLDialogElement>(null);
  const closed = useEffectEvent(onClose);
  useEffect(() => {
    const dialog = ref.current;
    if (dialog === null) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    if (typeof dialog.showModal === "function") {
      if (!dialog.open) dialog.showModal();
    } else {
      dialog.setAttribute("open", "");
    }
    // Focus the safe choice: the first element marked autofocus-safe
    // (Cancel), else the dialog itself.
    dialog.querySelector<HTMLElement>("[data-autofocus]")?.focus();
    const onCancel = () => {
      closed();
    };
    dialog.addEventListener("close", onCancel);
    return () => {
      dialog.removeEventListener("close", onCancel);
      if (dialog.open && typeof dialog.close === "function") dialog.close();
      opener?.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      aria-label={typeof title === "string" ? title : undefined}
      data-testid={testId}
      className={cn(
        "fixed inset-0 m-auto max-h-[calc(100vh-48px)] w-[min(560px,calc(100vw-32px))] overflow-auto rounded-lg border bg-popover p-0 text-[13px] text-popover-foreground shadow-[0_10px_30px_rgba(0,0,0,.2)] backdrop:bg-[hsl(var(--overlay))]",
        wide === true && "w-[min(760px,calc(100vw-32px))]",
        tone === "danger" && "border-unmatched/60",
        tone === "warn" && "border-dynamic/60",
      )}
    >
      <div className={cn("flex items-start gap-2 border-b px-4 py-3", tone === "danger" && "bg-unmatched-soft", tone === "warn" && "bg-dynamic-soft")}>
        <h2 className="min-w-0 flex-1 text-[15px] font-semibold">{title}</h2>
        <button
          type="button"
          aria-label="Close"
          onClick={() => {
            const d = ref.current;
            if (d !== null && d.open && typeof d.close === "function") d.close();
            else onClose();
          }}
          className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <X aria-hidden="true" className="size-4" />
        </button>
      </div>
      <div className="grid gap-3 px-4 py-3">{children}</div>
    </dialog>
  );
}

/** DiffBlock shows a unified diff of a file edit, added lines green and
 * removed lines red. */
export function DiffBlock({ diff }: { diff: string }) {
  return (
    <pre className="max-h-[260px] overflow-auto rounded-md border bg-code-bg px-2.5 py-1.5 font-mono text-[11.5px] leading-[1.5]" data-testid="edit-diff">
      {diff.split("\n").map((l, i) => (
        <div key={i} className={cn(l.startsWith("+") && !l.startsWith("+++") && "bg-linked-soft text-linked", l.startsWith("-") && !l.startsWith("---") && "bg-unmatched-soft text-unmatched")}>
          {l === "" ? " " : l}
        </div>
      ))}
    </pre>
  );
}
