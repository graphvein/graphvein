import { DataSource } from "typeorm";
import { SqliteEnum } from "./entity/SqliteEnum";

export const ds6 = new DataSource({ type: "sqlite", database: "f.sqlite", entities: [SqliteEnum] });
