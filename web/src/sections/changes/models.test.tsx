// T029 W2 (rows MT-14, MT-15, MT-19 of the route model types contract): an
// impact path that reaches a route through a changed model (relation I16)
// shows its wording and one provenance chip per use, from the golden the
// server computes.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ChangeItem } from "@/api/changes";
import { ImpactBody, modelRole } from "@/sections/changes/ImpactTab";
import { impactOf, itemsOf } from "@/test/changesFixtures";

function seedItem(identity: string): ChangeItem {
  const item = itemsOf("models", { base: "previous", group: "code" }).items.find((i) => i.identity === identity);
  if (item === undefined) throw new Error(`no item ${identity}`);
  return item;
}

describe("impact through request and response models (T029 W2)", () => {
  it("parses the I16 step's model and uses", () => {
    const data = impactOf("models", "class:py/app/models.py#UserBase");
    const post = data?.paths.find((p) => p.node === "route:py/app/main.py#POST%20%2Fusers");
    expect(post?.relation).toBe("I16");
    expect(post?.wording).toBe("through request body (fact) and response (fact) model UserBase");
    const model = post?.steps[0]?.model;
    expect(model?.name).toBe("UserBase");
    expect(model?.uses.map((u) => `${modelRole(u.section, u.status)} ${u.kind}`)).toEqual(["request body fact", "response 200 fact"]);
  });

  it("shows the model line with a provenance chip per use, returned uses labelled as such", () => {
    const id = "interface:web/src/types.ts#Pet";
    const data = impactOf("models", id);
    if (data === null) throw new Error("golden impact missing");
    render(<ImpactBody data={data} paths={data.paths} more={null} item={seedItem(id)} />);
    const line = screen.getByTestId("step-model");
    expect(line.textContent).toContain("model Pet:");
    expect(line.textContent).toContain("response 200");
    const chips = screen.getAllByTestId("provenance-chip");
    expect(chips.map((c) => c.getAttribute("data-kind"))).toEqual(["returned"]);
    expect(screen.getAllByText("through response (returned) model Pet").length).toBeGreaterThan(0);
  });
});
