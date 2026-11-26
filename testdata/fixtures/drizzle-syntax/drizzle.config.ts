// G2: imports drizzle-kit; S2 is the default export, wrapped in satisfies.
import { type Config, defineConfig } from "drizzle-kit";

export default defineConfig({
  schema: ["./schema.ts", "./more/*.ts"],
  dialect: "postgresql",
  casing: "snake_case",
  out: `./drizzle`,
  strict: true,
  verbose: false,
  migrations: null,
}) satisfies Config;
