import { integer, pgTable, text } from "drizzle-orm/pg-core";
import { audits, authors } from "../keys";

// DR-P1 (later addition): a foreign key to a column that is neither the
// primary key nor unique; Postgres rejects it, so the table is a failure.
export const byEmail = pgTable("by_email", {
  id: integer("id").primaryKey(),
  email: text("email").references(() => authors.email),
});

// A foreign key to a column that is unique only through a unique index:
// drizzle-kit writes foreign keys before indexes, so from one generate
// Postgres rejects it; the key is unknown.
export const byCode = pgTable("by_code", {
  code: text("code").references(() => audits.code),
});
