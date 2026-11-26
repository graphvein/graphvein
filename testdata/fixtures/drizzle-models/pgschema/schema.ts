import { integer, pgSchema, pgTable } from "drizzle-orm/pg-core";

// Without a schemaFilter, drizzle-kit push keeps only "public" while
// generate keeps every schema: the "audit" table is not claimed.
export const audit = pgSchema("audit");
export const events = audit.table("events", { id: integer("id").primaryKey() });
export const plain = pgTable("plain", { id: integer("id").primaryKey() });
