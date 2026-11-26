import { DataSource } from "typeorm";
import { Audited } from "./entity/Audited";
import { Typed } from "./entity/Typed";

// Postgres-only forms (investigation probe 2): native enums, arrays, jsonb,
// timestamptz, uuid, identity, an entity schema.
export const pgDataSource = new DataSource({
  type: "postgres",
  url: "postgres://localhost/app",
  entities: [Typed, Audited],
});
