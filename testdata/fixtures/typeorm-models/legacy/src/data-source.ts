import { DataSource } from "typeorm";
import { Old } from "./Old";

export const old = new DataSource({ type: "postgres", entities: [Old] });
