// T024c3: a table read from Supabase's generated TypeScript types is
// labelled "generated types", not an ORM model or a class: the source chip,
// the detail header, the declarations wording, the tool type label and the
// group's scope name say so.
import { describe, expect, it } from "vitest";

import { toolTypeLabel } from "@/sections/schema/cells";
import { groupScopeName } from "@/sections/schema/precision";
import { declaredIn, definedBy, sourceChipText } from "@/sections/schema/TableDetail";

describe("a table from Supabase's generated types", () => {
  it("is labelled generated types", () => {
    expect(sourceChipText(true, "supabase_types")).toBe("generated types · supabase");
    expect(sourceChipText(true, "prisma")).toBe("model · prisma");
    expect(sourceChipText(false, "supabase")).toBe("migration · supabase");
    expect(sourceChipText(false, null)).toBe("migration");
  });

  it("names the Tables entry and its TypeScript type", () => {
    expect(definedBy("supabase_types", {}, "public.todos")).toEqual({ label: "generated type", name: "public.todos" });
    expect(declaredIn("supabase_types")).toBe("the generated types");
    expect(toolTypeLabel("supabase_types")).toBe("TypeScript type");
  });

  it("names the group's scope", () => {
    expect(groupScopeName({ set: "models/supabase_types/lib/database.types.ts", source: "models", tool: "supabase_types", conflicts: [] })).toBe("Supabase generated types");
    expect(groupScopeName({ set: "models/prisma/schema.prisma", source: "models", tool: "prisma", conflicts: [] })).toBe("prisma models");
  });
});
