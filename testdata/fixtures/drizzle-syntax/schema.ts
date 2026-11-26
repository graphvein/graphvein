// G1: imports a drizzle-orm dialect module, so the file has a model payload.
import { sql } from "drizzle-orm";
import { relations } from "drizzle-orm";
import {
  customType,
  foreignKey,
  index,
  integer,
  pgEnum,
  pgTable,
  text,
  timestamp,
  uniqueIndex,
} from "drizzle-orm/pg-core";
import type { AnyPgColumn } from "drizzle-orm/pg-core";

// S1 with E-tagged and no substitution: a module-level sql const.
const utcNow = sql`(now() at time zone 'utc')`;

// E-method: customType's dataType() returns a literal; toDriver is unknown.
const bytea = customType<{ data: Buffer }>({
  dataType() {
    return "bytea";
  },
  toDriver(value: Buffer) {
    return value;
  },
});

export const role = pgEnum("role", ["owner", "member", "viewer"]);

// Column order, chained modifiers, negative numbers, arrays, spreads and an
// arrow extra config whose array holds a partial index with an sql template.
export const workspace = pgTable(
  "workspace",
  {
    id: text("id").primaryKey(),
    /* a comment between columns is never captured */
    name: text("name").notNull(),
    rank: integer("rank").default(-1),
    tags: text("tags").array().default(sql`'{}'::text[]`),
    createdAt: timestamp("created_at", { mode: "string", withTimezone: true }).default(utcNow).notNull(),
    avatar: bytea("avatar"),
    role: role("role").default("member"),
  },
  (table) => [
    index("workspace_name_idx").on(table.name),
    uniqueIndex("workspace_rank_idx").on(table.rank.desc(), table.name).where(sql`${table.rank} > ${0} and ${table.name} is not null`),
  ],
);

const audit = {
  createdBy: text("created_by"),
  "updated-at": timestamp("updated_at").$onUpdate(() => new Date()),
};

// E-spread-entry, E-shorthand and a block-bodied arrow with one return.
const parentId = integer("parent_id");
export const task = pgTable(
  "task",
  {
    id: integer("id").primaryKey().generatedAlwaysAsIdentity(),
    workspaceId: text("workspace_id").references(() => workspace.id, { onDelete: "cascade" }),
    parentId,
    ...audit,
  },
  (t) => {
    return [
      foreignKey({ columns: [t.parentId], foreignColumns: [t.id] }).onDelete("set null"),
      ...[],
    ];
  },
);

export const self = pgTable("self", {
  id: integer("id").primaryKey(),
  ref: integer("ref").references((): AnyPgColumn => self.id),
});

export const workspaceRelations = relations(workspace, ({ many }) => ({
  tasks: many(task),
}));
