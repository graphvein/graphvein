import { DataSource } from "typeorm";
import { NoKey } from "./entity/NoKey";

export const ds1 = new DataSource({ type: "postgres", database: "f.sqlite", entities: [NoKey] });
