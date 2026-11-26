import { describe, expect, it } from "vitest";

import type { PresentedNode } from "@/api/endpoints";
import { conditionalText } from "@/sections/api/parts";

function node(kind: string, attributes: Record<string, unknown>): PresentedNode {
  return { kind, attributes } as unknown as PresentedNode;
}

describe("conditionalText", () => {
  it("words a conditional server action use as a runtime choice (T024b R1)", () => {
    const n = node("action_call", { conditional: true, alternatives: ["server_action:a#ACTION%20x", "server_action:a#ACTION%20y"] });
    expect(conditionalText(n)).toBe("may submit to, depending on a runtime condition");
    expect(conditionalText(node("action_call", {}))).toBeNull();
  });
});
