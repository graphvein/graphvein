import { DataSource } from "typeorm";
import { options } from "./options";

// TO-K7: `new DataSource(X)` with X imported.
export const dataSource = new DataSource(options);
