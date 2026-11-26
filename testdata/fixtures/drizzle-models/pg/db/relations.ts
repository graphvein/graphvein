import { relations } from "drizzle-orm";
import { kinds } from "./schema";

// Outside the configured schema: counted in a marker, not read (DR-R1).
export const kindsRelations = relations(kinds, ({ many }) => ({ others: many(kinds) }));
