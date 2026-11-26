import path, { join } from "path";
import { DataSource, DataSourceOptions } from "typeorm";
import { SnakeNamingStrategy } from "typeorm-naming-strategies";
import Post from "./entity/Post";
import { Author } from "./entity/Author";

const isPg = process.env.DB_TYPE === "postgres";

const entities = [Post, Author, ...[]];

const base = isPg ? { url: process.env.DATABASE_URL } : { host: "localhost", ...(process.env.DB_USER && { username: process.env.DB_USER }) };

const testConfig: DataSourceOptions = {
  type: "sqlite",
  database: ":memory:",
  entities,
  synchronize: true,
};

const prodConfig: DataSourceOptions = {
  type: "sqlite",
  database: "db.sqlite3",
  entities,
  migrations: [path.join(__dirname, "migration/*.ts"), `${__dirname}/more/**/*.{ts,js}`],
};

const pgConfig = {
  type: "postgres",
  ...base,
  entities: [join(__dirname, "entity/**", "*.ts")],
  namingStrategy: new SnakeNamingStrategy(),
} as DataSourceOptions;

function getDataSource(): DataSourceOptions {
  if (process.env.NODE_ENV === "test") return testConfig;
  else if (process.env.NODE_ENV === "production") return isPg ? pgConfig : prodConfig;
  else {
    return isPg ? pgConfig : testConfig;
  }
}

export const dataSource = new DataSource(getDataSource());
export default dataSource;
