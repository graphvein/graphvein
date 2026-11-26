import { defineConfig } from "drizzle-kit";

export default defineConfig({
  dialect: "postgresql",
  schema: "./src/db/schema.ts",
  schemaFilter: ["public", "audit"],
  out: "./drizzle",
});
