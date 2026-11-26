import { defineConfig, env } from "prisma/config";

export default defineConfig({
  schema: "prisma/schema",
  migrations: { path: "db/migrations" },
  datasource: { url: env("DATABASE_URL") },
});
