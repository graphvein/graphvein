import { DataSource } from "typeorm";
import { Indexed } from "./entity/Indexed";

// TO-P1 (9): an @Index type option throws on a driver without index types.
export const indexed = new DataSource({ type: "better-sqlite3", database: "i.sqlite", entities: [Indexed] });
