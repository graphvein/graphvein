import { DataSource } from "typeorm";
import { SnakeNamingStrategy } from "typeorm-naming-strategies";
import { SnakeUser } from "./entity/SnakeUser";

export const dataSource = new DataSource({
  type: "postgres",
  namingStrategy: new SnakeNamingStrategy(),
  entities: [SnakeUser],
  migrations: ["src/migrations/*.ts"],
});
