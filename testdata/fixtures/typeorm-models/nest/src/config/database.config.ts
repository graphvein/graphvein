import { registerAs } from "@nestjs/config";
import type { DataSourceOptions } from "typeorm";

// TO-K5: the namespace the forRootAsync factory spreads.
export const DatabaseConfig = registerAs("database", (): DataSourceOptions => ({ type: "mysql", database: "nest" }));
