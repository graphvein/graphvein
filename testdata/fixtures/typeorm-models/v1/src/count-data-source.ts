import { DataSource } from "typeorm";
import { Counted } from "./entity/Counted";

// TO-P1 (8): @RelationCount does not exist in 1.x.
export const counted = new DataSource({ type: "better-sqlite3", database: "c.sqlite", entities: [Counted] });
