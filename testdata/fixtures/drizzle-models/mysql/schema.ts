import { bigint, boolean, char, datetime, index, int, mysqlEnum, mysqlSchema, mysqlTable, primaryKey, serial, text, timestamp, unique, varchar } from "drizzle-orm/mysql-core";

// MySQL facts are mapped from drizzle-kit's source (DR-V6).
export const items = mysqlTable("items", {
  id: int("id").primaryKey(),
  name: varchar("name", { length: 50 }).notNull().unique(),
  big: bigint("big", { mode: "number", unsigned: true }),
  kind: mysqlEnum("kind", ["a", "b"]),
  at: datetime("at", { fsp: 3 }),
  seq: serial("seq"),
  flag: boolean("flag").default(true),
  body: text("body"),
});

// drizzle-kit skips MySQL tables in a schema.
export const other = mysqlSchema("other");
export const inOther = other.table("in_other", { id: int("id") });

// Extra config on mysql (mapped): composite primary key, index, unique,
// an inline foreign key.
export const orders = mysqlTable(
  "orders",
  {
    itemId: int("item_id").notNull().references(() => items.id),
    code: varchar("code", { length: 10 }).notNull(),
  },
  (t) => [
    primaryKey({ columns: [t.itemId, t.code] }),
    index("orders_code_idx").on(t.code),
    unique("orders_code_unique").on(t.code),
  ],
);

// MySQL modifiers: AUTO_INCREMENT is not a default;
// defaultNow() is a default (presence); onUpdateNow() without a default
// leaves the default unknown; the other facts of each column are read.
export const counters = mysqlTable("counters", {
  id: int("id").autoincrement().primaryKey(),
  createdAt: timestamp("created_at").defaultNow().notNull(),
  updatedAt: timestamp("updated_at").onUpdateNow(),
  touchedAt: timestamp("touched_at", { fsp: 3 }).defaultNow().onUpdateNow(),
  code: char("code"),
});
