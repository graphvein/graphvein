import { integer, pgTableCreator } from "drizzle-orm/pg-core";

// Row D: a table creator; its marker names the tables it builds.
// The creator itself is not exported.
const createTable = pgTableCreator((name) => `app_${name}`);
export const created = createTable("created", { id: integer("id") });
