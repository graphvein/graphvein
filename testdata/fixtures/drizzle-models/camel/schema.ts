import { pgTable, text } from "drizzle-orm/pg-core";

// casing "camelCase" (DR-K2).
export const camelTable = pgTable("camel_table", {
  display_name: text(),
  api_key_v2: text().notNull(),
  kept_name: text("kept_name"),
});
