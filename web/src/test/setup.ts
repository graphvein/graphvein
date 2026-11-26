// Shared test setup: unmount rendered trees between tests and provide the
// browser APIs jsdom lacks but Radix uses when a tooltip opens.
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

class ResizeObserverStub {
  observe(): void {
    // jsdom does no layout, so there is nothing to observe.
  }
  unobserve(): void {
    // See observe.
  }
  disconnect(): void {
    // See observe.
  }
}

if (!("ResizeObserver" in globalThis)) {
  Object.defineProperty(globalThis, "ResizeObserver", { value: ResizeObserverStub, configurable: true });
}

afterEach(() => {
  cleanup();
});

// jsdom has no HTMLDialogElement methods. This stand-in does what the gap
// sheet relies on: showModal/close toggle `open`, close fires "close", and
// Escape inside an open modal dialog closes it as browsers do.
const dialogProto = HTMLDialogElement.prototype as HTMLDialogElement & { __stub?: boolean };
if (typeof (dialogProto as Partial<HTMLDialogElement>).showModal !== "function") {
  Object.defineProperties(dialogProto, {
    showModal: {
      configurable: true,
      value(this: HTMLDialogElement) {
        this.setAttribute("open", "");
      },
    },
    close: {
      configurable: true,
      value(this: HTMLDialogElement) {
        if (!this.hasAttribute("open")) return;
        this.removeAttribute("open");
        this.dispatchEvent(new Event("close"));
      },
    },
  });
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    const open = document.querySelector("dialog[open]");
    if (open instanceof HTMLDialogElement) {
      open.dispatchEvent(new Event("cancel"));
      open.close();
    }
  });
}
