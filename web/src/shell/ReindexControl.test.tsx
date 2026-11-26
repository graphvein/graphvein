import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { StatusView } from "@/hooks/useStatus";
import { formatShortTime } from "@/lib/time";
import { ReindexControl } from "@/shell/ReindexControl";
import { deriveReindex, type ReindexInputs, type RunView } from "@/shell/reindexModel";
import { goldenStatus } from "@/test/fixtures";
import { runDoc } from "@/test/indexRunFixtures";

const stale: StatusView = { kind: "ok", status: goldenStatus("stale") };

function run(lock: string, job: Parameters<typeof runDoc>[1] = null, error: string | null = null): RunView {
  return { kind: "ok", doc: runDoc(lock, job, error) };
}

function setup(over: Partial<ReindexInputs>) {
  const input: ReindexInputs = {
    status: stale,
    run: run("free"),
    pending: false,
    postError: null,
    startedJobId: null,
    dismissedJobId: null,
    ...over,
  };
  const onStart = vi.fn();
  const onDismiss = vi.fn();
  const utils = render(
    <ReindexControl model={deriveReindex(input)} status={input.status} onStart={onStart} onDismiss={onDismiss} />,
  );
  return { ...utils, onStart, onDismiss, slot: screen.getByTestId("reindex-slot") };
}

describe("ReindexControl idle", () => {
  it("starts a run on click; it is not a disclosure", () => {
    const { slot, onStart } = setup({});
    expect(slot.textContent).toBe("Re-index");
    expect(slot.getAttribute("aria-expanded")).toBeNull();
    expect(slot.className).toContain("bg-brand"); // D5: primary while stale
    fireEvent.click(slot);
    expect(onStart).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("reindex-details")).toBeNull();
  });

  it("is an outline button while the index is fresh", () => {
    const { slot } = setup({ status: { kind: "ok", status: goldenStatus("fresh") } });
    expect(slot.className).not.toContain("bg-brand ");
    expect(slot.className).toContain("bg-background");
  });

  it("draws the sizer labels with CSS only, so the slot text is the label", () => {
    const { slot } = setup({});
    expect(within(slot).getAllByTestId("slot-sizer").length).toBeGreaterThan(3);
    expect(slot.textContent).toBe("Re-index");
  });
});

describe("ReindexControl pending", () => {
  it("is aria-disabled, focusable and does nothing on click", () => {
    const { slot, onStart } = setup({ pending: true });
    expect(slot.textContent).toBe("Starting...");
    expect(slot.getAttribute("aria-disabled")).toBe("true");
    expect(slot.hasAttribute("disabled")).toBe(false);
    fireEvent.click(slot);
    expect(onStart).not.toHaveBeenCalled();
  });
});

describe("ReindexControl checking", () => {
  it("is aria-disabled, never idle, while a free lock sits next to a running job", () => {
    const { slot, onStart } = setup({ run: run("free", { phase: "finishing" }) });
    expect(slot.textContent).toBe("Checking...");
    expect(slot.getAttribute("aria-disabled")).toBe("true");
    expect(slot.getAttribute("aria-expanded")).toBeNull();
    fireEvent.click(slot);
    expect(onStart).not.toHaveBeenCalled();
  });
});

describe("ReindexControl running", () => {
  it("shows the counts with a determinate bar and opens the phase list", () => {
    const { slot, onStart } = setup({ run: run("held_here", { phase: "parsing", progress: { done: 212, total: 457, unit: "files" } }) });
    expect(slot.textContent).toBe("Indexing 212 / 457");
    expect(slot.getAttribute("aria-label")).toBe("Indexing, 212 of 457 files, show details");
    expect(screen.getByTestId("reindex-bar").getAttribute("data-determinate")).toBe("true");
    expect(slot.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(slot);
    expect(onStart).not.toHaveBeenCalled();
    expect(slot.getAttribute("aria-expanded")).toBe("true");
    const details = screen.getByTestId("reindex-details");
    expect(slot.getAttribute("aria-controls")).toBe(details.id);
    const phases = within(details).getByTestId("reindex-phases");
    const states = Array.from(phases.querySelectorAll("li")).map((li) => `${li.dataset.phase ?? ""}:${li.dataset.state ?? ""}`);
    expect(states).toEqual(["scanning:done", "cache:done", "parsing:now", "linking:todo", "writing:todo", "committing:todo"]);
    expect(phases.textContent).toContain("212 / 457 files");
    expect(details.textContent).toContain(`The views show the index of ${formatShortTime("2026-11-03T14:32:05.123Z")} (Stale, 3 files changed) until the new run commits.`);
    expect(details.textContent).toContain("Ctrl+C");
  });

  it("shows an indeterminate bar and no fraction without progress", () => {
    const { slot } = setup({ run: run("held_here", { phase: "linking", progress: null }) });
    expect(slot.textContent).toBe("Indexing");
    expect(slot.textContent).not.toMatch(/%|\//);
    expect(screen.getByTestId("reindex-bar").getAttribute("data-determinate")).toBe("false");
  });

  it("shows an unknown phase as the raw word", () => {
    const { slot } = setup({ run: run("held_here", { phase: "warming", progress: null }) });
    fireEvent.click(slot);
    expect(screen.getByTestId("reindex-raw-phase").textContent).toBe("Current phase: warming");
  });

  it("closes with Escape and returns focus to the slot", async () => {
    const { slot } = setup({ run: run("held_here") });
    slot.focus();
    fireEvent.keyDown(slot, { key: "Enter" });
    fireEvent.click(slot); // a button's Enter activates click
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    // Opening moves focus into the details (the first control).
    expect(screen.getByTestId("reindex-details").contains(document.activeElement)).toBe(true);
    const details = screen.getByTestId("reindex-details");
    fireEvent.keyDown(details, { key: "Escape" });
    expect(screen.queryByTestId("reindex-details")).toBeNull();
    expect(slot.getAttribute("aria-expanded")).toBe("false");
    // Radix moves focus after the content unmounted, in a timeout.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(document.activeElement).toBe(slot);
  });

  it("closes with the close button and a second click on the slot", () => {
    const { slot } = setup({ run: run("held_here") });
    fireEvent.click(slot);
    fireEvent.click(screen.getByRole("button", { name: "Close details" }));
    expect(screen.queryByTestId("reindex-details")).toBeNull();
    fireEvent.click(slot);
    expect(screen.getByTestId("reindex-details")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByTestId("reindex-details")).toBeNull();
  });
});

describe("ReindexControl other states", () => {
  it("explains a lock held elsewhere", () => {
    const { slot } = setup({ run: run("held_elsewhere") });
    expect(slot.textContent).toBe("Indexing (CLI)");
    expect(screen.getByTestId("reindex-bar").getAttribute("data-determinate")).toBe("false");
    fireEvent.click(slot);
    expect(screen.getByTestId("reindex-details").textContent).toContain("Another process holds the index lock for this store");
  });

  it("shows a failed job with code, message, the shown index, Dismiss and Retry", () => {
    const { slot, onStart, onDismiss } = setup({
      run: run("free", { state: "failed", phase: null, error: { code: "index_failed", message: "no space left on device" } }),
    });
    expect(slot.textContent).toBe("Re-index failed · details");
    fireEvent.click(slot);
    const details = screen.getByTestId("reindex-details");
    expect(within(details).getByTestId("reindex-error").textContent).toBe("index_failed: no space left on device");
    expect(details.textContent).toContain(`The views still show the index of ${formatShortTime("2026-11-03T14:32:05.123Z")}`);
    fireEvent.click(within(details).getByRole("button", { name: /Retry/ }));
    expect(onStart).toHaveBeenCalledTimes(1);
    fireEvent.click(within(details).getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("blocks Retry with the reason while the lock is not free", () => {
    const { slot, onStart } = setup({ run: run("unknown", { state: "failed", phase: null }, "read-only directory") });
    fireEvent.click(slot);
    const retry = screen.getByRole("button", { name: /Retry/ });
    expect(retry.getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(retry);
    expect(onStart).not.toHaveBeenCalled();
    expect(screen.getByTestId("reindex-details").textContent).toContain("read-only directory");
  });

  it("explains an unavailable re-index from the slot itself", () => {
    const { slot, onStart } = setup({ run: run("unknown", null, "lock directory is read-only") });
    expect(slot.textContent).toBe("Re-index");
    expect(slot.getAttribute("title")).toBe("The server could not check the index lock: lock directory is read-only");
    expect(slot.getAttribute("aria-label")).toBe("Re-index unavailable, show why");
    fireEvent.click(slot);
    expect(onStart).not.toHaveBeenCalled();
    expect(screen.getByTestId("reindex-unavailable-reason").textContent).toContain("lock directory is read-only");
  });

  it("shows a POST refusal, a cancelled job and an unknown outcome", () => {
    let s = setup({ postError: { code: "reindex_unsupported", message: "external snapshot" } });
    fireEvent.click(s.slot);
    expect(screen.getByTestId("reindex-error").textContent).toBe("reindex_unsupported: external snapshot");
    s.unmount();
    s = setup({ run: run("free", { state: "cancelled", phase: null }) });
    expect(s.slot.textContent).toBe("Re-index cancelled · details");
    s.unmount();
    s = setup({ startedJobId: "J0" });
    fireEvent.click(s.slot);
    expect(screen.getByTestId("reindex-details").textContent).toContain("graphvein serve restarted");
  });

  it("announces the phase in a live region", () => {
    setup({ run: run("held_here", { phase: "parsing", progress: { done: 1, total: 2, unit: "files" } }) });
    expect(screen.getByTestId("reindex-live").textContent).toBe("Indexing: Parsing");
  });

  it("does not reopen the details by itself when the next run starts", () => {
    const onStart = vi.fn();
    const props = (r: RunView) => ({
      model: deriveReindex({ status: stale, run: r, pending: false, postError: null, startedJobId: null, dismissedJobId: null }),
      status: stale,
      onStart,
      onDismiss: vi.fn(),
    });
    const { rerender } = render(<ReindexControl {...props(run("held_here"))} />);
    fireEvent.click(screen.getByTestId("reindex-slot"));
    expect(screen.getByTestId("reindex-details")).toBeTruthy();
    act(() => {
      rerender(<ReindexControl {...props(run("free"))} />);
    });
    expect(screen.queryByTestId("reindex-details")).toBeNull();
    act(() => {
      rerender(<ReindexControl {...props(run("held_here"))} />);
    });
    expect(screen.queryByTestId("reindex-details")).toBeNull();
  });
});
