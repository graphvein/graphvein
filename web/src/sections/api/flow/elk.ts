// The ELK layout worker of the Flow tab (T016f D5, ADR 0046).
//
// elkjs's worker ships unmodified as its own same-origin file under
// /assets/ (Vite copies it for the ?url import; the CSP allows a classic
// same-origin worker and blocks blob: workers, ADR 0041). This module talks
// to it with the worker's message protocol ({id, cmd, ...} in, {id, data}
// or {id, error} out) instead of bundling elkjs's elk-api.js, so no ELK
// code is in Graphvein's own chunks: the EPL-2.0 code stays in its own
// unmodified file. The protocol is the one of the pinned elkjs 0.12.0
// (lib/elk-api.js PromisedWorker); a version change is re-reviewed (ADR
// 0046), and the worker test runs this client against the real worker code.
//
// One worker per page, created on the first layout and reused. A layout
// that takes longer than the timeout rejects and terminates the worker, so
// a retry starts a fresh one.

import type { ElkNode } from "elkjs/lib/elk-api";
import workerUrl from "elkjs/lib/elk-worker.min.js?url";

/** LAYOUT_TIMEOUT_MS is how long one layout may take (D5: 10 s). */
export const LAYOUT_TIMEOUT_MS = 10_000;

/** The part of a Worker this client uses, so tests can pass elkjs's
 * in-process worker. */
export interface WorkerLike {
  postMessage(message: unknown): void;
  onmessage: ((event: { data: unknown }) => void) | null;
  terminate(): void;
}

type Pending = { resolve: (v: ElkNode) => void; reject: (e: Error) => void; timer: ReturnType<typeof setTimeout> };

/** ElkTimeoutError reports a layout that did not finish in time. */
export class ElkTimeoutError extends Error {
  constructor(ms: number) {
    super(`the layout did not finish within ${String(ms / 1000)} s`);
    this.name = "ElkTimeoutError";
  }
}

function errorText(err: unknown): string {
  if (typeof err === "object" && err !== null && "message" in err && typeof err.message === "string") return err.message;
  return typeof err === "string" ? err : "layout failed";
}

/** ElkClient runs layouts in one worker. */
export class ElkClient {
  private worker: WorkerLike | null = null;
  private next = 0;
  private readonly pending = new Map<number, Pending>();
  private readonly factory: () => WorkerLike;
  private readonly timeoutMs: number;

  constructor(factory: () => WorkerLike, timeoutMs = LAYOUT_TIMEOUT_MS) {
    this.factory = factory;
    this.timeoutMs = timeoutMs;
  }

  private start(): WorkerLike {
    if (this.worker !== null) return this.worker;
    const w = this.factory();
    w.onmessage = (event) => {
      this.receive(event.data);
    };
    this.worker = w;
    // The worker registers the algorithms it may run before the first
    // layout; messages are handled in order, so no wait is needed.
    w.postMessage({ id: this.next++, cmd: "register", algorithms: ["layered"] });
    return w;
  }

  private receive(data: unknown): void {
    if (typeof data !== "object" || data === null || !("id" in data) || typeof data.id !== "number") return;
    const p = this.pending.get(data.id);
    if (p === undefined) return;
    this.pending.delete(data.id);
    clearTimeout(p.timer);
    if ("error" in data && data.error !== undefined && data.error !== null) {
      p.reject(new Error(errorText(data.error)));
      return;
    }
    if (!("data" in data) || typeof data.data !== "object" || data.data === null) {
      p.reject(new Error("the layout worker answered without a graph"));
      return;
    }
    p.resolve(data.data as ElkNode);
  }

  /** layout runs ELK on graph; the options are the graph's own. */
  layout(graph: ElkNode): Promise<ElkNode> {
    const w = this.start();
    const id = this.next++;
    return new Promise<ElkNode>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        this.terminate();
        reject(new ElkTimeoutError(this.timeoutMs));
      }, this.timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
      w.postMessage({ id, cmd: "layout", graph, layoutOptions: {}, options: { logging: false, measureExecutionTime: false } });
    });
  }

  /** terminate stops the worker and fails what is pending; the next
   * layout starts a new one. */
  terminate(): void {
    const w = this.worker;
    this.worker = null;
    w?.terminate();
    for (const [id, p] of this.pending) {
      clearTimeout(p.timer);
      p.reject(new Error("the layout worker was stopped"));
      this.pending.delete(id);
    }
  }
}

let shared: ElkClient | null = null;

/** pageElk is the page's one layout client; its worker starts on the
 * first layout. */
export function pageElk(): ElkClient {
  shared ??= new ElkClient(() => new Worker(workerUrl) as unknown as WorkerLike);
  return shared;
}
