import { DataSource } from "typeorm";
import { Item } from "./entity/Item";

// TO-P1 (7): 1.x has no sqlite driver.
export const legacy = new DataSource({ type: "sqlite", database: "v1.sqlite", entities: [Item] });
