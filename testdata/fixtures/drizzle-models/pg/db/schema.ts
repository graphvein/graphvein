import { sql } from "drizzle-orm";
import {
  bigint,
  bigserial,
  boolean,
  char,
  cidr,
  date,
  doublePrecision,
  index,
  inet,
  integer,
  interval,
  json,
  jsonb,
  numeric,
  pgEnum,
  pgSchema,
  pgTable,
  real,
  serial,
  smallint,
  smallserial,
  text,
  time,
  timestamp,
  uuid,
  varchar,
} from "drizzle-orm/pg-core";
import { integer as sqliteInteger, sqliteTable } from "drizzle-orm/sqlite-core";
import { bytea, timestamps, wideType } from "../lib/shared";
import { mood } from "./enums/mood";

const nowUtc = sql`(now() AT TIME ZONE 'utc')`;
const settings = { theme: "dark", size: 2 };

export const status = pgEnum("status", ["active", "archived"]);
const hidden = pgEnum("hidden_enum", ["a", "b"]);

export const app = pgSchema("app");
export const appRole = app.enum("app_role", ["admin", "member"]);
const hiddenSchema = pgSchema("hid");

// Column builders, types and defaults (DR-C1, DR-C2, DR-C3, DR-E1).
export const kinds = pgTable("kinds", {
  id: serial("id").primaryKey(),
  small: smallserial("small"),
  big: bigserial("big", { mode: "number" }),
  name: varchar("name", { length: 20 }).notNull().unique(),
  code: char("code", { length: 3 }),
  flag: char("flag"),
  label: varchar("label"),
  note: text("note"),
  count: integer("count").default(0),
  neg: smallint("neg").default(-1),
  ratio: real("ratio").default(1.5),
  score: doublePrecision("score"),
  price: numeric("price", { precision: 10, scale: 2 }),
  whole: numeric("whole", { precision: 8 }),
  anyNumber: numeric("any_number"),
  total: bigint("total", { mode: "bigint" }),
  active: boolean("active").default(true).notNull(),
  createdAt: timestamp("created_at", { mode: "date" }).defaultNow().notNull(),
  at3: timestamp("at3", { precision: 3, withTimezone: true }),
  day: date("day"),
  clock: time("clock", { precision: 2 }),
  span: interval("span"),
  token: uuid("token").defaultRandom(),
  ip: inet("ip"),
  meta: json("meta").default({ a: 1, b: [true, null] }),
  doc: jsonb("doc").default([1, 2]),
  prefs: jsonb("prefs").default(settings),
  quoted: text("quoted").default("it's"),
  tags: text("tags").array().default(["a", "b"]),
  nums: integer("nums").array().notNull(),
  stamped: timestamp("stamped").default(nowUtc),
  raw: text("raw").default(sql`'x'`),
  client: text("client").$defaultFn(() => "x"),
  touched: timestamp("touched").$onUpdate(() => new Date()),
  typed: jsonb("typed").$type<{ a: number }>(),
  state: status("state").default("active").notNull(),
  states: status("states").array(),
  feeling: mood("feeling"),
  role: appRole("role"),
  data: bytea("data"),
  wide: wideType("wide"),
  addr: cidr("addr"),
  uniq: text("uniq").unique("kinds_uniq_key", { nulls: "not distinct" }),
  ident: integer("ident").generatedAlwaysAsIdentity(),
  ident2: bigint("ident2", { mode: "number" }).generatedByDefaultAsIdentity({ startWith: 10 }),
  rollback: serial(),
});

// Defaults and modifiers whose facts are unknown (DR-C2 refusals, D).
export const unknowns = pgTable("unknowns", {
  id: integer("id").primaryKey(),
  late: text("late").default(sql`${nowUtc}`),
  dyn: text("dyn").default(String(1)),
  when: timestamp("when").default(new Date()),
  nul: text("nul").default(null),
  odd: text("odd").notNull().generatedAlwaysAs(sql`'g'`).using(),
  pre: text("pre").notNull().array(),
  owner: integer("owner").references(() => kinds.id),
  ...timestamps,
});

// A generated column (DR-C2, rendering rule A11).
export const people = pgTable("people", {
  first: text("first").notNull(),
  last: text("last").notNull(),
  full: text("full").generatedAlwaysAs(() => sql`${people.first} || ' ' || ${people.last}`),
  constant: text("constant").generatedAlwaysAs(sql`'c'`),
});

// A table with extra config (row DR-X1, read by part 2).
export const indexed = pgTable(
  "indexed",
  {
    id: integer("id").primaryKey(),
    a: text("a"),
    b: text("b").notNull(),
  },
  (t) => [index("indexed_a_idx").on(t.a)],
);

// A pgSchema table (DR-T1 `<s>.table`).
export const appUsers = app.table("users", {
  id: uuid("id").primaryKey().defaultRandom(),
  role: appRole("role").notNull().default("member"),
});

// A table exported under a second name is one table.
export { people as persons };

// Not exported: drizzle-kit creates no table (A1).
const secret = pgTable("secret", { id: integer("id") });

// DR-P1: a non-exported enum and a non-exported pgSchema.
export const usesHidden = pgTable("uses_hidden", { h: hidden("h") });
export const inHidden = hiddenSchema.table("in_hidden", { id: integer("id") });

// DR-P1: two tables with one name, a column name twice, a unique name twice.
export const dupA = pgTable("dup", { id: integer("id") });
export const dupB = pgTable("dup", { id: integer("id") });
export const twice = pgTable("twice", { a: text("x"), b: text("x") });
export const twoUniques = pgTable("two_uniques", { a: text("a").unique("u"), b: text("b").unique("u") });

// Row D: forms no row reads.
const tableName = "dynamic";
export const dynamic = pgTable(tableName, { id: integer("id") });
export const callback = pgTable("callback", (t) => ({ id: t.integer() }));
export const rls = pgTable("rls", { id: integer("id") }).enableRLS();
export let mutable = pgTable("mutable", { id: integer("id") });
export const wrongDialect = sqliteTable("wrong", { id: sqliteInteger("id") });
export const partial = pgTable("partial", {
  id: integer("id"),
  ...settings.theme,
  named: text(tableName),
});
