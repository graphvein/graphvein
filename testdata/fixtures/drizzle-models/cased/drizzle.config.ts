import type { Config } from "drizzle-kit";

export default {
  dialect: "postgresql",
  schema: "./schema.ts",
  casing: "snake_case",
} satisfies Config;
