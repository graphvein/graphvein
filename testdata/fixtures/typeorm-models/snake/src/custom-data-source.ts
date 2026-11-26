import { DataSource, DefaultNamingStrategy } from "typeorm";
import { SnakeUser } from "./entity/SnakeUser";

class Upper extends DefaultNamingStrategy {}

// Row D: a custom naming strategy.
export const custom = new DataSource({ type: "postgres", namingStrategy: new Upper(), entities: [SnakeUser] });
