import { defineConfig } from "drizzle-kit";

// Default config name; the schema is an array with a glob and a directory
// (DR-K1); schemaFilter lists pgSchema "app" so its tables exist after push.
export default defineConfig({
  dialect: "postgresql",
  schema: ["./db/schema.ts", "./db/enums/*.ts", "./db/keys.ts", "./db/review"],
  schemaFilter: ["public", "app"],
  out: "./drizzle",
});
