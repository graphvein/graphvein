import { pgEnum } from "drizzle-orm/pg-core";

// An exported enum in a schema module, imported by schema.ts (DR-E1).
export const mood = pgEnum("mood", ["sad", "ok", "happy"]);

// A mixed-case enum name: format_type quotes it ("logLevel", G2B5).
export const logLevel = pgEnum("logLevel", ["debug", "info"]);
