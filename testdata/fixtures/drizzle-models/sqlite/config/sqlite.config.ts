import { defineConfig } from "drizzle-kit";

// Named by --config in the package's scripts; the schema path resolves
// against the package directory (A5).
export default defineConfig({
  dialect: "sqlite",
  schema: "./src/schema.ts",
});
