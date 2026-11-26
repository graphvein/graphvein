import { index, integer, pgTable, primaryKey, text, unique } from "drizzle-orm/pg-core";

// casing "snake_case": omitted names are converted (DR-K2), literal names
// are kept (DR-K3), generated unique names keep the key (A12).
export const casedTable = pgTable("cased_table", {
  userID: integer().primaryKey(),
  displayName: text().unique(),
  HTMLParser: text(),
  ExplicitName: text("ExplicitName"),
});

// Generated key names under casing (DR-K3, A12): primary and foreign key
// and index names use the converted names, unique names the keys.
export const casedChild = pgTable(
  "cased_child",
  {
    parentId: integer().references(() => casedTable.userID),
    otherKey: text(),
    createdAt: integer(),
  },
  (t) => [
    primaryKey({ columns: [t.parentId, t.otherKey] }),
    index().on(t.createdAt),
    unique().on(t.otherKey, t.createdAt),
  ],
);
