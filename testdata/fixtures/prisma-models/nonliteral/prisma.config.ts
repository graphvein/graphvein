import path from "node:path";
import { defineConfig } from "prisma/config";

// The schema path is computed: discovery cannot prove it (no claim).
export default defineConfig({
  schema: path.join("prisma", "schema.prisma"),
});
