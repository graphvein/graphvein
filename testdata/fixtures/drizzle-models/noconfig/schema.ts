import { pgTable, text } from "drizzle-orm/pg-core";

// A Drizzle package without a drizzle-kit config: no claims, one marker.
export const orphans = pgTable("orphans", { id: text("id") });
