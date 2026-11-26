import { DataSource } from "typeorm";
import { Quoted } from "./entity/Quoted";

export const ds4 = new DataSource({ type: "sqlite", database: "f.sqlite", entities: [Quoted] });
