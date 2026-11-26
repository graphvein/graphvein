import { DataSource } from "typeorm";
import { PgKeyword } from "./entity/PgKeyword";

export const ds10 = new DataSource({ type: "postgres", database: "f", entities: [PgKeyword] });
