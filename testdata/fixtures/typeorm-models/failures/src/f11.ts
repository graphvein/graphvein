import { DataSource } from "typeorm";
import { PgUuidText } from "./entity/PgUuidText";

export const ds11 = new DataSource({ type: "postgres", database: "f", entities: [PgUuidText] });
