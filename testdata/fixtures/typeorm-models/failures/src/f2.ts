import { DataSource } from "typeorm";
import { PgDatetime } from "./entity/PgDatetime";

const pg = { type: "postgres", entities: [PgDatetime] };
const lite = { type: "sqlite", database: "f.sqlite", entities: [PgDatetime] };

export const ds2 = new DataSource(process.env.PG ? pg : lite);
