import { DataSource } from "typeorm";
import { NoAnnotation } from "./entity/NoAnnotation";

export const ds3 = new DataSource({ type: "sqlite", database: "f.sqlite", entities: [NoAnnotation] });
