import { DataSource } from "typeorm";
import { MyDatetime } from "./entity/MyDatetime";

export const ds12 = new DataSource({ type: "mysql", database: "f", entities: [MyDatetime] });
