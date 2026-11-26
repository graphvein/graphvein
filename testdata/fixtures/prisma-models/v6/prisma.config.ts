import { defineConfig } from "prisma/config";

// Prisma 6.13.0 and later read this config (c12); its schema names a
// folder (prismaSchemaFolder is GA from 6.7.0).
export default defineConfig({
  schema: "models",
});
