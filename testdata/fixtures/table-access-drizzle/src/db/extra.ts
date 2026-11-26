import { integer, pgTable, text } from "drizzle-orm/pg-core";

// Outside the config's schema: not a claimed table, so a relation to it
// has no claimed target.
export const profiles = pgTable("profiles", {
  userId: integer("user_id").primaryKey(),
  bio: text("bio"),
});
