// A small "Copy" button for values a user compares or pastes elsewhere (a
// certificate fingerprint, a body hash). The value stays selectable on the
// page, so a clipboard that is not available (an insecure context) only
// changes the button's words.

import { Check, ClipboardCopy } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";

/** CopyButton copies text and says whether it worked. what names the
 * value for screen readers ("Copy the SHA-256"). */
export function CopyButton({ text, what, testId }: { text: string; what: string; testId?: string }) {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  const copy = () => {
    try {
      navigator.clipboard.writeText(text).then(
        () => {
          setState("done");
        },
        () => {
          setState("failed");
        },
      );
    } catch {
      // No clipboard API at all (an insecure context or a test DOM).
      setState("failed");
    }
  };
  return (
    <Button size="xs" variant="ghost" onClick={copy} aria-label={`Copy ${what}`} data-testid={testId} data-state={state}>
      {state === "done" ? <Check aria-hidden="true" /> : <ClipboardCopy aria-hidden="true" />}
      {state === "done" ? "Copied" : state === "failed" ? "Select and copy" : "Copy"}
    </Button>
  );
}
