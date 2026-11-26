import { DataSource } from "typeorm";
import { IntLength } from "./entity/IntLength";
import { TextLength } from "./entity/TextLength";

// TO-P1 (13) on postgres: a length on int and on text.
export const ds13 = new DataSource({ type: "postgres", database: "f", entities: [IntLength, TextLength] });
