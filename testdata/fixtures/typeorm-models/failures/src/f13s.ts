import { DataSource } from "typeorm";
import { IntLength } from "./entity/IntLength";

// TO-P1 (13) on sqlite: a length on int (sqlite's driver gives text one).
export const ds13s = new DataSource({ type: "sqlite", database: "f.sqlite", entities: [IntLength] });
