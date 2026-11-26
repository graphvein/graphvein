import { pgTable, text } from "drizzle-orm/pg-core";

// A stale schema file the config does not name (DR-P2 marker).
export const kinds = pgTable("kinds", { id: text("id") });
