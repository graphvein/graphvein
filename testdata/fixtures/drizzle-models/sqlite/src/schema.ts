import { sql } from "drizzle-orm";
import { blob, check, index, integer, numeric, primaryKey, real, sqliteTable, text, uniqueIndex } from "drizzle-orm/sqlite-core";

// SQLite rules: .unique() is a unique index.
export const notes = sqliteTable("notes", {
  id: integer("id").primaryKey({ autoIncrement: true }),
  title: text("title", { length: 40 }).notNull().unique(),
  kind: text("kind", { enum: ["a", "b"] }),
  data: blob("data", { mode: "json" }),
  meta: text("meta", { mode: "json" }).default({ a: 1 }),
  score: real("score").default(1.5),
  amount: numeric("amount"),
  done: integer("done", { mode: "boolean" }).default(false),
  label: text("label").default("it's"),
  stamp: integer("stamp").default(sql`(unixepoch())`),
});

// DR-P1: SQLite index names are per database.
export const left = sqliteTable("left", { code: text("code").unique("code_idx") });
export const right = sqliteTable("right", { code: text("code").unique("code_idx") });

// Extra config on sqlite (DR-X1, A10): an unnamed composite primary key
// (nullability unchanged), a partial index, a unique index, a check, an
// inline foreign key (unnamed), a one-column primaryKey().
export const tags = sqliteTable(
  "tags",
  {
    noteId: integer("note_id").references(() => notes.id, { onDelete: "cascade" }),
    name: text("name").notNull(),
    weight: integer("weight"),
  },
  (t) => [
    primaryKey({ columns: [t.noteId, t.name] }),
    index("tags_weight_idx").on(t.weight).where(sql`${t.weight} > 0`),
    uniqueIndex("tags_name_idx").on(t.name),
    check("tags_weight_check", sql`${t.weight} >= 0`),
  ],
);
export const single = sqliteTable("single", { code: text("code") }, (t) => [primaryKey({ columns: [t.code] })]);
