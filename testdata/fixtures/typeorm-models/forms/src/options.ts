import { join } from "path";
import { DataSourceOptions } from "typeorm";

// TO-K8: a spread whose keys are not scope options.
const extra = { logging: false, poolSize: 4 };

// TO-K7: an options object other modules import; TO-K1 a glob of the
// subset through `join` imported by name.
export const options: DataSourceOptions = {
  type: "postgres",
  url: "postgres://localhost/forms",
  ...extra,
  entities: [join(__dirname, "entity/*.entity.ts")],
};
