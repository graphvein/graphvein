import { DataSource } from "typeorm";
import { IntPrecision } from "./entity/IntPrecision";
import { TextPrecision } from "./entity/TextPrecision";

export const ds14 = new DataSource({ type: "postgres", database: "f", entities: [IntPrecision, TextPrecision] });
