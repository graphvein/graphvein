import { DataSource } from "typeorm";
import { Fraction } from "./entity/Fraction";

export const ds5 = new DataSource({ type: "postgres", database: "f.sqlite", entities: [Fraction] });
