// Forms the payload keeps as unknown, and statements it does not keep (SD).
import { pgTable, text } from "drizzle-orm/pg-core";

const KEY = "computed";
let counter = 0;
var legacy = pgTable("legacy", { id: text("id") });
export let mutable = pgTable("mutable", { id: text("id") }) as any;
const { a, b } = { a: 1, b: 2 };
declare const ambient: unknown;

export const odd = pgTable("odd", {
  [KEY]: text("computed"),
  get getter() {
    return text("getter");
  },
  async method() {
    return "x";
  },
  *generator() {
    yield 1;
  },
  plain() {
    const x = 1;
    return x;
  },
  maybe: text("maybe")?.notNull(),
  fn: function () {
    return 1;
  },
  made: new Date(),
  cond: counter > 0 ? text("a") : text("b"),
  later: async () => text("later"),
  nested: ({ deep }: { deep: number }, rest = 1, ...others: number[]) => deep,
  block: () => {
    counter++;
    return text("block");
  },
  escaped: "tab\there",
  flag: true,
  nothing: null,
  undef: undefined,
  hex: 0x1f,
  positive: +2,
  plainTemplate: `literal`,
  subst: sql`a${legacy.id}b`,
});

function helper() {
  return pgTable("inside", { id: text("id") });
}

export { helper, legacy as legacyTable };
export * from "./schema";
