// The worker client against elkjs's real worker code (its in-process
// build, which runs the same dispatcher as the worker file), and the
// timeout.
import { describe, expect, it, vi } from "vitest";

import { buildGraph } from "@/sections/api/flow/graph";
import { ElkClient, ElkTimeoutError, type WorkerLike } from "@/sections/api/flow/elk";
import { elkGraph, readLayout } from "@/sections/api/flow/layout";
import { FILTER_TIERS } from "@/sections/api/flow/tiers";
import { flowGolden } from "@/test/flowFixtures";

// elkjs/lib/elk-worker.js exports its in-process worker when loaded as a
// module; its typings declare no value, hence the cast.
const workerModule = (await import("elkjs/lib/elk-worker.js")) as unknown as { default: { Worker: new () => Omit<WorkerLike, "terminate"> } };
const FakeWorker = workerModule.default.Worker;

function realWorker(): WorkerLike {
  const w = new FakeWorker() as WorkerLike;
  w.terminate = () => undefined;
  return w;
}

describe("ElkClient", () => {
  it("lays out a golden through the worker protocol, starting one worker for many layouts", async () => {
    const factory = vi.fn(realWorker);
    const client = new ElkClient(factory);
    const g = buildGraph(flowGolden("flow-graph"), { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    const l = readLayout(g, await client.layout(elkGraph(g)));
    expect(l.boxes.size).toBe(g.nodes.length);
    const g1 = buildGraph(flowGolden("flow-command"), { hops: 1, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    expect(readLayout(g1, await client.layout(elkGraph(g1))).routes.size).toBe(g1.edges.length);
    expect(factory).toHaveBeenCalledTimes(1);
  });

  it("rejects ELK's error", async () => {
    const client = new ElkClient(realWorker);
    await expect(client.layout({ id: "root", children: [{ id: "a" }], edges: [{ id: "e", sources: ["missing"], targets: ["a"] }] })).rejects.toThrow();
  });

  it("times out, stops the worker and starts a new one on the next layout", async () => {
    vi.useFakeTimers();
    const terminate = vi.fn();
    const silent = (): WorkerLike => ({ postMessage: () => undefined, onmessage: null, terminate });
    const factory = vi.fn(silent);
    const client = new ElkClient(factory, 10_000);
    const p = client.layout({ id: "root" });
    const assertion = expect(p).rejects.toBeInstanceOf(ElkTimeoutError);
    vi.advanceTimersByTime(10_001);
    await assertion;
    expect(terminate).toHaveBeenCalledTimes(1);
    void client.layout({ id: "root" }).catch(() => undefined);
    expect(factory).toHaveBeenCalledTimes(2);
    client.terminate();
    vi.useRealTimers();
  });
});
