import { pgTable, text } from "drizzle-orm/pg-core";

// drizzle-orm 1.0 release candidate: refused until a corpus pins 1.0.
export const future = pgTable("future", { id: text("id") });
