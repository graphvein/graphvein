import { registerAs } from "@nestjs/config";
import type { DataSourceOptions } from "typeorm";

export const dbToken = "database";

const options: DataSourceOptions = { type: "mysql", entities: ["dist/**/*.entity.js"], migrations: ["dist/migrations/*.js"] };

export const DatabaseConfig = registerAs(dbToken, (): DataSourceOptions => options);
